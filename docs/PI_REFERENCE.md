# Pi Agent design reference

Sigma Agent follows Pi Agent's useful runtime ideas without importing or translating the SDK. Upstream research and links are preserved in `archive/ts-pi-prototype/research.md`.

| Pi concept | Go implementation | Status |
|---|---|---|
| Inner tool loop and outer conversation loop | `internal/agent/runtime.go` | Streaming turns, tool calls, steering/follow-up queues, bounded execution, lifecycle events. |
| Canonical session transcript and history | `internal/agent/runtime.go`, `internal/agent/session_store.go` | Durable snapshots, interrupted-tool recovery, append-only parent-linked entries, branch API, persisted model-change entries, and basic `/sessions`, `/tree`, `/branch` navigation. |
| Provider adapter seam | `internal/agent/minimax.go`, `internal/agent/openai_compatible.go` | MiniMax and Anthropic Messages SSE adapters plus OpenAI-compatible adapters for OpenAI, DeepSeek, and OpenRouter; no Agent SDK. |
| Tool validation and lifecycle hooks | `internal/agent/runtime.go` | JSON validation, tool validators, policy hooks, timeout, bounded result. |
| Coding tools | `internal/sigmaagent/tools.go` | Workspace-bounded file tools, Docker Go profiles, repository-native Docker commands, Git publishing, and diff review. |
| External tool integration | `internal/agent/github_mcp.go`, `internal/agent/command_plugins.go` | GitHub MCP context with opt-in scoped PR creation, plus config-driven trusted command tools. |
| Shared MCP client | `internal/mcp/client.go`, `internal/agent/mcp_tools.go` | One stdio MCP client for every server: protocol version discovery with fallback, frame limits, tool listing, namespaced `mcp__<server>__<tool>` tools, and process cleanup. |
| Package manager | `internal/pkgmgr/`, `internal/mcpconfig/` | npm, git, and local packages installed into a scope directory and recorded in a scope registry. A package contributes Agent Skills, and `mcp.json` can start the MCP server a package ships. |
| Extension package system | `internal/pkgmgr/` | Package extension modules are listed and reported, but their code is not executed. This is the remaining gap against Pi's extension host. |
| Context management | `internal/agent/context.go`, `internal/agent/compact_context.go` | Automatic bounded extractive compaction and model-requested session summaries; provider cache-token telemetry is recorded when returned. Real MiniMax hit rate and token savings remain unmeasured. |

The goal is a reliable coding-task execution loop, not a full replica of Pi's TUI, extension package system, or provider catalog. The active project is the Go runtime; the former TypeScript/Pi prototype is archived outside its build path.

The package and MCP subset is documented in [Packages and MCP servers](PACKAGES.md).
