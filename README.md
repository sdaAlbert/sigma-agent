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

## Project notes

- Local credentials, session data, run artifacts, and `.sigma-agent/MEMORY.md` are excluded from Git.
- Embeddings and Qdrant are optional; local BM25 retrieval works without them.
- Retrieval and issue-resolution evaluation procedures are documented under [`evals/`](evals/).

## Design references

These public projects, documentation, and research papers informed different parts of Sigma Agent's design:

- **Agent runtime, sessions, compaction, and Skills:** [Pi Agent](https://pi.dev/docs/latest/), especially its [sessions](https://pi.dev/docs/latest/sessions), [compaction](https://pi.dev/docs/latest/compaction), and [Skills](https://pi.dev/docs/latest/skills) documentation.
- **Persistent project memory:** [OpenHands persistent memory](https://github.com/OpenHands/docs/blob/main/sdk/guides/persistent-memory.mdx), [LangGraph memory and Store](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx), and [Mem0](https://github.com/mem0ai/mem0).
- **Codebase retrieval and RAG:** [Continue indexing](https://github.com/continuedev/continue/blob/main/core/indexing/README.md), [Aider's repository map](https://aider.chat/docs/repomap.html), the [RepoCoder paper](https://arxiv.org/abs/2303.12570), and [Qdrant hybrid search and filtering](https://qdrant.tech/documentation/hybrid-queries/).
- **Related code-search designs reviewed:** [Cursor codebase indexing](https://cursor.com/blog/secure-codebase-indexing) and [Sourcegraph Cody context](https://sourcegraph.com/docs/cody/core-concepts/context).
