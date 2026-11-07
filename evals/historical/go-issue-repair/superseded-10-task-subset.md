# Deferred 10-task Multi-SWE-Bench Go blind subset

**Status:** Superseded as of 2026-11-02; this 10-task plan was never run. The active plan is the [Pi 50-task baseline](../../planned/pi-multiswe-go/evaluation-plan.md).  
**Purpose:** This remains a possible future blind Sigma-versus-Pi comparison set.  
**Classification:** A project-selected subset drawn from Multi-SWE-Bench Go, not an official benchmark split, leaderboard result, or Verified result.

## Dataset pin and selection

- Dataset: [`ByteDance-Seed/Multi-SWE-bench`](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench), Go task files.
- Dataset revision inspected: `56ff018c04a38e27ada1e9d0a6d5839a51f88f0d` (2026-07-08).
- Official evaluator: [`multi-swe-bench`](https://github.com/multi-swe-bench/multi-swe-bench).
- Split: 5 tasks from `zeromicro/go-zero` and 5 from `cli/cli` to cover two Go codebases and different kinds of changes.
- Freshness check: these ten PR instance IDs were not found in the local Sigma evaluation reports/run folders searched on 2026-11-02.
- Static preflight: each selected instance has an issue statement, base commit, fix/test patch in the dataset, and a task image listed in the official `images_verified.txt`. This only checks dataset metadata and the image manifest; no image pull, gold-control execution, Agent run, or evaluator run has happened.

### Reserve task IDs (not the active set)

| # | Official instance | Issue | Task focus from issue | Base commit | Official evaluator image |
|---:|---|---:|---|---|---|
| 1 | `zeromicro__go-zero-2787` | [#2786](https://github.com/zeromicro/go-zero/issues/2786) | Expose the clock-injectable timing-wheel constructor | `b2571883cae4290b4ef0dd0a583d22c14116c4a3` | `mswebench/zeromicro_m_go-zero:pr-2787` |
| 2 | `zeromicro__go-zero-2463` | [#2351](https://github.com/zeromicro/go-zero/issues/2351) | Add global fields to logging | `63cfe60f1a7583865757780ebfe2a5309a542134` | `mswebench/zeromicro_m_go-zero:pr-2463` |
| 3 | `zeromicro__go-zero-1907` | [#1887](https://github.com/zeromicro/go-zero/issues/1887) | Allow custom gRPC stream client interceptors | `95282edb78f9ea1f70ce210de8b4fc58c636c458` | `mswebench/zeromicro_m_go-zero:pr-1907` |
| 4 | `zeromicro__go-zero-1821` | [#1606](https://github.com/zeromicro/go-zero/issues/1606) | Allow a custom panic recovery handler | `ec3e02624c0b6ea726b34de08d9192d4edaacb6b` | `mswebench/zeromicro_m_go-zero:pr-1821` |
| 5 | `zeromicro__go-zero-2363` | [#2360](https://github.com/zeromicro/go-zero/issues/2360) | Filter unwanted tracing spans such as health checks | `6078bf1a049acc7b95ca2cecef6944269509f88e` | `mswebench/zeromicro_m_go-zero:pr-2363` |
| 6 | `cli__cli-10354` | [#10352](https://github.com/cli/cli/issues/10352) | Fix `gh pr` commands failing outside a Git repository | `42c0cb038be4df1da392f544af5de9ef8b9c01fd` | `mswebench/cli_m_cli:pr-10354` |
| 7 | `cli__cli-10239` | [#10235](https://github.com/cli/cli/issues/10235) | Make extension update checks non-blocking | `6fe21d8f5224c5d8a58d210bd2c70f5a008294c` | `mswebench/cli_m_cli:pr-10239` |
| 8 | `cli__cli-10078` | [#10077](https://github.com/cli/cli/issues/10077) | Find a PR from a branch when the fork is in the same organization | `c35d725b0b89ef090aab17171d091f4630eed5ca` | `mswebench/cli_m_cli:pr-10078` |
| 9 | `cli__cli-10016` | [#10013](https://github.com/cli/cli/issues/10013) | Respect `--watch` when `gh pr checks` also receives `--json` | `179f0c479e955581b947b40d1e1cc3af3b2308c0` | `mswebench/cli_m_cli:pr-10016` |
| 10 | `cli__cli-10009` | [#10007](https://github.com/cli/cli/issues/10007) | Fix a false-positive path traversal check for valid artifact names | `179f0c479e955581b947b40d1e1cc3af3b2308c0` | `mswebench/cli_m_cli:pr-10009` |

## Proposed comparison boundary if this reserve set is used later

For both agents, construct the user task from the official resolved issue title and body only. Do not provide the PR title/body, PR URL, `hints`, `fix_patch`, `test_patch`, target diff, or benchmark acceptance details. Keep web search and GitHub access disabled. Each agent gets a fresh checkout at the listed base commit and a fresh session; submit each saved patch to the same official per-instance evaluator image.

For future runs, use the same model (`MiniMax-M3.1-Flash`), task text, wall-clock/token ceiling, and available read/write/shell/test permissions. Keep each agent's native system prompt and runtime intact, and document the exact versions/configuration before the first run. This is a product-level comparison; it does not isolate one internal feature such as memory or Skills. Any memory/Skill ablation should be reported separately. Existing results retain the model actually used.

For each task, report the official resolved result and stage counts, patch application status, target-test result, tokens, turns/tool calls, and provider/evaluator errors. If a gold-control or infrastructure preflight later shows an instance cannot be evaluated, mark it invalid with evidence; do not quietly replace it and retain the label “frozen 10-task set.”

## Naming the result

Before runs: **“We preselected a 10-task Go subset from Multi-SWE-Bench for a paired Sigma/Pi comparison. The task IDs and protocol are frozen; this is a self-selected subset, not an official split.”**

After runs: **“On our preselected 10-task Multi-SWE-Bench Go subset, Sigma resolved X/10 and Pi resolved Y/10 under the documented model, budgets, tool access, and evaluator settings.”** Do not call it an official leaderboard score or an official Multi-SWE-Bench split. The benchmark repository separately names its official releases such as `mini` and `flash`; this selection is neither one.

## Sources

- [Official Multi-SWE-Bench repository and evaluator README](https://github.com/multi-swe-bench/multi-swe-bench#readme)
- [Official Multi-SWE-Bench dataset card](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench)
- [Official Go-zero dataset file at the inspected revision](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench/blob/56ff018c04a38e27ada1e9d0a6d5839a51f88f0d/go/zeromicro__go-zero_dataset.jsonl)
- [Official CLI dataset file at the inspected revision](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench/blob/56ff018c04a38e27ada1e9d0a6d5839a51f88f0d/go/cli__cli_dataset.jsonl)
- [Official benchmark image manifest](https://github.com/multi-swe-bench/multi-swe-bench/blob/main/scripts/images_verified.txt)
- [Official benchmark submission/leaderboard instructions](https://github.com/multi-swe-bench/experiments#leaderboard-submission)
- [Subset naming research note](subset-naming-research.md)
