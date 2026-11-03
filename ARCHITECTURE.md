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
       native Go tools     GitHub read-only MCP
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

Within the session, enter a coding instruction at `You>`. While the Agent is running, enter plain text or `/steer <text>` to queue guidance for its next safe turn; `/followup <text>` queues a later instruction. `/diff` shows the current patch summary, `/abort` cancels the active turn, `/help` lists commands, and `/exit` saves and exits. The session ID and worktree path are printed after exit. This is a terminal REPL, not a full-screen TUI; one-shot CLI and HTTP/SSE service modes remain available.

`run_command` supports foreground execution plus `action=start`, `action=list`, `action=status`, and `action=stop` for detached Docker commands. Job metadata is stored under the session directory and survives `--resume`; one task can run at a time and expires after 30 minutes. Read-only repository tools remain available while it runs. File edits, shell commands, tests, diffs, and Git publishing wait until the job completes or is stopped, because the container shares the session worktree.

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

At startup, Sigma Agent scans only `.agents/skills/**/SKILL.md` below the task worktree. It parses bounded YAML frontmatter and adds each valid Skill's name and one-line description to the system context. If a description matches the task, the model can call `read_skill` with the exact Skill name to load its Markdown body (up to 12 KiB). The tool cannot read arbitrary paths or companion files, and loading a Skill does not run scripts. Set `skills_enabled` to `false` to run a no-Skill ablation; repository memory remains independently enabled.

The optional `.sigma-agent/MEMORY.md` is read from the current repository worktree with a 16 KiB source-file limit, but its contents are never injected wholesale into the prompt. It stores short durable facts shared across sessions; session snapshots remain separate recovery state, and source browsing remains separate. Memory should capture expensive-to-rediscover test procedures, architecture decisions, compatibility rules, and recurring pitfalls—not source code or task transcripts. New entries use `- YYYY-MM-DD | kind: testing | source: path | fact`; supported kinds are `testing`, `architecture`, `compatibility`, `pitfall`, and `workflow`. When entries exist, the Agent gets `search_memory`: local BM25 works without services, while configured embeddings and Qdrant add vector search and RRF. Vector failures or empty results fall back to local BM25. `memory_enabled: false` disables memory instructions and the `search_memory` tool. Agent edit tools cannot modify the file.

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
- A GitHub MCP adapter automatically adds read-only issue, repository, and pull-request context when credentials are configured. The runtime owns JSON-RPC framing, version discovery/fallback, tool listing, and process cleanup; all remote tools are namespaced and serialized.

## Initial scope and limits

The current Go milestone implements local resumable sessions with streaming, tool policies, bounded retry/cancel, a JSONL event journal, an extractive context window, and GitHub MCP issue/repository/PR context. Direct CLI sessions can opt in to commit and push one explicitly named new feature branch using local Git credentials; `--github-write` adds only pull-request creation, scoped to the checkout's `origin` repository and that branch, and never merges. It also has a Pi-style terminal REPL, a loopback HTTP/SSE task service with bounded workers and persisted task records, ordinary source browsing, on-demand project Skills, optional repository memory with a memory-only Qdrant index, and opt-in post-run memory capture. Coding tasks can use fixed Go test profiles or run repository-native Linux commands in a configured, resource-limited Docker image. Containers are offline by default; `--allow-container-network` lets the Agent download project dependencies and run Go tests with module downloads. Host packages and external service stacks still need to be present/prepared separately. The six-query labeled retrieval harness has not yet been run with a live embedding service. One matched Skill smoke verified the workflow but showed no better result and used more tokens/calls; broader task-quality effects remain unmeasured. Multi-host workers, authentication, model-generated compaction, independent memory-quality evaluation, and production-grade queue guarantees are not implied by this milestone.

The Go module has no Agent SDK dependency. The archived TypeScript/Pi prototype is not part of the Go runtime's execution path.

For the current implementation inventory, measured task evidence, known gaps, and recommended next steps, see [PROJECT_STATUS.md](PROJECT_STATUS.md). In particular, the recent PR #964 and #2537 evaluator passes were guided runs and should not be read as a blind success-rate result.
