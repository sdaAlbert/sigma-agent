<p align="center">
  <img src="docs/assets/sigma-agent-mark.svg" alt="Sigma Agent logo" width="112">
</p>

<h1 align="center">Sigma Agent</h1>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Sigma is a Go coding agent for real repository work. It connects language models to code tools and project commands, then guides a change from investigation through implementation and verification.

## Get started

Requirements: Docker Desktop and an API key for a supported model provider. Git is optional and enables Git diff review. Building from this checkout requires Go 1.26+.

From the `go-runtime` directory, build and install `sigma`:

```powershell
$binDir = Join-Path (go env GOPATH) 'bin'
go build -o (Join-Path $binDir 'sigma.exe') ./cmd/sigma-agent
sigma --version
```

Or build a Windows executable:

```powershell
go build -o sigma.exe ./cmd/sigma-agent
```

Then start Sigma in the repository you want to work on:

```powershell
cd C:\work\target-repository
sigma
```

The TUI starts a session automatically. Use `/login` to save a provider key, `/model` to select a model, then enter a task. See the [user guide](docs/USAGE.md) for interaction details and [configuration](docs/CONFIGURATION.md) for provider and runtime settings.

## Results and highlights

**Evaluation result:** The historical high on the public [SWE-bench Verified Mini leaderboard](https://hal.cs.princeton.edu/swebench_verified_mini) was **54%**, shared by SWE-Agent runs with o4-mini Low, Claude 3.7 Sonnet High, and Claude Opus 4.1 High (as of 2026-11-05). On Sigma’s fixed 50-task set, the mixed-model study stood at **34/50 (68%)** before DeepSeek thinking; its current cumulative result is **40/50 (80%)** after DeepSeek Flash thinking was introduced. Sigma runs used both MiniMax M3/M3.1 Flash and DeepSeek Flash. Historical runs also changed Skills and guidance, so this is not a controlled thinking-only comparison. See the [results and per-task evidence](evals/swebench-verified-mini/evaluation-results.md).

**Public reference:** The official 500-task [SWE-bench Verified leaderboard](https://www.swebench.com/) lists 79.2% as its highest full-agent entry (Claude 4.5 Opus with Sonar Foundation Agent / live-SWE-agent submissions); its same-harness Bash-only leader is 76.8% (Claude 4.5 Opus high + mini-SWE-agent 2.0.0). DeepSeek's [V4 model card](https://huggingface.co/deepseek-ai/DeepSeek-V4-Flash) reports six Verified scores across Flash/Pro and Non-Think/High/Max; their simple mean is **77.5%** (calculated here, not a vendor aggregate), with V4-Pro Max at 80.6% and V4-Flash Think-Max at 79.0%. DeepSeek says these code-agent runs used [DeepSeek Harness minimal mode](https://api-docs.deepseek.com/updates/), max effort, `top_p=0.95`, and `temperature=1.0`. These 500-task results use different models, harnesses, and evaluation conditions, so they are context, not a direct comparison with our Mini 50.

Our analysis of DeepSeek thinking settings and why the scores are not directly comparable is in the [experiment method](evals/swebench-verified-mini/experiment-method.md#deepseek-thinking-mode-and-result-attribution).

Runs used Skills and Memory according to their recorded configurations; they were not seeded with gold patches, hidden tests, or official answers. Skills provide reusable workflows, and their reference copies are available [here](evals/swebench-verified-mini/reference/README.md). Memory can accumulate knowledge across runs, so its state varied by task; see the run records for each condition.

- **Issue-focused coding workflow:** Repository exploration, edits, project-native commands, tests, and final diff review are handled in a persistent Agent session.
- **Repository-aware execution:** Sigma can inspect project setup and prepare a reusable Docker toolchain; repository commands run in a resource-bounded container.
- **Reliable long-running work:** Sessions preserve transcripts and support resume; runtime policies track changes and required verification.
- **Extensible agent:** Project Skills, repository memory, MCP tools, and JavaScript workflow extensions let teams add context and capabilities. Memory retrieval is experimental; its effect on issue-resolution success has not been isolated.
- **Go-native runtime:** A streaming model loop, tool lifecycle, permission policies, context management, and workspace tools are implemented in Go.

## How Sigma relates to Pi

Sigma takes architectural inspiration from [Pi](https://github.com/earendil-works/pi), particularly its agent loop, tool lifecycle, and persistent sessions. The projects have different goals and maturity.

| Area | Pi | Sigma |
| --- | --- | --- |
| Main goal | Minimal, customizable coding-agent harness | Go-native runtime for repository tasks and backend integrations |
| Runtime | TypeScript / Node.js | Go executable; Docker runs repository commands |
| Interaction | Interactive CLI, print/JSON, RPC, and TypeScript SDK | Integrated terminal TUI and one-shot CLI/JSONL |
| Customization | Extensions, Skills, prompt templates, themes, and shareable packages; see the [Coding Agent README](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md) for customization and interaction modes | Package Skills, MCP tools, and a JavaScript workflow host for slash commands, Agent tools, notifications, confirmations, and read-only lifecycle events |
| Execution isolation | Default tools run with the Pi process permissions; its docs describe optional container/VM sandbox setups | File tools edit the selected checkout directly; repository commands/tests run in Docker with that checkout mounted writable |
| Environment preparation | Integrate the local toolchain or configure a container/sandbox separately | Agent can inspect repository setup files and prepare a cached Docker image with a selected toolchain and system packages |
| Coding focus and evidence | A broader, mature, customizable coding-agent harness with multiple interaction modes and an extension ecosystem | Sigma focuses on repository issue repair through repository-aware Docker setup and runtime-gated test/diff verification. The Mini study's scope and evidence limits are described below. |

### Runtime outline

```text
User task / steering
        ↓
Persistent session transcript
        ↓
Streaming model adapter → assistant text and tool-call events
        ↓
Tool validation and permission policy
        ↓
Bounded execution: parallel independent reads; ordered writes and commands
        ↓
Tool results appended to the session → next model turn
```

The model boundary supports Anthropic Messages and OpenAI Chat Completions-compatible APIs. Built-in providers are MiniMax, Anthropic, OpenAI, DeepSeek, and OpenRouter. Runtime configuration covers turn/tool budgets, context compaction, model and tool timeouts, retries, and tool permissions; see [configuration](docs/CONFIGURATION.md).

## Docker execution sandbox and environment preparation

In Sigma's repository-task workflow, `run_command` executes project commands in Docker rather than directly on the host. The command container mounts the selected repository as `/workspace` with write access, drops Linux capabilities, enables `no-new-privileges`, and applies limits of 2 GiB RAM, 2 CPUs, 128 processes, and a five-minute command timeout. External network access is blocked by default; `/network on` enables it for the session. Package caches persist between commands.

Before running project commands, the Agent can inspect the repository's Dockerfile, devcontainer, CI workflow, README, and dependency manifests. It can then call `prepare_environment` with a base image and setup command. Sigma builds and caches a derived image for later commands and session resumes. The Docker build context contains only the generated Dockerfile, not repository source. Setup commands that download packages require network access to be enabled deliberately.

This is a **command-execution isolation boundary**, not a complete sandbox for the whole Agent. The model client and repository file tools run in the Sigma process. The selected repository is mounted read/write into Docker, so both file tools and commands can change it immediately. The container mount does not expose other host directories; this is not a hardened boundary for hostile multi-tenant workloads.

For image reuse, network behavior, limits, and cleanup, see the [Docker guide](docs/DOCKER.md) and [configuration reference](docs/CONFIGURATION.md).

## Project structure and design

The Go executable is composed in `cmd/sigma-agent`; reusable implementation
modules live in `internal/`. The CLI is the outer wiring layer and may depend
on internal modules. Internal modules must not import the CLI.

| Path | Responsibility |
| --- | --- |
| `cmd/sigma-agent` | The `sigma` executable: flags, process lifecycle, runtime composition, setup flow, TUI, package commands, and workflow-extension host. `main.go` is the composition root; `tui.go` owns the Bubble Tea UI. |
| `internal/agent` | Generic conversation runtime: sessions, transcripts, provider streaming adapters, tool dispatch and policy, context management, recovery, and session storage. |
| `internal/workspace` | Sigma's coding workspace: file/search/edit tools, Docker commands and prepared environments, project instructions, Skills, memory, and workspace diff. It depends on `agent` interfaces; `agent` does not depend on it. |
| `internal/model` | Provider/model catalog, selected-model configuration, credential lookup, and model construction. |
| `internal/config` | Runtime configuration schema, defaults, loading, and validation. |
| `internal/credentials` | Local credential storage used by model and GitHub integrations. |
| `internal/fsx` | Small OS-specific filesystem primitives, such as atomic replacement. |
| `internal/mcp` | Shared MCP stdio protocol client and server-process lifecycle. |
| `internal/mcpconfig` | MCP configuration loading and conversion of configured servers into runtime tools. |
| `internal/pkgmgr` | Package-source parsing, package installation, and local package registry. |
| `internal/server` | Backend task-service implementation. |
| `internal/workflow` | Workflow extension interfaces and dispatch for commands, tools, confirmations, notifications, and lifecycle events. |

The two similarly named packages have different roles: `agent` runs the model
conversation and tool lifecycle; `workspace` provides Sigma's project-facing
coding tools. `model` selects and constructs a provider adapter, while the
provider protocol implementations live with the runtime in `agent`. For the
full component design and dependency boundaries, see [architecture](docs/ARCHITECTURE.md), [configuration](docs/CONFIGURATION.md), and [Agent design notes](docs/AGENT_DESIGN_NOTES.md).

## Evaluation

The [`evals/`](evals/README.md) directory contains the 50-task results table and failure analysis, the experiment method, benchmark protocol, issue/recovery log, and detailed records for individual tasks. To understand the findings, start with [results and failure analysis](evals/swebench-verified-mini/evaluation-results.md), then read the [experiment method](evals/swebench-verified-mini/experiment-method.md); use the [benchmark protocol](evals/swebench-verified-mini/benchmark-protocol.md) and [issue log](evals/swebench-verified-mini/evaluation-issues.md) when reproducing runs or investigating operational problems.

For the project itself, [architecture](docs/ARCHITECTURE.md) explains component boundaries and runtime flow, [Agent design notes](docs/AGENT_DESIGN_NOTES.md) and [coding-agent iteration history](docs/SIGMA_CODING_AGENT_ITERATION_HISTORY.md) describe coding behavior and its evolution, and [configuration](docs/CONFIGURATION.md), the [user guide](docs/USAGE.md), and the [Docker guide](docs/DOCKER.md) cover setup and operation.

## Extensibility

Sigma supports project Skills, MCP tools, and JavaScript workflow extensions. These let teams add task-specific guidance, external tools, and workflow behavior without expanding the core coding loop. See [Packages and MCP servers](docs/PACKAGES.md) for setup and supported formats.

## Skills and repository memory

Skills provide on-demand project workflows; repository memory retrieves reviewed facts that may help across tasks in the same repository. Memory uses lexical search and can add vector retrieval when configured; it does not index source code. The Mini study has not isolated a memory-on/off improvement in issue-resolution results. See [Skills and packages](docs/PACKAGES.md), [memory configuration](docs/CONFIGURATION.md#repository-long-term-memory), [architecture](docs/ARCHITECTURE.md), and the [Mini memory study](evals/planned/skills-memory/repository-memory-status.md).

## Development

The Go runtime is in this repository; the TypeScript Pi prototype is archived and is not a runtime dependency. To build and run the Go checks from `go-runtime`:

```powershell
go build ./...
go test ./...
```

Docker-backed task and benchmark checks have additional image, network, and fixture requirements; see the evaluation protocol before running them. Sigma is licensed under the [MIT License](LICENSE).
