# Sigma Agent

Sigma is a Go coding agent for fixing issues in existing repositories. It gives a model tools to inspect and edit code, run the repository's own commands in Docker, and carry a change through verification. Work happens in a detached Git worktree, so the original checkout stays untouched.

## What Sigma focuses on

- **Issue to verified change:** Read a GitHub issue, explore the repository, make a patch, run relevant checks, and review the diff in one CLI session.
- **Repository-native execution:** Use `run_command` for project commands across languages. Docker environments can be prepared for a repository and reused across commands. `run_tests` provides built-in Go test profiles.
- **Recoverable work:** Persistent sessions, resumable tasks, and context compaction preserve progress across long runs.
- **Repository memory:** Optional repository-scoped memory, Skills, BM25 search, and Qdrant hybrid retrieval help carry reviewed project knowledge between tasks.
- **Opt-in publishing:** Enable a feature branch to commit and push; GitHub MCP can create a pull request when explicitly enabled.

Sigma is inspired by the [Pi agent harness](https://github.com/earendil-works/pi) and focuses on a Go implementation with an isolated, end-to-end repository issue workflow.

## Get started

Requirements: Go 1.26+, Git, Docker Desktop, and an API key from MiniMax, Anthropic, OpenAI, DeepSeek, or OpenRouter.

From the `go-runtime` directory, start Sigma:

```powershell
go run ./cmd/sigma-agent
```

At the setup prompt, use `/login minimax` to save a model key, `/model` to select a provider and model, and `/repo <path>` to choose a clean local checkout. Run `/help` to see all setup commands. Sigma also accepts provider keys from environment variables for scripted use.

## Fix a GitHub issue

Clone the target repository and make sure its working tree is clean. In the Sigma setup prompt, configure the repository and the features you want to enable:

```text
/repo C:\work\target-repository
/login github
/publish sigma/issue-123
/github-write on
```

Then describe the task, for example:

```text
Fix issue #123. Inspect the relevant code and project instructions, implement the fix, run the appropriate tests, review the complete diff, then commit and push to sigma/issue-123 and open a pull request. Do not merge it.
```

`/publish` enables commit and push to the named feature branch. `/github-write on` enables pull request creation through GitHub MCP. Git push uses your local Git credential helper; the GitHub token is used for MCP access. Network access for Docker starts disabled and can be enabled with `/network on` when dependency downloads are needed.

Sigma creates a detached task worktree, saves the patch and session, and leaves the original checkout unchanged. To continue a saved session, use `/resume issue-123` and re-enable publishing if needed.

## Scope and documentation

`run_command` can execute repository-native build, test, and check commands inside an isolated Linux container. Sigma can inspect the repository's manifests and CI configuration to choose a Docker environment, but it cannot guarantee that every project or service stack will build and run without setup.

See [configuration](CONFIGURATION.md) for providers, credentials, permissions, Docker, and runtime settings; [architecture](ARCHITECTURE.md) for the runtime design; and [`evals/`](evals/) for evaluation protocols and results.
