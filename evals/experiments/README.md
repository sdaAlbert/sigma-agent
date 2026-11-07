# Sigma Agent experiments

This directory is the public-facing index for Sigma Agent evaluation work. It
contains reproducible protocols, current status, and evidence-backed findings.
It does not replace the existing detailed plans, run logs, evaluator outputs,
or central archive.

## Current studies

| Study | Question | Status on 2026-11-07 | Evidence boundary |
| --- | --- | --- | --- |
| [SWE-bench Verified Mini, 50 tasks](swebench-verified-mini-50.md) | How does Sigma perform on a fixed set of repository issue tasks? | 50 task IDs are tracked; historical runs span multiple models and runtime conditions. | Descriptive history, not one matched model score. |
| [Skill ablation](skills-ablation.md) | Does loading a relevant Skill improve issue resolution enough to justify its cost? | One matched one-task smoke and several targeted, non-matched retests are recorded. | Too little controlled evidence for a general effect. |
| [Repository memory](repository-memory.md) | Can Sigma retrieve reviewed repository facts, and do those facts help later issue work? | A six-query fixture and protocols exist; live embedding evaluation and matched issue pairs remain incomplete. | No demonstrated coding-success gain. |

The [Pi 50-task plan](../pi-50-task-evaluation-plan.md) is a planned follow-up,
not a completed experiment. Its protocol requires fresh matched Sigma runs before
making a Pi/Sigma comparison.

## How to read findings

- **Observation** means directly present in a run record, source, or evaluator
  output. It does not imply causation.
- **Hypothesis** is a proposed explanation or next experiment, not a result.
- **Verified** means a specified check passed under the recorded conditions;
  it does not generalize beyond those conditions.
- `resolved`, `unresolved`, `invalid`, and `incomplete` describe different
  outcomes. Provider, setup, patch-application, or evaluator failures are not
  model failures.
- Keep the Agent's raw patch result separate from any source-only diagnostic
  patch. A source-only result helps diagnose behavior but does not silently
  replace the raw candidate result.

## Reproduction entry points

Start with the shared [issue benchmark protocol](../benchmark-protocol.md),
then follow the study-specific instructions. Canonical configs and helper
scripts remain in [`evals/`](..); this directory links to them rather than
copying a second set of settings. Use environment variables or Sigma's local
credential store for provider keys. Never commit keys, transcripts, full task
workspaces, evaluator patches, or resumable session state.

The detailed run artifacts are retained in the project's central archive,
outside this Git repository. Each study below cites stable batch/task IDs so a
reader with archive access can locate the underlying evidence. The summaries
here are intentionally sufficient to interpret the evidence without those
local files.

## Archive access

The central archive is stored beside this repository in the working project
tree and is not included in this Git branch. Older run plans may contain local
archive paths; use the cited batch and task IDs to locate those records in an
archive copy. This public experiment directory does not depend on those local
paths.

## Files

- [SWE-bench Verified Mini](swebench-verified-mini-50.md)
- [Skill ablation](skills-ablation.md)
- [Repository memory](repository-memory.md)
- [Cross-study findings](findings.md)
- [Portable config example](../configs/examples/issue-only.json)
