# Runtime configuration

Sigma Agent reads `config.json` from the current working directory by default. Pass
`--config path/to/file.json` to use another file. Missing files use built-in
defaults. A file can contain only the settings that need changing.

```json
{
  "max_turns": 32,
  "max_tool_calls": 64,
  "max_context_tokens": 64000,
  "summary_tokens": 800,
  "max_tool_result_bytes": 16384,
  "max_retrieval_query_bytes": 8192,
  "model_output_tokens": 16384,
  "tool_timeout_ms": 60000,
  "model_timeout_ms": 120000,
  "max_retries": 2,
  "go_test_image": "golang:1.26",
  "skills_enabled": true,
  "web_search_enabled": false,
  "default_tool_permission": "allow",
  "tool_permissions": {
    "apply_patch": "deny",
    "run_tests": "allow"
  }
}
```

| Setting | Default | Allowed range | Purpose |
| --- | ---: | ---: | --- |
| `max_turns` | 32 | 1–512 | Assistant/model turns allowed in one run |
| `max_tool_calls` | 64 | 1–4096 | Tool calls allowed in one run |
| `max_context_tokens` | 64000 | 512–1000000 | Estimated context budget passed to the model; if MiniMax explicitly rejects an oversized prompt, Sigma Agent compacts the request and retries up to three times |
| `summary_tokens` | 800 | 0 to less than context budget | Space reserved for the compact transcript summary |
| `max_tool_result_bytes` | 16384 | 1024–4194304 | Inline limit for each tool result. Larger results are saved as session artifacts (32 MiB per artifact, 128 MiB per session) and replaced by a preview/reference. If artifact storage fails or reaches its cap, the inline result is truncated. |
| `max_retrieval_query_bytes` | 8192 | 500–65536 | Maximum UTF-8 bytes accepted by `retrieve_code`; increase for long issue descriptions |
| `model_output_tokens` | 16384 | 256–65536 | Requested output limit for each model response; truncated responses get up to two continuation attempts |
| `tool_timeout_ms` | 60000 | 1000–1800000 | Timeout for each tool execution |
| `model_timeout_ms` | 120000 | 1000–1800000 | HTTP timeout for a model request |
| `max_retries` | 2 | 0–10 | Retries for retryable transport/server failures, including HTTP 429 and 529 |
| `go_test_image` | `golang:1.26` | Docker image reference | Image used by offline `run_tests`; it must include the repository's Go module dependencies |
| `skills_enabled` | `true` | boolean | Discover project `.agents/skills/**/SKILL.md` files. Set to `false` for a matched no-Skill evaluation; repository memory stays enabled |
| `memory_enabled` | `true` | boolean | Enable repository memory context and `search_memory`. Set to `false` for a matched no-memory comparison; code retrieval and indexing remain unchanged |
| `web_search_enabled` | `false` | boolean | Add MiniMax's hosted `web_search` as an explicit Agent tool. Uses provider credentials and quota. |
| `default_tool_permission` | `allow` | `allow` or `deny` | Permission applied to every registered tool unless overridden below. Set to `deny` to use an allowlist. |
| `tool_permissions` | `{}` | tool name → `allow` or `deny` | Exact per-tool overrides. Tool names match the names exposed to the model, including namespaced MCP tools. |

Tool permissions are checked after argument validation and before the handler runs. A denied call is not executed; the Agent receives a tool error and the JSONL trace records `tool_permission_denied`. This first version supports policy-based allow/deny only; it does not prompt for interactive approval.

For quick one-run adjustments, `--max-turns`, `--max-tool-calls`,
`--context-tokens`, and `--model-output-tokens` override the file. The HTTP
service passes the same config path to each task worker.

`run_tests` disables network access inside Docker. For benchmark or repository
workspaces, set `go_test_image` to a pinned image with the matching Go version
and module cache already populated. For example, the local Multi-SWE-bench
`go-zero#2537` task image is `mswebench/zeromicro_m_go-zero:pr-2537`; select it
in a task-specific config so `go test ./...` uses that image's preloaded Go
1.24.1 toolchain and dependencies without exposing test execution to network.
Go commands use `GOMAXPROCS=2` and `-p 2` to fit the test container's CPU and
process limits. `go-test-package` runs a focused package such as
`./core/mapping`; `go-test` and `go-vet` run across the module.

Set `web_search_enabled` to `true` to expose an explicit `web_search` tool to
the Agent. MiniMax performs the search server-side through its Anthropic
Messages API; Sigma Agent returns the answer and source URLs to the Agent. This
is separate from GitHub MCP and is disabled by default. Search use follows
MiniMax's server-tool availability and may consume provider quota.
The tool uses the configured `SIGMA_AGENT_BASE_URL` (default
`https://api.minimaxi.com/anthropic`). MiniMax's server-tool guide shows the
China endpoint as `https://api.minimax.cn/anthropic`; if the configured host
does not support `web_search`, set `SIGMA_AGENT_BASE_URL` to the supported host.

## Repository long-term memory

Keep reviewed, cross-task repository facts in `.sigma-agent/MEMORY.md`, one entry per line. The file is the source of truth and is not injected wholesale into the model prompt:

```markdown
- 2026-10-30 | kind: testing | source: docs/testing.md | Run focused package tests before the full Go suite.
- 2026-10-30 | kind: architecture | source: ARCHITECTURE.md | The HTTP service delegates each task to a Sigma Agent process and persists its session by task ID.
```

Supported kinds are `testing`, `architecture`, `compatibility`, `pitfall`, and
`workflow`. Keep facts short and repository-specific: capture decisions,
environment constraints, or recurring pitfalls that are expensive to
rediscover and cannot be reliably inferred from the current source. Do not copy
source code, archive task transcripts, store secrets, or treat memory as a
replacement for `AGENTS.md`, README files, tests, or the code itself. The legacy
`date | source | fact` format is still accepted as `kind: general`.
When vector search is configured, run `--index --repo <path>` after upgrading
this format so the derived Qdrant payloads include the category and new content
hash. Local BM25 always reads the current Markdown file directly.

When memory is retrieved, Sigma Agent labels entries reviewed more than 180 days
ago as `revalidation_required`. The Agent is instructed to verify applicable
facts against the current checkout and tests before using them. Current source,
repository instructions, and the user's request take precedence over memory.

Agent tasks run in detached Git worktrees, so this file must be present in the
task's starting commit for `search_memory` to be registered. Vector results
are validated against the current Markdown entry hashes before use.

When vector settings are configured, run `--index --repo <path>` to sync code
and memory into separate `sigma_agent_code` and `sigma_agent_memory` collections.
When only reviewed memory changes, run `--index-memory --repo <path>` to sync
the memory collection without embedding the repository's code again.
The Agent gets a `search_memory` tool and retrieves a small, repo-filtered set
of relevant entries. Changed or removed Markdown entries are synchronized on
the next index run; stale search results are discarded by content hash. Without
embedding configuration, `search_memory` uses local BM25 over the current
Markdown entries, so memory remains available without loading the whole file
into the prompt or requiring Qdrant. When vector search is configured but
unavailable or has no current hits, the tool falls back to local BM25.

The memory collection uses the same embedding endpoint configured for code
retrieval. The endpoint receives memory entry text during indexing and queries
during search; Qdrant stores the entry payloads and vectors. Configure both
services for data permitted to leave or enter those systems. The current
repository identity is local to a Git common directory, so separate clones do
not share one memory namespace automatically.

For a Hugging Face-compatible endpoint, set `SIGMA_AGENT_EMBEDDING_PROVIDER`
(`huggingface` is the default), `SIGMA_AGENT_EMBEDDING_MODEL`, and optionally
`HF_TOKEN` or `SIGMA_AGENT_EMBEDDING_API_KEY`. Qdrant defaults to
`http://localhost:6333`; override it with `SIGMA_AGENT_QDRANT_URL` when needed.
Start the existing Qdrant service and index a repository with:

```powershell
docker compose up -d qdrant
$env:SIGMA_AGENT_EMBEDDING_PROVIDER = 'huggingface'
$env:SIGMA_AGENT_EMBEDDING_MODEL = '<Hugging-Face-model-id>'
$env:HF_TOKEN = '<token>'
go run ./cmd/sigma-agent --index --repo C:\path\to\repository
```

The MiniMax chat key does not configure an embedding endpoint. For an
OpenAI-compatible embedding endpoint, set
`SIGMA_AGENT_EMBEDDING_PROVIDER=openai-compatible`,
`SIGMA_AGENT_EMBEDDING_BASE_URL`, `SIGMA_AGENT_EMBEDDING_MODEL`, and optionally
`SIGMA_AGENT_EMBEDDING_API_KEY` instead. If the API key environment variable is
unset, Sigma Agent reads `.secrets/embedding-api-key` from the current working
directory, matching the local MiniMax key-file setup. Use
`scripts/set-embedding-key.ps1` to save a key without displaying it. The
provider, base URL, and model can remain in the user's environment; keep the
secret itself out of `config.json` and tracked files.

Increasing a local budget does not increase provider account credits or remove
provider rate limits. Context size is estimated locally; the provider remains
the final authority on its account and request limits. When old history or tool
output no longer fits, Sigma Agent compacts it before sending the request while
preserving the current task and tool-call/result pairing.

## GitHub MCP credentials

Sigma Agent automatically enables GitHub MCP when it finds credentials. It checks
`GITHUB_PERSONAL_ACCESS_TOKEN` first and then reads `.secrets/github-pat`
relative to the current working directory. Keep the token in that local file;
`.secrets/` is ignored by Git.
The GitHub MCP server is started in Docker with the read-only `issues`, `repos`,
and `pull_requests` toolsets. Start Sigma Agent from `go-runtime` so it finds the
project-local credential file. A one-time setup helper can create or replace
the credential file without echoing the token:

```powershell
Set-Location C:\dev\Agent\coding-agent-harness\go-runtime
.\scripts\set-github-token.ps1
```

The helper prompts for a PAT using masked input. Never commit or share the
credential file; revoke the PAT in GitHub if it is exposed.

## MiniMax API credentials

MiniMax credentials can be supplied through `MINIMAX_API_KEY` (or the supported
Anthropic-compatible environment variables), or stored locally in
`.secrets/minimax-api-key`. From `go-runtime`, run the helper to enter the key
with masked input; Sigma Agent reads the ignored project-local file on later runs:

```powershell
Set-Location C:\dev\Agent\coding-agent-harness\go-runtime
.\scripts\set-minimax-key.ps1
```

The helper does not print the key. Do not commit or share the file.
