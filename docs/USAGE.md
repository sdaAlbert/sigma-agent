# Sigma Agent user guide

This guide covers the local interactive CLI: configuring a model, selecting a repository, starting a task, continuing a session, and changing runtime permissions. The HTTP task API and low-level settings have separate guides linked at the end.

## 1. Start Sigma

From the `go-runtime` directory, run:

```powershell
go run ./cmd/sigma-agent
```

You can also build an executable once and run it from the same working directory:

```powershell
go build -o sigma-agent.exe ./cmd/sigma-agent
.\sigma-agent.exe
```

The first command starts the setup prompt. Sigma needs Go 1.26+, Git, Docker Desktop, and an API key for one supported model provider. Run `/help` at the setup prompt to display the available setup commands.

## 2. Configure the model

At the `Sigma>` setup prompt, configure a provider key. For example:

```text
/login minimax
```

Sigma asks for the key using a hidden input prompt. It saves credentials in the per-user configuration directory, outside the repository. On Windows the file is `%APPDATA%\sigma-agent\model-auth.json`; credentials in this file are plaintext, so protect your Windows account and do not copy the file into the repository. For scripts or managed environments, use the provider's environment variable instead.

Supported providers:

| Provider ID | API protocol | Environment variable |
| --- | --- | --- |
| `minimax` | Anthropic Messages-compatible | `MINIMAX_API_KEY` |
| `anthropic` | Anthropic Messages | `ANTHROPIC_API_KEY` |
| `openai` | OpenAI Chat Completions-compatible | `OPENAI_API_KEY` |
| `deepseek` | OpenAI Chat Completions-compatible | `DEEPSEEK_API_KEY` |
| `openrouter` | OpenAI Chat Completions-compatible | `OPENROUTER_API_KEY` |

Then inspect provider/key status and choose a model:

```text
/model list
/model minimax/MiniMax-M3
/model save
```

`/model list` lists supported providers and whether a key is configured; it does not fetch a live model catalog. Enter the model ID accepted by your provider. `/model save` makes the current selection the default for new sessions. Without an explicit model selection, Sigma uses its saved default; on a fresh configuration its built-in default is `minimax/MiniMax-M3`.

During an interactive coding session, `/model` displays the current selection and provider/key status. `/model <provider>/<model-id>` switches models between turns; `/model save` saves that selection as the default for new sessions.

## 3. Select a repository and start a task

Choose a local Git repository with `/repo`. The worktree should be clean before starting:

```text
/repo C:\work\target-repository
```

You can type a task directly at the setup prompt to start immediately, or use `/start` to open an empty interactive session and enter tasks at `You>`:

```text
Fix issue #123. Inspect the relevant code and repository instructions, implement the smallest appropriate fix, run the relevant tests, and review the complete diff. Do not commit or push.
```

Sigma creates a detached Git worktree for the task. Its edits remain separate from the original checkout. Docker is used for repository commands and test execution; local model API calls run from the Sigma process.

If the task requires downloading project dependencies, Docker networking is off by default. In setup, opt in for the session with:

```text
/network on
```

Use `/network off` to disable it. Enabling network access allows container commands and package install scripts to contact external services; see [configuration](CONFIGURATION.md) for details.

## 4. Setup commands

| Command | What it does |
| --- | --- |
| `/login <provider>` | Save a model API key; input is hidden. Providers: `minimax`, `anthropic`, `openai`, `deepseek`, `openrouter`. |
| `/login github` | Save a GitHub token for GitHub MCP. |
| `/model` or `/model list` | Show the selected model and provider/key status. |
| `/model <provider>/<model-id>` | Select a model for this session. |
| `/model save` | Save the selected provider/model as the default for new sessions. |
| `/logout <provider>` | Remove a saved model key. Environment variables can still provide credentials. |
| `/logout github` | Remove the saved GitHub token. |
| `/repo <path>` | Select a local Git repository. |
| `/session <id>` | Set an explicit ID for a new session. |
| `/resume <id>` | Resume a saved session. Use the same repository as the original session. |
| `/network on\|off` | Allow or block internet access inside Docker command/test containers for this session. |
| `/publish <branch>` | Opt in to commit and push to a new feature branch on `origin`. |
| `/publish off` | Disable publishing for this session. |
| `/github-write on\|off` | Enable or disable GitHub pull-request creation. Requires GitHub MCP credentials and `/publish <branch>`. |
| `/start` | Start an empty interactive conversation with the current settings. |
| `/help` | Show setup commands. |
| `/exit` | Leave setup without starting a task. |

For GitHub issue context, configure `/login github`. Reading issue/repository/PR context uses the GitHub MCP integration. Creating a PR requires `/publish <branch>` and `/github-write on`; it is limited to the configured `origin` repository and feature branch. Git push uses the machine's Git credential helper, not the GitHub MCP token. Sigma never merges the PR.

## 5. Interactive session commands

Once a session starts, enter normal text at `You>` to start a task. While the Agent is running, enter plain text or `/steer <guidance>` to queue guidance for the next safe turn boundary. Use `/followup <instruction>` to queue another task after the active request settles.

| Command | What it does |
| --- | --- |
| `/help` | Show interactive-session commands. |
| `/model` | Show or change the model between turns; `/model save` saves the default. |
| `/diff` | Show a summary of changes made in the task worktree. |
| `/tree` | Show the session's saved conversation entries and current branch. |
| `/branch <entry-id>` | Move to a prior complete conversation point and continue from there. Find the full ID with `/tree`. |
| `/steer <guidance>` | Queue guidance while an Agent turn is active. Plain text during an active turn does the same. |
| `/followup <instruction>` | Queue a new instruction to run after the active task settles. |
| `/abort` | Cancel the active Agent turn while keeping the session open. |
| `/exit` or `/quit` | Stop the active turn if needed, save the session, and exit. |

When a run exits, Sigma prints its session ID, snapshot path, run-artifact path, and task-worktree path. Use that session ID in `/resume <id>` at the next setup prompt. There is no `/sessions` listing command in the current CLI.

Publishing permissions are session-scoped. After resuming a session, use `/publish <same-branch>` again before asking Sigma to commit or push. GitHub PR creation also needs `/github-write on` again.

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

Sigma provides only each available Skill's name and description to the model initially. When relevant, the model calls `read_skill` to load `SKILL.md`. It can then call `read_skill_resource` with the exact Skill name and a relative path, such as `references/test-matrix.md`, to load a small text reference or template. The resource reader is restricted to that Skill directory, rejects symlinks and path traversal, and supports text formats such as Markdown, JSON, YAML, TOML, CSV, XML, HTML, and SVG. Files are capped at 32 KiB.

Sigma does not execute Skill scripts when loading a Skill. A loaded Skill may describe scripts, but reading it does not launch them; commands still have to go through the normal Agent tools and Docker/tool-permission controls. Standalone root Markdown Skills have no isolated companion directory, so `read_skill_resource` is available only to Skills loaded from a directory. Set `skills_enabled` to `false` to disable project and user Skill discovery.

Sigma can install npm, git, and local packages that contribute Skills and MCP servers. See [packages and MCP servers](PACKAGES.md).

## 7. Other ways to run Sigma

- **One task:** `go run ./cmd/sigma-agent --repo C:\work\target-repository --task "Fix issue #123"`
- **JSONL process integration:** Add `--json` to emit versioned lifecycle events and a final result on stdout. This mode is one-shot; resume later with `--resume <session-id>`.
- **HTTP/SSE task service:** Start with `go run ./cmd/sigma-agent --serve --max-concurrent 2`. Use the API examples and endpoint reference in [architecture](ARCHITECTURE.md).

The HTTP service currently has no authentication and is intended for local use. It does not expose GitHub publishing. See [configuration](CONFIGURATION.md) for all runtime settings, environment variables, credential precedence, Docker environment preparation, Skills, and repository memory.
