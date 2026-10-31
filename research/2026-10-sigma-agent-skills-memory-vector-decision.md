# Sigma Agent: Skills, long-term memory, and vector storage decision

**Reviewed:** 2026-10-30
**Scope:** Current Go runtime plus first-party Pi, Qdrant, and embedding-provider documentation. This is a design recommendation; the evaluation thresholds below are proposed project gates, not source claims.

## Decision

| Capability | Recommendation | Reason |
|---|---|---|
| Pi-style Skills | **Add after freezing a no-Skill baseline.** Support project-local `.agents/skills/**/SKILL.md` with progressive disclosure. | Skills package reusable procedures and reveal full instructions only when a task matches. This is a small context/tooling feature that does not need another service. Measure it against the same tasks before claiming a gain. |
| Long-term memory | **Defer automatic memory.** First allow a small, human-curated Markdown memory file scoped to a repository; add agent-written memory only behind review. | The runtime already persists resumable session history, but it has no cross-session retrieval of durable project facts. Unreviewed memory adds stale, incorrect, or cross-repository context. |
| Vector database | **Evaluate repository-memory retrieval in a separate Qdrant collection on the existing service.** | Sigma Agent already has an embedding client and Qdrant code index. Keep memory payloads separate and compare BM25 against hybrid search before treating vectors as a proven improvement; do not deploy another Qdrant service. |

## What the runtime already does

- `internal/agent/session_store.go` persists each resumable conversation as a JSON snapshot; the session entry chain supports returning to earlier branches. `internal/agent/context.go` keeps canonical history and sends an extractive summary plus recent complete turns when the provider context is too large.
- `internal/sigmaagent/vector.go` implements hash-addressed code chunks, optional Hugging Face feature extraction (default) or an OpenAI-compatible embedding endpoint, and a Qdrant collection named `sigma_agent_code`. Retrieval uses a `repo_id` filter. Changed chunks are upserted before stale chunks are removed; search results are checked against current working-tree content hashes.
- `internal/sigmaagent/tools.go` exposes `retrieve_code` (BM25, or BM25 + vector RRF), `search_code`, and workspace-bounded `read_file`. `docker-compose.yml` already provides local Qdrant.
- Retrieval is evaluated on the public SWE-bench BM25 13K dev split, excluding SQLFluff: 175 issue cases across five repositories. The evaluator reports path-level Recall@K and MRR for BM25 and, when configured, hybrid search. The gold paths come from solution patches, so they are a proxy for useful context rather than a claim that every changed file is necessary.

Session persistence and context compaction are useful continuity within a session; neither is cross-session, curated project memory.

## Recommendation and staged design

### Stage 1 — Skills loader (implemented; evaluation pending)

1. Sigma Agent discovers `.agents/skills/**/SKILL.md` under the task repository. It adds only validated names and descriptions to system context; it does not inject each full skill body.
2. The read-only `read_skill` tool accepts an exact discovered name and reads only that bounded `SKILL.md`. Symlink paths are rejected. Companion files are not exposed by the tool.
3. A matching task can load the Skill through `read_skill`. Loading does not execute scripts. `skills_enabled: false` disables Skill metadata and the tool for controlled ablations.
4. Add global skill roots only after project-local discovery is proven and after defining which global files may enter each task's context.

This follows Pi's documented pattern: scan skill metadata, expose descriptions, and read the full `SKILL.md` on demand (progressive disclosure). Pi also warns that skills can instruct arbitrary actions and may contain executable code, and its project skills load only after the project is trusted. [Pi Skills documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/skills.md)

**Evaluation still required:** no pre-change no-Skill baseline was captured before this implementation. One matched one-task smoke now verifies the toggle and selection path; it had equal task outcome but higher turns, calls, and token use with Skills enabled. See the [smoke report](../evals/results/2026-10-30-skills-go-port-ablation-smoke.md). Run more matched tasks with the same model, prompt, repo state, memory, budget, and test image, toggling only `skills_enabled`. Include matching tasks and near-misses; report task success, correct Skill selection, turns, context tokens, and rejected out-of-workspace attempts. Skill invocation alone is not a success. No general quality gain is claimed.

### Stage 2 — Read-only repository memory (first iteration implemented; evaluation pending)

1. Sigma Agent reads bounded `.sigma-agent/MEMORY.md` from the current task worktree and adds it as reference context. Session snapshots remain the separate per-session recovery state.
2. The starter memory is human-reviewed and read-only through Agent edit tools. Entries record a source and date; memory is capped at 16 KiB and scoped to the current repository.
3. Start with human-maintained/read-only memory. Add an explicit memory proposal or write tool later, requiring user review, with append/edit history and a clear delete/reset path. Never promote arbitrary tool output or secrets automatically.
4. Keep global user preferences separate from repo memory and opt-in; do not mix memory across workspaces.

Pi's first-party docs describe auto-saved per-project session files, resume/branching, and lossy compaction while retaining the full session history. Those docs support durable sessions; they do not establish a built-in curated, cross-session project-memory store. The distinction and the defer decision here are architectural inferences from the documented capabilities. [Pi Sessions documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sessions.md) · [Pi Compaction documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/compaction.md)

**Evaluation still required:** use repeated-work tasks that depend on a prior project decision or convention. Compare memory disabled/absent and curated Markdown memory for task success, stale-fact conflicts, irrelevant injected tokens, and whether the Agent follows current source over outdated notes. Memory implementation alone does not demonstrate improvement. Only introduce agent-authored proposals if a human can inspect and undo them.

### Stage 3 — Separate Qdrant memory collection (implemented; quality evaluation pending)

Keep code retrieval and memory logically separate. Sigma Agent now syncs reviewed facts into `sigma_agent_memory` on the existing Qdrant service with payload fields for `repo_id`, `source`, `updated_at`, path/line, and `content_hash`. Search applies exact repository filters and verifies each hit against current Markdown before returning it. A live Qdrant integration smoke verified persistence, scope, update sync, and stale-hit rejection using a deterministic test embedder; it did not measure semantic retrieval quality. See the [live smoke report](../evals/results/2026-10-30-memory-qdrant-live-smoke.md). Qdrant supports JSON payloads and filtering at query time; its docs recommend creating payload indexes for fields used as filters. [Qdrant payloads](https://qdrant.tech/documentation/concepts/payload/) · [Qdrant filtering](https://qdrant.tech/documentation/search/filtering/)

Use the configured embedding provider consistently for both indexing and queries; verify model identity, vector dimension, and any query/document task prefix are stable. Hugging Face defines feature extraction as converting text to vectors for retrieval and documents vector-array responses. Gemini's first-party embedding guide illustrates why embedding task type matters: for asymmetric retrieval it distinguishes query and document use. These are provider-specific considerations; Sigma Agent's current Hugging Face path does not expose task prefixes. [Hugging Face feature extraction](https://huggingface.co/docs/inference-providers/en/tasks/feature-extraction) · [Gemini embeddings](https://ai.google.dev/gemini-api/docs/embeddings)

**Evaluation still required:** configure a real embedding provider and compare no retrieval, lexical search, and vector search on the same held-out repeated-work queries. Require better semantic-query Recall@5/MRR and task decisions without losing exact-term recall, cross-repository isolation, source freshness, or a material increase in irrelevant context. The current environment has no embedding provider configured, so real semantic retrieval and task benefit remain unverified. If vectors add no benefit over lexical retrieval for this corpus, keep the collection optional or remove it.

## Immediate work order

1. Configure a real embedding endpoint and run the memory query set against no-retrieval, lexical, and vector paths.
2. Run repeated-work tasks with and without reviewed `.sigma-agent/MEMORY.md`; keep human-only updates until utility and review workflow justify more.
3. Run Skills ablations with `skills_enabled: false` and `true` against matched tasks; the one-task smoke is not a general result.
4. Consider explicit memory proposals only after these evaluations establish value and a human approval workflow.

The reproducible ablation protocol and no-Skill config are in [`evals/skills-memory-evaluation.md`](../evals/skills-memory-evaluation.md) and [`evals/configs/skills-disabled.json`](../evals/configs/skills-disabled.json). GitHub implementation patterns are in [`2026-10-sigmaagent-vector-memory-github-patterns.md`](2026-10-sigmaagent-vector-memory-github-patterns.md).

## Sources

- [Pi Skills](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/skills.md)
- [Pi Sessions](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sessions.md)
- [Pi Compaction](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/compaction.md)
- [Qdrant payloads](https://qdrant.tech/documentation/concepts/payload/)
- [Qdrant filtering](https://qdrant.tech/documentation/search/filtering/)
- [Hugging Face feature extraction](https://huggingface.co/docs/inference-providers/en/tasks/feature-extraction)
- [Gemini embeddings](https://ai.google.dev/gemini-api/docs/embeddings)
