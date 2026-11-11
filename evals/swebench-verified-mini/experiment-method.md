# Experiment method

This document describes how the 50-task Sigma study was run and how to start a
new comparable run. In this project, Codex coordinates each task's preparation,
Sigma launch, official evaluation, and archive review; it does not run a
task-specific batch script. The full repository/Docker command sequence is in
the [benchmark protocol](benchmark-protocol.md); this page records the study
scope, comparison rules, and one concise example of a changed run condition.

## Study scope

- The fixed set is the 50 `instance_id`s in the [official results table](evaluation-results.md),
  from [`MariusHobbhahn/swe-bench-verified-mini`](https://github.com/mariushobbhahn/SWEBench-verified-mini),
  `test` split, revision `b316c349947c29963fce3f4a65967c9807a4b673`.
- The 50-task subset was selected by the dataset authors using clustering and
  optimization to approximate the larger benchmark's difficulty and pass-rate
  distribution; it is not a simple random sample. The [consolidated task
  record](tasks/README.md) contains repository versions, base commits, run
  history, and available archive references.
- This Mini study is an internal regression/evaluation set. Its result is not
  the public SWE-bench Verified 500-task score.

## What Sigma receives

Give Sigma the issue statement only. Do not provide the reference patch,
official test patch, hints, PR diff, target test names, or evaluator output.
Keep GitHub MCP and unrelated MCP tools disabled. For offline issue-solving
runs, set `web_search_enabled: false` and `allow_container_network: false`.
Skills and Memory must be recorded as explicit run conditions; task-specific
Skills may be staged into the isolated checkout and hashed in the run manifest.

## How one task runs

Benchmark tasks use the non-interactive CLI, not the TUI. Codex acts as the
experiment operator: it prepares the environment, reads the issue text from the
dataset, starts Sigma, collects the artifacts, runs the official evaluator,
and archives the results. It does not simulate keystrokes to operate the TUI
turn by turn or edit Sigma's candidate code. The Sigma runtime calls the model
API and executes the file and command tool calls requested by the model.

A typical launch looks like this. The run also records its session ID,
configuration, event file, and log paths:

```powershell
sigma-agent --repo <task-checkout> --config <run-config.json> --task "<issue title and body>" --github-mcp=false --json --event-file <events.jsonl>
```

Historical runs also used `go run ./cmd/sigma-agent`. Passing `--task` selects
the one-shot CLI path and does not open the TUI. API credentials come from the
local credential configuration; they are never placed in the prompt or run
configuration. The TUI is intended for interactive human sessions, not for
turn-by-turn operation of this fixed benchmark.

A task normally follows these steps:

1. Codex checks the fixed task list and dataset revision, including the
   `instance_id`, issue text, repository version, and exact `base_commit`. It
   prepares a clean checkout, command image, and evaluator image.
2. Codex completes the offline preflight and runs a positive control with the
   official gold patch. If the baseline, dependencies, images, or positive
   control are invalid, it does not spend model calls or classify an
   environment failure as an Agent failure.
3. Codex passes the dataset issue title and body to Sigma as `--task` text.
   Under the issue-only condition, Sigma receives no reference patch, official
   `test_patch`, hidden target tests, PR diff, or evaluator feedback. The model,
   thinking mode, Skills/Memory, budget, network, and MCP conditions are frozen
   in the effective configuration and archived.
4. Sigma runs its model-and-tool loop: it sends the task and conversation
   context to the model, executes requested repository browsing, file
   operations, tests, and shell commands, then returns tool results to the
   model. Project commands run in a prepared Docker command environment, with
   the repository mounted as the Agent workspace. Fixed offline runs disable
   container networking, web search, and unnecessary MCP tools.
5. When Sigma finishes or reaches its budget, the runtime saves the transcript,
   tool trace, events, metadata, and candidate diff/patch. If no valid
   candidate is produced or the run is interrupted, it is marked
   `incomplete` or `invalid/incomplete`, and its resumable environment is
   retained.
6. Outside Sigma's context, Codex submits the candidate patch to the official
   evaluator. If Agent-added tests conflict with the official test patch, Codex
   may also score a source-only diagnostic patch, recording the raw and
   source-only results separately.
7. Codex determines `resolved` or `unresolved` from the official F2P/P2P
   results and evaluator errors, then archives the configuration, patch, logs,
   score, and issue records. Task-specific resources are cleaned up only after
   the archive is verified.

The official evaluator's reference patch and hidden tests remain outside the
Agent workspace. The gold positive control, Sigma's local tests, and official
candidate scoring answer different questions and do not replace one another.

### Example: changed run conditions

Django #11848's later run used a generic Skill-loading hint and Memory off, so
it is a guided run rather than a pure issue-only comparison. Its conditions and
result are recorded in the [task report](tasks/README.md#django__django-11848);
do not attribute the outcome to the Skill alone or pool it with the original
run as if only one variable changed.

### Config template

Use [`templates/issue-only.json`](templates/issue-only.json)
as a starting profile, not as a task-ready file. Make a per-run copy, replace
`command_execution_image` with the exact prepared image for that task, and
record the effective JSON and its hash. For a non-DeepSeek model, remove or
adjust the DeepSeek-only thinking settings and record the provider's actual
reasoning mode. Do not place API keys in the template or run archive.

Codex should stop before the model call if the checkout base, image, Python
version, offline preflight, or gold control is wrong. If the run is interrupted,
preserve the checkout, containers, images, session, and logs and record
`incomplete` or `invalid/incomplete`; do not clean task resources until the
score and archive are complete.

## Configuration and comparison conditions

The former `full-sigma-5`, pilot, and task-override files have been removed
from the public package. Use [`templates/issue-only.json`](templates/issue-only.json)
as a starting point, create a separate effective config for each new run, and
set the exact prepared command image. [`tasks/task-configs.json`](tasks/task-configs.json)
contains sanitized historical snapshots keyed by task ID; those records are for
review and are not ready-to-run configs.

For each run, record the task/base commit, dataset revision, model/provider,
reasoning settings, executable SHA-256 and source commit, effective config and
config SHA-256, Skills/Memory state, prompt condition, runtime limits, task and
evaluator image digests, and session/run IDs. A comparison is controlled only
when these conditions are held constant except for the variable being studied.

### DeepSeek thinking mode and result attribution

Thinking is a run setting, not an automatic property of the DeepSeek model name.
Record `deepseek_thinking_enabled` and the reasoning effort from the effective
config. Earlier Sigma DeepSeek runs used the adapter's thinking-disabled path;
do not treat them as thinking-enabled baselines. The guided Pi #12406 run used
DeepSeek thinking at high effort, and its later Sigma replication used thinking
at medium effort. Those runs also differed in Skill version, steering, and other
conditions, so they do not isolate the effect of thinking.

#### Public DeepSeek V4 scores and harness

DeepSeek's official model card lists these SWE-bench Verified resolved scores:

| Model | Non-Think | Think High | Think Max |
|---|---:|---:|---:|
| V4-Flash | 73.7% | 78.6% | 79.0% |
| V4-Pro | 73.6% | 79.4% | 80.6% |

The six entries have a simple arithmetic mean of **77.5%**; this is our
calculation, not a separate DeepSeek-reported aggregate. The 79.0% Flash result
is specifically **Think-Max**, not Non-Think. [DeepSeek V4 model
card](https://huggingface.co/deepseek-ai/DeepSeek-V4-Flash).

For comparison, the [official 500-task SWE-bench Verified board](https://www.swebench.com/)
lists 79.2% as its highest full-agent entry (Claude 4.5 Opus with Sonar
Foundation Agent / live-SWE-agent submissions). In its same-harness Bash-only
view, the highest entry is 76.8% for Claude 4.5 Opus high with
mini-SWE-agent 2.0.0. These leaderboard entries and DeepSeek's vendor-reported
model-card scores use different evaluation conditions; none is a direct
comparison with Sigma's Mini 50 result.

DeepSeek's API update notes say its public code-agent benchmark tasks were run
with **DeepSeek Harness minimal mode**, **max reasoning effort**, `top_p=0.95`,
and `temperature=1.0`. [DeepSeek API update notes](https://api-docs.deepseek.com/updates/)
This describes a vendor-reported harness/configuration; it is not the Sigma
configuration used in this study. In particular, the 2026-11-07 #12325 Sigma
run used DeepSeek Flash with thinking at **medium** effort, a different harness,
and a task-specific Skill. It passed one task; it did not reproduce or
independently validate the vendor's 79% across SWE-bench Verified. The 79% is a
vendor-reported result, not a third-party rerun under our setup. Keep it as a
vendor benchmark reference, not a directly comparable Sigma result.

The 2026-11-07 Sigma #12325 run gives a useful but still confounded observation.
The 2026-11-06 Sigma run completed 53 turns / 53 tool calls and scored
unresolved (F2P 0/2, P2P 201/201). It had Memory enabled with a 35-entry
snapshot and Python Skill SHA-256
`BBBE22A7157D04E53CF7C14E006ECC3CF3456D48314B849322B86423771A6847`; its
effective config did not enable DeepSeek thinking, matching the then-current
adapter behavior. A separate 2026-11-06 Skill retest also ended unresolved at
29 turns (F2P 0/2, P2P 201/201), with a different Skill hash and Memory index
of 39 entries. These earlier attempts show that adding a general Python Skill
alone had not yet solved the issue. The 2026-11-07 run enabled thinking at
medium effort and completed in 21 turns / 34 calls; its source-only candidate
resolved (F2P 2/2,
P2P 201/201). This is about 60% fewer turns, 36% fewer tool calls, and 64%
fewer input tokens than the 2026-11-06 run.

Those numbers show a promising association, not a causal estimate. The Skill
also changed: the successful run used SHA-256
`4CEF816DFAACABB47A85627A2DD6BE5535A6B3CEFC84BDF192AD54DC0954985E`, which
adds a directly relevant rule to filter candidates by the explicit marker while
building the collection and to remove stale downstream guards. The older Skill
already advised checking representations and downstream validation, but did
not state that more specific rule. That new Skill rule closely matches the
failure in the earlier patch and the logic needed by the successful patch, so
it is the clearest candidate explanation for the correctness change and may
have been a major contributor. Thinking may have helped shorten the search,
but there is no same-Skill off/on comparison to establish that. Memory was also
turned off, and the scored candidate was source-only because the raw patch's
test edit conflicted with the official test patch. So the result cannot tell
us how much came from thinking, the Skill change, Memory, or run variation.
See the [#12325 comparison and
archive](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sigma-thinking-django-12325/run-result.md)
and [the earlier 53-turn run](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261106-python-skill-followup-reruns/django__django-12325/result-summary.md).

To measure thinking itself, rerun a small set with the same model/version,
runtime, exact Skill hash, Memory snapshot, prompt, images, budgets, and scoring
path, changing only thinking off/on; repeat enough tasks to avoid drawing a
conclusion from one issue. Until then, describe faster and correct completion
as an observed outcome under the combined configuration, not as proof that
thinking alone caused it. Thinking mode does not grant web access: the #12325
run had web search, GitHub MCP, and task-container networking disabled. Public
issue recall from model training cannot be ruled out, and the relevant Skill
also makes this an answer-adjacent run rather than a blind issue-only trial.

## Preparation and execution sequence

1. Select the fixed task and verify its exact dataset row and base commit.
   Start from a clean task checkout and a fresh issue-only session unless the
   recorded experiment explicitly tests continuation.
2. Prepare the source checkout, command image, and official evaluator image on
   the host before starting Sigma. Keep network access in this host-controlled
   preparation phase; the Agent container remains offline.
3. Run the protocol preflight. Confirm the task image, interpreter and
   dependencies, `/workspace` mount, checkout base, tracked-file count, Git
   status, evaluator image, and candidate workspace path. For a Django checkout
   mounted from Linux on Windows, set `core.symlinks=false`,
   `core.filemode=false`, and `core.autocrlf=false`, materialize tracked links
   as target text, then verify the host diff and preflight.
4. Run a gold control with the official evaluator. Do not interpret candidate
   results as valid if the gold control or evaluator environment is invalid.
5. Launch Sigma with the issue statement and frozen effective config. Capture
   the complete event stream, transcript, stdout/stderr, tool traces, usage and
   cache counters, run metadata, and final raw candidate patch.
6. Inspect progress at the configured budget checkpoints. Start at the
   configured turn cap (normally 128 for newer runs). Continue the same session
   only when tracked source changes and focused-test evidence show convergence;
   extend first to 160 turns, and go higher only with concrete progress and
   enough budget left for tests and a final diff review. Stop looping runs.
7. Apply the raw candidate patch and official test patch to a clean evaluator
   checkout and run the official evaluator. If test-file overlap prevents raw
   scoring, preserve that outcome and score a source-only diagnostic patch
   separately; never substitute it for the raw result without labeling it.
8. Archive the attempt, score reports, patches, images/manifests, config, and
   issue log. Mark unscored interruptions `incomplete` or `invalid/incomplete`;
   do not call them unresolved. Keep resumable runtime state until the run is
   resumed or its full archive is verified.

## Outcome and metric definitions

- **F2P (fail-to-pass):** tests that fail on the unmodified base and should pass
  after the fix. `F2P 2/3` means two of the three target checks passed.
- **P2P (pass-to-pass):** tests that already passed on base and must keep
  passing. Report both passed and total; any regression prevents `resolved`.
- **resolved:** every F2P passes and no P2P regression occurs, with no evaluator
  error.
- **unresolved / no:** an official candidate evaluation completed but did not
  meet the resolved condition.
- **incomplete / invalid-incomplete:** no valid official candidate score was
  produced, or the run/evaluator was stopped or invalidated before scoring.

Keep Agent verification status separate from official evaluator status. A run
can be incomplete while its candidate patch receives a separate diagnostic
score; label that distinction in both the task section and the archive summary.

## Archive layout

- [`tasks/README.md`](tasks/README.md) consolidates all 50 curated task records with an ID index; [`tasks/task-configs.json`](tasks/task-configs.json) consolidates the sanitized historical configuration snapshots keyed by task ID.
- New runs should start from [`templates/issue-only.json`](templates/issue-only.json), with a per-run copy updated to use the prepared task image.
- Raw run snapshots, transcripts, and batch artifacts remain in the local archive and are not part of the public task-record files.
- Full raw transcripts, patches, evaluator output, and retained container/image
  manifests remain in the [central archive](../../../archive/benchmarks/swebench-verified-mini/).

## Operational issue patterns and recoveries

This section consolidates recurring setup, runtime, and evaluation problems
seen during the study. The linked task reports are the source for task-specific
symptoms and outcomes; full traces and command output remain in the linked
batch archives. Keep these environment/runtime incidents separate from whether
Sigma's candidate passed the official evaluator.

- **Windows checkouts, long paths, and symlinks:** Django #12304 and Sphinx
  #10323. Verify the exact base commit and Git status after transferring a
  checkout; record any symlink materialization needed by the host filesystem.
  ([#12304](tasks/README.md#django__django-12304),
  [#10323](tasks/README.md#sphinx-doc__sphinx-10323))
- **Image and dependency preflight:** Django #12304 and Sphinx #10673. Match
  command-image dependencies to the evaluator before starting Sigma; a passed
  host-side check does not prove that the container image is ready.
  ([#12304](tasks/README.md#django__django-12304),
  [#10673](tasks/README.md#sphinx-doc__sphinx-10673))
- **Qdrant availability and fallback:** Django #12304, #12273, and Sphinx
  #7757. Vector indexing can fail when the shared service is stopped; local
  BM25 is a separate fallback. Memory availability alone does not prove the
  Agent retrieved or used a fact.
  ([#12304](tasks/README.md#django__django-12304),
  [#12273](tasks/README.md#django__django-12273),
  [#7757](tasks/README.md#sphinx-doc__sphinx-7757))
- **Provider streams and message ordering:** Django #11885 and Sphinx #7590.
  Empty or invalid assistant history can fail before a useful candidate exists;
  report provider/runtime failures separately from evaluator outcomes.
  ([#11885](tasks/README.md#django__django-11885),
  [#7590](tasks/README.md#sphinx-doc__sphinx-7590))
- **Runs without implementation progress:** Sphinx #8035 and #9229. Track
  source edits and focused-test progress, not turn count alone. Preserve an
  interrupted environment until the run resumes or its archive is complete.
  ([#8035](tasks/README.md#sphinx-doc__sphinx-8035),
  [#9229](tasks/README.md#sphinx-doc__sphinx-9229))
- **Candidate/test-patch overlap:** Sphinx #8721 and #9698. Preserve the raw
  candidate; if its test edits conflict with the official test patch, report a
  separate source-only diagnostic score.
  ([#8721](tasks/README.md#sphinx-doc__sphinx-8721),
  [#9698](tasks/README.md#sphinx-doc__sphinx-9698))
- **Docker networking and cleanup:** Sphinx #11510. Offline runs use the
  explicit offline network policy. Remove only task-owned containers and images
  after evaluation and archive checks; leave shared services intact.
  ([#11510](tasks/README.md#sphinx-doc__sphinx-11510))
- **Evaluator input and output layout:** Sphinx #9320. Validate the one-row
  dataset shape and task-specific output directories before launching the
  official evaluator.
  ([#9320](tasks/README.md#sphinx-doc__sphinx-9320))

For a new incident, record its task, stage, command or image, exact symptom,
recovery, and validity in the task report and batch archive. This concise
cross-task section is the study-level summary.
