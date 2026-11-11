# Pi Agent design reference

Sigma Agent follows Pi Agent's useful runtime ideas without importing or translating the SDK. Upstream research and links are preserved in `archive/ts-pi-prototype/research.md`.

For the benchmark-driven history of Sigma's own coding tools, context handling, workflow guards, and provider fixes, see the [Sigma coding-agent iteration history](SIGMA_CODING_AGENT_ITERATION_HISTORY.md).

| Pi concept | Go implementation | Status |
|---|---|---|
| Inner tool loop and outer conversation loop | `internal/agent/runtime.go` | Streaming turns, tool calls, steering/follow-up queues, bounded execution, lifecycle events. |
| Canonical session transcript and history | `internal/agent/runtime.go`, `internal/agent/session_store.go` | Durable snapshots, interrupted-tool recovery, append-only parent-linked entries, branch API, persisted model-change entries, and `/tree` history navigation. |
| Provider adapter seam | `internal/agent/anthropic_messages.go`, `internal/agent/openai_chat_completions.go` | MiniMax/Anthropic Messages SSE and OpenAI Chat Completions adapters for OpenAI, DeepSeek, and OpenRouter; no Agent SDK. |
| Tool validation and lifecycle hooks | `internal/agent/runtime.go` | JSON validation, tool validators, policy hooks, timeout, bounded result. |
| Coding tools | `internal/workspace/tools.go` | Workspace-bounded file tools, Docker Go profiles, repository-native Docker commands, and diff review. |
| External tool integration | `internal/agent/github_mcp.go`, `internal/agent/command_plugins.go` | GitHub MCP context with opt-in scoped PR creation, plus config-driven trusted command tools. |
| Shared MCP client | `internal/mcp/client.go`, `internal/agent/mcp_tools.go` | One stdio MCP client for every server: protocol version discovery with fallback, frame limits, tool listing, namespaced `mcp__<server>__<tool>` tools, and process cleanup. |
| Package manager | `internal/pkgmgr/`, `internal/mcpconfig/` | npm, git, and local packages installed into a scope directory and recorded in a scope registry. A package contributes Agent Skills, and `mcp.json` can start the MCP server a package ships. |
| Extension package system | `internal/pkgmgr/`, `internal/workflow/` | JavaScript extensions can register slash commands and Agent tools, use notify/confirm, and observe read-only lifecycle events. Built-in and configured MCP tools share the host registration path. Custom TUI rendering and mutable session hooks are not exposed. |
| Context management | `internal/agent/context.go`, `internal/agent/compact_context.go` | Automatic bounded extractive compaction and model-requested session summaries; provider cache-token telemetry is recorded when returned. Real MiniMax hit rate and token savings remain unmeasured. |

The goal is a reliable coding-task execution loop, not a full replica of Pi's TUI, extension package system, or provider catalog. The active project is the Go runtime; the former TypeScript/Pi prototype is archived outside its build path.

The package and MCP subset is documented in [Packages and MCP servers](PACKAGES.md).

## What differs in coding tasks

Sigma is not a fork of Pi and does not share Pi's TypeScript agent loop. The table above maps concepts we adopted; the behavior below is implemented independently in Go and can diverge from Pi in defaults, edge cases, and maturity.

| Area | Sigma implementation and observed distinction | Practical consequence |
|---|---|---|
| Repository edits | Sigma has its own workspace file tools, read-before-edit version checks, bounded whole-file writes, diff inspection, and Docker-backed command execution. Later benchmark work added recovery messages for rejected large-file shrink operations. | The runtime can prevent some accidental damage and keep commands scoped to the candidate workspace. These controls do not identify the right code path or guarantee a correct patch. |
| Context and continuation | Sigma keeps a canonical persisted transcript, supports session resume/branching, performs automatic extractive window compaction, and has an optional model-generated `compact_context` handoff. | It can continue long work without deleting the source transcript, but summaries can omit important details and context compaction itself has caused provider-message ordering bugs that needed a Sigma-specific fix. |
| Completion workflow | Sigma tracks whether the latest source edit was followed by a test and diff review, emits recovery/checkpoint feedback, and has a configurable repeated-failure guard. Pi's hooks are a different extension mechanism; these exact policies are Sigma-owned. | Sigma can make missing verification visible and avoid some identical retries. It cannot reliably recognize every semantically repeated dead end, and hard enforcement previously blocked reasonable finishes, so the review gate was softened. |
| DeepSeek reasoning | Sigma's adapter originally disabled thinking and did not preserve `reasoning_content`; a Pi trace exposed the configuration mismatch. Sigma later added its own thinking configuration, streaming support, and reasoning round-trip. | This was a real Sigma capability gap, not a prompt or Skill issue. It does not establish that Sigma's reasoning quality now matches Pi's. |
| Provider protocol recovery | Sigma handles provider-specific message serialization, empty thinking-only messages, missing finish reasons, cache counters, and tool-call ordering in its own adapter. | These fixes prevent some infrastructure failures from consuming a coding run. They are compatibility work, not evidence of better code generation. |
| Skills and project guidance | Sigma has on-demand project Skills and repository instructions; Pi has its own Skill and AGENTS.md handling. | A result with a Skill loaded measures the whole guided setup. It cannot be attributed to Sigma's runtime alone or compared to a Pi run unless the loaded content and timing match. |

## What the coding-task results support

Sigma has demonstrated that it can make repository changes that pass official issue-specific tests. The current [50-task Mini results](../evals/swebench-verified-mini/evaluation-results.md) list 38 resolved out of 50 under the latest mixed historical conditions. That is useful evidence that this runtime can solve a meaningful share of the selected Django and Sphinx tasks. It is **not** a clean, fixed-condition 76% estimate: the table incorporates different models and budgets, resumptions, and later guided retests; several latest passes used updated Skills or other steering. It is also not the public 500-task SWE-bench Verified leaderboard score.

The earlier Go issue study is a useful counterweight. Its [inventory](../evals/historical/issue-benchmark-inventory.md) records one clean issue-only success (#1783), alongside valid issue-only failures. Other successful outcomes were guided by implementation hints, detailed acceptance cases, or a PR diff; #2032 had positive source-only target-test evidence, but the raw patch collided with the evaluator test patch. Those runs prove the end-to-end workflow and some repair ability, but they do not support a stable general success rate.

The practical picture is therefore:

- **What Sigma does well enough to claim:** it has an implemented coding loop that can inspect a repository, edit files, run repository commands, preserve/resume sessions, and produce patches that have passed official F2P/P2P evaluation on multiple tasks. Runtime guardrails now target avoidable failures such as destructive large-file edits and malformed DeepSeek tool-history requests; the overall benchmark does not isolate how much those guardrails change the solve rate.
- **What remains uneven:** Sigma can spend many turns exploring without changing production code, make a plausible edit at the wrong abstraction layer, miss framework-specific contracts, or introduce regressions. More turns and generic workflow reminders have not reliably solved those cases.
- **What is not established:** a controlled success rate for issue-only work across a representative task set; a causal gain from any single runtime change or Skill; or parity/superiority to Pi.

## What we can and cannot compare with Pi

The clearest local comparison is Django #12406. Pi resolved it in 67 turns; Sigma's first run used 128 turns and remained unresolved. Trace review found two concrete setup differences: Sigma had an older Python Skill snapshot and had DeepSeek thinking disabled while dropping `reasoning_content`. After those gaps were fixed, a later Sigma auxiliary run used Pi's exact Skill snapshot, Memory off, offline execution, and the same short steer after turn 32; it resolved in 41 turns with thinking at medium effort. This is useful evidence that Sigma could solve the task after its setup and guidance were corrected. It is still not a clean blind or single-variable head-to-head: the successful Sigma run was answer-adjacent guided, and the reasoning settings were not identical in every recorded detail. It does not establish that Sigma is faster or stronger than Pi.

The planned [50-task Pi Go comparison](../evals/historical/pi-multiswe-go-evaluation-plan.md) was not run. The early Go Sigma inventory is explicitly heterogeneous and warns against pooling issue-only, assisted, evaluator-diagnostic, and incomplete attempts. Therefore, today's defensible answer is that Sigma is a **coding-task-oriented runtime with real but uneven issue-resolution evidence**. Whether it is as capable as Pi at solving code issues remains unmeasured under matched conditions.

For a fair Pi comparison, use the same held-out issue IDs, base commits, model and reasoning mode, issue-only prompt, Skills/instructions, tool access, network policy, turn and token budgets, and official evaluator. Keep every attempt in the denominator, report infrastructure-invalid runs separately, and compare both resolved rate and cost. Until that experiment exists, describe architectural differences and individual trace findings rather than ranking the agents.
