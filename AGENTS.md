# Sigma Agent project instructions

These are Sigma's default project instructions. The runtime loads this file
when the active repository is Sigma; a run config may also supply it to an
isolated task workspace as an external profile. Keep it short and stable.

## Where project guidance belongs

- Put stable project workflow and document pointers here.
- Put reusable, source-backed facts in `.sigma-agent/MEMORY.md` using its dated
  `kind` and `source` format. Memory is retrieved on demand; it is not a task
  transcript or a place for general procedures.
- Put task-specific procedures in `.agents/skills/<name>/SKILL.md`. Sigma shows
  Skill names and descriptions first, then loads a matching Skill when asked.
- Session snapshots are only for resuming one conversation.

## Working on Sigma

- Read the relevant implementation, callers, and tests before changing code;
  follow the runtime's change-contract and verification workflow.
- Treat `config.json` and `evals/configs/*.json` as the source of runtime and
  experiment settings. Avoid copying their values into long-lived notes.
- When benchmark reference files are available in the active workspace, read
  `evals/benchmark-protocol.md` and the relevant plan under `evals/` before
  preparing or interpreting a run. The 50-task SWE-bench Verified Mini list
  and results live in `evals/swebench-verified-mini-50-evaluation-plan.md`.
- A separate task checkout does not inherit this file automatically. If a run
  config supplies it externally, treat Sigma-repository paths as references to
  the profile source, not as files in the task checkout.

## Issue-only benchmark workspace boundary

When solving an issue-only benchmark in a mounted repository, treat `/workspace`
as the only permitted filesystem root. Run `pwd` first and keep every read,
write, generated script, and test fixture inside that checkout. Put temporary
diagnostics under `/workspace/.scratch/`; do not use `/tmp`, home directories,
installed packages, caches, wheels, or alternate checkouts. Before a command
creates a file, verify its destination is under `/workspace`. Keep scratch
artifacts out of the candidate patch and inspect the final diff. If a diagnostic
cannot be done inside the checkout, explain the limitation and continue from
the repository files and tests already available.

## Persistent memory

Keep memory factual, concise, repository-scoped, and attributable to a file,
test, or evaluation record. Verify a retrieved fact against the current source
and tests before applying it. Do not store credentials, raw transcripts, or
benchmark instructions that already belong in the evaluation protocol.
When a run config supplies an external memory file, it is the shared source of
truth for that configured repository scope; Sigma copies it only into the
isolated session workspace and searches it under the configured stable ID.

## Benchmark archive layout

- Put completed SWE-bench task and batch archives under `C:\dev\Agent\coding-agent-harness\archive\benchmarks\swebench-verified-mini\runs`. Keep the shared issue index at `go-runtime\var\benchmark-data\swebench-verified-mini\evaluation-issues.md`; this local benchmark log is ignored and is not a commit artifact.
- Keep run-specific temporary logs and active evaluation staging under `go-runtime\var`; after the Agent run, official evaluation, and archive are complete, move the finished archive into the central `archive` tree.
- Keep Sigma's resumable runtime state in `go-runtime\var\runs` and `go-runtime\var\sessions`; do not move incomplete session state as part of archive cleanup.
- Historical `swb5\outputs` records are in `archive\legacy\swb5\outputs`; shared datasets and task checkouts remain under `swb5`.
