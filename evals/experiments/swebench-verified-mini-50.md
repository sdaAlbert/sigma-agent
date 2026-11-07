# SWE-bench Verified Mini: 50-task Sigma study

**Status date:** 2026-11-07
**Status:** 50 fixed task IDs are tracked in the existing plan. Runs and
retests are archived across 35 batch directories. Conditions vary across the
historical series, so this is not a single controlled score.

## Question

Measure whether Sigma can resolve real repository issues using the fixed
SWE-bench Verified Mini task set and official evaluator. Keep the public
500-task Verified leaderboard numbers separate: the Mini set is a project
regression/evaluation set, not an official 500-task score.

## Fixed inputs and scoring

- Dataset: `MariusHobbhahn/swe-bench-verified-mini`, `test` split, 50 task IDs.
- Dataset revision: `b316c349947c29963fce3f4a65967c9807a4b673`.
- The Agent receives the issue statement. Gold patches, test patches, hints,
  pull request references, target test names, and evaluator output stay outside
  the Agent workspace.
- Use the official task evaluator and run a gold control before interpreting a
  candidate score.
- Save the Agent's raw patch unchanged. If a separate source-only patch is
  needed to diagnose overlapping test edits, report it as a separate diagnostic
  result and preserve both outcomes.

The original task list and historical per-task records remain in the
[existing 50-task plan](../swebench-verified-mini-50-evaluation-plan.md). This
public summary does not rewrite that source record.

## Reproducing a new run

1. Use a clean task checkout at the exact base commit and a fresh Sigma session.
2. Prepare the command image and official evaluator image before the model run.
   Run [`preflight-swebench-task.ps1`](../scripts/preflight-swebench-task.ps1) and
   require a successful offline preflight.
3. Build Sigma and record the executable SHA-256, Git commit, provider/model,
   reasoning settings, effective config, Skills/Memory state, and task base.
4. Use an issue-only prompt and explicitly disable GitHub MCP. Keep web search,
   container networking, and unrelated MCPs disabled. Keep the model, prompt,
   runtime limits, image digests, and evaluator fixed across any comparison.
5. Run the task once under the frozen budget. Record every attempt, including
   incomplete or invalid runs; do not silently replace failed tasks.
6. Run the gold control and candidate evaluator. Archive predictions,
   transcript, patches, evaluator reports, and checksums only after the run is
   complete or explicitly classified.

Use [`benchmark-protocol.md`](../benchmark-protocol.md) for the full operational
sequence and the existing example config
[`swebench-verified-mini-full-sigma-5.json`](../configs/swebench-verified-mini-full-sigma-5.json).
For a new clean baseline, start from
[`issue-only.example.json`](configs/issue-only.example.json), then set the
repository-specific command image and frozen limits. Load API credentials from
the local environment or credential store; never put them in the config.

## Current interpretation

- **Observation:** the 50-task plan contains multiple models, Skills/Memory
  settings, runtime versions, task retests, and continuation histories. Its
  entries should be read per task and condition, not averaged as if they were
  one pass@1 experiment.
- **Observation:** evaluator validity and patch integration matter. Sphinx
  #7757 had a raw-patch application problem involving generated Cython output;
  its separate source-only candidate resolved 1/1 with F2P 1/1 and P2P 33/33.
  Archive record: `batch-20261106-sphinx-skill-7757`, task
  `sphinx-doc__sphinx-7757`.
- **Observation:** Sphinx #8638's valid source-only candidate was unresolved
  (F2P 0/1, P2P 33/34). A separate continuation that accidentally enabled
  GitHub MCP was stopped and excluded from the offline run. Archive record:
  `batch-20261105-remaining-9-gate-909c58d`, task
  `sphinx-doc__sphinx-8638`.
- **Observation:** the 2026-11-07 #7985 MiniMax retest ended at cumulative turn
  92 after provider HTTP 429 / error 2056. It made no tracked source change and
  ran no target test, so it is incomplete and unscored; the earlier score stays
  separate. Archive record: `batch-20261107-sphinx-7985-minimax-skill-retest`,
  task `sphinx-doc__sphinx-7985`.
- **Hypothesis:** freezing one model, runtime build, prompt, budget, Skills and
  Memory condition across all 50 tasks would make a useful regression score.
  It would still be a small benchmark subset, not a current frontier ranking.

## Limits

The recorded series is useful for implementation feedback and evaluator
diagnosis. It does not support a clean causal comparison among models, Skills,
Memory, or runtime changes because those variables changed across runs. The
public full Verified scores in the existing plan are background references and
must not be mixed with the Mini 50 results.
