<p align="center">
  <img src="docs/assets/sigma-agent-mark.svg" alt="Sigma Agent 标志" width="112">
</p>

<h1 align="center">Sigma Agent</h1>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Sigma 是一个用 Go 编写的 Coding Agent，面向已有代码仓库中的 issue 修复。它把模型与仓库工具连接起来，在 Docker 中运行项目命令，并串起代码调查、修改、测试和 diff 检查。每个任务在独立的 Git worktree 中执行，不会直接改动原始工作区。

## Sigma 的特点

- **Go 原生 Agent Runtime：** Sigma 用 Go 自己实现模型与工具调用循环，以及任务运行时。它可以编译为原生可执行文件，适合接入 Go 后端环境；Docker 则单独负责隔离仓库命令。
- **完整的仓库任务流程：** CLI 将文件查看与修改、命令执行、测试、Git diff 检查、会话持久化与恢复、可选发布整合到面向 issue 的工作流中。
- **面向后端的执行控制：** 除交互式 CLI 外，运行时还提供任务持久化、并发上限、SSE 事件、取消和恢复 API。
- **可选的仓库知识：** Skills 按需加载任务指引。人工审核、按仓库隔离的长期记忆可通过 BM25 和可选的 Qdrant 混合检索召回。这是已实现的工程能力；目前不宣称记忆已经提高了基准任务通过率。
- **外部访问显式启用：** GitHub MCP 和代码发布都需要主动开启，便于控制仓库读取与写入操作。

Sigma 从 [Pi](https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent) 的 Agent Loop、工具调用和会话工作流中获得启发。Pi 是一个范围更广、成熟度更高的 Coding Agent 项目，支持多种交互方式和扩展生态。Sigma 聚焦的问题更具体：Go 原生的仓库任务运行时如何实现，以及怎样提供后端式的执行控制。选择 Go 的价值在部署和后端集成；这本身不代表 Sigma 的代码修复能力强于其他 Agent。

## 开始使用

需要 Go 1.26+、Git、Docker Desktop，以及一个受支持模型提供商的 API Key。

在 `go-runtime` 目录启动：

```powershell
go run ./cmd/sigma-agent
```

进入设置提示后，使用 `/login minimax` 保存模型 Key，使用 `/model` 选择提供商和模型，再用 `/repo <path>` 选择一个干净的本地仓库。运行 `/help` 查看可用命令。脚本模式也可以通过环境变量提供模型 Key。

## 修复 GitHub Issue

先克隆目标仓库并确认工作区干净。在 Sigma 的设置提示中选择仓库，并按需配置 GitHub 访问：

```text
/repo C:\work\target-repository
/login github
/publish sigma/issue-123
/github-write on
```

然后描述任务，例如：

```text
修复 issue #123。检查相关代码和项目指引，完成修改，运行合适的测试并检查完整 diff。然后提交并推送到 sigma/issue-123，创建 Pull Request。不要合并。
```

`/publish` 允许提交并推送到指定功能分支。`/github-write on` 允许通过 GitHub MCP 创建 Pull Request。Git push 使用本机 Git credential helper；GitHub Token 用于 MCP 访问。Docker 网络默认关闭；依赖下载需要联网时，可使用 `/network on` 开启。

Sigma 会创建独立任务 worktree，并保存补丁和会话，不会修改原始工作区。使用 `/sessions` 查找已保存会话，再用 `/resume <id>` 继续。在交互会话中，`/tree` 查看对话分支，`/branch <entry-id>` 从较早的完整节点继续。恢复会话后需要重新启用发布权限。

## 能力范围与文档

`run_command` 可以在隔离的 Linux 容器中运行仓库自己的构建、测试和检查命令。Sigma 能查看仓库清单文件和 CI 配置来选择 Docker 环境；如果项目依赖额外服务或准备步骤，可能仍需手动配置。

查看[配置说明](docs/CONFIGURATION.md)、[架构说明](docs/ARCHITECTURE.md)，以及 [`evals/`](evals/) 中的基准协议和评测记录。
