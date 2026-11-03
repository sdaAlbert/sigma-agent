<p align="center">
  <img src="docs/assets/sigma-agent-mark.svg" alt="Sigma Agent logo" width="112">
</p>

<h1 align="center">Sigma Agent</h1>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Sigma is a Go coding agent for working on issues in existing repositories. It connects a model to repository tools, runs project commands in Docker, and guides changes through inspection, implementation, testing, and diff review. Each task runs in a detached Git worktree, leaving the original checkout untouched.

## Why Sigma

- **Built to improve issue-fixing results:** Sigma is being developed to help coding agents solve real repository issues more reliably, not just to reproduce Pi's harness in Go. Its coding-specific focus is to ground changes in the repository's actual setup and verify each patch before finishing.
- **Repository-aware execution and verification:** The Agent can inspect project manifests and CI instructions, prepare a reusable Docker toolchain with `prepare_environment`, and run repository commands in a resource-bounded container. After edits, the runtime gates completion on the relevant tests and a Git diff review.
- **A complete issue workflow:** Each task uses a detached Git worktree and a persistent session. The integrated TUI and CLI bring together code browsing, edits, repository commands, tests, diff review, resume, and optional publishing.
- **Backend execution interfaces:** Use the integrated terminal TUI, one-shot CLI/JSONL integration, or a local HTTP task API with a bounded worker pool, persisted task state, SSE events, steering, cancellation, and resume.
- **Repository-memory RAG (an experimental focus):** Sigma can retrieve reviewed facts across issues in the same repository, such as test setup constraints, architecture decisions, compatibility rules, and recurring pitfalls. It retrieves memory facts—not source-code chunks—using BM25 plus optional embeddings/Qdrant and rank fusion. Whether this improves issue-resolution results remains to be tested.
- **Deliberate access controls:** GitHub MCP tools are read-only by default. Pushing a feature branch and opening a pull request require explicit setup and opt-in.

## How Sigma relates to Pi

Sigma takes architectural inspiration from [Pi](https://github.com/earendil-works/pi), particularly its agent loop, tool lifecycle, and persistent sessions. The projects have different goals and maturity.

| Area | Pi | Sigma |
| --- | --- | --- |
| Main goal | Minimal, customizable coding-agent harness | Go-native runtime for repository tasks and backend integrations |
| Runtime | TypeScript / Node.js | Go executable; Docker runs repository commands |
| Interaction | Interactive CLI, print/JSON, RPC, and TypeScript SDK | Integrated terminal TUI, one-shot CLI/JSONL, HTTP/SSE task API |
| Customization | Extensions, Skills, prompt templates, themes, and shareable packages; see the [Coding Agent README](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/README.md) for customization and interaction modes | Package Skills, MCP tools, and a JavaScript workflow host for slash commands, Agent tools, notifications, confirmations, and read-only lifecycle events |
| Execution isolation | Default tools run with the Pi process permissions; its docs describe optional container/VM sandbox setups | Repository command/test tools use built-in Docker execution; each task also gets a detached Git worktree |
| Environment preparation | Integrate the local toolchain or configure a container/sandbox separately | Agent can inspect repository setup files and prepare a cached Docker image with a selected toolchain and system packages |
| Coding focus and evidence | A broader, mature, customizable coding-agent harness with multiple interaction modes and an extension ecosystem | Sigma is purpose-built around repository issue repair: repository-aware Docker setup, isolated task worktrees, and runtime-gated test/diff verification. These are workflow and execution strengths; no matched evaluation currently shows a higher issue-resolution rate. Go benefits deployment and backend integration, not model reasoning by itself. |

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

In Sigma's repository-task workflow, `run_command` executes in Docker rather than invoking project commands directly on the host. The command container mounts the isolated task worktree as its writable workspace, drops Linux capabilities, enables `no-new-privileges`, and applies limits of 2 GiB RAM, 2 CPUs, 128 processes, and a five-minute command timeout. Command containers block external network access by default; `/network on` enables it for dependency downloads during the session. The task worktree and its caches persist between commands.

Before running a project command, the Agent can inspect the repository's Dockerfile, devcontainer, CI workflow, README, and dependency manifests. It can then call `prepare_environment` with a base image and setup command. Sigma builds and caches a derived image for later commands and session resumes. The Docker build context contains only the generated Dockerfile—not the repository source. If package downloads are required during setup, network access must be enabled deliberately.

This is a **command-execution isolation boundary**, not a claim of a complete security sandbox for the whole Agent. Sigma's model client and repository file tools run in the Sigma process, and the task worktree is mounted read/write into Docker; a command can therefore change files in that task worktree. It cannot thereby change files outside the mounted worktree through that mount. Do not treat this as a hardened boundary for hostile multi-tenant workloads.

This is a useful integration difference from Pi's default: Pi's security guide says its built-in tools run with the Pi process's user permissions and that Pi does not include a built-in sandbox. Pi separately documents containerization and external isolation options such as Docker, Docker Sandboxes, OpenShell, and Gondolin. Sigma's distinction is that Docker command execution, bounded resources/network behavior, and reusable environment preparation are built into its repository-task workflow; it does not claim stronger isolation than every optional Pi setup. See Pi's [security guide](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/security.md) and [containerization guide](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/containerization.md), plus Sigma's [Docker configuration details](docs/CONFIGURATION.md).

## Get started

Requirements: Go 1.26+, Git, Docker Desktop, and an API key for a supported model provider. Sigma uses provider APIs; it does not include or download a model.

From the `go-runtime` directory, run from source:

```powershell
go run ./cmd/sigma-agent
```

Or build a Windows executable:

```powershell
go build -o sigma-agent.exe ./cmd/sigma-agent
.\sigma-agent.exe
```

In the TUI setup screen, use `/login minimax` to save a model key, `/model list` to see supported providers and key status, `/model <provider>/<model-id>` to select a model, and `/repo <path>` to choose a clean local checkout. Use `/model save` to make the selection the default for new sessions, then type a task or run `/start`. Provider keys can also be supplied through environment variables for scripted use. The complete [user guide](docs/USAGE.md) covers setup and interactive commands; [configuration](docs/CONFIGURATION.md) documents providers, credentials, runtime limits, and Docker settings.

Docker command containers block outbound network access by default. Use `/network on` when a session needs to download dependencies; see the configuration guide for details.

## Use Sigma

### Interactive coding session

```powershell
go run ./cmd/sigma-agent --repo C:\work\target-repository --interactive
```

For the integrated setup and chat UI, run `go run ./cmd/sigma-agent --tui --repo C:\work\target-repository`. Setup, credential entry, multiline task input, streamed output, and session commands all stay in the TUI. Press `Ctrl+Enter` to submit; the main-screen renderer keeps terminal scrollback.

Type a task in the multiline TUI input and press `Ctrl+Enter`. While Sigma is working, submit text or `/steer <text>` to guide its next safe turn; `/followup <text>` queues a later request. `/diff` summarizes the patch, `/tree` shows session history, `/branch <entry-id>` continues from an earlier complete point, `/abort` cancels the active turn, and `/exit` saves the session. The session ID is shown in the TUI; use `/resume <id>` at the next setup screen to continue. See the [user guide](docs/USAGE.md) for the full command reference.

TUI shortcuts: `Ctrl+L` opens the model selector, `Ctrl+R` opens the saved-session selector during setup, `Tab` completes slash commands and paths (`@path` in a prompt or a path after `/repo`), `Up`/`Down` browse prompt history at the input boundaries, `PageUp`/`PageDown` or the mouse wheel scroll the transcript, and `Ctrl+O` expands or folds tool output.

For example:

```text
Fix issue #123. Inspect the relevant code and project instructions, implement the fix, run appropriate tests, and review the complete diff. Do not commit or push.
```

Each task runs in a detached worktree. The original checkout is left untouched. For GitHub issue context, configure `/login github`; the GitHub MCP integration is read-only unless write actions are explicitly enabled.

### One-shot CLI and process integration

Run one task without entering the interactive REPL:

```powershell
go run ./cmd/sigma-agent --repo C:\work\target-repository --task "Fix issue #123 and run the relevant tests"
```

Add `--json` to write versioned JSONL lifecycle events and a final result to stdout:

```powershell
go run ./cmd/sigma-agent --repo C:\work\target-repository --task "Fix issue #123" --json
```

The final JSONL record contains the session ID and artifact paths. A later process can continue that session with `--resume <session-id>`. This mode is one-shot; use the HTTP API below for bidirectional task control while the Agent is running.

### Local HTTP task API

Start the task service on loopback (default address `127.0.0.1:8080`):

```powershell
go run ./cmd/sigma-agent --serve --max-concurrent 2
```

Submit a task from PowerShell:

```powershell
$body = @{
  repo = 'C:\work\target-repository'
  task = 'Fix issue #123 and run the relevant tests'
} | ConvertTo-Json

$task = Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:8080/tasks `
  -ContentType 'application/json' `
  -Body $body
$task

# Stream events for the returned task ID (Ctrl+C to stop streaming)
curl.exe -N "http://127.0.0.1:8080/tasks/$($task.id)/events"

# Queue an instruction while the task is running
$message = @{ message = 'Check the caller path before changing the API'; mode = 'steer' } | ConvertTo-Json
Invoke-RestMethod -Method Post `
  -Uri "http://127.0.0.1:8080/tasks/$($task.id)/messages" `
  -ContentType 'application/json' `
  -Body $message
```

The service provides these endpoints:

| Method and path | Purpose |
| --- | --- |
| `POST /tasks` | Submit a repository path and task prompt; returns a task ID |
| `GET /tasks/{id}` | Read task status and timestamps |
| `GET /tasks/{id}/events` | Stream task and Agent lifecycle events over SSE; reconnect with `Last-Event-ID` |
| `POST /tasks/{id}/messages` | Add a `steer` message or queue a `follow_up` |
| `POST /tasks/{id}/cancel` | Cancel a queued or running task |
| `POST /tasks/{id}/resume` | Resume an interrupted task from its saved session |

Task state, messages, and event logs are persisted locally so queued tasks can be restored and interrupted sessions can be resumed after a process restart. `--max-concurrent` sets the worker count (1–32); tasks beyond that capacity wait in the queue. See [architecture](docs/ARCHITECTURE.md) for request/response details and SSE replay behavior.

**Service boundary:** The HTTP service is a single-process local prototype and has no authentication. It binds to loopback by default. Do not expose it to a network as-is. Publishing and GitHub PR creation are not available through `--serve`.

## Packages and MCP servers

Sigma Agent can install packages and connect to MCP servers. Packages
contribute Agent Skills, and an MCP server contributes tools that are exposed
to the model as `mcp__<server>__<tool>`.

```powershell
# Install a package into the user scope (~/.sigma-agent/packages/)
go run ./cmd/sigma-agent --install npm:pi-web-access

# Install into one repository's project scope
go run ./cmd/sigma-agent --repo . --local --install npm:pi-web-access

# List the installed packages
go run ./cmd/sigma-agent --list-packages

# Connect to the configured MCP servers and list their tools
go run ./cmd/sigma-agent --list-mcp
```

MCP servers are configured in `mcp.json`, using the `mcpServers` shape shared
by other MCP clients, so an existing configuration can be reused as is. A
server can start from an installed package:

```json
{
  "mcpServers": {
    "web": {
      "command": "node",
      "args": ["./dist/mcp-cli.js"],
      "package": "pi-web-access"
    }
  }
}
```

Sigma Agent reads `~/.sigma-agent/mcp.json` for the user scope and
`<repo>/.sigma-agent/mcp.json` for the project scope; `<repo>/.pi/mcp.json` is
read as well. Later configuration files override earlier entries with the
same server name. Packages are resolved project-first, then user-wide, so a
project can pin its own copy while still using packages installed for the user.
Package sources are `npm:<name>`, `git:<repo>`, or a local path. Project
installation and removal require `--repo <path> --local`.

Use `--list-mcp` to start each enabled stdio server, inspect its tools, and
shut it down. A server with invalid configuration or a failed startup is
reported as a warning; other configured servers remain available. MCP tools
run through Sigma's configured tool permission policy. Tool names use the
`mcp__<server>__<tool>` namespace; names containing characters that need
sanitizing receive a short stable suffix to avoid collisions.

An MCP server is a program that runs with the permissions of the current user,
and installing a package runs the package manager's install scripts. Only
install packages you trust. Package-provided tools still pass through the
configured tool policy.

See [packages and MCP servers](docs/PACKAGES.md) for the package format, the
supported sources, workflow extension commands and events, and current limits.
HTTP MCP servers are not supported.

## Repository-memory RAG and Skills

The memory experiment targets repeated work in one repository. Useful facts can include a non-obvious test command, an architectural contract, or a compatibility issue learned from a previous task. Each fact is kept short and carries a repository scope, category, source, and review date. Human-reviewed `.sigma-agent/MEMORY.md` is the canonical record; Qdrant is a derived index.

The retrieval path is:

```text
Reviewed repo facts in MEMORY.md
       ├── lexical ranking (BM25) ───────────┐
       └── embeddings → Qdrant vector search ├── RRF → search_memory → Agent context
                                             ┘
```

Qdrant applies an exact repository filter. The Agent receives matching facts with their provenance and must check them against the current code and tests. If vector search is unavailable, retrieval falls back to BM25. This is memory RAG: Sigma does not chunk, embed, or index the repository source code, and a retrieved fact is not an answer or a patch to copy.

Skills and long-term memory solve different problems:

- A repository-root `AGENTS.md` is loaded as project guidance at the start of each session, up to 16 KiB. Use it for stable conventions and pointers. Sigma's automatic discovery reads only the repository-root file, not ancestor or nested `AGENTS.md` files. `project_instructions_file` can add a shared profile outside a task checkout.
- Sigma discovers project `.agents/skills/` and `.pi/skills/`, plus user `~/.agents/skills/` and `~/.pi/agent/skills/`. It initially exposes Skill names and descriptions, then loads a matching body with `read_skill`; use `read_skill_resource` for a referenced Markdown guide or text template in that Skill directory. In an interactive session, `/skill:<name> <task>` explicitly invokes a Skill, including Skills marked `disable-model-invocation: true`.
- Common Agent Skills fields (`name`, `description`, `license`, `compatibility`, `disable-model-invocation`, and `user-invocable`) are supported. Other frontmatter is tolerated, but Pi-specific `allowed-tools` permission behavior is not implemented; Sigma's configured tool policy remains authoritative. Loading a Skill or reference never automatically runs a bundled script. Only small text references/templates are readable; scripts and binary assets are not exposed through `read_skill_resource`.
- `.sigma-agent/MEMORY.md` stores short, human-reviewed facts that may help across tasks in the same repository, with category, source, and date; BM25/vector search retrieves them on demand instead of injecting the whole file. `memory_file` plus a stable `memory_repository_id` lets independent task checkouts share one external memory source, copied only into isolated session worktrees. Session snapshots store recovery state; they are not long-term memory.
- Current source code and tests remain authoritative. Memory is a reference to verify, not a substitute for reading the code.
- BM25 works locally. When embeddings and Qdrant are configured, Sigma adds repository-filtered vector candidates and combines rankings with reciprocal-rank fusion. Source files are not embedded or indexed by this memory feature.

### What we plan to measure

The project evaluates memory at two separate levels:

| Experiment | Question | Evidence |
| --- | --- | --- |
| Labeled retrieval queries | Does hybrid BM25 + vector retrieval return the reviewed facts for a query, including cases with no relevant fact? | Recall@K, MRR, per-query rankings, empty-result behavior, and latency |
| Held-out issue pairs | Does retrieved memory help solve a new issue in the same repository? | Fresh matched runs with memory off/on, same model, prompt, budget, base commit, Docker image, and evaluator; compare resolution, tests, token/tool use, and whether facts were applied correctly |

The first experiment measures retrieval, not coding ability. The second is the evidence needed to argue that repository-memory RAG helped issue resolution. The current README reports no retrieval score or memory-on issue success-rate gain. Read the [memory retrieval protocol](evals/memory-retrieval-evaluation.md) and [Skills/memory ablation plan](evals/skills-memory-evaluation.md) for dataset boundaries and run controls. See [architecture](docs/ARCHITECTURE.md) for the implementation, [configuration](docs/CONFIGURATION.md) for setup, and the [user guide](docs/USAGE.md) for Skill directories and invocation.

## Evaluation and project status

Sigma has a documented issue-benchmark workflow and historical attempts. A benchmark result is only counted when the task input, Agent run, official evaluator, baseline behavior, and result record can be inspected together. Guided attempts, evaluator fixture problems, and resolved issues are reported separately; results from a small task subset are not presented as a general success rate or a Pi comparison.

Start with the [benchmark protocol](evals/benchmark-protocol.md) and [issue inventory](evals/issue-benchmark-inventory.md). The [SWE-bench Verified Mini plan](evals/swebench-verified-mini-50-evaluation-plan.md) describes the proposed 50-task evaluation; the [memory retrieval plan](evals/memory-retrieval-evaluation.md) covers retrieval metrics separately from issue-resolution outcomes. These documents describe protocols and plans, not a completed 50-task score. The current README does not claim that Sigma matches Pi or that repository memory improves coding success. Project-specific implementation notes are in [architecture](docs/ARCHITECTURE.md) and [configuration](docs/CONFIGURATION.md).

## Development

The Go runtime is in this repository; the TypeScript Pi prototype is archived and is not a runtime dependency. To build and run the Go checks from `go-runtime`:

```powershell
go build ./...
go test ./...
```

Docker-backed task and benchmark checks have additional image, network, and fixture requirements; see the evaluation protocol before running them. Sigma is licensed under the [MIT License](LICENSE).
