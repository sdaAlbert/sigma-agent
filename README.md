# Sigma Agent

Sigma Agent is an experimental coding-agent runtime written in Go, inspired by ideas from Pi Agent. It gives a model a bounded set of repository tools and runs coding tasks against a local Git worktree.

Current capabilities include persistent sessions, context compaction, BM25 code retrieval with optional embedding and Qdrant hybrid search, project Skills and memory, and a read-only investigation subagent.

## Quick start

Requirements: Go 1.26+ and a MiniMax API key.

```powershell
$env:MINIMAX_API_KEY = "your-api-key"
go run ./cmd/sigma-agent --repo C:\path\to\your\repository --interactive
```

For configuration, credentials, optional Qdrant setup, and CLI options, see [CONFIGURATION.md](CONFIGURATION.md) and [ARCHITECTURE.md](ARCHITECTURE.md).

## Run an issue task and open a pull request

Sigma Agent can work from a clean local clone in a separate Git worktree. With
Docker running and a GitHub token available in `.secrets/github-pat` (or
`GITHUB_PERSONAL_ACCESS_TOKEN`), a one-shot task can download missing Go modules
for tests, push a new feature branch, and open a pull request:

```powershell
$env:MINIMAX_API_KEY = "your-api-key"
go run ./cmd/sigma-agent `
  --repo C:\path\to\your\repository `
  --task "Fix issue #123: describe the requested change" `
  --allow-container-network `
  --push-branch sigma/issue-123 `
  --github-write
```

The source checkout must be clean before starting. Sigma edits a detached task
worktree, runs its Go test tool in Docker, then commits and pushes the selected
new branch and opens a pull request against `main` (override with
`--base-branch`) after the Agent run completes normally. Review the pull request
and confirm its test results before merging; a completed run does not guarantee
that every test passed.
Existing local or remote branches are rejected; pushes are never forced, and
Sigma does not merge the pull request.

Outbound access from the test container is off by default. The explicit
`--allow-container-network` option enables outbound networking for `run_tests`
so it can run `go mod download` before tests. This does not add a general Bash
tool or install operating-system packages. Docker and GitHub credentials must
already be configured on the host. See [CONFIGURATION.md](CONFIGURATION.md) for
the narrower default mode and credential setup.

## Project notes

- Local credentials, session data, run artifacts, and `.sigma-agent/MEMORY.md` are excluded from Git.
- Embeddings and Qdrant are optional; local BM25 retrieval works without them.
- Retrieval and issue-resolution evaluation procedures are documented under [`evals/`](evals/).

## Design references

These public projects, documentation, and research papers informed different parts of Sigma Agent's design:

- **Agent runtime, sessions, compaction, and Skills:** [Pi Agent](https://pi.dev/docs/latest/), especially its [sessions](https://pi.dev/docs/latest/sessions), [compaction](https://pi.dev/docs/latest/compaction), and [Skills](https://pi.dev/docs/latest/skills) documentation.
- **Memory and context management:** [Claude Code memory](https://code.claude.com/docs/en/memory) and [context management](https://code.claude.com/docs/en/costs#manage-context-proactively). Sigma Agent currently uses human-reviewed repository memory retrieved on demand and extractive context compaction; it does not implement Claude Code's automatic memory-writing workflow.
- **Persistent project memory:** [OpenHands persistent memory](https://github.com/OpenHands/docs/blob/main/sdk/guides/persistent-memory.mdx), [LangGraph memory and Store](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx), and [Mem0](https://github.com/mem0ai/mem0).
- **Codebase retrieval and RAG:** [Continue indexing](https://github.com/continuedev/continue/blob/main/core/indexing/README.md), [Aider's repository map](https://aider.chat/docs/repomap.html), the [RepoCoder paper](https://arxiv.org/abs/2303.12570), and [Qdrant hybrid search and filtering](https://qdrant.tech/documentation/hybrid-queries/).
- **Related code-search designs reviewed:** [Cursor codebase indexing](https://cursor.com/blog/secure-codebase-indexing) and [Sourcegraph Cody context](https://sourcegraph.com/docs/cody/core-concepts/context).
