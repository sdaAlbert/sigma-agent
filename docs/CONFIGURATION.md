# Runtime configuration

Sigma Agent reads `config.json` from the current working directory by default. Pass
`--config path/to/file.json` to use another file. Missing files use built-in
defaults. A file can contain only the settings that need changing.

```json
{
  "max_turns": 128,
  "max_tool_calls": 256,
  "max_context_tokens": 64000,
  "context_compaction_threshold_tokens": 24000,
  "summary_tokens": 800,
  "max_tool_result_bytes": 16384,
  "max_retrieval_query_bytes": 8192,
  "model_output_tokens": 16384,
  "tool_timeout_ms": 600000,
  "model_timeout_ms": 120000,
  "max_retries": 2,
  "go_test_image": "golang:1.26",
  "command_execution_image": "golang:1.26",
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
| `max_turns` | 128 | 1–512 | Assistant/model turns allowed in one run |
| `max_tool_calls` | 256 | 1–4096 | Tool calls allowed in one run |
| `max_context_tokens` | 64000 | 512–1000000 | Hard estimated context ceiling; proactive compaction normally uses the lower threshold below. If MiniMax explicitly rejects an oversized prompt, Sigma Agent compacts the request and retries up to three times |
| `context_compaction_threshold_tokens` | `min(24000, max_context_tokens)` | 0–`max_context_tokens` | Proactively compact older conversation history when estimated input exceeds this threshold. `0` selects the automatic default. Compaction keeps recent complete turns, adds a bounded extractive summary, and records before/after estimates in the event log. |
| `summary_tokens` | 800 | 0 to less than compaction threshold | Maximum estimated size of summaries produced by automatic or model-requested context compaction |
| `max_tool_result_bytes` | 16384 | 1024–4194304 | Inline limit for each tool result. Larger results are saved as session artifacts (32 MiB per artifact, 128 MiB per session) and replaced by a preview/reference. If artifact storage fails or reaches its cap, the inline result is truncated. |
| `max_retrieval_query_bytes` | 8192 | 500–65536 | Maximum UTF-8 bytes accepted by `search_memory` (legacy setting name) |
| `model_output_tokens` | 16384 | 256–65536 | Requested output limit for each model response; truncated responses get up to two continuation attempts |
| `tool_timeout_ms` | 600000 | 1000–1800000 | Timeout for each tool execution, including Docker environment image preparation |
| `model_timeout_ms` | 120000 | 1000–1800000 | HTTP timeout for a model request |
| `max_retries` | 2 | 0–10 | Retries for retryable transport/server failures, including HTTP 429 and 529 |
| `go_test_image` | `golang:1.26` | Docker image reference | Image used by `run_tests`; when outbound network is disabled it should include the repository's Go module dependencies |
| `command_execution_image` | `golang:1.26` | Linux Docker image reference | Fallback image used by `run_command` until the Agent prepares an environment or chooses an image for a command |
| `allow_container_network` | `false` | boolean | Give test and command containers outbound internet access. Enable it per interactive session with `/network on` or at startup with `--allow-container-network`; useful for downloading project dependencies, but commands and package install scripts can then contact external services |
| `skills_enabled` | `true` | boolean | Discover project `.agents/skills/` and `.pi/skills/`, plus user `~/.agents/skills/` and `~/.pi/agent/skills/`. Set to `false` for a matched no-Skill evaluation; repository memory stays enabled |
| `memory_enabled` | `true` | boolean | Enable repository memory context and `search_memory`. Set to `false` for a matched no-memory comparison; code browsing tools remain available |
| `memory_auto_write` | `false` | boolean | After a successful non-interactive run, use one bounded model call to extract up to three durable facts into `.sigma-agent/MEMORY.md`, then refresh Qdrant when configured. Requires `memory_enabled`; generated notes and extra tokens are recorded in `run.json`. |
| `web_search_enabled` | `false` | boolean | Add MiniMax's hosted `web_search` as an explicit Agent tool. Uses provider credentials and quota. |
| `default_tool_permission` | `allow` | `allow` or `deny` | Permission applied to every registered tool unless overridden below. Set to `deny` to use an allowlist. |
| `tool_permissions` | `{}` | tool name → `allow` or `deny` | Exact per-tool overrides. Tool names match the names exposed to the model, including namespaced MCP tools. |

Tool permissions are checked after argument validation and before the handler runs. A denied call is not executed; the Agent receives a tool error and the JSONL trace records `tool_permission_denied`. This first version supports policy-based allow/deny only; it does not prompt for interactive approval.

The model can also call `compact_context` by itself when it judges that older
conversation or tool output can be summarized without losing information needed
for the task. This makes one additional model request, is bounded by
`summary_tokens`, and is lossy. The generated handoff summary and its message
boundary are saved with the session, so resumed sessions keep using the compact
context. The full conversation remains in the session history. Run metadata
reports `context_compactions`, `context_compaction_input_tokens`, and
`context_compaction_output_tokens`. Run metadata and `model_usage` events also
record input/output tokens plus cache-read and cache-creation input tokens when
the provider returns them. For MiniMax's Anthropic-compatible API, `input_tokens`
is normalized to include uncached, cache-read, and cache-creation input tokens;
cache fields remain separate subsets for calculating reuse. The event log records
`context_compacted_model` and any failed summary attempt. Automatic extractive
compaction at `context_compaction_threshold_tokens` remains available as a
separate safety mechanism.

For quick one-run adjustments, `--max-turns`, `--max-tool-calls`,
`--context-tokens`, `--context-compaction-tokens`, and `--model-output-tokens`
override the file. The effective compaction threshold is written to `run.json`;
each `context_compacted` event records estimated tokens before and after
compaction. The HTTP service passes the same config path to each task worker.

## Packages and MCP servers

`config.json` holds the runtime limits. Packages and MCP servers are
configured separately, because they are installed artifacts rather than
tunable limits.

- Packages are installed with `sigma-agent --install npm:<name>`, listed with
  `sigma-agent --list-packages`, and recorded in a scope registry
  (`~/.sigma-agent/packages.json` for the user scope,
  `<repo>/.sigma-agent/packages.json` for the project scope).
- MCP servers are configured in `mcp.json`, using the `mcpServers` shape shared
  by other MCP clients. Sigma Agent reads `~/.sigma-agent/mcp.json` and
  `<repo>/.sigma-agent/mcp.json`, and also reads `<repo>/.pi/mcp.json`.

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

A server that names a `package` starts with the package directory as its
working directory, and `./`-prefixed arguments are resolved against the package
root. Server tools are exposed as `mcp__<server>__<tool>` and pass through the
configured tool policy. `sigma-agent --list-mcp` connects to every enabled
server and prints the tools it contributes.

See [Packages and MCP servers](PACKAGES.md) for the package format, the
supported sources, and the current limits.

## External command tools

`command_tools` can expose trusted local executables as Agent tools. Each call
starts the configured command in the task workspace, writes one JSON object to
stdin, and returns stdout as the tool result. Configure the tool's JSON Schema
so the model knows the accepted arguments:

```json
{
  "command_tools": [
    {
      "name": "my_repo_helper",
      "description": "Run the repository-specific helper for a requested operation.",
      "input_schema": {
        "type": "object",
        "properties": {"operation": {"type": "string"}},
        "required": ["operation"],
        "additionalProperties": false
      },
      "command": "python",
      "args": ["tools/sigma_plugin.py"],
      "modifies_workspace": false
    }
  ]
}
```

The executable runs on the host with the user's permissions and inherited
environment. Only add commands you trust. Tool permission rules apply using the
configured tool name; output is capped before it enters model context. Set
`modifies_workspace` to `true` if the tool may edit repository files; successful
calls then trigger Sigma's normal test-and-diff review gate. This is a
lightweight executable adapter, not a package manager or process sandbox.

Interactive CLI setup uses `/login <provider>` to save API keys or the GitHub token and `/model`
to show or select a provider/model. The initial API-key providers are MiniMax,
Anthropic, OpenAI, DeepSeek, and OpenRouter. MiniMax and Anthropic use the
Anthropic Messages protocol; OpenAI, DeepSeek, and OpenRouter use Chat
Completions-compatible endpoints. Model IDs are entered as
`/model <provider>/<model-id>` rather than fetched from a remote catalog. Use
`/model save` to set the default for new sessions. `/logout <provider>` removes
the locally saved key; environment variables are used when no saved key exists.

Keys entered through `/login` are stored as plaintext credentials in
`%APPDATA%\sigma-agent\model-auth.json` on Windows, or the platform's user
configuration directory on other systems. `/login github` saves the GitHub
token there too; `/logout github` removes that saved token. A saved GitHub
token takes precedence over `GITHUB_PERSONAL_ACCESS_TOKEN`, which takes
precedence over `.secrets/github-pat` in the current directory. The file is
kept outside the repository and created with restrictive directory/file
permissions where the operating system supports them. Prefer environment
variables for managed or ephemeral environments.

In an interactive CLI session, `/publish <name>` explicitly enables local Git
commit and push tools for one feature branch. Sigma Agent creates that branch in
the detached task worktree and rejects names already present locally or on
`origin`, the remote's default branch, and common trunk names. `/publish off`
disables publishing. The commit tool stages non-ignored worktree changes; the
push tool targets only that configured branch on `origin` and does not
force-push. Git push uses the machine's normal Git credential helper, not the
GitHub MCP token. The user must request commit/push in the task instructions,
and the tools still pass through the configured tool permission policy. On
resume, use `/publish <same-branch>` to re-enable those tools. The equivalent
`--push-branch` startup flag remains available for scripted sessions. Publishing
is unavailable through `--serve` and disabled by default.

Use `/github-write on` to expose the GitHub MCP `create_pull_request` tool. This
requires `/publish <branch>`, GitHub MCP credentials, and write permission to
open a pull request. The MCP tool is restricted to the current `origin`
repository and the configured feature branch. It cannot merge a PR or write to
issues. Sigma creates a PR only when the task explicitly asks it to, after
pushing and verification. `/github-write off` disables PR creation. The
equivalent `--github-write` startup flag remains available for scripted
sessions. This option is not available through `--serve`.

`run_command` accepts any Docker image reference through its optional `image`
field. The Agent should inspect the repository's Dockerfile/devcontainer, CI,
README, and manifests, then use `prepare_environment` to select a matching base
image and install missing system packages or libraries. Sigma builds a derived
image from a generated Dockerfile and remembers it for subsequent commands and
session resumes. The build context contains only that generated Dockerfile;
Docker pulls a missing base image on first use and caches the resulting image.
`command_execution_image` remains the fallback before an environment is
prepared. `run_tests` remains a Go-specific convenience; other languages use
their project-native commands through `run_command`.

Set `allow_container_network` to `true`, use `/network on` before starting a
task, or pass `--allow-container-network` for one scripted CLI session to let
test and command containers use Docker's bridge network. This lets the Agent attempt project dependency setup, such as
`npm ci`, `pip install`, `mvn test`, or `cargo test`. In this mode `run_tests`
downloads Go modules before running the selected Go test/vet command. The
mounted task worktree persists between commands. Package-manager home caches
also persist under `.sigma-agent/container-home`, so Maven, Cargo, pip, npm,
and Go dependencies need not be downloaded again on every invocation. The mode
does not install host software, expose the Docker socket to the container,
start sibling service containers, or guarantee that arbitrary setup
instructions will work. Each command gets a fresh container, but images built
with `prepare_environment` preserve system package manager changes between
commands. The default remains offline; network access can expose repository
data to package registries or other endpoints contacted by install scripts.

By default, `run_tests` attaches each test container to a temporary Docker
internal network. This gives tests a virtual network interface while blocking
external egress; loopback remains available. With `allow_container_network`, it
uses Docker's bridge network. For benchmark or repository workspaces, set
`go_test_image` to a pinned image with the matching Go version and module cache
already populated. For example, the local Multi-SWE-bench
`go-zero#2537` task image is `mswebench/zeromicro_m_go-zero:pr-2537`; select it
in a task-specific config so `go test ./...` uses that image's preloaded Go
1.24.1 toolchain and dependencies without exposing test execution to the internet.
Go commands use `GOMAXPROCS=2` and `-p 2` to fit the test container's CPU and
process limits. `go-test-package` runs a focused package such as
`./core/mapping`; `go-test` and `go-vet` run across the module.

`prepare_environment` accepts any valid Docker base image and a shell setup
command. When network is enabled, that command can install OS packages, headers,
and compilers into a derived image; those changes persist across test/build
commands. Its generated build context contains no repository files. When
network is disabled, Docker build steps have no network, so choose a base image
that already contains needed packages or enable `--allow-container-network`.
`run_command` executes one Linux shell command in the active prepared image (or
the configured fallback), and an optional `image` can override the image for a
single command. It mounts only the isolated task worktree as writable and is
capped at 2 GiB RAM, 2 CPUs, 128 processes, and five minutes. Project dependency
caches persist under `.sigma-agent/container-home`. Use the tool's
`purpose=test` for test commands so the runtime can require a fresh test pass
after workspace changes. The CLI's existing `default_tool_permission` and
`tool_permissions` policies can deny `run_command` and
`prepare_environment` when command execution is not wanted.

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

Keep durable, cross-task repository facts in `.sigma-agent/MEMORY.md`, one entry per line. The file is the source of truth and is not injected wholesale into the model prompt:

```markdown
- 2026-10-30 | kind: testing | source: docs/testing.md | Run focused package tests before the full Go suite.
- 2026-10-30 | kind: architecture | source: docs/ARCHITECTURE.md | The HTTP service delegates each task to a Sigma Agent process and persists its session by task ID.
```

Supported kinds are `testing`, `architecture`, `compatibility`, `pitfall`, and
`workflow`. Keep facts short and repository-specific: capture decisions,
environment constraints, or recurring pitfalls that are expensive to
rediscover and cannot be reliably inferred from the current source. Do not copy
source code, archive task transcripts, store secrets, or treat memory as a
replacement for `AGENTS.md`, README files, tests, or the code itself. The legacy
`date | source | fact` format is still accepted as `kind: general`.
When vector search is configured, run `--index-memory --repo <path>` after
updating entries so the derived Qdrant payloads include the category and new
content hash. Local BM25 always reads the current Markdown file directly.

When memory is retrieved, Sigma Agent labels entries reviewed more than 180 days
ago as `revalidation_required`. The Agent is instructed to verify applicable
facts against the current checkout and tests before using them. Current source,
repository instructions, and the user's request take precedence over memory.

`memory_auto_write` is an optional post-task capture step. It runs only after a
non-interactive run returns successfully; failed, cancelled, or interrupted
runs do not write. The same model receives a bounded evidence bundle containing
the task, patch, test output, and final notes, and may return up to three short
facts. The writer validates categories and source paths, removes common secret
patterns, deduplicates exact facts, and enforces the 16 KiB memory-file limit.
The memory Markdown is updated atomically and excluded from the task patch. The
extra model call consumes tokens. If Qdrant or the embedding endpoint is
unavailable, the Markdown entry remains available to local BM25. Automatically
captured notes are model-generated and may be wrong; verify them before relying
on them. Keep this setting disabled in controlled memory/no-memory evaluations
so one task cannot alter another task's memory condition.

Agent tasks run in detached Git worktrees, so this file must be present in the
task's starting commit for `search_memory` to be registered. Vector results
are validated against the current Markdown entry hashes before use.

When vector settings are configured, run `--index-memory --repo <path>` to sync
only repository memory facts into the `sigma_agent_memory` collection. Source files are
not embedded or stored in Qdrant. The Agent keeps ordinary `search_code`,
`list_files`, and `read_file` tools for current-code exploration.

The `search_memory` tool retrieves a small, repository-filtered set of relevant
entries. With embeddings and Qdrant available, it fuses BM25 and semantic vector
ranks using reciprocal-rank fusion (RRF); without them, it uses local BM25.
Changed or removed Markdown entries are synchronized on the next index run;
stale vector hits are discarded by content hash. Vector failures or empty
results fall back to local BM25.

The embedding endpoint receives repository memory facts during indexing and
queries during search; Qdrant stores their vectors and provenance payloads.
Configure both services only for data permitted to leave or enter those
systems. The current repository identity is local to a Git common directory, so
separate clones do not share one memory namespace automatically.

Persist local embedding endpoint settings in the Git-ignored
`.secrets/embedding-config.json` (`provider`, `model`, and `base_url`); keep the
credential in `.secrets/embedding-api-key`. Environment variables
`SIGMA_AGENT_EMBEDDING_PROVIDER`, `SIGMA_AGENT_EMBEDDING_MODEL`,
`SIGMA_AGENT_EMBEDDING_BASE_URL`, and `SIGMA_AGENT_EMBEDDING_API_KEY` override
the local settings when set. For Hugging Face, `HF_TOKEN` can be used instead
of the embedding API key. Qdrant defaults to `http://localhost:6333`; override
it with `SIGMA_AGENT_QDRANT_URL` when needed.
Start the existing Qdrant service and index a repository with:

```powershell
docker compose up -d qdrant
$env:SIGMA_AGENT_EMBEDDING_PROVIDER = 'huggingface'
$env:SIGMA_AGENT_EMBEDDING_MODEL = '<Hugging-Face-model-id>'
$env:HF_TOKEN = '<token>'
go run ./cmd/sigma-agent --index-memory --repo C:\path\to\repository
```

To compare BM25, vector, and hybrid ranking against the checked-in labeled
memory query set, use its isolated fixture corpus:

```powershell
go run ./cmd/sigma-agent --eval-memory --repo .\evals\fixtures\memory-retrieval-v1
```

The evaluation indexes only the fixture's six reviewed memory facts using the
configured real embedding endpoint and Qdrant. It reports Recall@K and MRR for
BM25, vector, and RRF rankings. The fixture checks retrieval behavior; it does
not show that memory improves issue resolution. That requires separate matched
Agent runs on held-out issues.

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

Sigma Agent automatically enables GitHub MCP when it finds credentials. It
checks the saved user configuration first, then
`GITHUB_PERSONAL_ACCESS_TOKEN`, then `.secrets/github-pat` relative to the
current working directory. Keep the fallback token in that local file;
`.secrets/` is ignored by Git. The GitHub MCP server runs in Docker with the
read-only `issues`, `repos`, and `pull_requests` toolsets unless scoped PR
creation is explicitly enabled. Start Sigma Agent from `go-runtime` to find the
project-local fallback credential. A helper can create or replace the file
without echoing the token:

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
