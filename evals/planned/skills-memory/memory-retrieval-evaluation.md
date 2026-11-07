# Repository memory retrieval evaluation

## Question and boundary

This experiment asks whether Sigma Agent can retrieve useful, sourced facts learned while working on the same repository across different issues. It does not evaluate source-code search: code browsing remains file listing, literal search, and bounded reads. Qdrant stores only reviewed repository-memory facts, never source chunks, issue transcripts, secrets, or gold patches.

Memory facts should describe durable information that is costly to rediscover and not obvious from the current checkout: test-environment constraints, architecture decisions, compatibility requirements, and recurring non-obvious pitfalls. Each fact needs repository scope, category, source, and review date. Human-reviewed Markdown remains canonical; Qdrant is a derived search index. Current source and tests override a stale or conflicting memory entry.

## Phase A: labeled retrieval plumbing check

The versioned query set is [`memory-retrieval-v1.json`](../../memory-retrieval-v1.json). Its six queries have human-assigned relevant source/fact labels and run against an isolated six-fact fixture at [`fixtures/memory-retrieval-v1`](../../fixtures/memory-retrieval-v1). These examples are deliberately small and synthetic: they test loading, repository isolation, labels, ranking, and metrics, not production distribution or coding ability.

With a real embedding endpoint and Qdrant configured, run from `go-runtime`:

```powershell
docker compose up -d qdrant
go run ./cmd/sigma-agent --eval-memory --repo .\evals\fixtures\memory-retrieval-v1 --memory-dataset .\evals\memory-retrieval-v1.json --memory-top-k 5
```

The evaluator indexes the fixture's reviewed facts and reports per-query Recall@5 and MRR for three arms: local BM25, dense vector search, and reciprocal-rank fusion (RRF). Relevant facts match by both labeled source and fact substring. Record the embedding provider/model, vector dimensions, Qdrant version/collection, configuration, elapsed time, and full output. Do not substitute deterministic or random vectors in this quality measurement. It makes embedding requests but does not call the MiniMax generation model.

The current environment has not completed Phase A because no embedding provider/model is configured and Qdrant is not running. Do not describe the checked-in fixture or unit tests as a real semantic-retrieval result.

## Phase B: real repository queries

After the plumbing check passes, build a small labeled corpus from reviewed go-zero facts collected before the evaluated tasks. Choose issue descriptions or realistic failure reports as queries. Two people should review the relevance labels where practical; otherwise record who labeled each query and preserve disagreements. Keep the fact corpus version fixed across arms. Include useful and hard-negative queries, including cases where no fact is relevant.

Report Recall@K, MRR, per-query rankings, relevant-source accuracy, empty-result behavior, stale-fact handling, latency, and embedding/indexing cost. Compare BM25, dense, and RRF on identical facts and queries. A higher aggregate score is not enough if it comes from one repeated or answer-revealing fact.

## Phase C: issue-resolution value

Retrieval quality alone does not establish that memory improves coding. Use a few **fresh, held-out Go issues** from the same repository, not the fixture questions. For each task, prepare a memory snapshot from genuinely earlier work and record its sources and timestamps. The memory must not include the target issue's answer, target PR diff, gold patch, hidden test, or a near-verbatim solution.

Run each task in two fresh worktrees/sessions from the same base commit:

| Arm | `memory_enabled` | Other conditions |
| --- | --- | --- |
| Control | `false` | Same model, prompt, source tools, issue text, budget, Docker image, and evaluator |
| Treatment | `true` | Same settings and exact same repository-memory snapshot |

Use the standard per-instance benchmark evaluator. Report resolved outcomes and test results per task, plus tool calls, turns, tokens/cost when available, latency, retrieved facts, whether each fact was used correctly, and any irrelevant/stale context. Preserve every attempt. Do not change prompts, model, or budgets between a task's pair.

Historical runs of #1969 and #2537 are not a valid memory on/off pair: #1969's successful run included root-cause guidance, and the #2537 attempts used different inputs/conditions. Keep them as historical evidence only; choose new held-out tasks for this experiment.

## Interpretation

- Phase A/B can show whether semantic search helps find labeled repository facts over BM25.
- Phase C can provide preliminary evidence about whether those facts help issue resolution under matched conditions.
- A small number of issue pairs is a pilot, not a reliable estimate of general success rate.
- If memories are few and BM25 already returns the right facts, do not add more RAG machinery. Improve fact quality, scoping, and review before expanding the corpus.
- This is repository memory, not enterprise knowledge RAG. Enterprise uploads require separate ingestion, permissions, citations, deletion, auditing, and tenant-isolation design.
