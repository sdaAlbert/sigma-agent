# Go Agent Runtime

This runtime uses Pi Agent as an architecture reference, not as a dependency. It keeps the agent loop, transcript, tool lifecycle, policy hooks, cancellation, and event model in Go. Provider adapters normalize Anthropic Messages and OpenAI Chat Completions-compatible APIs into Sigma's model interface; the interactive CLI exposes `/login` for model and GitHub credentials and `/model` for model selection.

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
          (parallel reads; path-locked writes;
                 serialized shell/tests)
                       |
                       v
                  tool_result messages
                       |
                       +----------------------> next model turn
```

## Terminal interaction

The CLI supports both a one-shot task and a Pi-style persistent terminal conversation. The interactive mode keeps one session transcript across prompts, streams model text and tool lifecycle events to the terminal, and checkpoints each prompt so the session can be resumed later.

```powershell
# Start an interactive session
go run ./cmd/sigma-agent --repo C:\path\to\clean\repo --interactive

# Start with an initial instruction
go run ./cmd/sigma-agent --repo C:\path\to\clean\repo --interactive --task "Find and fix the bug in ..."

# Resume a saved session
go run ./cmd/sigma-agent --repo C:\path\to\clean\repo --interactive --resume <session-id>
```

Interactive invocations start the Bubble Tea TUI. Setup, credential entry, model/repository/session selection, multiline prompts, streamed responses, tool cards, and session commands are handled inside the same UI. It renders on the main screen to preserve terminal scrollback. Use `--task` without `--tui` for the one-shot CLI; `--json` and `--serve` remain separate modes. See [the user guide](USAGE.md) for setup and command details.

`run_command` supports foreground execution plus `action=start`, `action=list`, `action=status`, and `action=stop` for detached Docker commands. Job metadata is stored under the session directory and survives `--resume`; one task can run at a time and expires after 30 minutes. Read-only repository tools remain available while it runs. File edits, shell commands, tests, diffs, and Git publishing wait until the job completes or is stopped, because the container shares the session worktree.

After a workspace write, runtime tracks whether tests and `git_diff` have run against that revision. During this review, it exposes only outstanding checks plus targeted file reads and edits; each new write reopens both checks. The finalization exploration budget limits broad searches and inspection commands, while targeted file reads remain available so a test or diff finding can be corrected and reverified.

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

The service binds to loopback by default and stores task records and event logs under `var/server/`. It is a single-process prototype without authentication; do not expose it on a network. A saved session is required for resume.

The HTTP API accepts a task with `POST /tasks`, reports state through `GET /tasks/{id}`, and streams lifecycle/model/tool events through `GET /tasks/{id}/events`. `POST /tasks/{id}/messages` accepts `{"message":"...","mode":"steer"}` or `{"message":"...","mode":"follow_up"}`. Steering is inserted at the next safe Agent turn boundary; follow-up runs after the current request would otherwise settle. Messages are durably queued and deduplicated across worker restarts. Sending a message to a completed task queues continuation of its saved session. SSE reconnects replay events after `Last-Event-ID`; that resumes event delivery, while session continuation resumes Agent work. The runtime currently exposes these interfaces through HTTP and CLI without a browser chat client.

## Source browsing and repository-memory retrieval

Current source is explored with ordinary file listing, literal search, and bounded reads. Sigma Agent does not chunk, embed, or store source files in Qdrant.

Long-term memory is a separate, opt-in retrieval path:

```text
.sigma-agent/MEMORY.md -> parse facts + provenance -> embed facts -> Qdrant sigma_agent_memory
query -> local BM25 ranks ----------------------------------------------\
query -> embedding -> repository-filtered vector ranks -------------------+-> RRF -> search_memory
```

The Markdown file remains the source of truth. `--index-memory --repo <path>` syncs only its facts into the derived Qdrant collection. At runtime, the `search_memory` tool combines local BM25 with vector candidates using reciprocal-rank fusion; Qdrant filters by exact repository ID. If embedding/Qdrant is unavailable or no vector candidates match, it falls back to BM25. Returned facts retain source, category, and date; current source and tests take precedence. When `memory_auto_write` is enabled, a successful non-interactive run gets a separate bounded post-run model call that may append up to three facts; failed/interrupted runs do not write. Markdown stays outside the result patch, and vector indexing is refreshed after additions.

The labeled retrieval harness is `evals/memory-retrieval-v1.json`, paired with a six-fact isolated fixture repository. `--eval-memory` compares BM25, dense vector, and hybrid RRF with Recall@K and MRR using actual configured embeddings. This small fixture checks retrieval and metric plumbing only. A separate matched evaluation on fresh Go issues is needed to establish repair value; historical #1969/#2537 runs are not a clean memory ablation.

## Skills and repository memory

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

At startup, Sigma scans the project `.agents/skills/` and `.pi/skills/` roots, followed by user `~/.agents/skills/` and `~/.pi/agent/skills/`. Project Skills win name collisions over user Skills. It discovers nested `SKILL.md` files and nested frontmatter Markdown under `.agents/skills/`; `.pi/skills/` and `~/.pi/agent/skills/` also accept standalone root Markdown Skill files. Frontmatter requires `name` and `description`; `license`, `compatibility`, `disable-model-invocation`, and `user-invocable` are also read. Other fields are tolerated but do not change Sigma's tool policy. Only Skill names/descriptions (except `disable-model-invocation: true`) enter the system context. A matching Skill body loads on demand through `read_skill` (up to 12 KiB); a user can explicitly invoke a Skill from the REPL with `/skill:<name> <task>`. The `read_skill_resource` tool reads supported text references/templates within a Skill folder, capped at 32 KiB, and rejects symlinks and path traversal. Standalone root Markdown Skills have no isolated companion folder, so they cannot use this tool. Loading Skill files does not automatically run scripts; Pi's `allowed-tools` permission behavior and npm/git Pi package installation are not implemented. Set `skills_enabled` to `false` to run a no-Skill ablation; repository memory remains independently enabled.

The optional `.sigma-agent/MEMORY.md` is read from the current repository worktree with a 16 KiB source-file limit, but its contents are never injected wholesale into the prompt. It stores short durable facts shared across sessions; session snapshots remain separate recovery state, and source browsing remains separate. Memory should capture expensive-to-rediscover test procedures, architecture decisions, compatibility rules, and recurring pitfalls—not source code or task transcripts. New entries use `- YYYY-MM-DD | kind: testing | source: path | fact`; supported kinds are `testing`, `architecture`, `compatibility`, `pitfall`, and `workflow`. When entries exist, the Agent gets `search_memory`: local BM25 works without services, while configured embeddings and Qdrant add vector search and RRF. Vector failures or empty results fall back to local BM25. `memory_enabled: false` disables memory instructions and the `search_memory` tool. Agent edit tools cannot modify the file.

For cross-clone work, `memory_file` can point at a reviewed Markdown source
outside the target checkout and `memory_repository_id` gives it a stable scope.
Sigma copies that file into the isolated session worktree so BM25 and vector
search use the same local facts and stable Qdrant filter. `--index-memory` also
uses the configured external source. When `memory_auto_write` is enabled,
successful sessions append to the external source and refresh that same scope;
the original target checkout remains untouched.

`--index-memory --repo <path>` synchronizes memory facts to `sigma_agent_memory`; no code collection or code indexing command exists. Vector search applies exact `repo_id` filtering and returns kind, provenance, and age. Entries older than 180 days are marked for revalidation. The prompt asks the Agent to verify retrieved facts against current code/tests, treats memory as untrusted reference data, and gives current source and user instructions precedence. Automatic capture is controlled by `memory_auto_write` and must remain disabled in controlled memory/no-memory evaluations to prevent cross-task leakage. Symlink paths are rejected so project context cannot escape the worktree.

## Pi concepts to reproduce in Go

- A stateful session owns the canonical message transcript. Provider-specific request objects are created only at the model boundary.
- The runtime is an inner tool loop plus an outer conversation loop. A turn may contain several assistant/tool cycles before it settles.
- Model output streams as normalized lifecycle events. Tool argument deltas are accumulated and validated before execution.
- Tool calls are validated and pass through before/after policy hooks. Tool errors become tool results so the model can recover.
- Independent reads may execute concurrently; state-changing tools run in order. Tool results are appended in the same order as the assistant's calls.
- Tool results larger than the inline threshold are written to a private per-session artifact directory. The transcript stores a bounded preview and artifact reference; `read_tool_output` loads later byte ranges on demand. Artifacts are capped at 32 MiB each and 128 MiB per session.
- Retry is bounded and limited to retryable provider failures. Cancellation propagates to provider requests and tools.
- Steering messages interrupt at a safe turn boundary; follow-up messages run only after the current request settles.
- Session events and tool policy hooks are independent of the provider adapter. The file session store checkpoints transcript and queues; the context manager compacts only the provider request while retaining canonical history.
- A GitHub MCP adapter automatically adds issue, repository, and pull-request context when credentials are configured. Scoped PR creation is added only when publishing is explicitly enabled. The runtime owns JSON-RPC framing, version discovery/fallback, tool listing, and process cleanup; all remote tools are namespaced and serialized.
- A shared MCP client (`internal/mcp`) serves every stdio MCP server. The GitHub adapter and the `mcp.json` servers use the same protocol layer, so version discovery, frame limits, tool namespacing, and process cleanup behave identically. Remote tools are exposed as `mcp__<server>__<tool>` and run in sequence.
- A package manager (`internal/pkgmgr`) installs npm and git packages into a scope directory and records them in a scope registry. A package contributes Agent Skills, stdio MCP servers, and JavaScript workflow extensions. The workflow host supports slash commands, Agent tool registration, notifications, TUI confirmations, and read-only lifecycle events; it does not expose mutable session state or custom TUI rendering. Built-in GitHub MCP and configured MCP server tools use the same host tool registration and dispatch path.
- Configured external command tools run one trusted host executable per tool call with JSON stdin and bounded stdout. They inherit the user's environment and permissions; this adapter is not a sandbox or package manager.

## Initial scope and limits

The current Go milestone implements local resumable sessions with streaming, tool policies, bounded retry/cancel, a JSONL event journal, extractive request-window compaction, and optional model-requested session summaries. Provider usage events capture cache-read and cache-creation tokens when returned; the actual MiniMax cache hit rate and token savings have not been measured. Direct CLI sessions can opt in to commit and push one explicitly named new feature branch using local Git credentials; `--github-write` adds only pull-request creation, scoped to the checkout's `origin` repository and that branch, and never merges. The CLI has basic session listing and transcript branching, plus a config-driven host-command tool adapter for trusted external executables. It also has a Pi-style terminal REPL, a loopback HTTP/SSE task service with bounded workers and persisted task records, ordinary source browsing, on-demand project Skills, optional repository memory with a memory-only Qdrant index, and optional post-run memory capture. Packages can be installed from npm, git, or a local path, and `mcp.json` servers contribute namespaced tools through the shared MCP client. JavaScript extensions can register commands and Agent tools, use notify/confirm, and observe read-only lifecycle events; built-in GitHub MCP and configured MCP servers use the same host tool registration path. HTTP MCP servers are not supported. Coding tasks run repository-native Linux build, lint, and test commands through a configured, resource-limited Docker image; Sigma has no language-specific built-in test profiles. Command containers have networking disabled by default; sessions can opt in when dependency downloads are needed. Host packages and external service stacks still need to be present/prepared separately. The six-query labeled retrieval harness has not yet been run with a live embedding service. One matched Skill smoke verified the workflow but showed no better result and used more tokens/calls; broader task-quality effects remain unmeasured. Multi-host workers, authentication, independent memory-quality evaluation, and production-grade queue guarantees are not implied by this milestone.

The Go module has no Agent SDK dependency. The archived TypeScript/Pi prototype is not part of the Go runtime's execution path.

For measured task evidence and benchmark protocols, see the files under [`evals/`](../evals/)
