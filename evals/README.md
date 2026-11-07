# Sigma Agent evaluations

This directory separates the primary public benchmark from proposed studies
and historical issue-repair work.

## Primary: SWE-bench Verified Mini

Start at [`swebench-verified-mini/README.md`](swebench-verified-mini/README.md).
It links the 50-task plan, current task-level interpretation, configs, and
Docker preparation scripts. Shared validity rules are in
[`benchmark-protocol.md`](benchmark-protocol.md).

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

- [`templates/issue-only.json`](templates/issue-only.json) is an example for
  preparing a future issue-only run; it is not a frozen experiment config.

## Directory map

```text
evals/
├── swebench-verified-mini/  # primary benchmark: plan, evidence, configs, scripts
├── planned/                 # experiments that still need to be run or frozen
├── historical/              # early Go evidence and superseded plans
├── templates/               # reusable examples, not scored configs
└── benchmark-protocol.md    # shared run validity and environment rules
```
