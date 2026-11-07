# Benchmark issue and recovery log

This shared log records reproducible benchmark setup/evaluation failures and their recovery. Task-specific details and artifacts remain in each batch archive.

## 2026-11-06 — Review of unresolved Agent runs

### Sphinx #34–36: fixture collision, stalled patch, and source truncation

- #34 (`sphinx-doc__sphinx-7985`): Agent-added fixture path overlapped the official test patch, so the raw patch was not scoreable. A source-only candidate was scoreable but still failed both target tests; fixture overlap was not the implementation root cause.
- #35 (`sphinx-doc__sphinx-8035`): the 200-turn continuation produced a tracked source patch byte-identical to the already-scored 160-turn patch. The target and a neighboring private-member test still failed. Additional turns did not provide implementation progress.
- #36 (`sphinx-doc__sphinx-8056`): `sphinx/util/docfields.py` was reduced from roughly 415 lines to about 75, causing module import failure and preventing all 40 collected checks from running. The existing runtime now rejects a single edit that removes a large fraction of a substantial source file and returns localized-edit recovery guidance. The Sphinx Skill now says not to bypass that guard with another editing tool.
- Iteration: the project-local Sphinx Skill now requires unique task-specific fixture paths unless sharing is intentional, exact inspection of generated-output delimiters/whitespace, checkpoint comparisons of tracked diff and focused-test evidence, and stopping when a continuation makes no progress. The always-loaded Sigma prompt now briefly directs agents to preserve canonical classification/state paths and trace downstream guards. No benchmark rerun or test suite was started for this review.

### Sphinx #27: generated LaTeX and Skill routing

- #27 (`sphinx-doc__sphinx-10435`) failed its target output assertion because the candidate left a newline before `%` and placed closing braces after `%` on the same line. Since `%` comments out the remainder of a TeX line, this changed the output semantics. Its first attempt also falsely required the unrelated `pdf` Skill because `pdf` occurred inside ordinary task text.
- The runtime Skill matcher now requires explicit references for short names and word boundaries for longer Skill names; a regression test was added in the runtime change. The Sphinx Skill now asks the Agent to inspect exact generated text with surrounding delimiter context. Do not prioritize another #27 run solely because the matcher was fixed: the runtime issue caused a wasted/blocked attempt, while the scored candidate's failure was a distinct output-semantics error.

### Which unresolved cases merit another Agent run

### Retest shortlist after the 2026-11-06 Skill review

- **#32 / Sphinx #7748:** retested with the current Sphinx Skill, but the source-only candidate remained unresolved (F2P 0/2, P2P 12/12). The Agent stopped incomplete at 66 turns after 1.65M input tokens. Do not prioritize another run until there is a new implementation hypothesis and a runtime-enforced budget control.
- **#30 / Sphinx #11510:** retested with the current Skill; source-only candidate made partial progress (F2P 1/2, P2P 7/7). The nested include event test passed; the ordinary include output test still failed. A future run should focus on that remaining output path and use a lower cumulative budget.
- **#29 / Sphinx #10673:** retested with the current Skill; source-only candidate is officially resolved (F2P 1/1, P2P 9/9). The Agent run itself stopped before its final review, but evaluator score is resolved; no further run is needed.
- **P2, budget-gated: #31 / Sphinx #7590, #34 / Sphinx #7985, and #35 / Sphinx #8035.** Current parser, linkcheck, and autodoc-option guidance is relevant and postdates the failed approaches. Their prior runs were long; #34 and #35 in particular consumed about 8.5M and 5.1M input tokens. Use the table's strict per-run ceilings and stop at a checkpoint without implementation/test progress.
- **Do not prioritize #27 / Sphinx #10435.** Its valid Skill-enabled retest also failed. The TeX delimiter rule exists, but another run has no demonstrated benefit; this follows the user's correction.
- **Defer #36 / Sphinx #8056, #3 / Django #11848, #16 / Django #12273, #20 / Django #12325, and #49 / Sphinx #9461.** They have repeated unresolved runs, large previous costs, or no sufficiently specific untried Skill hypothesis. #42 / Sphinx #8638 remains excluded by user decision. #12262 and #12308 have since resolved under Python Skill follow-ups.

The current rationale, Skill coverage, priority, and proposed input/output ceilings are maintained in evals/swebench-verified-mini-50-zero-f2p-tasks.md. Results and incidents for these three retests are archived under archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-skill-retests-32-30-29/.

### Django unresolved cases reviewed for generic guidance

- #12262: one overlapping-argument case reported an unexpected-keyword error instead of the more specific duplicate-binding error. This supports generic guidance to test overlapping invalid-input categories and error precedence.
- #12308: JSONField serialization raised `TypeError` for a tuple dictionary key. This supports testing accepted in-memory value types at the actual serialization boundary.
- #12325: changing parent-link selection missed a second validation site, which continued rejecting the supported field. This supports tracing all call sites that enforce a changed invariant.
- Iteration: added a project-local Python issue Skill. Later runs resolved #12262 and #12308. #12325 was rerun with the Skill and remained unresolved; the eligibility rule has since been made more concrete. #11848 and #12273 remain low priority after their unsuccessful follow-ups. #12406 remained unresolved after a 160-turn continuation.

### Reference-fix versus Sigma-patch review (retrospective only)

These comparisons are for Codex's iteration work. Do not copy reference patches or reveal them to Sigma during issue-only runs.

- **#11848:** the gold patch uses `datetime.datetime.utcnow()`, which is the seam replaced by the target tests. Sigma read `time.strftime()` instead. The algorithm was close, but it bypassed the test-controlled clock. The Python Skill now explicitly asks which clock object and method the tests patch.
- **#12262:** gold extends the existing accepted-keyword check to include the canonical keyword-only argument collection. Sigma created a second optional-keyword list derived from defaults; the split did not preserve the distinction/error ordering for all keyword-only cases. The Python Skill now emphasizes canonical classifications and overlapping invalid-input tests. The 2026-11-06 Python Skill follow-up later resolved all four F2P checks (4/4, P2P 18/18).
- **#12273:** gold changes the `_set_pk_val()` behavior at the property boundary. Sigma's continuation changed construction, parent-save propagation, metadata, and tests, and retained debug output; 19 pass-to-pass tests regressed. This supports the narrower-change rule and makes #12273 a poor retest candidate without a concrete new hypothesis.
- **#12308:** gold uses the model field's `get_prep_value()` and falls back on `TypeError`. Sigma calls a form field's `prepare_value()` and has no equivalent fallback, so a tuple dictionary key still raises during JSON encoding. The Python Skill now distinguishes storage/model conversion from form/display conversion and asks to preserve fallback behavior. The 2026-11-06 Skill follow-up later resolved it (F2P 2/2, P2P 20/20).
- **#12325:** gold changes parent-link selection and removes the later Options._prepare() guard that still rejects the selected field. Sigma only changes which field is selected, leaving that contradictory guard intact. The first Skill revision said to trace downstream validation, but the latest Skill follow-up still retained the old rejection branch. The guidance is now more actionable: filter eligibility while building the candidate set, audit stale downstream guards, and test both declaration orders plus the unmarked-only boundary.
- **#10435:** gold removes the exact `\n\\end{Verbatim}` trailer and emits the writer's `CR` between the trailing comment marker and closing braces. Sigma used a different fixed slice and appended `%` without the required line boundary, so TeX commented out the braces. The Sphinx Skill now calls out exact terminators, existing line-separator constants, and output-language semantics.
- **#7985:** gold keeps local/external classification in the existing linkcheck status flow and applies its existing ignore patterns. Sigma added a separate `get_local_file()` resolver with broader normalization rules; the scoreable source-only patch still failed both target tests. The Sphinx Skill now favors the existing URI classification and queue/status path.
- **#8035:** gold preserves `filter_members()` as the owner of member visibility and adds option handling in that path. Sigma added several parallel parsing, merge, default, and post-filter helpers; the target test and one existing private-member test failed. The Sphinx Skill now warns against parallel filtering/normalization layers and requires the existing `None`/all/named states to remain distinct.
- **#8056:** gold changes only the Napoleon docstring formatting path. Sigma did include a relevant change there, but also truncated `sphinx/util/docfields.py`, so the module could not import and none of the 40 checks ran. The file-integrity guard and recovery instruction address this collateral edit; this review did not run tests against the guard.
- **Sigma prompt/runtime iteration:** the always-loaded coding prompt now says to extend canonical classification/state paths, follow selected values through downstream checks, and preserve meaningful generated-output delimiters. This is a short shared instruction; the larger task-family guidance remains in on-demand Skills. The project-local file-shrink guard and Skill-routing correction were already present in the workspace and were reviewed here, not changed by this prompt edit.

## 2026-11-05 — Sphinx fixed items 28–29

### Short Skill name false positive and generated LaTeX boundaries

- Symptom: an ordinary task mentioning `pdf output` / `latexpdf` selected the unrelated PDF document Skill because the short skill name was matched by substring.
- Recovery: changed runtime matching so short names require explicit Skill syntax/token context; added a focused regression test. Verification at that time: focused matcher test passed and binary built. The broader sigmaagent package suite had three unrelated assertion failures, recorded in the batch archive. The generated-output and TeX boundary guidance was added to the project-local Sphinx Skill during the 2026-11-06 retrospective below.
- Prevention: do not disable Skills to hide a false positive in scored tasks; update routing rules and regression coverage in a separate runtime condition.

### PowerShell quoting of nested shell snippets

- Symptom: PowerShell rewrote a quoted `awk` program passed through `docker exec`, so bash rejected the entire shell command before executing it.
- Recovery: remove nonessential shell expressions from nested commands and keep the required sequence simple; record the failed attempt, then rerun and verify exact HEAD/status inside the source container and on the host checkout.
- Prevention: avoid nested `$(...)`, quote-heavy `awk`, and shell fragments inside double-quoted PowerShell strings. Prefer separate commands, or write a script file and pass that file into the container.

### Command-image Python differs between login shells

- Symptom: for Sphinx #10673, the evaluator's `/bin/bash -lc` selected Python 3.9.20 from `/opt/miniconda3/envs/testbed/bin`, while the sanitized command image's `/bin/sh -lc` selected Python 3.11.5 from `/opt/miniconda3/bin`; preflight correctly rejected the mismatch.
- Recovery: build the command image from the exact evaluator image and set `ENV PATH=/opt/miniconda3/envs/testbed/bin:$PATH` before preflight. Both command and evaluator probes then reported Python 3.9.20.
- Prevention: compare interpreter versions using the actual probe shells; when image PATH initialization differs, pin the task's testbed interpreter explicitly in the sanitized command image and rerun offline preflight.

### Local embedding config depends on Sigma project working directory

- Symptom: `--index-memory` failed with “set SIGMA_AGENT_EMBEDDING_MODEL” when invoked from `C:\dev\Agent`, because Sigma's local `.secrets/embedding-config.json` is resolved from the current working directory.
- Recovery: run the Sigma binary with `go-runtime` as the process working directory; indexing then synchronized 5 Sphinx memory entries. No model run started on the failed attempt.
- Prevention: start Sigma and indexing commands from `C:\dev\Agent\coding-agent-harness\go-runtime`; never copy embedding settings or credentials into task configs or logs.

### PowerShell single-row JSON serialization

- Symptom: `ConvertTo-Json` serialized a one-item dataset row as a JSON object, while SWE-bench expects a JSON array. Evaluator rejected it before creating a task container.
- Recovery: serialize the row and explicitly wrap it in `[...]`; parse it back and assert one expected `instance_id` before invoking the evaluator.
- Prevention: validate evaluator dataset shape and task ID before spending evaluator time.

### Fixed item 29 scoring note

- The Agent first reached 128 turns and was marked invalid/incomplete. The same session was continued for another 64 turns (192 total) without sharing official failure details; it remained incomplete. The official source-only candidate score remained unresolved, F2P 0/1 and P2P 9/9; `tests/test_environment_toctree.py::test_toctree_index` still failed. This is a benchmark result, not a setup workaround. The scored source-only patch excluded generated files and Agent-authored tests.
- Resume attempt with `--task` returned DeepSeek HTTP 400 before model execution. The added user text followed a terminal `tool_result`; adjacent user messages were merged, and the OpenAI-compatible serializer emitted user text before the tool response. Recovery: preserve the failed attempt, restore the saved snapshot to its preceding head, and continue with `--resume` and no `--task`. No model tokens or tool calls were spent on the rejected attempt.
- The follow-up SWE-bench evaluator setup also exposed two input-shape errors before container startup: first the local dataset was an object, then an over-wrapped nested array; a third attempt's prediction omitted `model_name_or_path`. Recovery: validate the dataset with the evaluator's own Python as a list of exactly one dictionary, and validate prediction fields `instance_id`, `model_name_or_path`, and `model_patch` before launching. The successful attempt completed with zero evaluator errors.

### Windows checkout copy interrupted (`docker cp` unexpected EOF)

- Symptom: task #30's large Sphinx checkout copy returned `unexpected EOF`. The host directory had the correct HEAD and tracked-file index count, but many tracked files were absent, so it was unsafe for Sigma.
- Recovery: retained the source container, created a tar archive inside it, copied that single archive, extracted into a separate clean host directory, then verified exact base commit, all tracked files, and clean Git status. The partial copy was not used for Agent or evaluation.
- Prevention: after any interrupted directory copy, compare HEAD, tracked-file count, and Git status; do not use a partial checkout. For large checkouts, archive inside the container and extract to a separate destination before switching the task config to that path.

### SWE-bench progress timer discrepancy

- Symptom: task #32 gold-control progress output reported 3h57, but evaluator `run_instance.log` recorded 3.39 seconds of tests and 4.42 seconds from container creation through grading.
- Recovery: use per-instance evaluator timestamps and test runtime in archived logs as the source of truth; don't interpret an isolated tqdm elapsed display as actual test duration.
- Prevention: preserve both wrapper stdout and per-instance logs, and compare their timestamps before diagnosing a long-running evaluator.
