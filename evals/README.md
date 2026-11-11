# Sigma Agent evaluations

This directory separates the primary public benchmark from proposed studies
and historical issue-repair work.

## Primary: SWE-bench Verified Mini

The primary study's [50-task results (English)](swebench-verified-mini/evaluation-results.md)
are the starting point; the [Chinese results](swebench-verified-mini/evaluation-results.zh-CN.md)
are also available. See the [failure analysis](swebench-verified-mini/failure-analysis.md)
for causes and iteration evidence, the [experiment method](swebench-verified-mini/experiment-method.md)
for run design and recurring operational issues, and the [benchmark protocol](swebench-verified-mini/benchmark-protocol.md)
for step-by-step preparation and evaluation. Task-specific records are indexed in
[`swebench-verified-mini/tasks/`](swebench-verified-mini/tasks/README.md).

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
├── swebench-verified-mini/  # results, experiment method, task records, configs, scripts
├── planned/                 # experiments that still need to be run or frozen
├── historical/              # early Go evidence and superseded plans
├── templates/               # reusable examples, not scored configs
└── swebench-verified-mini/  # primary study, protocol, and task records
```
