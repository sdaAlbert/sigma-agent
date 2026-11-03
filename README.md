<p align="center">
  <img src="docs/assets/sigma-agent-mark.svg" alt="Sigma Agent logo" width="112">
</p>

<h1 align="center">Sigma Agent</h1>

<p align="center">
  <a href="README.md">English</a> · <a href="README.zh-CN.md">简体中文</a>
</p>

Sigma is a Go coding agent for working on issues in existing repositories. It connects a model to repository tools, runs project commands in Docker, and guides a change through inspection, implementation, testing, and diff review. Tasks run in detached Git worktrees so the original checkout stays untouched.

## What makes Sigma different

- **A Go-first agent runtime:** Sigma implements its own model/tool loop and task runtime in Go. It builds as a native executable and fits naturally into Go-based backend environments; Docker is used separately to isolate repository commands.
- **A complete repository workflow:** The CLI combines file inspection and edits, shell commands, tests, Git diff review, persistent sessions, resume, and optional publishing in one issue-focused workflow.
- **Backend-oriented execution controls:** The runtime includes task persistence, bounded task concurrency, SSE events, cancellation, and resume APIs, alongside the interactive CLI.
- **Optional repository knowledge:** Skills load task guidance on demand. Human-reviewed, repository-scoped memories can be retrieved with BM25 and optional Qdrant hybrid search. This is an engineering capability; Sigma does not claim that memory has already improved benchmark resolution rates.
- **Explicit external access:** GitHub MCP and publishing are opt-in, so repository access and write actions can be enabled deliberately.

Sigma takes inspiration from [Pi](https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent), especially its agent loop, tool use, and session workflow. Pi is a broader and more mature coding-agent project with several interaction modes and an extension ecosystem. Sigma explores a narrower question: what does a Go-native, repository-task runtime look like, and how can it expose backend-style execution controls? The Go implementation is a deployment and integration choice, not evidence that Sigma is more capable at solving coding tasks.

## Get started

Requirements: Go 1.26+, Git, Docker Desktop, and an API key for a supported model provider.

From the `go-runtime` directory, start Sigma:

```powershell
go run ./cmd/sigma-agent
```

At the setup prompt, use `/login minimax` to save a model key, `/model` to select a provider and model, and `/repo <path>` to choose a clean local checkout. Run `/help` to see the available commands. Provider keys can also be supplied through environment variables for scripted use.

## Work on a GitHub issue

Clone the target repository and make sure its working tree is clean. In Sigma's setup prompt, choose the repository and configure optional GitHub access:

```text
/repo C:\work\target-repository
/login github
/publish sigma/issue-123
/github-write on
```

Then describe the task. For example:

```text
Fix issue #123. Inspect the relevant code and project instructions, implement the fix, run appropriate tests, and review the complete diff. Then commit and push to sigma/issue-123 and open a pull request. Do not merge it.
```

`/publish` enables commit and push to the named feature branch. `/github-write on` enables pull-request creation through GitHub MCP. Git push uses your local Git credential helper; the GitHub token is used for MCP access. Docker network access starts disabled and can be enabled with `/network on` when dependency downloads are needed.

Sigma creates a detached task worktree and saves the patch and session without changing the original checkout. Use `/sessions` to find saved sessions and `/resume <id>` to continue one. In an interactive session, `/tree` lists conversation points and `/branch <entry-id>` continues from an earlier complete point. Publishing must be enabled again after resume.

## Scope and documentation

`run_command` executes repository-native build, test, and check commands inside an isolated Linux container. Sigma can inspect repository manifests and CI configuration to select a Docker environment, but projects with additional services or setup requirements may need manual preparation.

See [configuration](docs/CONFIGURATION.md) for providers, credentials, permissions, Docker, and runtime settings; [architecture](docs/ARCHITECTURE.md) for the runtime design; and [`evals/`](evals/) for benchmark protocols and evaluation notes.
