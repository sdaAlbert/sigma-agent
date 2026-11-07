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
completed evaluation. The repository-memory retrieval and matched coding
experiment are **not done**; its plan and configs are kept under
[`planned/skills-memory/`](planned/skills-memory/).

## Historical and superseded work

[`historical/README.md`](historical/README.md) indexes early Go issue runs,
superseded Multi-SWE task sets, and cross-study observations. Keep those
results separate from the Mini 50 evidence; they are not a common score or a
matched comparison.

## Shared inputs and local outputs

- [`fixtures/`](fixtures/) contains small test workspaces, including the
  synthetic memory-retrieval fixture and Skill fixture.
- [`memory-retrieval-v1.json`](memory-retrieval-v1.json) stays at this path
  because the evaluator and tests read it directly. It is a labeled query
  dataset, not a run config or completed result.
- [`templates/issue-only.json`](templates/issue-only.json) is an example for
  preparing a future issue-only run; it is not a frozen experiment config.
- `results/` contains ignored local reports. Full transcripts, patches,
  evaluator outputs, and resumable run state remain in the separate central
  archive and are not part of this Git repository.

## Directory map

```text
evals/
├── swebench-verified-mini/  # primary benchmark: plan, evidence, configs, scripts
├── planned/                 # experiments that still need to be run or frozen
├── historical/              # early Go evidence and superseded plans
├── fixtures/                # small checked-in input workspaces
├── templates/               # reusable examples, not scored configs
├── results/                 # ignored local run summaries
└── benchmark-protocol.md    # shared run validity and environment rules
```
