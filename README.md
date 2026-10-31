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
