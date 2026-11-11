# How to describe a selected Multi-SWE-Bench Go task set

## Finding

If we select ten task instances from the official Go portion of Multi-SWE-Bench, it is accurate to call them **“our selected 10-task subset of Multi-SWE-Bench Go”** or **“a custom 10-instance evaluation subset drawn from Multi-SWE-Bench Go.”** It is not an official benchmark split or leaderboard result unless the benchmark maintainers accept and publish it as such.

The official repository describes Multi-SWE-Bench as a multilingual issue-resolution benchmark, identifies Go as one of its supported languages, accepts dataset/patch JSONL inputs, and emits per-instance evaluation summaries such as resolved and unresolved counts. Its README separately names the official `mini` and `flash` releases; a hand-selected ten-task list should not be presented as either official release. [Official benchmark README](https://github.com/multi-swe-bench/multi-swe-bench#readme)

The maintainers' experiments repository documents a separate leaderboard-submission process: entries include predictions, evaluator results/logs, metadata, and task trajectories; a “Verified” marker requires the maintainers to reproduce generations on a random subset. A local ten-task comparison therefore supports a reproducible project experiment, but does not itself confer official leaderboard or verified status. [Official experiments and submission instructions](https://github.com/multi-swe-bench/experiments#leaderboard-submission)

## Recommended wording

Before the runs:

> We preselected a 10-task Go evaluation subset from Multi-SWE-Bench for a paired Sigma/Pi comparison. The task IDs and protocol are frozen before generation; the subset is self-selected and is not an official Multi-SWE-Bench split.

After the runs:

> On our preselected 10-task Multi-SWE-Bench Go subset, Sigma resolved X/10 and Pi resolved Y/10 under the stated model, budget, tool access, and evaluator settings. This is a local subset result, not an official leaderboard score.

For reproducibility, freeze and publish the exact instance IDs, source dataset/version, prompt and tool-access policy, model/provider, per-agent budgets, evaluator/image versions, and all per-task outputs. The benchmark repository's evaluator expects patch predictions keyed by organization, repository, and PR number, and generates `final_report.json`; preserve the instance-level logs alongside the aggregate. [Evaluation inputs and output format](https://github.com/multi-swe-bench/multi-swe-bench#run-evaluation)
