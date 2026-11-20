# Sigma Agent user guide

This guide covers Sigma's local interactive TUI: configuring a model, starting a task in the current folder, continuing a session, and changing runtime permissions. Git is optional. Ask Sigma in natural language for local status, diff, repository initialization, staging, or commits; remote operations such as push and pull remain terminal tasks. The HTTP task API and low-level settings have separate guides linked at the end.

## 1. Start Sigma

After cloning the repository, build and install the command from the `go-runtime` directory:

```powershell
$binDir = Join-Path (go env GOPATH) 'bin'
go build -o (Join-Path $binDir 'sigma.exe') ./cmd/sigma-agent
sigma --version
```

Go does not add `GOPATH\bin` to Windows `PATH` automatically. To use `sigma`
from any folder in the current PowerShell window, add the install directory to
that window's path after building:

```powershell
$env:Path = "$binDir;$env:Path"
sigma --version
```

For future terminals, add the same `$binDir` (normally `%USERPROFILE%\go\bin`)
to your Windows user `Path` once. After that, open a new PowerShell window and
run `sigma` from the repository you want Sigma to work in. If you do not want
to change `Path`, run it by its full path: `& (Join-Path $binDir 'sigma.exe')`.

You can also build an executable once and run it from the same working directory:

```powershell
go build -o sigma.exe ./cmd/sigma-agent
.\sigma.exe
```

The installed command starts the integrated setup screen in the current directory and can be run from any folder. Sigma needs an API key for one supported model provider. Host command execution is the default and does not require Docker; set `command_execution_mode` to `docker` or pass `--command-execution-mode docker` to run project shell commands in containers. Git is optional. Go 1.26+ is needed to build from source, not for each launch. Sigma works on the folder where you run it; change folders with `cd`. Rerun the build command from the checkout to update `sigma.exe`. Press `Enter` to submit a task or setup command; use `Shift+Enter` to insert a line break.

## 2. Configure the model

In the TUI setup screen, configure a provider key. For example:

```text
/login
```

Choose MiniMax in the credential picker, then paste the key. You do not need to add the provider name to `/login`.

Sigma masks the key while you type and saves it in the per-user configuration directory, outside the repository. On Windows the file is `%APPDATA%\sigma-agent\model-auth.json`; credentials in this file are plaintext, so protect your Windows account and do not copy the file into the repository. For scripts or managed environments, use the provider's environment variable instead.

Supported providers:

| Provider ID | API protocol | Environment variable |
| --- | --- | --- |
| `minimax` | Anthropic Messages-compatible | `MINIMAX_API_KEY` |
| `anthropic` | Anthropic Messages | `ANTHROPIC_API_KEY` |
| `openai` | OpenAI Chat Completions-compatible | `OPENAI_API_KEY` |
| `deepseek` | OpenAI Chat Completions-compatible | `DEEPSEEK_API_KEY` |
| `openrouter` | OpenAI Chat Completions-compatible | `OPENROUTER_API_KEY` |

In TUI, type `/model`, choose a provider with Up/Down, then choose a model from the provider catalog below the input:

```text
/model
/model save
```

The provider catalog is fetched using its configured API key. If no key is configured, use `/login` first. `/model save` makes the current selection the default for new sessions. Without an explicit model selection, Sigma uses its saved default; on a fresh configuration its built-in default is `minimax/MiniMax-M3.1-Flash`.

During an interactive coding session, `/model` opens the same provider and model suggestions; `/model save` saves that selection as the default for new sessions.

For a one-shot task or a resumed session, pass `--model <provider>/<model-id>`
to override the saved selection and the model recorded in the session. This is
useful when continuing a session after a provider quota interruption.

## 3. Start a task in the current folder

Sigma uses the current folder directly, like Pi. It does not ask you to select
a repository or require Git. Use `cd C:\work\target-folder` before running
Sigma to choose a different working folder. Agent edits appear there
immediately. Ask Sigma to perform local Git operations when you want them;
Sigma does not initialize a repository or commit unless you ask.

Sigma opens an interactive session automatically. Slash commands are available from the input immediately; type a task and press `Enter`, or use `Shift+Enter` to add lines:

```text
Fix issue #123. Inspect the relevant code and repository instructions, implement the smallest appropriate fix, run the relevant tests, and review the complete diff. Do not commit or push.
```

All new tasks use the current folder as their workspace. Sigma's file
tools edit it directly, and host-mode commands run from that directory with
the current user's permissions. Optional Docker mode mounts the same folder
at `/workspace` for commands and tests. Local model API calls run from the
Sigma process.
Uncommitted changes need no copying; they are already in the workspace.

## Integrated TUI

Run `sigma --tui` to use Sigma in one Bubble Tea interface. Sigma switches to a full-screen TUI and restores the previous terminal screen on exit. A session starts automatically; the same command suggestions are available from the input at startup and in chat. `Enter` sends and `Shift+Enter` inserts a line break; `/tree`, `/versions`, `/restore`, `/diff`, `/model`, `/session`, `/context`, `/memory`, `/new`, `/fork`, `/clone`, `/steer`, `/followup`, and `/abort` are handled in the TUI. `/new` saves the current session and begins another; `/session` shows session details; `/context` separates loaded project instructions, the current task checkpoint, and long-term project memory; `/memory` lists dated facts with stable content IDs and `/memory forget <id-prefix>` removes exactly one selected fact from its configured source. If vector memory is configured, the command refreshes that index after deletion. Enter `/login` and press `Enter` to open the credential picker, choose GitHub or a model provider, then paste/type the credential and press `Enter` to save. Credential input is masked; press `Esc` to cancel.

TUI shortcuts: `Enter` submits or accepts the highlighted suggestion, `Shift+Enter` inserts a line break, `Ctrl+L` opens the model selector, `Ctrl+R` opens the saved-session selector during setup, `Tab` completes file paths (`@path` in prompts), `Up`/`Down` navigate command/model suggestions or browse prompt history, `PageUp`/`PageDown` or the mouse wheel scroll the transcript, and `Ctrl+O` toggles expanded tool output.

In Docker mode, interactive sessions reuse one foreground container; one-shot `--task` / `--resume` runs create one container per command. Background `action=start` also requires Docker mode. `/quit` removes the interactive container while keeping Docker images and `.sigma/container-home` caches for the next session. Use `/settings` to change Docker network access. Non-interactive runs can set `allow_container_network` in `config.json` or pass `--allow-container-network`. These settings do not restrict host mode, Sigma's model requests, or Docker image pulls. See [Docker command environments](DOCKER.md) for details.

## 4. Setup commands

| Command | What it does |
| --- | --- |
| `/login` | Open a picker for GitHub or a model provider, then securely enter its credential. |
| `/login` | Open the credential picker for a model provider or GitHub, then securely enter its key. |
| `/login github` | Save a GitHub token for GitHub MCP. |
| `/model` | Choose a provider and model from the suggestions below the input. |
| `/model save` | Save the selected provider/model as the default for new sessions. |
| `/logout <provider>` | Remove a saved model key. Environment variables can still provide credentials. |
| `/logout github` | Remove the saved GitHub token. |
| `/session <id>` | Set an explicit ID for a new session. |
| `/resume <id>` | Resume a saved session. Use the same repository as the original session. |
| `/settings` | Change saved runtime settings, including Docker network access and context-compression settings applied before the next user request. |
| `/help` | Show setup commands. |
| `/quit` | Save and close Sigma. |

For GitHub issue context, configure `/login github`. The GitHub MCP integration is read-only. The built-in `git` tool handles local status, diff, init, staging, and commits when requested. Use your terminal for remote operations such as push and pull.

## 5. Interactive session commands

Once a session starts, enter normal text at `You>` to start a task. While the Agent is running, enter plain text or `/steer <guidance>` to queue guidance for the next safe turn boundary. Use `/followup <instruction>` to queue another task after the active request settles.

| Command | What it does |
| --- | --- |
| `/help` | Show interactive-session commands. |
| `/model` | Show or change the model between turns; `/model save` saves the default. |
| `/session` | Show the current session ID. |
| `/context` | Show loaded project instructions, the current session checkpoint, and long-term memory source, scope, entry count, capacity, and automatic-write setting. |
| `/memory` | List long-term project facts with stable content IDs, category, source, date, and revalidation status. |
| `/memory remember [kind] <fact>` | Save a preference, correction, or durable project fact to `.sigma/MEMORY.md` immediately. The kind defaults to `preference`. |
| `/memory forget <id-prefix>` | Remove one selected fact from the active Markdown memory source and synchronize configured vector memory. Use at least the first 8 characters of its displayed ID. |
| `/new` | Save the current session and return to setup to start a fresh one. |
| `/diff` | Show a summary of changes in the active repository, compared with its starting commit. This includes pre-existing local changes. |
| `/tree` | In the TUI, open the session history picker and choose a point to continue from. The previous history remains available as another branch; the terminal CLI prints the tree. |
| `/tree <entry-id>` | Continue from a specific saved conversation entry. Find the ID in `/tree`. |
| `/versions` | List code checkpoints captured for this session. Requires a Git repository with a commit. |
| `/restore <checkpoint-id>` | Preview changed files for a checkpoint. This also saves the current workspace as a recovery checkpoint. |
| `/restore <checkpoint-id> confirm` | Restore the checkpoint after preview. The current workspace is checkpointed first; the branch and Git index are left unchanged. |
| `/steer <guidance>` | Queue guidance while an Agent turn is active. Plain text during an active turn does the same. |
| `/followup <instruction>` | Queue a new instruction to run after the active task settles. |
| `/abort` | Cancel the active Agent turn while keeping the session open. |
| `/quit` | Stop the active turn if needed, save the session, and exit. |

When a run exits, Sigma prints its session ID, snapshot path, run-artifact path, and active repository path. Use that session ID in `/resume <id>` at the next setup prompt. A session has an OS-backed exclusive lease while Sigma is using it: a second process cannot resume and write the same session until the first process exits. Sessions created by older versions that used a worktree continue to resume from their saved workspace. There is no `/sessions` listing command in the current CLI.

The model-facing `search` tool has separate scopes for `session_history` (the current conversation), `prior_sessions` (older saved transcripts for the same repository), and `memory` (curated durable project facts). Prior-session search scans saved session snapshots with BM25 lexical ranking and returns source session IDs, timestamps, and message indices. It does not search other repositories or use vector embeddings; use distinctive phrases, paths, symbols, or error text for best results. Session snapshots are stored under `.sigma/sessions` relative to the directory where the Sigma process starts, not inside the selected project by default. For example, starting Sigma from `C:\dev\Agent\coding-agent-harness\go-runtime` stores them under that runtime's `.sigma\sessions`; starting it from a project folder stores them under that project's `.sigma\sessions`. Each session is a `<session-id>.json` snapshot plus a `<session-id>\` directory for metadata, logs, artifacts, and checkpoints. `--serve` uses the service's working directory in the same way.

These stores have different purposes: the session snapshot is the complete transcript and recovery state; `prior_sessions` is an on-demand lexical search over saved transcripts; `.sigma/MEMORY.md` is the small, reviewed set of durable project facts. Today, prior-session search scans the saved snapshots each time and matches only sessions whose metadata names the same repository path. It is useful for recovering an old decision or exact detail, but it is not yet a curated, repository-portable long-term memory system.

The `--serve` task service also takes an OS-backed lock for each canonical
workspace. Tasks targeting the same directory wait in the service queue;
different repositories or Git worktrees can run concurrently. This protects
shared working files from overlapping tasks and does not prevent ordinary
independent Sigma processes from using separate workspaces.

## 6. Use Agent Skills

Sigma can load portable Agent Skills from project and user directories:

| Scope | Directory |
| --- | --- |
| Project | `.agents/skills/` and `.pi/skills/` |
| User | `~/.agents/skills/` and `~/.pi/agent/skills/` |
| Package | the `skills/` directory of an installed package |

Directories containing `SKILL.md` are discovered recursively. In `.agents/skills/`, root Markdown files are ignored while nested Markdown files with valid Skill frontmatter can also be Skills. For compatibility with Pi, `.pi/skills/` and `~/.pi/agent/skills/` accept standalone root Markdown Skills. If project and user Skills share a name, the project Skill takes precedence. A Skill contributed by an installed package is searched last and is bounded by its package directory, so it can only read reference files that ship with that package.

Example Skill layout:

```text
.pi/skills/go-issue-review/
├── SKILL.md
└── references/
    └── test-matrix.md
```

Example frontmatter:

```yaml
---
name: go-issue-review
description: Review Go issue fixes. Use for repository issue repair and regression testing.
license: MIT
compatibility: Requires Go and Docker.
disable-model-invocation: false
user-invocable: true
---
```

`name` and `description` are required. `license` and `compatibility` are optional. `disable-model-invocation: true` hides the Skill from automatic matching; `user-invocable: false` prevents explicit `/skill:` invocation. Other frontmatter is tolerated, but Pi's `allowed-tools` permission behavior is not implemented. Sigma's normal tool permission policy remains authoritative.

Sigma does not put the full Skill catalog into every prompt. It tells the model to use `search` with `scope=skills` and task or technology keywords; the router returns up to ten ranked name/description matches using local lexical search. The model calls `read_skill` with the exact name to load a `SKILL.md`, or supplies a Skill-relative `path` to load one referenced text file. Both operations use the same tool entry. Resource reads stay inside the Skill directory, reject symlinks and path traversal, support text formats such as Markdown, JSON, YAML, TOML, CSV, XML, HTML, and SVG, and cap files at 32 KiB. Search is lexical metadata retrieval, not semantic embedding search; if results are weak, try different task/framework terms.

When the user explicitly asks Sigma to preserve reusable workflow guidance as a Skill, it can call `create_skill` to create a project Skill at `.agents/skills/<name>/SKILL.md`. It validates the name, description, format, and current 12 KiB file limit; it refuses to overwrite an existing Skill. The new Skill is discoverable after `/reload` or a session restart. General file tools remain unable to edit Skill directories.

Sigma does not execute Skill scripts when loading a Skill. A loaded Skill may describe scripts, but reading it does not launch them; commands still have to go through the normal Agent tools and tool-permission controls (and Docker controls when Docker mode is selected). Standalone root Markdown Skills have no isolated companion directory, so `read_skill` cannot load companion resources for them. Skill search, reading, and creation tools are registered whenever `skills_enabled` is true; with an empty catalog, search returns no matches and reads reject unknown names. A newly created Skill becomes discoverable after `/reload` or a session restart. Set `skills_enabled` to `false` to disable project and user Skill discovery.

Sigma can install npm, git, and local packages that contribute Skills, MCP
servers, JavaScript workflow extensions, and package-declared command tools.
New package tools are discovered when Sigma starts the next run. See [packages
and MCP servers](PACKAGES.md).

## 7. Other ways to run Sigma

- **One task:** change into the folder with `cd C:\work\target-repository`, then run `sigma --task "Fix issue #123"`.
- **Isolated Git task:** run `sigma --isolated --task "Fix issue #123"` from a Git repository with a commit. Sigma creates a detached Git worktree for this session, prints its path, and keeps it for resume. Resume from the same Sigma runtime directory with `sigma --repo C:\work\target-repository --resume <session-id>`. The worktree starts at the current commit, so uncommitted changes in the original folder are not copied into it. Remove the worktree only after you have saved any changes you need.
- **Session work:** add `--list-sessions` to list saved sessions for the current repository, `--session-diff <session-id>` to print a session's patch, or `--export-patch <session-id>=<path>` to save it. These commands also work for sessions created directly in the repository.
- **Publish an isolated session:** explicitly run `sigma --repo C:\work\target-repository --publish-session <session-id> --publish-branch fix/issue-123 --draft-pr-title "Fix issue 123" --draft-pr-body "Summary and test results"`. Sigma commits the isolated worktree, pushes that feature branch to `origin`, then opens a draft PR on GitHub. Add `--draft-pr-base main` if the origin default branch cannot be detected. Publishing is a separate command and the built-in GitHub MCP stays read-only during normal coding sessions.
- **JSONL process integration:** Add `--json` to emit versioned lifecycle events and a final result on stdout. This mode is one-shot; resume later with `--resume <session-id>`.
- **HTTP/SSE task service:** Start with `sigma --serve --max-concurrent 2`. Use the API examples and endpoint reference in [architecture](ARCHITECTURE.md).

The HTTP service currently has no authentication and is intended for local use. It works directly in the selected repository and has no Sigma-specific Git publishing workflow. See [configuration](CONFIGURATION.md) for all runtime settings, environment variables, credential precedence, Docker environment preparation, Skills, and repository memory.
