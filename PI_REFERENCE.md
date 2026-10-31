# Pi Agent design reference

Sigma Agent follows Pi Agent's useful runtime ideas without importing or translating the SDK. Upstream research and links are preserved in `archive/ts-pi-prototype/research.md`.

| Pi concept | Go implementation | Status |
|---|---|---|
| Inner tool loop and outer conversation loop | `internal/agent/runtime.go` | Streaming turns, tool calls, steering/follow-up queues, bounded execution, lifecycle events. |
| Canonical session transcript and history | `internal/agent/runtime.go`, `internal/agent/session_store.go` | Durable snapshots, interrupted-tool recovery, append-only parent-linked entries, branch API, and persisted model-change entries. CLI history navigation and model switching are still missing. |
| Provider adapter seam | `internal/agent/minimax.go` | Direct MiniMax Messages SSE adapter; no Agent SDK. |
| Tool validation and lifecycle hooks | `internal/agent/runtime.go` | JSON validation, tool validators, policy hooks, timeout, bounded result. |
| Coding tools and retrieval | `internal/sigmaagent/tools.go` | Native Go allowlist, BM25 retrieval, hash-checked edits, Docker test profiles, diff. |
| External tool integration | `internal/agent/github_mcp.go` | Optional, read-only GitHub MCP client for issue, repo, and PR context. |
| Context management | `internal/agent/context.go` | Extractive provider-request window; full canonical history remains in session storage. |

The goal is a reliable coding-task execution loop, not a full replica of Pi's TUI, extension system, or provider catalog. The active project is the Go runtime; the former TypeScript/Pi prototype is archived outside its build path.
