# Repository memory status

**Status date:** 2026-11-07
**Status:** the runtime feature is active and has been enabled in some Mini SWE
runs. A matched memory-on/off comparison has not established a coding benefit.

## Current role

Sigma stores durable repository facts in `.sigma-agent/MEMORY.md`. The file is
the source of truth; `search_memory` retrieves a small relevant subset on
demand. BM25 works locally. With an embedding provider and Qdrant configured,
Sigma adds repository-filtered vector search and fuses the rankings. Qdrant
contains vectors and provenance for memory facts, not source-code chunks,
transcripts, secrets, or patches. See the [runtime usage and setup
guide](../../../docs/CONFIGURATION.md#repository-long-term-memory).

## Evidence from Mini SWE

- Some Mini SWE configs enable both `memory_enabled` and
  `memory_auto_write`; the pilot config disables both. Inspect the effective
  config for each batch rather than assuming every run had the same condition.
- The archived `batch-20261105-10tasks-from-12304` record reports 26 reviewed
  Django memory entries indexed before the run. After a successful run,
  automatic writeback added three facts and reindexed 29 entries.
- Other Mini SWE tasks had memory enabled but no `search_memory` call. Memory
  availability, indexing, retrieval, and task success are separate events.
- These runs do not form a matched ablation. Do not attribute an official score
  to memory or claim a coding uplift from them.

## Future issue-resolution comparison

If this study is scheduled, choose fresh held-out issues whose expected behavior
depends on a genuinely earlier reviewed fact. Use clean worktrees from the same
base, the same memory snapshot, model, prompt, image, budgets, Skills setting,
and evaluator; change only `memory_enabled`. Record whether `search_memory` was
called, which sourced facts it returned, whether the Agent used them correctly,
the official result, regressions, turns, calls, tokens, and every incomplete
attempt. Keep `memory_auto_write` disabled during the pair so one arm cannot
change the other's memory. The paired configs and broader controls are in
[`ablation-protocol.md`](ablation-protocol.md).
