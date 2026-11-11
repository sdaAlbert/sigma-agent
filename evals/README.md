# Sigma Agent evaluations

This directory separates the primary public benchmark from proposed studies
and historical issue-repair work.

## Primary: SWE-bench Verified Mini

Use this reading order for the primary study:

1. Start with the [50-task results (English)](swebench-verified-mini/evaluation-results.md);
   the [Chinese results](swebench-verified-mini/evaluation-results.zh-CN.md)
   contain the same table in Chinese.
2. Read the [experiment method](swebench-verified-mini/experiment-method.md)
   for the study scope, issue-only rules, comparison conditions, and how each
   task run proceeds.
3. See the [failure analysis](swebench-verified-mini/failure-analysis.md)
   for why tasks failed and how benchmark evidence informed Sigma iterations.
4. Follow the [benchmark protocol](swebench-verified-mini/benchmark-protocol.md)
   to prepare and evaluate a new run. The [task report index](swebench-verified-mini/tasks/README.md)
   links task-level run conditions and archived evidence.

## Planned studies

[`planned/README.md`](planned/README.md) lists work that has not produced a
completed evaluation. A controlled memory-on/off coding comparison is **not
done**; its protocol and configs are kept under
[`planned/skills-memory/`](planned/skills-memory/).

## Historical and superseded work

[`historical/README.md`](historical/README.md) indexes early Go issue runs,
superseded Multi-SWE task sets, and cross-study observations. Keep those
results separate from the Mini 50 evidence; they are not a common score or a
matched comparison.

## Shared inputs and local outputs

- [`issue-only.json`](swebench-verified-mini/templates/issue-only.json) is an example for
  preparing a future issue-only run; it is not a frozen experiment config.

## Directory map

```text
evals/
├── swebench-verified-mini/  # primary study: results, method, task records, protocol
├── planned/                 # experiments that still need to be run or frozen
├── historical/              # early Go evidence and superseded plans
└── templates/               # reusable examples, not scored configs
```
