# Runtime configuration

Sigma Agent reads `config.json` from the current working directory by default. Pass
`--config path/to/file.json` to use another file. Missing files use built-in
defaults. A file can contain only the settings that need changing.

```json
{
  "max_turns": 128,
  "max_tool_calls": 256,
  "max_repeated_failed_commands": 3,
  "max_context_tokens": 64000,
  "context_compaction_threshold_tokens": 30000,
  "summary_tokens": 800,
  "max_tool_result_bytes": 51200,
  "max_retrieval_query_bytes": 8192,
  "model_output_tokens": 16384,
  "model_reasoning_effort": "max",
  "tool_timeout_ms": 600000,
  "model_timeout_ms": 120000,
  "max_retries": 2,
  "command_execution_image": "debian:trixie-slim",
  "allow_container_network": false,
  "skills_enabled": true,
  "web_search_enabled": false,
  "default_tool_permission": "allow",
  "tool_permissions": {
    "apply_patch": "deny",
    "run_command": "allow"
  }
}
```

Optional project-context paths can be set in the same config. Relative paths are
resolved from the config file's directory. This lets benchmark runs load a
shared profile outside a target checkout without editing that checkout:

```json
{
  "project_instructions_file": "../../AGENTS.md",
  "memory_file": "../../.sigma-agent/profiles/my-benchmark/MEMORY.md",
  "memory_repository_id": "benchmark:owner/repository"
}
```

The external instructions file is added alongside any root `AGENTS.md` in the
task workspace. An external memory file is copied into the active repository
workspace's `.sigma-agent/MEMORY.md`; BM25 and Qdrant search use the configured
stable `memory_repository_id`, so separate clones of the same upstream repo can
share one memory namespace. The target checkout itself is not changed. If
`memory_auto_write` is enabled, successful runs append reviewed-format facts to
the configured external file and refresh its Qdrant index. Run metadata records
the external paths, instruction hash, memory scope, and starting memory hash.

| Setting | Default | Allowed range | Purpose |
| --- | ---: | ---: | --- |
| `max_turns` | 128 | 1–512 | Assistant/model turns allowed in one run |
| `max_tool_calls` | 256 | 1–4096 | Tool calls allowed in one run |
| `max_repeated_failed_commands` | 0 (disabled) | 0–20 | Pause the run after this many identical failed `run_command` calls without a workspace change; benchmark configs can set 3 to limit command loops |
| `max_context_tokens` | 64000 | 512–1000000 | Hard estimated context ceiling; proactive compaction normally uses the lower threshold below. If MiniMax explicitly rejects an oversized prompt, Sigma Agent compacts the request and retries up to three times |
| `context_compaction_threshold_tokens` | `min(30000, max_context_tokens)` | 0–`max_context_tokens` | Proactively compact older conversation history when estimated input exceeds this soft threshold. The context manager permits up to 256 estimated tokens above the threshold, capped by `max_context_tokens`; if the newest complete turn still cannot fit the soft budget, it can retain that turn up to the hard context ceiling after shrinking tool output. `0` selects the automatic default. |
| `summary_tokens` | 800 | 0 to less than compaction threshold | Maximum estimated size of summaries produced by automatic or model-requested context compaction |
| `max_tool_result_bytes` | 51200 | 1024–4194304 | Inline limit for each tool result, matching Pi's 50 KiB output budget. Larger results are saved as session artifacts (32 MiB per artifact, 128 MiB per session) and replaced by a preview/reference. Use `read_tool_output` with offsets to fetch long results in chunks. If artifact storage fails or reaches its cap, the inline result is truncated. |
| `max_retrieval_query_bytes` | 8192 | 500–65536 | Maximum UTF-8 bytes accepted by `search_memory` (legacy setting name) |
| `model_output_tokens` | 16384 | 256–65536 | Requested output limit for each model response; truncated responses get up to two continuation attempts |
| `model_reasoning_effort` | `max` | `low`, `medium`, `high`, `xhigh`, `max` | For MiniMax M3.1 Flash, sends `output_config.effort`. M3.1 Flash always thinks; lower effort can reduce latency and reasoning tokens. For DeepSeek, this value is sent as `reasoning_effort` only when `deepseek_thinking_enabled` is true. |
| `deepseek_thinking_enabled` | `false` | boolean | Enables DeepSeek Chat Completions thinking mode for non-TUI runs. In the TUI setup screen, `/thinking on` or `/thinking off` overrides this for the current TUI run. Sigma preserves returned `reasoning_content` in the session and sends it back on subsequent tool turns, as required by DeepSeek. |
| `tool_timeout_ms` | 600000 | 1000–1800000 | Timeout for each tool execution, including Docker environment image preparation |
| `model_timeout_ms` | 120000 | 1000–1800000 | HTTP timeout for a model request |
| `max_retries` | 2 | 0–10 | Retries for retryable transport/server failures, including HTTP 429 and 529 |
| `command_execution_image` | `debian:trixie-slim` | Linux Docker image reference | Fallback image used by `run_command` until the Agent prepares an environment or chooses an image for a command |
| `command_filesystem_scope` | `unrestricted` | `unrestricted` or `workspace` | In `workspace` mode, rejects explicit absolute paths outside `/workspace` and parent-directory traversal before starting a command. This is a command admission guard, not a complete OS sandbox against programs that construct paths dynamically |
| `allow_container_network` | `false` | boolean | Allow Docker command and environment-build steps to access the internet. Change the saved setting in the TUI `/settings` picker or set it in configuration; see [Docker command environments](DOCKER.md) |
| `skills_enabled` | `true` | boolean | Discover project `.agents/skills/` and `.pi/skills/`, plus user `~/.agents/skills/` and `~/.pi/agent/skills/`. Set to `false` for a matched no-Skill evaluation; repository memory stays enabled |
| `memory_enabled` | `true` | boolean | Enable repository memory context and `search_memory`. Set to `false` for a matched no-memory comparison; code browsing tools remain available |
| `memory_auto_write` | `false` | boolean | After a successful non-interactive run, use one bounded model call to extract up to three durable repository facts into `.sigma-agent/MEMORY.md`, then refresh Qdrant when configured. Requires `memory_enabled`; generated notes and extra tokens are recorded in `run.json`. Set it to `true` to opt in. |
| `project_instructions_file` | unset | absolute path after config resolution | Add an external Markdown instruction file to the session prompt without copying it into the target checkout. Maximum 16 KiB; `run.json` records its path and SHA-256. |
| `memory_file` | unset | absolute path after config resolution | Use an external reviewed memory source instead of the selected repository's `.sigma-agent/MEMORY.md`; it is loaded into the active repository workspace and can receive automatic memory writes. Maximum 16 KiB. |
| `memory_repository_id` | unset | 1–128 letters, digits, `.`, `_`, `:`, `/`, or `-` | Required with `memory_file`; stable Qdrant/BM25 scope shared by independent clones of one upstream repository. |
| `web_search_enabled` | `false` | boolean | Add MiniMax's hosted `web_search` as an explicit Agent tool. Uses provider credentials and quota. |
| `disable_configured_mcp_servers` | `false` | boolean | Skip every MCP server in user/project `mcp.json`; use `true` for issue-only benchmark runs. |
| `disable_workflow_extensions` | `false` | boolean | Skip user/project JavaScript workflow extensions, which can register additional tools. |
| `default_tool_permission` | `allow` | `allow` or `deny` | Permission applied to every registered tool unless overridden below. Set to `deny` to use an allowlist. |
| `tool_permissions` | `{}` | tool name → `allow` or `deny` | Exact per-tool overrides. Tool names match the names exposed to the model, including namespaced MCP tools. |

Tool permissions are checked after argument validation and before the handler runs. A denied call is not executed; the Agent receives a tool error and the JSONL trace records `tool_permission_denied`. This first version supports policy-based allow/deny only; it does not prompt for interactive approval.

In the TUI, `/trust` saves a per-folder decision under the user's Sigma configuration. Project-scoped JavaScript extensions and MCP servers stay disabled until that folder is trusted; use `/reload` after changing the decision. User-scoped integrations and project Skills remain available. Trust does not override `tool_permissions`, enable container networking, or grant operating-system permissions.

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

- Packages are installed with `sigma --install npm:<name>`, listed with
  `sigma --list-packages`, and recorded in a scope registry
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
configured tool policy. `sigma --list-mcp` connects to every enabled
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

Interactive TUI setup uses `/login` to open a credential picker, or `/login <provider>`
to enter one directly. It saves API keys or the GitHub token; `/model`
to show or select a provider/model. The initial API-key providers are MiniMax,
Anthropic, OpenAI, DeepSeek, and OpenRouter. MiniMax and Anthropic use the
Anthropic Messages protocol; OpenAI, DeepSeek, and OpenRouter use Chat
Completions-compatible endpoints. In the TUI, `/model` loads the chosen
provider's model catalog and displays it as keyboard suggestions below the
input. Use `/model save` to set the default for new sessions. `/logout <provider>` removes
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

Sigma's built-in `git` tool can inspect local status, diffs, history, and
branches, initialize the current workspace, stage selected paths, and commit
when requested. It runs on the Sigma host in the selected workspace, so Git
does not need to be installed in the Docker command image. The tool does not
contact remotes; use the terminal for push and pull. The GitHub MCP integration
is read-only and can provide issue, repository, and pull-request context.

See [Docker command environments](DOCKER.md) for the full lifecycle and cache
model. In brief, interactive foreground `run_command` calls reuse one named
container during a session. Sigma removes that container on graceful exit; the
selected Docker image, prepared image, mounted workspace, `.sigma-agent` state,
and `.sigma-agent/container-home` package caches remain available to the next
session. `prepare_environment` is optional and should be used only when the
project needs a missing system tool or library. An empty setup command only
selects an existing base image; a non-empty setup command builds a reusable
derived image. The default `debian:trixie-slim` already supports simple shell
tasks, which do not need environment preparation or dependency installation.

The interactive session container mounts the current workspace at `/workspace`
and is capped at 2 GiB RAM, 2 CPUs, 128 processes, and five minutes per command.
The default network blocks external egress. Change the saved network setting in
the TUI `/settings` picker when a task genuinely needs package downloads; this
does not change how the host Sigma process reaches the model API. The separate
background task container created by `run_command action=start` can continue
after Sigma exits and is managed through `action=list`, `action=status`, and
`action=stop`. The CLI's `default_tool_permission` and `tool_permissions`
policies can deny `run_command` and `prepare_environment` when command execution
is not wanted.

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

`memory_enabled` defaults to `true`. During an ordinary task run, Sigma exposes
`search_memory`; the Agent can retrieve relevant facts when useful. Local BM25
reads the Markdown directly, so no extra command or Qdrant service is needed
for the local-only path. Set `memory_enabled` to `false` to disable memory for
a run.

When vector search is configured, run `sigma --index-memory` from the workspace after
updating entries so the derived Qdrant payloads include the category and new
content hash. Local BM25 always reads the current Markdown file directly.

When memory is retrieved, Sigma Agent labels entries reviewed more than 180 days
ago as `revalidation_required`. The Agent is instructed to verify applicable
facts against the current checkout and tests before using them. Current source,
repository instructions, and the user's request take precedence over memory.

`memory_auto_write` is an opt-in post-task capture step. It runs only after a
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

Agent tasks use the current working folder, so this file must be present there
for `search_memory` to be registered. Vector results
are validated against the current Markdown entry hashes before use.

When vector settings are configured, run `sigma --index-memory` from the workspace to sync
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
cd C:\path\to\workspace
go run ./cmd/sigma-agent --index-memory
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
Set-Location C:\path\to\sigma-agent\go-runtime
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
Set-Location C:\path\to\sigma-agent\go-runtime
.\scripts\set-minimax-key.ps1
```

The helper does not print the key. Do not commit or share the file.
