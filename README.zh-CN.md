<p align="center">
  <img src="docs/assets/sigma-agent-mark.svg" alt="Sigma Agent 标志" width="112">
</p>

<h1 align="center">Sigma Agent</h1>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Sigma 是一个用 Go 编写的 Coding Agent，面向已有代码仓库中的 issue 修复。它把模型与仓库工具连接起来，在 Docker 中运行项目命令，并串起代码调查、修改、测试和 diff 检查。每个任务都在独立的 Git worktree 中运行，不会直接改动原始工作区。

## 为什么做 Sigma

- **目标是提升 Issue 修复效果：** Sigma 的开发目标是让 Coding Agent 更可靠地修复真实仓库里的 issue，而不只是用 Go 重写 Pi。重点是贴合仓库实际环境，并在结束前验证每个补丁。
- **贴合仓库的执行与验证：** Agent 可以读取项目清单和 CI 指引，通过 `prepare_environment` 准备可复用的 Docker 工具链，并在有资源限制的容器中运行仓库命令。代码修改后，Runtime 会要求完成相关测试和 Git diff 检查，才能结束任务。
- **完整的 Issue 修复流程：** 每个任务使用独立 Git worktree 和持久化会话。CLI 串起代码浏览、修改、仓库命令、测试、diff 检查、会话恢复和可选发布。
- **面向后端的执行接口：** 可通过终端 REPL、单次 CLI/JSONL 集成或本地 HTTP 任务 API 使用。任务 API 支持有上限的 worker pool、任务状态持久化、SSE 事件、运行中追加指令、取消和恢复。
- **仓库长期记忆 RAG（实验方向）：** Sigma 可以在同一个仓库的不同 issue 之间检索经过审核的经验，例如测试环境限制、架构决策、兼容性规则和反复出现的问题。检索对象是记忆事实，不是源码块；可结合 BM25、Embedding、Qdrant 和排名融合。它能否提高 issue 修复效果仍需实验验证。
- **明确控制外部访问：** GitHub MCP 默认提供只读工具；推送功能分支和创建 Pull Request 需要显式配置并主动开启。

## Sigma 与 Pi 的关系

Sigma 从 [Pi](https://github.com/earendil-works/pi) 的 Agent Loop、工具生命周期和持久化会话设计中获得启发。两者定位和成熟度不同。

| 方面 | Pi | Sigma |
| --- | --- | --- |
| 主要目标 | 极简、可定制的 Coding Agent Harness | 面向仓库任务和后端集成的 Go 原生 Runtime |
| 运行时 | TypeScript / Node.js | Go 可执行程序；Docker 执行仓库命令 |
| 交互方式 | 交互式 CLI、print/JSON、RPC 和 TypeScript SDK | 交互式终端 REPL、单次 CLI/JSONL、本地 HTTP/SSE 任务 API |
| 扩展方式 | Extensions、Skills、提示词模板、主题和可分发的包；自定义机制与交互模式见 [Coding Agent README](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md) | Agent Skills 目录/frontmatter 子集与按需参考文件读取、可信命令工具适配器、GitHub MCP；不运行 Pi 扩展/包 |
| 执行隔离 | 默认工具与 Pi 进程使用相同权限；官方文档另行介绍可选容器/虚拟机隔离 | 仓库命令/测试工具内建 Docker 执行；每个任务另有独立 Git worktree |
| 环境准备 | 使用本机工具链，或另行配置容器/沙箱 | Agent 可检查项目配置并准备缓存的 Docker 镜像，安装选择的工具链和系统依赖 |
| 编码任务重点与证据 | Pi 是更广泛、成熟且可定制的 Coding Agent Harness，支持多种交互方式和扩展生态 | Sigma 专注仓库 Issue 修复 |

### Runtime 执行流程

```text
用户任务 / steering 指令
        ↓
持久化会话 transcript
        ↓
流式模型适配器 → assistant 文本与工具调用事件
        ↓
工具参数校验与权限策略
        ↓
有界执行：独立只读操作可并行；写入和命令按序执行
        ↓
工具结果追加到会话 → 下一轮模型调用
```

当前模型边界支持 Anthropic Messages 和兼容 OpenAI Chat Completions 的 API；内置 MiniMax、Anthropic、OpenAI、DeepSeek 和 OpenRouter 提供商。运行时配置可调整轮数/工具预算、上下文压缩、模型和工具超时、重试与工具权限，详见[配置说明](docs/CONFIGURATION.md)。

## Docker 命令隔离与环境准备

在 Sigma 的仓库任务流程里，`run_command` 和 Go 测试工具会在 Docker 中执行项目命令，而不是直接在宿主机运行。命令容器只把当前任务的独立 worktree 挂载为可写工作区；命令容器会移除 Linux capabilities、启用 `no-new-privileges`，并限制为 2 GiB 内存、2 个 CPU、128 个进程和 5 分钟运行时间。测试和命令容器默认使用临时 Docker internal 网络，不能访问外网；依赖下载需要联网时，可对当前会话使用 `/network on`，切换到 Docker bridge 网络。任务 worktree 和依赖缓存会在多次命令间保留。

执行项目命令前，Agent 可以检查仓库 Dockerfile、devcontainer、CI workflow、README 和依赖清单，再调用 `prepare_environment` 指定基础镜像与准备命令。Sigma 会构建并缓存派生镜像，供后续命令和恢复的 session 复用。Docker 构建上下文只有自动生成的 Dockerfile，不包含仓库源码。若准备环境时要下载系统包或依赖，需要主动开启网络。

这属于**命令执行隔离**，不等于整个 Agent 都处在完整安全沙箱中。模型客户端和仓库文件工具仍由 Sigma 进程运行；任务 worktree 以读写方式挂入 Docker，所以容器命令仍可以修改该任务的文件，但通过这个挂载不能修改 worktree 以外的宿主机文件。它不是为恶意多租户任务设计的强化安全边界。

这也是 Sigma 相对 Pi 默认行为的一个集成差异：Pi 的安全文档说明，其内建工具默认使用 Pi 进程所在用户的权限，而且 Pi 本身不内置 sandbox。Pi 另有容器化和外部隔离选项，包括 Docker、Docker Sandboxes、OpenShell 和 Gondolin。Sigma 的特点是把 Docker 命令执行、资源/网络限制和可复用环境准备直接集成在仓库任务工作流里；这不代表它比 Pi 的所有可选隔离方案都更安全。详见 Pi 的[安全说明](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/security.md)、[容器化指南](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/containerization.md)和 Sigma 的[Docker 配置说明](docs/CONFIGURATION.md)。

## 开始使用

需要 Go 1.26+、Git、Docker Desktop，以及一个受支持模型提供商的 API Key。Sigma 调用模型服务商的 API，不内置或下载模型。

在 `go-runtime` 目录从源码启动：

```powershell
go run ./cmd/sigma-agent
```

或构建 Windows 可执行文件：

```powershell
go build -o sigma-agent.exe ./cmd/sigma-agent
.\sigma-agent.exe
```

进入设置提示后，使用 `/login minimax` 保存模型 Key，使用 `/model list` 查看支持的提供商和 Key 配置状态，使用 `/model <provider>/<model-id>` 选择模型，再用 `/repo <path>` 选择一个干净的本地仓库。使用 `/model save` 将当前模型设为新会话默认值，然后直接输入任务或运行 `/start`。脚本模式也可以通过环境变量提供模型 Key。完整命令说明见[使用指南](docs/USAGE.md)；提供商、凭据、运行时限制和 Docker 设置见[配置说明](docs/CONFIGURATION.md)。

Docker 命令容器默认无法访问外网。如果任务需要下载依赖，可以用 `/network on` 为当前会话主动开启网络；安全边界和依赖缓存行为见配置说明。

## 使用 Sigma

### 交互式 Coding Session

```powershell
go run ./cmd/sigma-agent --repo C:\work\target-repository --interactive
```

在 `You>` 提示符中输入任务。Sigma 运行时，可以输入普通文本或使用 `/steer <text>` 在下一个安全轮次边界加入指令；`/followup <text>` 用于排队后续任务。`/diff` 查看补丁摘要，`/tree` 查看会话历史，`/branch <entry-id>` 从较早的完整节点继续，`/abort` 取消当前轮次，`/exit` 保存并退出。任务退出时会打印 session ID；下次在 setup 提示中用 `/resume <id>` 恢复。完整命令列表见[使用指南](docs/USAGE.md)。

例如：

```text
修复 issue #123。检查相关代码和项目指引，完成修改，运行合适的测试并检查完整 diff。不要提交或推送。
```

每个任务都在独立 worktree 中运行，原始仓库工作区保持不变。配置 `/login github` 后可获取 GitHub issue 上下文；GitHub MCP 默认只读，写操作需要显式开启。

### 单次 CLI 与进程集成

无需进入交互 REPL，直接运行一个任务：

```powershell
go run ./cmd/sigma-agent --repo C:\work\target-repository --task "修复 issue #123 并运行相关测试"
```

添加 `--json` 后，Sigma 会向标准输出写入带版本号的 JSONL 生命周期事件和最终结果：

```powershell
go run ./cmd/sigma-agent --repo C:\work\target-repository --task "修复 issue #123" --json
```

最终 JSONL 记录包含 session ID 和产物路径，之后可通过 `--resume <session-id>` 恢复。该模式是单次运行；如果要在任务运行时双向控制，请使用下方 HTTP API。

### 本地 HTTP 任务 API

在本机 loopback 地址启动服务（默认 `127.0.0.1:8080`）：

```powershell
go run ./cmd/sigma-agent --serve --max-concurrent 2
```

使用 PowerShell 提交任务：

```powershell
$body = @{
  repo = 'C:\work\target-repository'
  task = '修复 issue #123 并运行相关测试'
} | ConvertTo-Json

$task = Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:8080/tasks `
  -ContentType 'application/json' `
  -Body $body
$task

# 持续接收该任务的事件（按 Ctrl+C 停止）
curl.exe -N "http://127.0.0.1:8080/tasks/$($task.id)/events"

# 任务运行中追加一条指令
$message = @{ message = '修改 API 前检查调用方路径'; mode = 'steer' } | ConvertTo-Json
Invoke-RestMethod -Method Post `
  -Uri "http://127.0.0.1:8080/tasks/$($task.id)/messages" `
  -ContentType 'application/json' `
  -Body $message
```

API 端点如下：

| 方法与路径 | 用途 |
| --- | --- |
| `POST /tasks` | 提交仓库路径和任务描述，返回任务 ID |
| `GET /tasks/{id}` | 查询状态和时间戳 |
| `GET /tasks/{id}/events` | 通过 SSE 接收任务与 Agent 生命周期事件；可用 `Last-Event-ID` 重连 |
| `POST /tasks/{id}/messages` | 追加 `steer` 指令，或排队 `follow_up` 后续指令 |
| `POST /tasks/{id}/cancel` | 取消排队中或运行中的任务 |
| `POST /tasks/{id}/resume` | 从保存的 session 恢复中断任务 |

任务状态、消息和事件日志会写入本地。服务重启后可以恢复排队任务，也可以从保存的 session 继续中断任务。`--max-concurrent` 设置 worker 数量（1–32），超过并发容量的任务进入队列等待。请求/响应格式和 SSE 重放细节见[架构说明](docs/ARCHITECTURE.md)。

**服务边界：** HTTP 服务是单进程本地原型，没有身份认证，默认只监听 loopback；不要直接暴露到网络。`--serve` 模式不提供发布和 GitHub PR 创建能力。

## 仓库长期记忆 RAG 与 Skills

记忆实验关注同一仓库的重复开发任务。可复用事实包括不明显的测试命令、架构约定或之前任务中发现的兼容性问题。每条记忆保持简短，并记录仓库范围、类别、来源和审核日期。人工审核的 `.sigma-agent/MEMORY.md` 是权威记录；Qdrant 是由它生成的检索索引。

检索流程如下：

```text
MEMORY.md 中经审核的仓库事实
       ├── 词法排名（BM25）─────────────┐
       └── Embedding → Qdrant 向量检索 ├── RRF → search_memory → Agent 上下文
                                        ┘
```

Qdrant 使用精确仓库范围过滤。Agent 得到带出处的候选事实后，还要对照当前源码和测试核实；向量检索不可用时会回退到 BM25。这是“记忆 RAG”：Sigma 不会对仓库源码切块、做 Embedding 或建立源码索引，检索到的事实也不是答案或可直接复制的补丁。

### Skills 与长期记忆的区别

Skill 和长期记忆解决不同问题：

- Sigma 会发现项目级 `.agents/skills/`、`.pi/skills/`，以及用户级 `~/.agents/skills/`、`~/.pi/agent/skills/`。初始上下文只暴露 Skill 名称和描述；匹配后通过 `read_skill` 加载正文，再用 `read_skill_resource` 按需读取同一 Skill 目录下的 Markdown 指引或文本模板。交互会话中可用 `/skill:<name> <task>` 显式调用 Skill，包括 frontmatter 标记 `disable-model-invocation: true` 的 Skill。
- 支持常见 Agent Skills 字段：`name`、`description`、`license`、`compatibility`、`disable-model-invocation` 和 `user-invocable`。其他 frontmatter 字段会被容忍，但 Pi 的 `allowed-tools` 权限语义尚未实现；工具权限仍以 Sigma 配置为准。加载 Skill 或参考文件不会自动运行脚本。`read_skill_resource` 只读小型文本资料/模板，不开放脚本和二进制资源。
- `.sigma-agent/MEMORY.md` 保存可能跨任务复用的短事实，每条带类别、来源和日期，并按仓库隔离。Session snapshot 保存的是中断恢复状态，不是长期记忆。
- 当前源码和测试优先级更高。记忆是需要对照当前代码验证的参考，不应取代源码阅读。
- BM25 可在本机运行。配置 Embedding 和 Qdrant 后，Sigma 会加入按仓库过滤的向量候选，再用 Reciprocal Rank Fusion 融合排名。这个记忆功能不会给源码分块或建立源码向量索引。

### 计划验证什么

项目分两层评估记忆价值：

| 实验 | 要回答的问题 | 证据 |
| --- | --- | --- |
| 标注过的记忆查询 | BM25 + 向量混合检索能否找回相关事实？没有相关记忆时能否正确返回空结果？ | Recall@K、MRR、逐查询排名、空结果行为和延迟 |
| 留出的 Issue 成对实验 | 检索到的记忆是否帮助 Agent 修复同一仓库的新 issue？ | 记忆关闭/开启两组新鲜运行；模型、提示词、预算、基线提交、Docker 镜像和 evaluator 相同；比较是否解决、测试、token/工具使用及记忆是否被正确应用 |

第一层衡量检索，不代表代码修复能力。第二层才是说明仓库长期记忆 RAG 是否帮助解决 issue 的关键证据。目前 README 不报告检索分数，也不宣称开启记忆提高了 issue 通过率。数据边界和实验控制见[记忆检索协议](evals/memory-retrieval-evaluation.md)及[Skills/记忆消融计划](evals/skills-memory-evaluation.md)；实现细节见[架构说明](docs/ARCHITECTURE.md)，配置方法见[配置说明](docs/CONFIGURATION.md)，Skill 目录和调用方式见[使用指南](docs/USAGE.md)。

## 评测与项目状态

Sigma 记录了 issue 基准评测流程和历史尝试。只有任务输入、Agent 运行、官方 evaluator、基线行为和结果记录可以一起核查时，才把结果计为有效评测。带提示的尝试、评测夹具问题和真正解决的任务应分开报告；少量子集结果不能代表整体成功率，也不能据此宣称与 Pi 的能力相当。

可先看[基准评测协议](evals/benchmark-protocol.md)和[Issue 清单](evals/issue-benchmark-inventory.md)；[SWE-bench Verified Mini 计划](evals/swebench-verified-mini-50-evaluation-plan.md)说明了拟议的 50 题评测，[记忆检索评测计划](evals/memory-retrieval-evaluation.md)则把检索指标与 issue 修复结果分开考察。这些文档描述的是协议和计划，不代表 50 题评测已经完成。本文不宣称 Sigma 已达到 Pi 的水平，也不宣称仓库记忆提高了代码修复成功率。实现细节见[架构说明](docs/ARCHITECTURE.md)和[配置说明](docs/CONFIGURATION.md)。

## 开发

Go Runtime 位于本仓库；归档的 TypeScript/Pi 原型不参与运行。进入 `go-runtime` 后，可构建并运行 Go 检查：

```powershell
go build ./...
go test ./...
```

基于 Docker 的任务和基准检查还依赖特定镜像、网络和测试夹具；执行前请查看评测协议。Sigma 使用 [MIT License](LICENSE)。
