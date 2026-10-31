# GitHub patterns for Sigma Agent's repository-scoped vector memory

**Reviewed:** 2026-10-30
**Scope:** first-party repository documentation/source and one small coding-agent memory implementation. Findings describe the cited projects; the Sigma Agent design at the end is our proposal, not a claim that any one project is a drop-in fit.

## Sigma Agent starting point

- `.sigma-agent/MEMORY.md` is the human-reviewed source of project facts; the agent retrieves relevant entries on demand with BM25 or optional vector search. The Agent cannot change it through `edit_file` or `apply_patch`.
- `internal/sigmaagent/vector.go` already calls an embedding endpoint and Qdrant. It stores code vectors in `sigma_agent_code`, attaches a `repo_id` payload, and scopes searches with exact Qdrant payload equality. The current `repositoryID` hashes the Git common directory path; that is useful for isolating worktrees belonging to the same local checkout, but it is not a portable ID across cloned directories.
- This makes a new database service unnecessary for a first memory search experiment. A separate memory collection could reuse the existing Qdrant endpoint and embedding configuration without mixing memory records into code chunks.

The minimum `sigma_agent_memory` implementation is now present. A live Qdrant smoke verified repository filtering, Markdown synchronization, provenance output, and stale-hit rejection with a local deterministic test embedder. It does not measure actual semantic retrieval quality; see the [test report](../evals/results/2026-10-30-memory-qdrant-live-smoke.md).

## Repository patterns worth borrowing

### LangGraph Store — separate thread snapshots from cross-thread records

LangGraph explicitly models thread state through a checkpointer and cross-thread data through a Store. Store entries are key/value items in hierarchical namespaces; search can take a semantic query and limit, while exact get/update/delete operations remain available. Its docs show namespaces such as `(user_id, "memories")`, and optionally enable indexing with configured embedding dimensions and fields. See [LangGraph Store docs](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx) and [memory guide](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/add-memory.mdx).

**Sigma Agent takeaway:** model memory as independent records under an explicit repository scope; keep per-session snapshots separate. Use exact repo filtering before vector ranking. LangGraph namespace searches can be prefix-based, and a [LangGraph advisory](https://github.com/langchain-ai/langgraph/security/advisories/GHSA-47pj-3jcm-6whg) documents how delimiter-unaware prefix matching caused cross-namespace reads in affected backends. Our implementation should use exact equality filters and include an isolation regression test.

### Mem0 — scoped search and explicit memory lifecycle

The [Mem0 repository README](https://github.com/mem0ai/mem0) demonstrates searching with a query plus `filters={"user_id": ...}`, then injecting a small top-k result into the next model prompt. Its [search guide](https://github.com/mem0ai/mem0/blob/main/docs/core-concepts/memory-operations/search.mdx) documents semantic retrieval and scope filters, with optional keyword/entity signals and score explanations. The API includes add, search, get, update, delete, and memory history; memory records can carry custom metadata and timestamps in the [API reference](https://github.com/mem0ai/mem0/blob/main/skills/mem0/references/api-reference.md).

**Sigma Agent takeaway:** borrow the mandatory scope filter, top-k retrieval, provenance metadata, and update/delete lifecycle. Do not copy Mem0's automatic extraction pipeline yet: the project wants reviewed project facts, and its task does not need an LLM to turn every conversation into a memory.

### OpenHands SDK — project memory file in a coding agent

The [OpenHands persistent-memory example](https://github.com/OpenHands/software-agent-sdk/blob/main/examples/01_standalone_sdk/55_persistent_memory.py) uses a workspace-local `.openhands/memory/MEMORY.md`, injects it when a new conversation starts, and demonstrates one conversation recording a decision that a second conversation can recall. It also has a separate user-level memory directory.

**Sigma Agent takeaway:** the repository file remains a clear source of truth and supports human review. Sigma Agent already implements the read-only form of this pattern. A vector index can later be derived from that file rather than becoming an opaque second source of truth.

### mem0ry4ai — lexical-first local coding-agent memory (small third-party project)

The [mem0ry4ai README](https://github.com/cremenescu/mem0ry4ai/blob/main/README.md) describes Markdown plus Git as the source of truth and a derived SQLite FTS5/BM25 index. Optional local vectors are combined with lexical results; provenance, invalidation, and superseding facts are represented separately. The README describes a human review queue for batch transcript-extraction candidates, while direct agent capture is not always approval-gated.

**Sigma Agent takeaway:** use lexical retrieval as a real baseline, preserve source/freshness and supersession metadata, and gate low-trust generated proposals. This is a small author's project, so its README is evidence of its own design, not independent proof that its retrieval or quality claims hold.

## Proposed Sigma Agent design

Keep `.sigma-agent/MEMORY.md` canonical and human-reviewed. Treat Qdrant as a rebuildable index only:

1. Parse each reviewed bullet/entry into a stable record containing `memory_id`, `repo_id`, `text`, `source`, `updated_at`, optional `valid_until`, and `content_hash`.
2. Write vectors to a new `sigma_agent_memory` collection in the existing Qdrant service. Reuse the current embedding provider/client. Keep its collection and payload type separate from `sigma_agent_code`.
3. At query time, require exact `repo_id` equality in the Qdrant filter before ranking; retrieve a small top-k. Combine vector ranks with a lexical baseline using reciprocal-rank fusion only if a matched evaluation shows it helps.
4. Return memory text together with source/date. Before injecting a hit, verify that the canonical Markdown entry still exists and its hash matches; discard stale index entries and rebuild them from the file.
5. Do not add model-authored writes yet. If memory proposals are later added, present the candidate, evidence/source, and proposed action to the user; write only after explicit approval and keep update/delete history.

### Identity and isolation

The current `repo_id` hashes the local Git common directory. That gives all worktrees from one checkout the same identity, but it will differ in another clone. For this local prototype, retain the current ID and prove that sibling repositories cannot retrieve each other's facts. If memory must travel across clones or machines, derive a stable opaque identity from a normalized Git remote URL (or an explicit repository ID); avoid storing a private remote URL in Qdrant payloads. This identity change is a design choice, not required for a single-machine demo.

### Evaluation gate

Before adding the Qdrant collection, prepare repeated-session questions whose answers depend on curated memory. Compare:

- no memory retrieval;
- lexical/BM25 search over the same memory entries;
- BM25 plus vector search with the same corpus and top-k context budget.

Measure Recall@K/MRR against relevant memory IDs, correct task decisions, stale-fact conflicts, irrelevant context bytes, latency, and cross-repository leakage. Include paraphrased queries and exact-name/identifier queries. Only keep vector retrieval if it helps semantic/paraphrased cases without weakening exact-term recall, source tracing, or scope isolation.

This experiment does not require new embedding credentials if the configured Sigma Agent embedding endpoint is already working. The existing MiniMax chat key is not itself an embedding endpoint; the embedding provider remains a separate configuration concern.

## Source links

- [LangGraph Store source/docs](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx)
- [LangGraph long-term memory guide](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/add-memory.mdx)
- [LangGraph namespace isolation advisory](https://github.com/langchain-ai/langgraph/security/advisories/GHSA-47pj-3jcm-6whg)
- [Mem0 repository](https://github.com/mem0ai/mem0)
- [Mem0 search guide](https://github.com/mem0ai/mem0/blob/main/docs/core-concepts/memory-operations/search.mdx)
- [Mem0 API reference](https://github.com/mem0ai/mem0/blob/main/skills/mem0/references/api-reference.md)
- [OpenHands persistent-memory example](https://github.com/OpenHands/software-agent-sdk/blob/main/examples/01_standalone_sdk/55_persistent_memory.py)
- [mem0ry4ai repository README](https://github.com/cremenescu/mem0ry4ai/blob/main/README.md)
