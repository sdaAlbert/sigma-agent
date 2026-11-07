# Repository memory and retrieval

**Status date:** 2026-11-07
**Status:** design and synthetic fixture are present. The six-query retrieval
study has not completed against a live embedding service; matched issue runs
have not established coding benefit.

## Questions

1. Can Sigma retrieve reviewed, sourced facts about the same repository more
   accurately than local BM25 alone?
2. Do those facts improve later issue-resolution work under matched conditions?

Keep retrieval quality separate from coding outcomes. Memory contains reviewed
repository facts, not source-code chunks, transcripts, secrets, benchmark
instructions, or gold patches. Human-reviewed Markdown is canonical; Qdrant is
a derived index. Current source and tests take precedence over stale memory.

## Phase A: six-query plumbing check

The query labels are in [`memory-retrieval-v1.json`](../../memory-retrieval-v1.json)
and the isolated fixture is
[`fixtures/memory-retrieval-v1`](../../fixtures/memory-retrieval-v1). Run from the
repository root after configuring a real embedding provider and Qdrant:

```powershell
docker compose up -d qdrant
go run ./cmd/sigma-agent --eval-memory --repo .\evals\fixtures\memory-retrieval-v1 --memory-dataset .\evals\memory-retrieval-v1.json --memory-top-k 5
```

The command reports Recall@5 and MRR for BM25, dense search, and reciprocal-rank
fusion. Record embedding provider/model, vector dimensions, Qdrant version and
collection, config, elapsed time, and output. Do not substitute random or
deterministic vectors. This retrieval-only check does not call a generation
model.

## Phase B: real repository queries

Use a fixed corpus of reviewed facts collected before the held-out queries.
Have a second person review relevance labels where practical. Include useful,
hard-negative, and no-relevant-fact queries. Report per-query rankings,
Recall@K, MRR, source accuracy, empty-result behavior, stale-fact handling,
latency, and indexing cost.

## Phase C: issue-resolution value

Choose fresh held-out repository issues whose solution can use a genuinely
earlier reviewed fact. Run each issue in two clean worktrees from the same base:

| Arm | Memory setting | Other conditions |
| --- | --- | --- |
| Control | `memory_enabled: false` | Same model, prompt, source tools, issue text, budget, image, and evaluator |
| Treatment | `memory_enabled: true` | Same settings and exact same memory snapshot |

Use Skills disabled in both arms unless Skill is an independently balanced
factor. Record the memory snapshot hash/sources, retrieved facts, whether they
were applied correctly, task/evaluator outcomes, stale or irrelevant context,
tokens, calls, latency, and every failed attempt. Do not use the old #1969 or
#2537 runs as a matched pair; their prompts and assistance conditions differ.

The detailed procedures live in
[`memory-retrieval-evaluation.md`](memory-retrieval-evaluation.md) and
[`ablation-protocol.md`](ablation-protocol.md). Canonical
standalone configs are [`memory-disabled.json`](configs/memory-disabled.json)
and [`memory-enabled.json`](configs/memory-enabled.json). Both keep
Skills disabled; copy any task-specific runtime fields into both configs.

## Evidence boundary

- **Observation:** the six-query set and labeled fixture are versioned, but the
  live embedding/Qdrant evaluation is incomplete according to the current
  protocol and architecture status.
- **Observation:** a recent VibeMemBench attempt did not produce an interpretable
  memory comparison because patch application conflicted before tests ran.
  It is not a memory win or loss. Record ID: `google__yamlfmt-42` in the
  archived VibeMemBench run notes.
- **Hypothesis:** repository memory may help repeated tasks when it supplies
  durable facts that are costly to rediscover. A retrieval score alone cannot
  establish that.
