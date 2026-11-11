<p align="center">
  <img src="docs/assets/sigma-agent-mark.svg" alt="Sigma Agent 标志" width="112">
</p>

<h1 align="center">Sigma Agent</h1>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Sigma 是一个用 Go 编写的 Coding Agent，面向真实仓库里的代码任务。它连接模型与项目工具，支持从代码调查、实现修改到验证的完整工作流。

## 开始使用

需要 Docker Desktop 和一个受支持模型提供商的 API Key。Git 是可选的，安装后 Sigma 还能提供 Git diff 检查。从当前源码构建需要 Go 1.26+。

在 `go-runtime` 目录构建并安装 `sigma`：

```powershell
$binDir = Join-Path (go env GOPATH) 'bin'
go build -o (Join-Path $binDir 'sigma.exe') ./cmd/sigma-agent
sigma --version
```

或构建 Windows 可执行文件：

```powershell
go build -o sigma.exe ./cmd/sigma-agent
```

进入要处理的仓库启动 Sigma：

```powershell
cd C:\work\target-repository
sigma
```

TUI 会自动启动会话。输入 `/login` 保存提供商 Key，输入 `/model` 选择模型，然后直接输入任务。脚本模式也可以通过环境变量提供模型 Key。交互细节见[使用指南](docs/USAGE.md)；提供商、凭据和运行时设置见[配置说明](docs/CONFIGURATION.md)。

## 实验成果与产品亮点

**评测成果：** [SWE-bench Verified Mini 公开榜单](https://hal.cs.princeton.edu/swebench_verified_mini)截至 2026-11-05 的历史最高成绩为 **54%**，由 SWE-Agent 搭配 o4-mini Low、Claude 3.7 Sonnet High 和 Claude Opus 4.1 High 的公开运行并列取得。Sigma 在固定 50 题上的混合模型实验，DeepSeek thinking 开启前为 **34/50（68%）**，引入 DeepSeek Flash thinking 后当前累计为 **40/50（80%）**。运行使用过 MiniMax M3/M3.1 Flash 和 DeepSeek Flash。查看[结果与逐题证据](evals/swebench-verified-mini/evaluation-results.md)。

**公开成绩参照：** 官方 500 题 [SWE-bench Verified 榜单](https://www.swebench.com/)的全 Agent 列表最高条目为 79.2%（Claude 4.5 Opus，Sonar Foundation Agent / live-SWE-agent 两个提交）；统一 Harness 的 Bash-only 列表最高为 76.8%（Claude 4.5 Opus high + mini-SWE-agent 2.0.0）。DeepSeek 的 [V4 模型卡](https://huggingface.co/deepseek-ai/DeepSeek-V4-Flash)列出 Flash/Pro 在 Non-Think/High/Max 六种设置下的 Verified 成绩；六项简单平均为 **77.5%**（此处计算，不是厂商公布的汇总分），其中 V4-Pro Max 为 80.6%，V4-Flash Think-Max 为 79.0%。DeepSeek 表示其代码 Agent 评测使用 [DeepSeek Harness minimal mode](https://api-docs.deepseek.com/updates/)、max effort、`top_p=0.95` 和 `temperature=1.0`。这些 500 题结果使用的模型、Harness 和评测条件不同，只作背景参照，不能与我们的 Mini 50 直接比较。


各次运行按记录的配置启用 Skills 和 Memory；没有把官方补丁、隐藏测试或标准答案预置进去。Skill 写的是可复用的通用工作方法，快照可在 [reference 目录](evals/swebench-verified-mini/reference/README.md)查看。Memory 会随运行累积，不同题目的记忆状态可能不同，具体条件见逐题记录。

- **面向真实 Issue 的编码流程：** 在持久化会话中浏览仓库、修改代码、运行项目命令和测试，并检查最终 diff。
- **贴合仓库的执行环境：** Agent 可检查项目配置并准备可复用 Docker 工具链；仓库命令在有资源限制的容器中运行。
- **支持长任务：** 会话保存运行记录并支持恢复；Runtime 跟踪文件变更和所需验证。
- **可扩展：** 支持项目 Skills、仓库记忆、MCP 工具和 JavaScript 工作流扩展。记忆检索仍属实验能力，尚无隔离实验能证明它提高了修复成功率。
- **Go 原生 Agent Runtime：** 模型流式调用、工具生命周期、权限策略、上下文管理和工作区工具均由 Go 实现。

## Sigma 与 Pi 的关系

Sigma 从 [Pi](https://github.com/earendil-works/pi) 的 Agent Loop、工具生命周期和持久化会话设计中获得启发。两者定位和成熟度不同。

| 方面 | Pi | Sigma |
| --- | --- | --- |
| 主要目标 | 极简、可定制的 Coding Agent Harness | 面向仓库任务和后端集成的 Go 原生 Runtime |
| 运行时 | TypeScript / Node.js | Go 可执行程序；Docker 执行仓库命令 |
| 交互方式 | 交互式 CLI、print/JSON、RPC 和 TypeScript SDK | 集成终端 TUI、单次 CLI/JSONL |
| 扩展方式 | Extensions、Skills、提示词模板、主题和可分发的包；自定义机制与交互模式见 [Coding Agent README](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md) | 包内 Skills、MCP 工具，以及支持斜杠命令、Agent 工具、通知、确认和只读生命周期事件的 JavaScript 工作流扩展宿主 |
| 执行隔离 | 默认工具与 Pi 进程使用相同权限；官方文档另行介绍可选容器/虚拟机隔离 | 文件工具直接修改所选仓库；仓库命令/测试工具在挂载该仓库的 Docker 容器中执行 |
| 环境准备 | 使用本机工具链，或另行配置容器/沙箱 | Agent 可检查项目配置并准备缓存的 Docker 镜像，安装选择的工具链和系统依赖 |
| 编码任务重点与证据 | Pi 是更广泛、成熟且可定制的 Coding Agent Harness，支持多种交互方式和扩展生态 | Sigma 专注仓库 Issue 修复，提供仓库感知的 Docker 环境准备和由 Runtime 管控的测试/diff 验证；Mini 实验的范围和证据边界见下文 |

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

Sigma 的 `run_command` 会在 Docker 中执行项目命令，而不是直接在宿主机运行。命令容器以可写方式挂载所选仓库到 `/workspace`，移除 Linux capabilities、启用 `no-new-privileges`，并限制为 2 GiB 内存、2 个 CPU、128 个进程和 5 分钟运行时间。容器默认不能访问外网；需要下载依赖时，可用 `/network on` 为当前会话启用网络。依赖包缓存会在多次命令间保留。

运行命令前，Agent 可以检查仓库 Dockerfile、devcontainer、CI workflow、README 和依赖清单，再调用 `prepare_environment` 指定基础镜像与准备命令。Sigma 会构建并缓存派生镜像，供后续命令和恢复的 session 复用。Docker 构建上下文只有自动生成的 Dockerfile，不包含仓库源码；需要下载系统包或项目依赖时，应有意识地开启网络。

这属于**命令执行隔离**，不等于整个 Agent 都处在完整安全沙箱中。模型客户端和仓库文件工具仍由 Sigma 进程运行。所选仓库会以读写方式挂入 Docker，因此 Agent 的文件修改和命令都会直接作用于该仓库。这个容器不是面向恶意多租户任务的强化安全边界。

镜像复用、网络行为、资源限制和清理方式见[Docker 文档](docs/DOCKER.md)与[配置说明](docs/CONFIGURATION.md)。

## 项目结构与设计

Go 可执行程序由 `cmd/sigma-agent` 负责组装，可复用的实现模块放在 `internal/`。CLI 是最外层的组装入口，可以依赖内部模块；内部模块不能反向依赖 CLI。

| 路径 | 职责 |
| --- | --- |
| `cmd/sigma-agent` | `sigma` 可执行程序：命令行参数、进程生命周期、运行时组装、setup、TUI、包管理命令和工作流扩展宿主。`main.go` 负责组装，`tui.go` 负责 Bubble Tea 界面。 |
| `internal/agent` | 通用对话运行时：会话与 transcript、流式模型适配器、工具调度和策略、上下文管理、故障恢复及会话存储。 |
| `internal/workspace` | Sigma 的编码工作区：文件读写/搜索工具、Docker 命令和环境准备、项目指引、Skills、记忆及工作区 diff。它依赖 `agent` 的接口，`agent` 不依赖它。 |
| `internal/model` | Provider/模型列表、模型选择配置、凭据查询和模型构造。 |
| `internal/config` | 运行时配置结构、默认值、加载和校验。 |
| `internal/credentials` | 模型与 GitHub 集成使用的本地凭据存储。 |
| `internal/fsx` | 跨操作系统的小型文件系统原语，例如原子替换。 |
| `internal/mcp` | 通用 MCP stdio 协议客户端及服务进程生命周期管理。 |
| `internal/mcpconfig` | MCP 配置加载，并将配置的服务器转换为运行时工具。 |
| `internal/pkgmgr` | 包来源解析、包安装和本地包注册表。 |
| `internal/server` | 后端任务服务实现。 |
| `internal/workflow` | 工作流扩展接口与分发：命令、工具、确认、通知和生命周期事件。 |

`agent` 负责模型对话和工具生命周期；`workspace` 提供面向项目的编码工具。`model` 选择并构造 Provider 适配器，协议实现位于 `agent`。完整组件关系与依赖边界见[架构说明](docs/ARCHITECTURE.md)、[配置说明](docs/CONFIGURATION.md)和[Agent 设计说明](docs/AGENT_DESIGN_NOTES.md)。

## 评测

[`evals/`](evals/README.md) 收录 50 题结果表与失败分析、实验方法、评测协议、问题与恢复记录，以及逐题详情。建议先读[结果与失败分析](evals/swebench-verified-mini/evaluation-results.md)，再读[实验方法](evals/swebench-verified-mini/experiment-method.md)；复现实验或排查运行问题时，再查[评测协议](evals/swebench-verified-mini/benchmark-protocol.md)和[问题日志](evals/swebench-verified-mini/evaluation-issues.md)。

了解项目本身可先读[架构说明](docs/ARCHITECTURE.md)，其中介绍组件边界和 Runtime 流程；[Agent 设计说明](docs/AGENT_DESIGN_NOTES.md)与[写代码能力迭代记录](docs/SIGMA_CODING_AGENT_ITERATION_HISTORY.md)记录编码行为设计及其演进；[配置说明](docs/CONFIGURATION.md)、[使用指南](docs/USAGE.md)和[Docker 文档](docs/DOCKER.md)介绍配置与操作。

## 扩展能力

Sigma 支持项目 Skills、MCP 工具和 JavaScript 工作流扩展，可按项目需要增加指引、外部工具和流程行为。安装方式与支持格式见[包与 MCP 服务器](docs/PACKAGES.md)。

## Skills 与仓库记忆

Skills 按需提供项目工作流程；仓库记忆按需检索同一仓库中经过审核的事实。记忆使用词法搜索，也可按配置增加向量检索，不会索引源码。Mini 实验尚未通过开关对照隔离出记忆对 Issue 修复率的影响。详情见[Skills 与包](docs/PACKAGES.md)、[记忆配置](docs/CONFIGURATION.md#repository-long-term-memory)、[架构说明](docs/ARCHITECTURE.md)和[Mini 记忆实验记录](evals/planned/skills-memory/repository-memory-status.md).

## 开发

进入 `go-runtime` 后，可构建并运行 Go 检查：

```powershell
go build ./...
go test ./...
```

基于 Docker 的任务和基准检查还依赖特定镜像、网络和测试夹具；执行前请查看评测协议。Sigma 使用 [MIT License](LICENSE)。
