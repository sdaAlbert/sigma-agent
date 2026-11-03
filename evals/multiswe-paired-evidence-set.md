# Sigma/Pi paired evidence set (superseded)

**Status:** Superseded on 2026-11-02 by the [Pi 50-task baseline plan](pi-50-task-evaluation-plan.md); no runs were started for this set.  
**Purpose:** Historical planning record. It is not the active evaluation set.

The user asked to include successful Sigma runs as evidence. Five historical positive cases are included below with their assistance conditions intact. Two new go-zero cases are added as issue-only checks. The five historical Sigma results were produced by earlier Agent versions and different prompts/budgets; for any Pi comparison, run both agents fresh under the same recorded task input and budget. Historical results are project evidence, not directly paired Pi/Sigma results.

## Historical positive cases

| Benchmark task | Sigma evidence | Input/assistance condition | Correct interpretation |
|---|---|---|---|
| go-zero issue #1729 / PR #1783 | Official evaluator **1/1 resolved**. | Official issue title/body only; no GitHub MCP, web search, Skill, memory, target diff, or hint. | The clean issue-only success in this project so far. [Report](issue-only-go-zero-1783.md) |
| go-zero PR #1969 | Official evaluator **1/1 resolved**. | Successful run received implementation-location and root-cause hints; earlier attempts failed. | Pipeline smoke evidence; not an unassisted solve-rate sample. [Report](results/2026-10-30-multiswe-go-zero-1969-smoke.md) |
| go-zero PR #964 | Guided evaluator run **1/1 resolved** after evaluator fixture workarounds. | Detailed implementation/acceptance specification; no target PR diff. | Evidence that Sigma can implement a detailed backend change; not issue-only. [Report](results/2026-10-31-guided-pr-964-and-2537.md) |
| go-zero issue #2508 / PR #2537 | Guided evaluator run **1/1 resolved**. | Explicit acceptance cases and GitHub MCP access to the target PR diff. | Reference-guided reproduction, not autonomous issue repair. [Report](results/2026-10-31-guided-pr-964-and-2537.md) |
| go-zero PR #2032 | Production-only candidate passed the official `rest` target tests after its overlapping Agent-authored test edits were removed. | 128-turn run received the official benchmark hint; no target PR diff or test patch was shown to the Agent. | Target-behavior pass for the production patch; raw combined patch missed the evaluator's `resolved=1` label because Agent and benchmark test edits overlapped. Not issue-only. [Report](results/2026-11-02-multiswe-go-zero-2032-hinted-128-resolver-skill.md) |

## Two fresh issue-only tasks

These task IDs were not found in the local Sigma evaluation reports/run folders checked on 2026-11-02. Both have official task images listed in the Multi-SWE-Bench `images_verified.txt`; image availability has not been tested by pulling or running them.

| Benchmark task | Issue | Task focus from issue | Base commit | Evaluator image |
|---|---:|---|---|---|
| `zeromicro__go-zero-1907` | [#1887](https://github.com/zeromicro/go-zero/issues/1887) | Add a way to register custom gRPC stream client interceptors | `95282edb78f9ea1f70ce210de8b4fc58c636c458` | `mswebench/zeromicro_m_go-zero:pr-1907` |
| `zeromicro__go-zero-1821` | [#1606](https://github.com/zeromicro/go-zero/issues/1606) | Allow customizing the panic recovery handler and response | `ec3e02624c0b6ea726b34de08d9192d4edaacb6b` | `mswebench/zeromicro_m_go-zero:pr-1821` |

## Paired-run boundary

- For the historical cases, give Pi and Sigma the same archived task input and the same assistance level shown above; do not silently downgrade guided inputs into issue-only tasks or combine their results into one score.
- For #1907 and #1821, give both agents only the official issue title/body and a generic repair instruction. Withhold PR title/body, PR URL, `hints`, `fix_patch`, `test_patch`, target diff, GitHub MCP, and web search.
- Use fresh sessions/worktrees at the listed base commits. Keep the model, token/time budget, evaluator image, and scoring procedure matched per pair. Record each case separately, including patch application, target tests, token use, and tool calls.
- Report the five historical cases as demonstrations with their caveats. Report the two new cases as a two-task issue-only check. Do not present the selected set as an unbiased success rate: five cases were selected because Sigma had positive results.

## Dataset/source pin

- Dataset revision: `56ff018c04a38e27ada1e9d0a6d5839a51f88f0d`.
- [Official Multi-SWE-Bench Go-zero dataset](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench/blob/56ff018c04a38e27ada1e9d0a6d5839a51f88f0d/go/zeromicro__go-zero_dataset.jsonl)
- [Official Multi-SWE-Bench evaluator](https://github.com/multi-swe-bench/multi-swe-bench#readme)
- Naming/research note: [what “subset” means](multiswe-subset-naming-research.md).
