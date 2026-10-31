# Long-term memory patterns in coding agents and agent frameworks

**Reviewed:** 2026-10-31
**Scope:** First-party GitHub documentation and source for OpenHands, LangGraph, Mem0, Continue, and Aider. This compares documented mechanisms and evaluation evidence; it is not an independent benchmark.

## Summary

The clearest coding-agent memory example here is OpenHands: opt-in Markdown files at user and workspace scope, with a small index injected at session start and longer daily notes opened on demand. LangGraph provides lower-level store primitives (namespaces, keyed records, CRUD, and optional vector search) but leaves memory policy and retrieval triggers to the application. Mem0 adds automated extraction and multi-signal retrieval, with temporal metadata and a lifecycle API, but its published scores are from conversational-memory benchmarks and its docs say reported results reflect the managed platform. Aider and Continue document useful repository retrieval/indexing, but these sources describe current-code context rather than durable lessons learned across sessions.

## OpenHands: explicit user and project memory files

OpenHands' [persistent-memory guide](https://github.com/OpenHands/docs/blob/main/sdk/guides/persistent-memory.mdx) describes two Markdown tiers: `~/.openhands/memory/` for cross-project knowledge/preferences and `<workspace>/.openhands/memory/` for repository-specific knowledge. Each tier has a curated `MEMORY.md` index; longer `YYYY-MM-DD.md` daily logs are not injected automatically and are read with file tools when the index points to them. Project memory can be reviewed, edited, deleted, or committed with the project.

With `AgentContext(load_memory=True)`, OpenHands resolves the two indexes and injects them into the system prompt when each new conversation starts. The system prompt also tells the agent to record durable facts near task completion, keep the index concise, merge duplicates, prune stale entries, avoid secrets, and leave general repository instructions in `AGENTS.md`. This makes retrieval automatic at session start for the index, while details in logs are retrieved on demand by the agent. The combined index has an approximately 6,000-character budget; when over budget, whole older lines are dropped and a truncation notice is included. The injected text is marked untrusted because the repository/workspace can supply or alter it.

The [SDK example](https://github.com/OpenHands/software-agent-sdk/blob/main/examples/01_standalone_sdk/55_persistent_memory.py) demonstrates one conversation writing a project dependency-management decision and a new conversation receiving it. It is a functional cross-session example, not a quantitative retrieval or coding-task evaluation. The docs describe curation and user review, but the example does not require a human approval step before an agent writes.

**Design pattern:** keep durable, human-readable project facts in a source-controlled repository layer; inject only a compact index and require explicit on-demand reads for larger notes. Workspace path scoping is simple and transparent, though it does not itself establish a canonical identity across separate clones or worktrees (inference from the path-based layout).

## LangGraph Store: application-owned memory policy

LangGraph distinguishes per-thread checkpoint state from long-term records held in a Store. The [Store guide](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx) models memories as JSON-like key/value items in tuple namespaces, e.g. `(user_id, "memories")`, and allows namespaces of arbitrary length. The [memory concepts guide](https://github.com/langchain-ai/docs/blob/main/src/oss/concepts/memory.mdx) describes common namespace components as user/org IDs and other labels. A repository identifier can therefore be part of an application-defined namespace, but the framework does not prescribe repository identity or source provenance.

The Store API offers `put`/`get`/`delete` and `search`; a write to an existing key overwrites its value. Items carry `created_at` and `updated_at`. Search can enumerate namespace items or rank a natural-language query semantically when embedding configuration is provided; metadata filters can narrow results. Namespace search is prefix-based, so an application needing strict repository isolation should use a full namespace or filter explicitly. The docs do not specify a built-in TTL or human-approval policy.

Memory timing is also left to the application. LangGraph's [writing-memory guide](https://github.com/langchain-ai/docs/blob/main/src/oss/concepts/memory.mdx) contrasts hot-path writes (immediate but may add latency and reasoning burden) with background writes (off the user-facing path but require a trigger/frequency policy). It lists scheduled, cron, and manual/application triggers as options. Thus a `Store` is a substrate, not an autonomous memory lifecycle.

**Design pattern:** separate session state from cross-session records; make repository scope explicit in the namespace; keep source, confidence, expiry, and approval fields in the record if the application needs them. These last fields are application design suggestions, not Store defaults.

## Mem0: extracted facts, temporal metadata, and hybrid retrieval

Mem0 is a general agent-memory system rather than a coding-agent-specific repository memory. Its [search guide](https://github.com/mem0ai/mem0/blob/main/docs/core-concepts/memory-operations/search.mdx) uses an explicit natural-language search call with entity filters; it says to provide `user_id` to prevent cross-user contamination. The same guide documents `agent_id`, `run_id`, and metadata/category filters. The [API reference](https://github.com/mem0ai/mem0/blob/main/skills/mem0/references/api-reference.md) lists add, search, get, update, delete, delete-by-filter, and memory-history operations. Memory objects can carry user/agent/app/run IDs, custom metadata, categories, expiration date, and creation/update timestamps. For repository isolation, an application could map a repo to an ID or metadata filter; the docs do not define a special repository scope.

The current [Mem0 architecture and evaluation docs](https://github.com/mem0ai/mem0/blob/main/docs/core-concepts/memory-evaluation.mdx) describe extraction after an agent response, with context lookup, LLM distillation, hash deduplication, embedding, entity linking, and a separate temporal pass. Storage includes vector text/embeddings/metadata, entity links, and an SQL history log. Extraction is described as ADD-only: revised facts remain beside prior facts. At query time, semantic vector similarity, BM25 keyword matching, entity matches, and temporal intent scores are fused. Timestamps and temporal attributes help rank time-relevant facts, but ADD-only storage also means old conflicting facts can remain candidates.

The published evaluations are useful as broad memory evidence, not code-agent outcomes. Mem0 reports 92.5 LoCoMo, 94.4 LongMemEval, and 64.1/48.6 on BEAM at 1M/10M tokens, each at top-200 retrieval and roughly 6.7–7.0k mean context tokens. The docs say these headline numbers reflect Mem0's managed platform and proprietary optimizations, not the open-source SDK, and give a ±1 point judge-variance interval. Its open [memory-benchmarks repository](https://github.com/mem0ai/memory-benchmarks) supports reproducibility with cloud and self-hosted backends. These datasets evaluate conversational memory; they do not show whether a coding agent solves repository tasks better.

**Design pattern:** retrieve only when the application issues a scoped query; use exact identity filters alongside semantic ranking; preserve timestamps and history; benchmark the complete extraction/retrieval/answer path at matched token and latency budgets. For coding use, add repo/branch state and code-task outcomes to the evaluation.

## Continue and Aider: current repository context, not durable learned memory

Continue's [indexing design](https://github.com/continuedev/continue/blob/main/core/indexing/README.md) uses content hashes to reuse artifacts across branches, checks file timestamps against a SQLite catalog, and updates artifacts for changed/removed files. It documents tree-sitter top-level snippets, SQLite FTS5, structure-aware chunks, and LanceDB chunk embeddings with SQLite metadata. Branches get separate LanceDB tables, but its docs call out that FTS5 does not distinguish branch/repository tags, so results may cross those tags. This is an index-freshness and scope warning, not a long-term fact-memory feature.

Aider's [repo-map documentation](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/repomap.md) says it sends a graph-ranked map of important classes/functions/signatures with each user change request. The map reflects the repository and current chat state, is trimmed to a token budget, and lets the model request full files when needed. This answers codebase-orientation questions; the cited docs do not describe persisting agent-learned facts from one task/session for later tasks.

These systems suggest separating two stores: a code index that is refreshed from current repository contents, and a durable memory of decisions, gotchas, and prior outcomes. Do not let old memory substitute for current source files.

## Evaluation evidence and limits

- OpenHands supplies a two-conversation demonstration. The cited persistent-memory docs do not report accuracy, stale-memory rate, or task-completion metrics.
- LangGraph's Store docs define operations and recommend reference-style correctness checks for custom stores; they do not report a memory benchmark.
- Mem0 reports accuracy and token-cost results on conversational datasets, plus scale results on BEAM. Its docs explicitly distinguish managed-platform scores from the OSS backend and identify weaker 10M-token temporal, event-ordering, and multi-session results.
- Continue and Aider explain repository-context mechanisms. The cited docs do not establish that those code retrieval systems improve multi-session memory or coding-task success.
- Therefore the primary-source set offers useful lifecycle and retrieval designs but no controlled head-to-head coding-agent long-term-memory evaluation. That is a limit of the sources reviewed, not proof that no such evaluation exists.

For a repository-memory evaluation, keep retrieval coverage separate from task success: measure whether the right prior fact is recalled, whether it is stale/contradicted, tokens and latency consumed, and whether it improves a later code task. Compare against both no-memory and a human-maintained project file, using the same model and token budget. This is a proposed evaluation design, not a reported result.
