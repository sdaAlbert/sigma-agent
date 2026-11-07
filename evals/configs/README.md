# Evaluation configs

This directory contains Sigma Agent runtime configs used by evaluation runs.
Sigma loads one JSON file per run and fills omitted fields from runtime
defaults; config files cannot be layered together through the CLI. The ablation
files below are therefore standalone default-based configs, not composable
overlays. If a task needs a custom Docker image or shared project profile, put
the same values in both arms and record the exact config and SHA-256 used.

## Config map

| Study | Configs | Role |
| --- | --- | --- |
| SWE-bench Verified Mini | [`swebench-verified-mini/full-sigma-5.json`](swebench-verified-mini/full-sigma-5.json) | Shared full-Sigma profile for the 50-task study. |
| SWE-bench Verified Mini | [`swebench-verified-mini/pilot.json`](swebench-verified-mini/pilot.json) | Pilot settings; Skills and memory are disabled. |
| SWE-bench Verified Mini | [`swebench-verified-mini/task-overrides/`](swebench-verified-mini/task-overrides/) | Django task-specific command images and shared-profile paths. Fourteen task overrides are currently checked in; this is not a config for every task in the 50-task set. |
| Skill ablation | [`ablations/skills-disabled.json`](ablations/skills-disabled.json), [`ablations/skills-enabled.json`](ablations/skills-enabled.json) | Standalone default-based configs; only `skills_enabled` differs. Copy any task-specific settings into both arms. |
| Repository-memory ablation | [`ablations/memory-disabled.json`](ablations/memory-disabled.json), [`ablations/memory-enabled.json`](ablations/memory-enabled.json) | Standalone default-based configs; `skills_enabled` is false in both, and only `memory_enabled` differs. |
| Issue-only example | [`examples/issue-only.json`](examples/issue-only.json) | Portable starting example, not a frozen benchmark run config. |

The retrieval plumbing study in [`../memory-retrieval-evaluation.md`](../memory-retrieval-evaluation.md)
uses a labeled query dataset and command-line flags; it is not an Agent runtime
config. Multi-SWE-Go and Pi plans do not yet have frozen, study-specific Sigma
configs. Add those when their model, runtime, environment, task set, and
ablation conditions have been selected; do not treat the example as evidence
that those studies are configured.

## Layout

```text
configs/
├── ablations/                 # one-variable Skill and memory overlays
├── examples/                  # portable, non-study-specific examples
└── swebench-verified-mini/    # study base configs and task overrides
```

Relative paths inside task overrides are resolved from the config file's
directory. When moving one, preserve its resolved target. These files contain
no provider credentials; use Sigma's local credential store or environment
variables for provider access.
