# Repository layout / 仓库目录说明

This guide describes the purpose of each tracked top-level directory and the
Go source packages. It is a navigation guide, not a claim that every individual
file is required by every build target.

本文说明仓库中已跟踪的顶层目录和 Go 源码包。它用于帮助定位代码，不表示
每一个文件都会进入每一种构建产物。

## `cmd/` and `internal/` / 可执行入口与实现模块

`cmd/sigma-agent` builds the `sigma-agent` executable (which can be installed
under the command name `sigma`). It owns process startup, CLI flags, setup,
terminal UI, and dependency wiring. Its `main.go` is the composition root: it
connects reusable modules and starts the selected mode.

`cmd/sigma-agent` 构建 `sigma-agent` 可执行程序（可以安装为 `sigma` 命令）。它负责
进程启动、命令行参数、setup、TUI 和依赖组装。`main.go` 是组装入口，负责把可复用模块
连接起来并启动相应运行模式。

`internal/` contains implementation packages used by the executable. In Go,
the `internal` path also prevents unrelated modules outside this repository's
parent from importing these packages. Dependencies point inward from `cmd` to
`internal`; implementation packages must not import the CLI entry package.

`internal/` 保存可执行程序使用的实现包。Go 的 `internal` 目录规则也会阻止仓库之外的
无关模块导入这些包。依赖方向是 `cmd` 调用 `internal`；实现包不能反过来导入 CLI 入口。

| Package / 包 | Responsibility / 职责 |
| --- | --- |
| `cmd/sigma-agent` | Executable, CLI, TUI, setup, process wiring, and built-in command handling. The adjacent `workflow-guidance.md` is embedded in the executable. / 可执行入口、CLI、TUI、setup、进程组装和内置命令；同目录的 `workflow-guidance.md` 会嵌入程序。 |
| `internal/agent` | Conversation loop, provider streaming, sessions, tool dispatch/policy, context, and recovery. / 对话循环、模型流式适配、会话、工具调度与策略、上下文和恢复。 |
| `internal/workspace` | Repository tools: file/search/edit, Git operations, Docker commands, project context, Skills, memory, and diff. / 仓库工具：文件/搜索/编辑、Git、Docker 命令、项目上下文、Skills、记忆和 diff。 |
| `internal/model` | Provider/model catalog, selection, credentials lookup, and provider construction. / Provider 与模型目录、选择、凭据查询和 Provider 构造。 |
| `internal/config` | Runtime configuration types, defaults, loading, and validation. / Runtime 配置结构、默认值、读取和校验。 |
| `internal/credentials` | Local credential storage. / 本地凭据存储。 |
| `internal/fsx` | Small filesystem primitives, with OS-specific implementations. / 小型文件系统原语，含操作系统专用实现。 |
| `internal/mcp` | MCP stdio protocol client and child-process lifecycle. / MCP stdio 协议客户端和子进程生命周期管理。 |
| `internal/mcpconfig` | MCP server configuration loading and runtime tool conversion. / MCP 服务配置读取及运行时工具转换。 |
| `internal/pkgmgr` | Package source parsing, installation, and local registry. / 包来源解析、安装和本地注册表。 |
| `internal/server` | Loopback HTTP task service and worker lifecycle. / 本机 HTTP 任务服务和 worker 生命周期。 |
| `internal/workflow` | Interfaces and dispatch for JavaScript workflow extensions. / JavaScript 工作流扩展的接口和事件分发。 |

At the current revision, the `sigma-agent` dependency graph includes every
`internal/*` package listed above. Removing one would require removing or
replacing the feature that depends on it.

当前版本中，`sigma-agent` 的依赖图包含上表列出的全部 `internal/*` 包。删除某个包前，
需要先移除或替换依赖它的功能。

Not every file is compiled into every executable: `_test.go` files are used by
`go test`, not included in the `sigma` binary; files guarded by build tags such
as `replace_windows.go` and `replace_unix.go` are selected for the target OS.
Some non-Go files are runtime inputs: for example, `workflow-guidance.md` is
embedded by `cmd/sigma-agent/prompt.go`.

并非所有文件都会编入每个可执行文件：`_test.go` 只供 `go test` 使用，不会进入 `sigma`
二进制；带构建标签的文件（如 `replace_windows.go`、`replace_unix.go`）会按目标系统选择。
部分非 Go 文件也是运行时资源，例如 `workflow-guidance.md` 由
`cmd/sigma-agent/prompt.go` 嵌入程序。

## Other tracked directories / 其他已跟踪目录

| Path / 路径 | Purpose / 用途 |
| --- | --- |
| `.agents/skills/` | Project-scoped Skills loaded on demand by Sigma. / Sigma 按需加载的项目级 Skills。 |
| `docs/` | User, configuration, architecture, design, and Docker documentation; `assets/` contains documentation images and marks. / 使用、配置、架构、设计和 Docker 文档；`assets/` 保存文档图片与标志。 |
| `evals/` | Public benchmark results, methods, protocols, issue records, and planned or historical studies. The directory's README is its index. / 公开基准结果、方法、协议、问题记录及计划中或历史实验；目录 README 是索引。 |
| `examples/workflow-extension/` | Minimal JavaScript extension example. / JavaScript 扩展示例。 |
| `scripts/` | Three PowerShell helpers referenced by the configuration guide: save embedding, GitHub, or MiniMax credentials locally without echoing the secret. / 配置文档引用的 3 个 PowerShell 辅助脚本：将 embedding、GitHub 或 MiniMax 凭据隐藏输入并保存到本地。 |

The repository-root files `go.mod` and `go.sum` define and lock Go dependencies;
`config.json` is the checked-in runtime configuration example. The live service
test itself remains in `internal/server/server_live_test.go`; the unreferenced
`scripts/test-service-e2e.ps1` wrapper is a local convenience, is not needed to
build Sigma, and is intentionally excluded from GitHub. Local state such as
`.secrets/`, `.sigma-agent/`, `.learnings/`, `var/`, and `research/` is ignored
by Git and is not distributed from GitHub.

The three published credential scripts are optional setup conveniences, not
runtime dependencies; Sigma can run without them when credentials are
configured by another supported method.

根目录的 `go.mod`、`go.sum` 定义并锁定 Go 依赖；`config.json` 是纳入版本管理的运行配置
示例。服务集成测试本身仍在 `internal/server/server_live_test.go`；没有被文档或工作流引用的
`scripts/test-service-e2e.ps1` 只是本机便捷包装，不是构建 Sigma 的必需文件，现已设为仅本地保留。
`.secrets/`、`.sigma-agent/`、`.learnings/`、`var/` 和 `research/` 等本地状态目录由 Git 忽略，
不会从 GitHub 分发。

公开保留的 3 个凭据脚本只是可选配置辅助工具，不是 Sigma 的运行依赖；也可以用其他受支持的
方式配置凭据后直接运行 Sigma。
