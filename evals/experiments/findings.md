# Cross-study findings

This is a small, source-linked synthesis of observations from the current
Sigma evaluation work. It is not a leaderboard and does not estimate general
success rates.

## Evaluation validity

- **Observation:** a failed setup, provider interruption, patch-application
  conflict, or evaluator failure must be separated from a valid unresolved
  Agent attempt. Example: Sphinx #7985's 2026-11-07 retest stopped on provider
  HTTP 429 / error 2056 before a tracked source edit or target test; it remains
  incomplete and unscored. Record ID:
  `batch-20261107-sphinx-7985-minimax-skill-retest`,
  `sphinx-doc__sphinx-7985`.
- **Verified procedure:** run a gold control, check exact task base and image,
  and preflight patch application before interpreting a candidate score. The
  complete operational checklist is
  [`benchmark-protocol.md`](../benchmark-protocol.md).

## Raw patch and source-only diagnostics

- **Observation:** an Agent-authored test or generated file can collide with
  the benchmark's test patch. Sphinx #7757's raw candidate did not apply
  cleanly because generated Cython output overlapped evaluator state, while the
  separately retained source-only patch resolved 1/1 (F2P 1/1, P2P 33/33).
  Record ID: `batch-20261106-sphinx-skill-7757`,
  `sphinx-doc__sphinx-7757`.
- **Reporting rule:** preserve both candidates and outcomes. The source-only
  score is a diagnostic of production-code behavior; never silently substitute
  it for the raw candidate result.

## Progress is not resolution

- **Observation:** hitting a turn limit or recording a plausible implementation
  contract is not evidence of a successful fix. #7985 reached a useful
  task-specific plan but did not edit tracked source or run the target test
  before the provider limit. Record ID:
  `batch-20261107-sphinx-7985-minimax-skill-retest`,
  `sphinx-doc__sphinx-7985`.
- **Hypothesis:** a checkpointed continuation is useful only when source changes
  and targeted test results show convergence, or the remaining fix is tightly
  isolated with enough budget reserved for verification.

## Skill and memory claims

- **Observation, one Skill pair:** the go-port smoke showed no outcome gain and
  higher tokens/calls with Skills enabled. This verifies the toggle path, not a
  general effect. Record ID: `2026-10-30-skills-go-port-ablation-smoke`.
- **Observation:** the current memory retrieval fixture is synthetic and the
  live embedding run is not complete; the VibeMemBench `google__yamlfmt-42`
  evaluator patch conflict made that attempted memory comparison uninterpretable.
- **Hypothesis:** Skills and memory may help on selected repeated-work tasks,
  but each requires a matched, held-out, multi-task study with separate cost
  and outcome reporting.

## Retrospective implementation lessons

These are engineering hypotheses drawn from archived patch/failure reviews,
not independent benchmark findings:

- Keep classification and filtering in the repository's established pipeline
  instead of adding a parallel path that bypasses existing state.
- Identify the layer that owns a conversion or semantic split, then test the
  affected output and nearby regression cases.
- For file edits, preserve untouched content and inspect the final diff before
  spending the remaining budget on broader tests.
- Treat test completion, diff review, and task acceptance as distinct checks;
  an import smoke test is not a target test.

The retrospective evidence is indexed in the archive record
`evaluation-issues.md` and the related per-batch issue notes. Detailed raw
traces and patches remain in the central archive, outside this Git repository.
