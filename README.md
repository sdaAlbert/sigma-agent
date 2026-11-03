# Sigma Agent

Sigma Agent is an experimental coding-agent runtime written in Go, inspired by ideas from Pi Agent. It gives a model a bounded set of repository tools and runs coding tasks against a local Git worktree.

Current capabilities include persistent sessions, context compaction, source browsing and editing, safe parallel edits to different files, Docker-isolated Go test profiles, agent-selected and reusable Docker environments for repository commands, session-persistent background command jobs, project Skills, repository-scoped long-term memory with optional embedding and Qdrant hybrid search, and a read-only investigation subagent.

## Quick start

Requirements: Go 1.26+, Git, Docker Desktop, and an API key from one of the supported model providers (MiniMax, Anthropic, OpenAI, DeepSeek, or OpenRouter).

```powershell
$env:MINIMAX_API_KEY = "your-api-key"
go run ./cmd/sigma-agent
```

Sigma opens a setup prompt. Use `/repo <path>` to select a checkout, `/login`
to save a model API key, and `/model` to select a model. Configure optional
session, network, and GitHub publishing settings with slash commands, then type
a task to start. You can also run Sigma from inside the target Git repository
and it will select the current directory by default. Type `/help` at the setup
prompt to see the available commands.

For configuration, credentials, optional Qdrant setup, and CLI options, see [CONFIGURATION.md](CONFIGURATION.md) and [ARCHITECTURE.md](ARCHITECTURE.md).

## Run Sigma on a GitHub issue

Sigma Agent runs as a standalone CLI from your terminal; Codex is not required. It takes a local Git clone, creates a detached worktree for the task, reads/edits that worktree, runs commands and tests in Docker, and saves a patch and session transcript. The original checkout is left untouched.

### One-time setup

Install Go 1.26+, Git, and Docker Desktop. Start Docker Desktop, then configure your model key and GitHub token from this directory:

```powershell
Set-Location C:\path\to\sigma-agent\go-runtime
.\scripts\set-minimax-key.ps1
.\scripts\set-github-token.ps1
```

The GitHub token is used to read issues and pull requests. For PR creation it must also have permission to create pull requests in the target repository. Git push uses your local Git credential helper (for example, Git Credential Manager); the MCP token is not used for `git push`.

### Run an issue repair

Clone the target repository locally and make sure its working tree is clean. Launch Sigma from this directory:

```powershell
go run ./cmd/sigma-agent
```

At the `Sigma>` setup prompt, configure the repository and optional permissions. Publishing and network access are off unless enabled:

```text
/repo C:\work\target-repository
/session issue-123
/network on
/publish sigma/issue-123
/github-write on
```

Configure a model in the same prompt. `/login minimax` reads the API key without
echoing it and stores it in your user configuration directory. `/login github`
does the same for the GitHub token. `/model` lists
supported providers and reports whether a key is available; choose a provider
and model ID with `/model minimax/MiniMax-M3` or `/model openai/<model-id>`.
Use `/model save` to make the current selection the default for new sessions,
and `/logout <provider|github>` to delete a saved credential. The first adapters support
MiniMax and Anthropic Messages, plus OpenAI Chat Completions-compatible OpenAI,
DeepSeek, and OpenRouter endpoints. Sigma does not automatically download a
model catalog; enter the model ID accepted by your provider.

Then enter the task directly, or type `/start` to open an empty conversation first. For example:

```text
Fix issue #123 in this repository. Read the issue and relevant code, inspect the project setup and dependency manifests, install only the dependencies needed to validate the change, implement the fix, run the relevant tests, inspect the complete diff, then commit and push to sigma/issue-123 and open a pull request. Do not merge the pull request.
```

Sigma can read the issue through GitHub MCP, inspect repository files, edit code, and use `run_command` for project-specific Linux commands. Ask it to inspect the repository's Dockerfile/devcontainer, CI workflows, README, and dependency manifests, then prepare a matching environment before testing. `prepare_environment` accepts any Docker base image and optional setup command; Sigma builds and caches a derived image so compiler and system-package installs persist between commands and session resumes. Docker pulls missing images on first use. If network access is enabled, Sigma can run project dependency commands such as `go mod download`, `npm ci`, `pip install`, Maven, or Cargo. Go's `run_tests` tool downloads modules before running tests in this mode. The worktree, package-manager caches, and project-local environments (for example `node_modules` or `.venv`) persist between commands.

After it finishes, inspect the reported patch, test results, commit, and pull request. To resume an interrupted session, start Sigma, select the same repository, then enter `/resume issue-123`. Re-enable `/publish sigma/issue-123` and `/github-write on` if you want the resumed session to publish. A model switch made during the session is stored in its transcript and restored when resuming.

### What it can prepare automatically

- **Project-level package dependencies:** when `--allow-container-network` is enabled, the Agent can use the package manager already present in the configured Docker image and can save generated dependencies/environments inside the task worktree.
- **Language toolchains and system packages:** the Agent selects any suitable Docker image after inspecting repository instructions and CI, then calls `prepare_environment` to install missing tools, headers, and OS packages into a derived cached image. No fixed language list is required. Maven, Cargo, pip, npm, and Go package caches persist in `.sigma-agent/container-home` between command containers.
- **Go modules:** `run_tests` runs `go mod download` before tests when network access is enabled.
- **Repository-native checks:** use `run_command` for commands such as `make test`, `ctest`, `mvn test`, `npm test`, `pytest`, `cargo test`, or project-specific scripts. `prepare_environment` persists system packages in its derived image, so the same configured environment is used for subsequent build and test commands.

It cannot install software on your host machine, access the Docker socket from inside the command container, or automatically launch a sibling Docker Compose database/service stack. System packages and service containers must be supplied by the configured image or prepared separately. Dependency installers execute repository/package scripts and can contact external hosts, so network access is opt-in and disabled by default. It does not guarantee arbitrary repositories will build or tests will pass.

### GitHub submission boundaries

- `/publish <name>` enables local Git commit and push tools for one new branch on `origin`; pushing uses your local Git credentials and never force-pushes. `/publish off` disables publishing.
- `/github-write on` requires a configured GitHub token and a publish branch, and exposes only the GitHub `create_pull_request` write tool. Sigma restricts the PR to the current `origin` repository and configured head branch. It cannot merge the PR or write to issues.
- `/network on|off` controls internet access for Docker build, test, and command containers. It starts off unless the runtime config enables it.
- The equivalent startup flags remain available for scripted or non-interactive use; interactive sessions can configure these options at the `Sigma>` setup prompt.
- Sigma creates/updates remote state only when the task explicitly requests it and the corresponding tool was enabled. The HTTP service does not currently support Git publishing.
- Without these options, Sigma returns a patch for review and GitHub MCP remains read-only.

See [CONFIGURATION.md](CONFIGURATION.md) for Docker network behavior, tool permissions, token setup, and runtime settings.

## Project notes

- Local credentials, session data, run artifacts, and `.sigma-agent/MEMORY.md` are excluded from Git.
- Embeddings and Qdrant are optional; local BM25 memory retrieval works without them. Source code is not vector-indexed.
- The memory-retrieval and issue-resolution evaluation procedures are documented under [`evals/`](evals/).

## Design references

These public projects, documentation, and research papers informed different parts of Sigma Agent's design:

- **Agent runtime, sessions, compaction, and Skills:** [Pi Agent](https://pi.dev/docs/latest/), especially its [sessions](https://pi.dev/docs/latest/sessions), [compaction](https://pi.dev/docs/latest/compaction), and [Skills](https://pi.dev/docs/latest/skills) documentation.
- **Memory and context management:** [Claude Code memory](https://code.claude.com/docs/en/memory) and [context management](https://code.claude.com/docs/en/costs#manage-context-proactively). Sigma Agent retrieves repository memory on demand and can optionally capture up to three durable notes after a successful task; context compaction remains extractive.
- **Persistent project memory:** [OpenHands persistent memory](https://github.com/OpenHands/docs/blob/main/sdk/guides/persistent-memory.mdx), [LangGraph memory and Store](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx), and [Mem0](https://github.com/mem0ai/mem0).
- **Memory retrieval and writeback:** [Mem0](https://github.com/mem0ai/mem0) informed the explicit memory-store/search boundary and scoped semantic retrieval; Sigma Agent keeps Markdown as the source of truth and Qdrant as a derived index.
- **Lifecycle integration:** [Pi extensions](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/extensions.md) informed the use of agent lifecycle boundaries for post-run behavior; Sigma Agent's memory capture is a separate, bounded CLI completion step.
- **Coding-memory evaluation:** [VibeMemBench](https://github.com/AlibabaResearch/DAMO-ConvAI/tree/main/VibeMemBench) supplies coding tasks and an official patch evaluator. Sigma can be the solver in paired memory/control runs; see [`evals/vibemembench/`](evals/vibemembench/README.md).
- **General repository commands:** `run_command` executes repository-native commands inside an isolated Docker container. Containers have no network by default; `/network on` lets the Agent try package-manager downloads and lets Go tests download modules in interactive sessions.
- **Related code-search designs reviewed:** [Cursor codebase indexing](https://cursor.com/blog/secure-codebase-indexing) and [Sourcegraph Cody context](https://sourcegraph.com/docs/cody/core-concepts/context).
