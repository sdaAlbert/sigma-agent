# Sigma Docker 命令环境

本文说明选择 `command_execution_mode: "docker"` 后，Sigma 如何运行项目命令、复用哪些资源、退出时清理什么，以及下次启动会保留什么。Sigma 默认使用主机 shell；Docker 是可选的命令执行模式。模型请求和文件工具始终由 Sigma 主机进程运行。

## 快速结论

- Docker 模式下，启动 Sigma 本身不会启动项目命令容器。第一次调用 `run_command` 时，Sigma 才按需创建交互会话容器。
- 同一次交互会话中的前台 `run_command` 复用同一个容器；命令通过 `docker exec` 运行。
- Sigma 交互进程正常退出时（例如输入 `/quit` 或关闭 TUI），会取消当前运行、等待工具退出，再移除本次的 `sigma-agent-session-*` 容器；启用 Docker 网络时也会移除本次专用网络。单轮任务完成不会删除容器，后续命令仍可复用它。
- 删除的是短期运行容器。Docker 镜像、工作区文件和挂载在 `.sigma/container-home` 的包缓存会保留；下次会创建新容器并继续使用它们。
- `run_command` 的后台 `action=start` 是单独的、有记录的任务容器；它可在 Sigma 退出后继续运行，直到完成、被 `action=stop` 停止或达到 30 分钟上限。

| 运行方式 | 前台命令 `action=run` 使用的容器 | Sigma 退出时 | 仍会保留 |
| --- | --- | --- | --- |
| 主机模式（默认） | 不创建命令容器，直接在当前系统 shell 中运行 | 无 Docker 命令容器 | 主机上的文件、环境和进程行为遵循当前用户权限 |
| Docker 模式下的一次性 `--task` / `--resume` / `--serve` | 每条命令新建一次性容器，命令结束后自动删除 | 没有待清理的前台命令容器 | 镜像、工作区文件、工作区内的包缓存 |
| Docker 模式下的交互式 TUI | 同一会话复用一个 `sigma-agent-session-*` 容器 | 正常退出时删除 | 镜像、工作区文件、工作区内的包缓存 |
| Docker 模式下的后台 `action=start` | 单独创建 `sigma-bg-*` 任务容器 | 任务继续运行 | 任务状态会保存；容器在完成、停止或超时后删除 |

`docker-compose.yml` 只定义可选的 Qdrant 服务，用于配置了向量记忆时提供存储；它不负责启动 Sigma，也不负责创建项目命令容器。

## 一次前台命令的生命周期

1. Sigma 在主机上直接读取和编辑当前工作区；Git 内置工具也在主机上运行。默认命令模式同样使用主机 shell，不需要 Docker。
2. 第一次前台 `run_command` 默认使用 `debian:trixie-slim`，镜像缺失时 Docker 拉取一次。Sigma 创建一个命名为 `sigma-agent-session-*` 的容器，把当前工作区挂载到容器的 `/workspace`。
3. Shell 命令在容器内运行，输出和退出码返回给 Sigma。之后的前台命令继续使用这个容器，除非选用了不同镜像或执行环境发生切换。
4. Sigma 交互进程退出时，会移除本次会话容器；启用 Docker 网络时也会移除专用网络。容器中的临时文件随容器删除；写入 `/workspace` 的内容实际位于主机工作区，会保留。

前台容器有 2 GiB 内存、2 个 CPU、128 个进程上限，移除 Linux capabilities，并使用 `no-new-privileges`。容器镜像的根文件系统以只读方式挂载；`/tmp`、`/var/tmp` 和 `/dev/shm` 是有容量上限的临时区。只有 `/workspace` 工作区挂载可持久写入，Go 等包缓存也放在工作区的 `.sigma/container-home`。默认网络禁止外部访问；Sigma 仍可从主机连接模型 API。

后台任务容器和非复用的一次性命令容器使用相同的根文件系统只读、临时目录、资源限制和 capability 限制。启用网络时只改变 Docker 网络策略，不会让容器访问工作区以外的主机文件。

这层 Docker 隔离限制的是**项目命令进程**。Sigma 的读写文件工具和 Git 工具仍由主机进程直接操作当前目录；列文件和搜代码则通过 `run_command` 在所选执行环境里完成。Docker 命令能读写挂载的整个工作区；因此不要把密钥或个人凭证放在项目目录里。主机工作区之外的凭证不会被显式传入容器，但工作区内的 `.env`、`.secrets` 等文件仍对容器可见。默认任务挂载当前目录；`--isolated` 会先创建 Git worktree，再把那个 worktree 挂入容器。Docker 本身不会替每个任务复制一份代码。该设计不是完整的 Agent 安全沙箱。

## Docker 镜像中的文件列举和代码搜索

Docker 命令运行在 Linux 镜像里。`rg`（ripgrep）不是每个项目镜像都自带；`grep` 用于搜文本，`find` 用于列文件。先在当前容器运行 `command -v rg grep find` 确认可用项。Sigma 优先建议 `rg --files` 和 `rg -n`；没有 `rg` 时，可用 `find` 列文件、`grep -RIn` 搜文本。不要假设自定义项目镜像包含这些程序；缺少必要工具时，在任务镜像准备阶段安装或预装，再运行 Agent。离线评测需要在 Agent 开始前把工具放进评测镜像，不能依赖 Agent 运行时联网补装。

## 会保留的缓存和环境

容器退出不会等于清空 Docker。以下资源会留下来，供后续 Sigma 会话使用：

| 资源 | 位置 | 用途 |
| --- | --- | --- |
| 基础镜像，如 `debian:trixie-slim` | Docker image store | 下次创建容器时直接复用，不必重新拉取 |
| 准备好的派生镜像 | Docker image store，名称形如 `sigma-agent/env:<hash>` | 保留 `prepare_environment` 安装的系统工具和库；相同基础镜像及 setup 命令会得到相同镜像标识 |
| Go/Python/npm 等用户级包缓存 | 工作区 `.sigma/container-home` | 挂载到容器 `HOME`；Go 的 `GOMODCACHE` 和 `GOCACHE` 也放在这里 |
| 当前选择的命令镜像 | 工作区 `.sigma/command-environment.json` | 后续会话默认继续使用所选或准备好的镜像 |
| 代码和测试文件 | 工作区本身 | 容器通过 `/workspace` 访问同一份文件 |

`docker rm` 只移除容器，不删除镜像、工作区文件或包缓存。不要为了退出 Sigma 执行 `docker system prune`；这会删掉 Sigma 之外也可能需要的镜像和缓存。

## 什么时候使用 `prepare_environment`

先读项目的 Dockerfile、devcontainer、CI 和依赖文件，再判断镜像是否缺少实际需要的系统工具。

- 若配置的 `debian:trixie-slim` 已有 `sh`，简单 POSIX shell 任务直接运行 `run_command` 即可，不需要调用 `prepare_environment`。
- `prepare_environment` 只记录镜像且 `setup_command` 留空时，不构建派生镜像，也不安装软件；它将该镜像记为后续命令的首选环境。交互会话若已经用某个镜像创建了容器，后续命令会继续复用当前容器；要切换镜像，需在 `run_command` 显式传入 `image`，这会重建本次会话容器。
- 只有缺少必要工具时，才填写最小的 `setup_command`。Sigma 会从基础镜像和命令生成临时 Dockerfile，构建并缓存派生镜像；构建上下文不包含整个仓库。
- 默认关闭 Docker 命令容器和环境构建步骤的网络，所以 `apt-get`、`pip`、`npm` 等下载命令会失败。TUI 用 `/settings` 切换；非交互运行可设置 `allow_container_network` 或传 `--allow-container-network`。该开关只控制容器内的命令和构建步骤，不控制主机上的模型 API 请求，也不阻止 Docker 引擎拉取缺失镜像。
- 准备镜像不会自动清理 Docker 中的镜像。Sigma 退出后，派生镜像仍可供恢复会话或下一个会话复用。

## 后台命令与前台容器不同

`run_command` 的 `action=start` 会创建 `sigma-bg-*` 后台任务容器，并把任务状态保存在当前会话记录中。它与一次交互会话复用的 `sigma-agent-session-*` 前台容器不是同一个容器。

后台任务按设计可在 Sigma 关闭后继续执行，以便重新打开同一个会话后查看状态。使用 `action=list` 或 `action=status` 查看任务，使用 `action=stop` 停止任务。任务完成、停止或超时后，Sigma 会移除其容器和临时网络。不要把正在运行的后台任务当成退出时应自动删除的临时前台容器。

## 演示复用和退出清理

在 SigmaDemo 中依次让 Sigma 执行两次独立 `run_command`：

```text
sh tests/test-calc.sh
printf 'container reuse check\n'
```

两次命令结果都应显示相同的 image，执行模式为 `reused-session-container`。在 Sigma 运行时，外部 PowerShell 可查看前台容器：

```powershell
docker ps -a --filter name=sigma-agent-session --format '{{.Names}} {{.Status}} {{.Image}}'
```

输入 `/quit` 并等待 Sigma 返回 PowerShell 提示符后，再运行同一条 `docker ps -a`；本次前台容器应不再出现。若这次没有运行任何 Docker 命令，前后两次列表都为空是正常的。

退出后可以检查镜像和缓存仍在：

```powershell
docker image inspect debian:trixie-slim --format '{{.Id}}'
Test-Path .sigma\container-home
```

下一次运行会创建新的会话容器，但使用相同镜像、当前工作区和包缓存。若 Sigma 被操作系统强制终止（例如 `taskkill /F`），进程无法执行清理回调；这种情况下只检查并移除确认属于已退出 Sigma 的 `sigma-agent-session-*` 容器，不要执行全局 prune。正常使用 `/quit` 或关闭 TUI 会走有等待的清理流程。

## 工具错误的常见原因

- `create_file` 会在工作区内自动建立缺失的父目录，但不会覆盖已有文件；已有文件使用 `edit_file` 修改。
- `purpose=test` 的 `run_command` 拒绝可能吞掉退出码的管道或分号。直接运行测试，例如 `sh tests/test-calc.sh`；需要准备步骤时用 `&&` 连接。
- `prepare_environment` 若被用户中断，会记录 Docker 构建已取消；这不是测试失败，也不代表产生了可复用的准备镜像。先确认 Sigma 已退出，再重新启动并按需准备环境。
- 读取 `read_file` 的报错应根据路径和工具返回的具体错误定位。相对路径必须位于当前工作区；不要把 Windows 绝对路径或 Docker 内部路径交给文件工具。

更多运行时字段见[配置说明](CONFIGURATION.md)，TUI 操作见[使用指南](USAGE.md)。
