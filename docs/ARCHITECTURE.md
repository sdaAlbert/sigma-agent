# Go Agent Runtime

This runtime uses Pi Agent as an architecture reference, not as a dependency. It keeps the agent loop, transcript, tool lifecycle, policy hooks, cancellation, and event model in Go. Provider adapters normalize Anthropic Messages and OpenAI Chat Completions-compatible APIs into Sigma's model interface; the interactive CLI exposes `/login` for model and GitHub credentials and `/model` for model selection. For the rationale and evidence limits behind Skills, memory, retrieval, cache-stable prompts, and runtime workflow safeguards, see [Agent context and reliability design notes](AGENT_DESIGN_NOTES.md).

For a folder-by-folder map of the tracked source, support files, evaluation data,
examples, and scripts, see [Repository layout](REPOSITORY_LAYOUT.md).

## Runtime flow

```text
Prompt / steering / follow-up
            |
            v
     Stateful session transcript
            |
            v
   Streaming model adapter -----> normalized model events
            |
            +---- assistant text -------------------> event journal / CLI
            |
            +---- tool calls
                       |
                       v
                 Tool policy + schema validation
                       |
                       v
              bounded tool dispatch
                /             \
       native Go tools     GitHub MCP / command tools
                \             /
                 bounded execution
     (parallel safe reads and path-locked writes;
       serialized mixed batches and shell/tests)
                       |
                       v
                  tool_result messages
                       |
                       +----------------------> next model turn
```

## Tool catalog and on-demand exposure

The runtime keeps one registry of handlers and schemas. Ordinary Sigma tools
remain visible to the model; tools marked `OnDemand` stay registered and
permission checked but their schemas are omitted from model requests until
activated. One `search` tool routes by scope: `tools` searches the catalog and
activates matching schemas for the next turn; `skills`, `memory`, and
`session_history` search their respective indexes. The first user prompt
preselects likely relevant extensions before the initial model request, so
common tasks usually avoid a discovery turn. Activated tool names are stored
in the session snapshot and restored on resume.

Configured MCP servers still start and return their tool list during session
startup. This mechanism delays prompt-schema exposure, not server startup or
MCP discovery. Command plugins, workflow extension tools, and optional web
search use on-demand exposure. Skill discovery uses `search(scope=skills)`
followed by `read_skill`; catalog discovery uses `search(scope=tools)`. If the
first-task preselection misses a needed extension, the model spends one search
call and continues on the next turn; the default turn limit is unchanged.

## Terminal interaction

The CLI supports one-shot tasks and a persistent terminal conversation. New sessions work in the selected checkout by default. The `--isolated` one-shot option creates a detached Git worktree at the current commit and stores it with the session for later resume. Its built-in Git tool performs local status, diff, history, branch listing, init, staging, and commit operations in the workspace; remote operations remain outside the tool. Sessions keep one transcript across prompts and checkpoint each prompt for later resume. Sessions created by older versions can still resume from their saved worktree.

```powershell
# Start an interactive session in the current folder
cd C:\path\to\workspace
go run ./cmd/sigma-agent

# Start with an initial instruction
go run ./cmd/sigma-agent --task "Find and fix the bug in ..."

# Resume a saved session
go run ./cmd/sigma-agent --resume <session-id>
```

Interactive invocations start the Bubble Tea TUI in the process's current directory. Setup, credential entry, model/session selection, multiline prompts, streamed responses, tool cards, and session commands are handled inside the same UI. It switches to the alternate screen for a full-screen interface, then restores the terminal on exit. Use `--task` for a one-shot CLI task in that same directory; `--json` and `--serve` remain separate modes. See [the user guide](USAGE.md) for setup and command details.

In Docker mode, `run_command` supports foreground execution plus `action=start`, `action=list`, `action=status`, and `action=stop` for detached commands. Job metadata is stored under the session directory and survives `--resume`; one task can run at a time and expires after 30 minutes. Read-only repository tools remain available while it runs. File edits, shell commands, and tests wait until the job completes or is stopped, because the container shares the active repository workspace. Host mode exposes foreground shell execution only.

## Tool authorization and execution isolation

`run_command` defaults to the host shell (`PowerShell` on Windows, `bash` or
`sh` on macOS/Linux). Set `command_execution_mode` to `docker` to use the
container adapter instead. Host commands inherit the current user's operating
system permissions, environment, filesystem access, and network. Docker mode
mounts the selected workspace read/write at `/workspace`, applies container
resource limits, and disables external networking by default. File tools run
in the Sigma process in both modes. Docker mode exposes `prepare_environment`;
host mode does not. Background `action=start` remains Docker-only.

Before a registered tool handler runs, Sigma classifies the request as
`read_only`, `workspace_write`, `network`, `external_service`, or
`commit_publish`, then resolves the configured default, risk-class policy, and
per-tool override in that order. Policies support `allow`, `deny`, and `ask`.
An interactive `ask` pauses at a TUI approval card; denial, Esc, timeout, or a
non-interactive run prevents the handler from running. The JSONL trace and a
session-scoped audit file record the request parameters, target, reason,
decision, tool-call ID, and local account label. Sigma flushes these records
before the handler starts, so prior decisions remain available after session
recovery. The policy configuration and limits are summarized in
[runtime configuration](CONFIGURATION.md).

This is an auditable tool gate, not a unified execution sandbox. Host mode is
the default: shell commands run with the current user's normal host permissions
and environment. In optional Docker mode, Sigma's repository file tools still
access the selected workspace from the host process, while commands run in a
container with that repository mounted writable at `/workspace`. Docker command
containers use a read-only image root filesystem, bounded tmpfs scratch
locations, resource limits, dropped Linux capabilities, and
`no-new-privileges`; the network is disabled unless the session setting enables
it. The Docker container is removed on graceful interactive exit. The mounted
workspace is still fully visible to commands, including any credentials stored
inside it. This workflow does not create a per-task worktree, filter workspace
secrets, or route file operations through the same executor as commands.
Network approval also does not change the session's network setting; networking
must already be enabled for that container. Command risk classification is
conservative but heuristic, so it cannot prove what arbitrary shell code will
do.

The next execution-isolation increment needs a real executor interface used by
file, Git, command, and test operations; task-scoped workspace materialization;
and explicit result integration/cleanup. Until those pieces exist, Docker
limits should be described as command-container restrictions, not as a
complete Agent sandbox.

After a workspace write, runtime tracks whether tests and `git` with `action=diff` have run against that revision. During this review, it exposes only outstanding checks plus targeted file reads and edits; each new write reopens both checks. The finalization exploration budget limits broad searches and inspection commands, while targeted file reads remain available so a test or diff finding can be corrected and reverified.

## Tool surface boundaries

The default host-mode model surface is organized as follows:

| Model-facing entry | Purpose | Availability |
| --- | --- | --- |
| `run_command` | Host shell commands; in Docker mode also manages the command image and foreground/background actions. Use it for file listing and source search. | Always; Schema depends on host/Docker mode |
| `read_file` | Read a workspace file by line range, or read a long tool result by `artifact_id` and byte range. | Always |
| `create_file`, `edit_file` | Create a new file, or make a read-validated exact replacement in an existing file. | Always |
| `git` | Local repository inspection and explicitly requested local Git changes. | Always |
| `search` | Route to `session_history`, and conditionally to `skills`, `memory`, or `tools`. | Always; scopes depend on enabled features and registered extensions |
| `record_change_contract`, `compact_context`, `delegate_investigation` | Planning evidence, context handoff, and bounded child investigation/implementation. | Always in the coding-agent CLI |
| `read_skill` | Load a Skill by name/path. Skill body and reference reads share this entry. | When Skills are enabled |
| `create_skill` | Create reusable project guidance with Skill metadata and path validation. | When Skills are enabled; on-demand through `search(scope=tools)` |
| `prepare_environment` | Prepare a reusable Docker image. | Docker command mode only |
| `web_search` | Current external information. | When configured; on-demand through `search(scope=tools)` |
| MCP, command-plugin, workflow-extension tools | Package or external-service capabilities. | When configured; schemas are on-demand through `search(scope=tools)` |

`search_memory`, `search_skills`, `search_session_history`, and `tool_search`
are internal routes behind `search`, not separate model-visible entries.
Likewise, `read_tool_output` remains an internal compatibility handler;
long-result reads use `read_file` with `artifact_id`. `workspace_search` is no
longer registered in the parent model session; the bounded implementation is
retained only for isolated child agents.

The top-level model tool list leaves out a dedicated file-listing/code-search tool. With the default host command mode, use `run_command` with `rg --files` to list paths and `rg -n` to search code; Sigma detects whether `rg` is on `PATH` and supplies a PowerShell or POSIX fallback in the tool description. Host commands still inherit the user's broad filesystem permissions, so keep discovery paths relative to the selected workspace. The read-only child investigation agent retains a bounded internal workspace search tool; its interface is not exposed in the parent session.

`git` owns local Git inspection and mutation. Use `action=diff` to inspect changes against the task's starting commit, including untracked non-ignored files. This replaces the former standalone `git_diff` entry while preserving the runtime's verification signal. Git operations remain separate from `run_command` so local Git permissions, path validation, and no-remote policy are enforced consistently.

For process integrations that only need to submit a task and consume progress, `--json` writes one JSON object per stdout line, each with `protocol_version: 1`. Agent lifecycle events are wrapped as `{"type":"agent_event","event":...}`; the final record is `run_completed` or `run_failed` and includes the session ID and artifact paths. The existing session store persists state exactly as it does for CLI runs, so a client can later resume with `--resume <session-id>`. This mode is intentionally one-shot; bidirectional control while a task is running remains in the HTTP task API.

## HTTP task lifecycle

```text
POST /tasks -> persisted queued task -> bounded worker pool -> Sigma Agent process
                         |                                      |
GET /tasks/{id} <--------+                               JSONL agent events
GET /tasks/{id}/events <-------------------------------- SSE stream
POST /tasks/{id}/messages -> durable steer / follow-up inbox
POST /cancel -> process context cancel -> cancelled session snapshot
POST /resume -> same task/session id -> Continue persisted transcript
```

The service binds to loopback by default and stores task records and event logs under `var/server/`. It is a single-process prototype without authentication; do not expose it on a network. A saved session is required for resume. Tasks targeting the same canonical workspace are queued behind an OS-backed workspace lock; separate worktrees remain eligible to run concurrently. CLI processes also hold a per-session OS lock for their lifetime, so the same session cannot be resumed by two Sigma processes at once. Locks are released by the operating system when a process exits.

The HTTP API accepts a task with `POST /tasks`, reports state through `GET /tasks/{id}`, and streams lifecycle/model/tool events through `GET /tasks/{id}/events`. `POST /tasks/{id}/messages` accepts `{"message":"...","mode":"steer"}` or `{"message":"...","mode":"follow_up"}`. Steering is inserted at the next safe Agent turn boundary; follow-up runs after the current request would otherwise settle. Messages are durably queued and deduplicated across worker restarts. Sending a message to a completed task queues continuation of its saved session. SSE reconnects replay events after `Last-Event-ID`; that resumes event delivery, while session continuation resumes Agent work. The runtime currently exposes these interfaces through HTTP and CLI without a browser chat client.

## Source browsing and repository-memory retrieval

Current source is explored with ordinary file listing, literal search, and bounded reads. Sigma Agent does not chunk, embed, or store source files in Qdrant.

Long-term memory is a separate, opt-in retrieval path:

```text
.sigma-agent/MEMORY.md -> parse facts + provenance -> embed facts -> Qdrant sigma_agent_memory
query -> local BM25 ranks ----------------------------------------------\
query -> embedding -> repository-filtered vector ranks -------------------+-> RRF -> search(scope=memory)
```

The Markdown file remains the source of truth. Run `--index-memory` from its workspace to sync only its facts into the derived Qdrant collection. At runtime, `search(scope=memory)` combines local BM25 with vector candidates using reciprocal-rank fusion; Qdrant filters by exact workspace identity. If embedding/Qdrant is unavailable or no vector candidates match, it falls back to BM25. Returned facts retain source, category, and date; current source and tests take precedence. When `memory_auto_write` is enabled, a successful non-interactive run gets a separate bounded post-run model call that may append up to three facts; failed/interrupted runs do not write. Markdown stays outside the result patch, and vector indexing is refreshed after additions.

Repository memory is used during ordinary task runs through `search` with
`scope=memory`. Mini SWE configs may enable it, but a memory-enabled run
does not imply that the Agent searched or used a fact. Historical Mini SWE
records include indexing and automatic writeback activity; they do not isolate
memory's effect on issue resolution. See the [memory usage and configuration
guide](CONFIGURATION.md#repository-long-term-memory) and the
[Mini SWE memory status](../evals/planned/skills-memory/repository-memory-status.md).

## Skills and repository memory

Shared coding workflow instructions live in the embedded
`cmd/sigma-agent/workflow-guidance.md` resource, separate from the session
orchestrator. Per-run edit/test/diff state, repeated-command recovery, and
long-task checkpoints live in `internal/agent/workflow_policy.go`; concise
state changes are returned after relevant tools or as checkpoint messages.
The system prompt prefix and ordered tool schema stay stable across turns.
The orchestrator retains only the hook and the finalization gate: it asks the
policy for current state, holds a premature final response when checks are
stale, and preserves an incomplete candidate with a continuation instruction
when the turn limit is reached.

If the repository root contains `AGENTS.md`, Sigma reads it at session start
and adds its bounded contents (up to 16 KiB) as project instructions. This
project-level file is for stable conventions and pointers; Sigma currently
loads the repository-root file only, not ancestor or nested `AGENTS.md` files.
Keep task-specific procedures in Skills and source-backed cross-task facts in
`.sigma-agent/MEMORY.md` instead of duplicating them in `AGENTS.md`.

`project_instructions_file` can add an external Markdown profile to a run. This
is useful when issue tasks use independent checkouts: Sigma reads the profile
from outside the target checkout, and run metadata records its path and hash.
The target repository does not need an `AGENTS.md` copy.

At startup, Sigma scans the project `.agents/skills/` and `.pi/skills/` roots, followed by user `~/.agents/skills/` and `~/.pi/agent/skills/`. Project Skills win name collisions over user Skills. It discovers nested `SKILL.md` files and nested frontmatter Markdown under `.agents/skills/`; `.pi/skills/` and `~/.pi/agent/skills/` also accept standalone root Markdown Skill files. Frontmatter requires `name` and `description`; `license`, `compatibility`, `disable-model-invocation`, and `user-invocable` are also read. Other fields are tolerated but do not change Sigma's tool policy. When Skills are present, the model uses `search` with `scope=skills` to retrieve up to ten lexical matches from Skill metadata, then calls `read_skill` with a name to load the body (up to 12 KiB) or with a Skill-relative path to load a supported text reference (up to 32 KiB). A user can explicitly invoke a Skill from the REPL with `/skill:<name> <task>`. When explicitly asked to preserve reusable guidance, the model can create a project Skill through `create_skill`; it refuses to overwrite existing files, and the new Skill becomes visible after `/reload` or a session restart. Skill reads reject symlinks and path traversal; loading Skill files does not automatically run scripts. Pi's `allowed-tools` permission behavior and full npm/git Pi package behavior are not implemented. Set `skills_enabled` to `false` to run a no-Skill ablation; repository memory remains independently enabled.

The optional `.sigma-agent/MEMORY.md` is read from the current repository worktree with a 16 KiB source-file limit, but its contents are never injected wholesale into the prompt. It stores short durable facts shared across sessions; session snapshots remain separate recovery state, and source browsing remains separate. Memory should capture expensive-to-rediscover test procedures, architecture decisions, compatibility rules, and recurring pitfalls—not source code or task transcripts. New entries use `- YYYY-MM-DD | kind: testing | source: path | fact`; supported kinds are `testing`, `architecture`, `compatibility`, `pitfall`, and `workflow`. When entries exist, the `search` router exposes `scope=memory`: local BM25 works without services, while configured embeddings and Qdrant add vector search and RRF. Vector failures or empty results fall back to local BM25. `memory_enabled: false` removes this scope. Agent edit tools cannot modify the file.

For cross-clone work, `memory_file` can point at a reviewed Markdown source
outside the target checkout and `memory_repository_id` gives it a stable scope.
Sigma materializes that file in the active workspace so BM25 and vector search
use the same local facts and stable Qdrant filter. For direct TUI sessions this
is the selected checkout; legacy sessions created with a worktree use that
saved worktree. `--index-memory` also uses the configured external source. When
`memory_auto_write` is enabled,
successful sessions append to the external source and refresh that same scope.

`--index-memory` synchronizes current-workspace memory facts to `sigma_agent_memory`; no code collection or code indexing command exists. Vector search applies exact `repo_id` filtering and returns kind, provenance, and age. Entries older than 180 days are marked for revalidation. The prompt asks the Agent to verify retrieved facts against current code/tests, treats memory as untrusted reference data, and gives current source and user instructions precedence. Automatic capture is controlled by `memory_auto_write` and must remain disabled in controlled memory/no-memory evaluations to prevent cross-task leakage. Symlink paths are rejected so project context cannot escape the workspace.

## Pi concepts to reproduce in Go

- A stateful session owns the canonical message transcript. Provider-specific request objects are created only at the model boundary.
- The runtime is an inner tool loop plus an outer conversation loop. A turn may contain several assistant/tool cycles before it settles.
- Model output streams as normalized lifecycle events. Tool argument deltas are accumulated and validated before execution.
- A saved partial model draft is recovery context, not a resumable provider stream: after interruption Sigma makes a new model request rather than continuing from the exact prior token.
- Tool calls are validated and pass through before/after policy hooks. Tool errors become tool results so the model can recover.
- A batch executes concurrently only when all its tools use the same supported parallel mode: safe reads, or workspace writes protected by per-path locks. Mixed modes, MCP calls, and shell/test commands execute sequentially. Tool results are appended in the original call order. Path locks coordinate writes inside the workspace runtime; they do not coordinate arbitrary external processes.
- Tool results larger than the inline threshold are written to a private per-session artifact directory. The transcript stores a bounded preview and artifact reference; `read_file` loads later byte ranges with `artifact_id` and `byte_offset`. Artifacts are capped at 32 MiB each and 128 MiB per session.
- Retry is bounded and limited to retryable provider failures. Cancellation propagates to provider requests and tools.
- Steering messages interrupt at a safe turn boundary; follow-up messages run only after the current request settles.
- Session events and tool policy hooks are independent of the provider adapter. The file session store checkpoints transcript, queued messages, partial streamed text, and the tool-operation journal; the context manager compacts only the provider request while retaining canonical history. A per-session OS lease prevents multiple Sigma processes from resuming and writing the same session concurrently. Each tool registration defaults to `never_replay`; an adapter may explicitly opt into `safe_to_replay` and receives a stable idempotency key through its context. On resume, Sigma reuses persisted results, asks an optional recovery hook about external state, replays only when both the saved operation and current registration allow it, and otherwise returns an unknown-outcome result for inspection. Safe replay is an adapter contract, not a universal exactly-once guarantee; external systems must honor the key for deduplication. Compensation remains explicit and domain-specific.
- A read-only GitHub MCP adapter can add issue, repository, and pull-request context when credentials are configured. The built-in integration cannot push branches or create pull requests. Separately configured MCP servers can expose additional tools. The runtime owns JSON-RPC framing, version discovery/fallback, tool listing, and process cleanup; all remote tools are namespaced and serialized.
- A shared MCP client (`internal/mcp`) supports stdio child processes and Streamable HTTP endpoints. It handles protocol discovery/fallback, JSON-RPC requests, tool listing, and transport shutdown. Configured HTTP servers can supply static request headers; OAuth and legacy HTTP+SSE are not implemented. The GitHub adapter and configured MCP servers use the same host tool registration and dispatch path. Remote tools are exposed as `mcp__<server>__<tool>` and run in sequence.
- A package manager (`internal/pkgmgr`) installs npm and git packages into a scope directory and records them in a scope registry. A package contributes Agent Skills, MCP servers over stdio or Streamable HTTP, and JavaScript workflow extensions. The workflow host supports slash commands, Agent tool registration, notifications, TUI confirmations, and read-only lifecycle events; it does not expose mutable session state or custom TUI rendering. Built-in GitHub MCP and configured MCP server tools use the same host tool registration and dispatch path.
- Configured external command tools run one trusted host executable per tool call with JSON stdin and bounded stdout. They inherit the user's environment and permissions; this adapter is not a sandbox or package manager.

## Initial scope and limits

The Go runtime implements resumable sessions with partial-stream and tool-operation recovery state, tool policies, bounded retries/cancellation, a JSONL event journal, request-window compaction, and optional session summaries. It provides an integrated TUI, one-shot CLI/JSONL modes, transcript branching, code checkpoints, local and HTTP MCP transports, JavaScript workflow extensions, repository Skills and optional memory, host-command adapters, and a loopback HTTP/SSE task service. New sessions edit the selected repository directly. The service serializes tasks that target the same canonical workspace and permits separate worktrees to run concurrently; it remains a single-process prototype without authentication or production-grade queue guarantees. Repository commands run in resource-limited Docker containers, offline by default, with the selected workspace mounted writable. GitHub MCP is read-only. Provider cache counters are recorded when returned, but MiniMax cache-hit rate and token savings have not been measured. Controlled evidence has not isolated memory's effect on issue-resolution quality.

The Go module has no Agent SDK dependency. The archived TypeScript/Pi prototype is not part of the Go runtime's execution path.

For measured task evidence and benchmark protocols, see the files under [`evals/`](../evals/)
