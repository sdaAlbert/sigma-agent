# Retrieval evaluation fixture

This small reviewed corpus exists only for the labeled memory retrieval evaluation.

- 2026-11-01 | kind: testing | source: `docs/CONFIGURATION.md` (run_tests) | The `run_tests` tool uses a temporary Docker internal network with no external egress; tests have a virtual network interface. Use a Go image with dependencies cached and prefer focused package tests.
- 2026-11-01 | kind: workflow | source: `docs/CONFIGURATION.md` (embedding settings) | The MiniMax chat key does not configure embeddings; memory vector search requires a separately configured Hugging Face or OpenAI-compatible embedding endpoint.
- 2026-11-01 | kind: architecture | source: `docs/CONFIGURATION.md` (repository memory) | `.sigma-agent/MEMORY.md` is the source of truth; Qdrant is a derived index of reviewed facts, and the Agent does not write memory automatically.
- 2026-11-01 | kind: architecture | source: `docs/CONFIGURATION.md` (repository identity) | Qdrant memory search filters by repository identity derived from the Git common directory; separate clones do not share a memory namespace automatically.
- 2026-11-01 | kind: pitfall | source: `docs/ARCHITECTURE.md` (repository memory) | Memory entries older than 180 days require revalidation; current source, repository instructions, and user requests take precedence.
- 2026-11-01 | kind: workflow | source: `docs/ARCHITECTURE.md` (workspace tools) | Use `search_code`, `list_files`, and `read_file` to inspect current source; repository memory is for durable facts, not a code index or task transcript archive.
