# Coding-agent retrieval designs relevant to Sigma Agent

**Reviewed:** 2026-10-30
**Scope:** First-party docs/source and a primary research paper. This is a design survey, not an independent product benchmark.

Sigma Agent already has 80-line/12-line-overlap chunks, content hashes, BM25, optional embeddings + Qdrant, and reciprocal-rank fusion; its retrieval benchmark measures path-level Recall@5 and MRR ([runtime architecture](../docs/ARCHITECTURE.md)). The designs below suggest useful complementary signals and operational details.

## Actual vector RAG and hybrid indexes

### Continue: incremental, multi-artifact codebase index

Continue's source describes a common incremental indexer. It checks file timestamps against a SQLite catalog, uses content hashes to reuse artifacts across branches, and applies add/remove/tag operations to each index. Its artifacts include tree-sitter-extracted top-level symbols, SQLite FTS5, structure-aware recursive chunks, and chunk embeddings in LanceDB with SQLite metadata. Branches receive separate vector tables; the source calls out a known limitation where FTS5 does not distinguish repo/branch tags. [Indexing design/source](https://github.com/continuedev/continue/blob/main/core/indexing/README.md)

**Actionable for Sigma Agent:** preserve the current hash-based incremental update, but treat lexical, vector, and symbol indexes as independent artifacts sharing one file/chunk catalog. Scope retrieval explicitly by repository and revision/branch; branch-specific vector tables alone do not fix leakage in every index. Keep progress/checkpoint state truthful if indexing can be interrupted. Continue's current docs describe codebase awareness as agent/tool-driven; older `@Codebase`/context-provider docs are marked deprecated, so the source design is more reliable evidence of implementation than old UX docs. [Current codebase awareness guide](https://docs.continue.dev/guides/codebase-documentation-awareness)

### Cursor: documented semantic index, now apparently superseded

Cursor's official security page describes a semantic index: client-side Merkle trees detect changed files; the server chunks and embeds content; vectors are stored in Turbopuffer with obfuscated relative paths and line ranges; embeddings are cached by chunk hash; retrieval returns path/range and the client reads the local chunk. [Cursor security page](https://cursor.com/security). A January 2026 engineering post further describes syntactic chunking, asynchronous embeddings, hash-keyed chunk reuse, and simhash-based selection/reuse of similar team indexes. [Securely indexing large codebases](https://cursor.com/blog/secure-codebase-indexing)

However, an August 2026 Cursor staff response says agents now use local “Instant Grep” and ordinary file reads, and no longer use the dedicated semantic embedding index. [Staff response in Cursor forum](https://forum.cursor.com/t/codebase-indexing-not-working-in-v-3-12-onwards/167527/7). Thus the semantic design is useful as an indexing pattern, but should not be represented as Cursor's current agent retrieval design. The two first-party pages describe different product generations.

**Actionable for Sigma Agent:** content-addressed chunk reuse and Merkle/delta discovery are practical ways to reduce re-index cost. For a local-first runtime, return only path/line metadata from a search stage and read the current working-tree text on demand; this avoids serving stale indexed text and limits retained code. This is an architectural inference from Cursor's documented flow, not evidence of Sigma Agent-specific gains.

### RepoCoder: retrieve again after generation

RepoCoder proposes an iterative retrieval-generation loop for repository-level completion: generate a draft, use it to retrieve additional relevant repository context, then generate with the updated context. The paper reports gains over its in-file baseline and vanilla retrieval setup on RepoEval, but evaluates completion, not issue-resolving coding agents. [Paper](https://arxiv.org/abs/2303.12570) · [Authors' code and benchmark](https://github.com/microsoft/CodeT/tree/main/RepoCoder)

**Actionable for Sigma Agent:** consider a bounded second retrieval after the model has formed a concrete hypothesis or named symbols/files, rather than relying only on the initial natural-language task query. This could be an opt-in follow-up retrieval step and should be evaluated separately from the existing single-query Recall@5/MRR benchmark.

## Lexical, symbolic, and structural retrieval (not vector RAG)

### Aider: compact symbol/dependency map ranked to a token budget

Aider builds a repository map containing files and selected class/function definitions and signatures. Its docs say it parses key symbols, models files as nodes connected by dependencies, ranks the graph, and selects map entries to fit a configurable token budget (default documented as 1k). The map is sent with each change request; the model can then request full files as needed. [Repo-map design](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/repomap.md) · [Implementation](https://github.com/Aider-AI/aider/blob/main/aider/repomap.py)

This is symbolic graph context, not semantic embedding search. It answers “what are the important symbols and how are files connected?” rather than nearest-neighbor semantic similarity.

**Actionable for Sigma Agent:** add a compact symbol/signature map as a distinct context source. Prioritize symbols connected to already-mentioned/open/changed files and fit the result under a token budget; this gives broad architectural orientation while vector/BM25 retrieval supplies task-specific snippets. For languages without a parser, fall back gracefully to paths and lexical search. This recommendation is an inference from Aider's documented design.

### Sourcegraph Cody: search stack + code graph, not enterprise embeddings

Cody's docs list keyword search (with query rewriting), Sourcegraph Search, and a code graph that follows relationships between code elements as context sources. They also describe explicit file/symbol/repository context selection and prioritizing explicitly mentioned files. [Cody context sources](https://sourcegraph.com/docs/cody/core-concepts/context) · [Cody chat and context behavior](https://sourcegraph.com/docs/cody/capabilities/chat)

Sourcegraph's FAQ explicitly says Cody Enterprise replaced embeddings with Sourcegraph Search for scale across repos and lower maintenance. [Cody FAQ: embeddings](https://sourcegraph.com/docs/cody/faq)

**Actionable for Sigma Agent:** combine lexical search with explicit user/task context and relationship expansion (definitions, callers, imports) instead of treating vectors as the only relevance signal. Preserve the provenance of each hit and favor explicitly named paths/symbols. Cody's hosted search infrastructure and scale claims do not establish that its approach outperforms local hybrid retrieval in a single-repository agent.

## Practical design takeaways

1. Keep vector retrieval optional and evaluate it against BM25; the primary sources show capable systems using search, symbols, and graph structure alongside or instead of embeddings.
2. Build a separate syntax-aware symbol index and small dependency/call graph. Use that for repository orientation and related-file expansion, not as a replacement for retrieving source snippets.
3. Treat index correctness as part of retrieval quality: content-address chunks, update incrementally, scope all artifacts by repo/revision, and fetch live working-tree text by path and line range.
4. Apply a token budget after ranking and deduplication; use explicit references and current/modified files as ranking signals. Search results should be traceable to paths and line ranges.
5. Evaluate each signal and combination on Sigma Agent's own task set. Existing path-level Recall@5/MRR measures retrieval coverage/rank, but cannot establish task success, stale-context rate, latency, or useful context per token. Add ablations and agent-level outcome measurements before adopting complexity.

## Evidence limits

- Product docs explain intended behavior; they do not independently verify quality or provide comparable head-to-head results.
- Continue and Aider links are mutable `main` branch documentation/source; behavior may change.
- Cursor's semantic-index details are inconsistent across official product materials over time. The later staff forum answer indicates the agent stopped using that index; forum answers are first-party statements but are less durable than versioned product documentation.
- RepoCoder is a peer-reviewed-style research preprint with code/benchmark; its completion setting and benchmark do not directly establish benefits for multi-step issue fixing.
- Recommendations above are hypotheses for Sigma Agent, not claims that these approaches will improve its measured success rate.
