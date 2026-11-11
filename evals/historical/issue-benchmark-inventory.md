# Sigma Agent issue benchmark inventory

**Snapshot:** 2026-11-02
**Scope:** issue-level Go-zero runs and directly related controls found in `evals/` and the local benchmark artifacts. This is an experiment inventory, not a benchmark-wide success rate.

## Results by task

| Benchmark task | Sigma issue-only result | Other runs / controls | Count as clean issue-only success? |
|---|---|---|---|
| go-zero issue #1729 / Multi-SWE-Bench PR #1783 | **1/1 resolved.** Official issue title/body only; no GitHub MCP, web search, Skill, memory, target diff, or hint. One sample, and the Agent used its full 32-turn budget. | None needed for the reported result. | **Yes — the one clean success currently documented.** [Run report](README.md#archive-access) (local detailed report not bundled) |
| go-zero PR #1969 | No clean issue-only score. | **1/1 resolved after implementation-location and root-cause hints.** Earlier attempts on the same task failed. This was explicitly a pipeline smoke run, not a capability score. | No. [Run report](README.md#archive-access) (local detailed report not bundled) |
| go-zero issue #2508 / PR #2537 | **0/1 resolved** on the formal issue-only input; later attempts still did not produce a clean issue-only resolution. One early run also ended in a compile error from an unused import. | **1/1 resolved only in a guided reproduction** with explicit acceptance details and GitHub MCP access to the target PR diff. That is not autonomous issue repair. | No. [Issue-only report](README.md#archive-access) (local detailed report not bundled), [guided report](README.md#archive-access) (local detailed report not bundled) |
| go-zero PR #964 | **0/1 resolved.** The clean issue-only attempt used 32 turns and a 64-turn continuation; the resulting API did not match the tested config/client/Registry contract. One test dependency was also missing, but that did not explain the API mismatch. | A detailed-specification-assisted run was accepted only after evaluator fixture workarounds. | No. [Issue-only report](README.md#archive-access) (local detailed report not bundled), [guided report](README.md#archive-access) (local detailed report not bundled) |
| go-zero PR #2032 | **Resolved at the target-behavior level in the 128-turn hinted run:** after removing the Agent's overlapping test-file edits, its production-only patch applied with the official test patch and the `rest` target tests passed. The overlap includes an Agent-authored `TestWithIgnoreTimeout` case that collides with the benchmark's test changes. A separate corrected official-hint attempt stopped at 64 turns / 74 tool calls; the `rest` package failed to build and no resolved score was produced. | The source-only candidate from the 128-turn run passed target acceptance. The raw combined patch did not receive `resolved=1` because the test patches conflict. The corrected 64-turn attempt was also hint-assisted and unresolved. | Keep both separate from clean issue-only results. [128-turn report](README.md#archive-access) (local detailed report not bundled), [latest issue-only report](README.md#archive-access) (local detailed report not bundled), [corrected hint run](README.md#archive-access) (local detailed report not bundled) |
| go-zero PR #2283 | **0/1 resolved** for Sigma's Skill-gated run and its memory-assisted run. The benchmark test patch refers to `corsRouter`, which is undefined against the candidate, so its target package did not compile. | Gold control passed, confirming the test image/evaluator can score this task; that control is not an Agent result. | No. [Sigma Skill run](README.md#archive-access) (local detailed report not bundled), [Sigma memory run](README.md#archive-access) (local detailed report not bundled) |
| go-zero PR #1456 | Two attempts were interrupted; the user chose not to resume on 2026-11-03. | No finalized evaluator result. | Abandoned with no score; excluded from denominators. [Record](README.md#archive-access) (local detailed report not bundled) |
| go-zero PR #1792 | No run report or matching benchmark artifacts found. | A previous reference to deleting/running “1792” appears to have been a typo for #1783; the recorded successful task is PR #1783 / issue #1729. | Not run based on the available local evidence. |

## Other evaluator controls, not issue-fix scores

- **PR #2032 gold controls:** demonstrate that the official evaluator can score the task, but are reference controls, not Agent successes.
- **PR #2283 gold control:** validates the test image/evaluator; it is not an Agent run.

## What the current evidence supports

There are six completed Go-zero issue/PR targets with recorded Sigma results (#1783, #1969, #2537, #964, #2032, #2283), plus the incomplete #1456 attempt. #1783 is the only documented clean issue-only success. #2032 has positive functional evidence: its hinted production patch passed the official target tests after the overlapping Agent-authored test was removed. The evaluator label was blocked by that test-patch collision. Keep #2032 in a separate hinted track, not the clean issue-only score. Other assisted reports also remain separate. Replays of the same issue are not independent benchmark samples.

Do not present these runs as a stable pass rate or as proof of parity with Pi. For a resume, describe the task conditions and exact counts; do not combine assisted, issue-only, evaluator-diagnostic, or black-box results into one number. A meaningful next score requires a preselected held-out task set, fixed prompt/model/budget, issue-only inputs, clean worktrees, and submission of every attempt to the official evaluator.

## Cleanup record

Cleanup was attempted on 2026-11-02 for two superseded #2283 evaluator patch variants (`candidate/` and `candidate-normalized/`) and an unreferenced duplicate checkout under `var/benchmark-data/pi-go-zero-2283-2026-11-02/workspace/`. The desktop filesystem policy blocked the recursive deletion, so **these three directories are still present** (about 25 MB combined). The canonical evaluator report, run transcripts, benchmark datasets, and Sigma patches remain untouched.
