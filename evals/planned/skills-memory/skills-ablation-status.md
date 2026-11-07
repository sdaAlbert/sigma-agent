# Skill ablation

**Status date:** 2026-11-07
**Status:** exploratory. One matched one-task smoke is recorded, followed by
targeted retests that changed additional conditions. There is not yet a
multi-task controlled estimate of Skill effect.

## Question

Does loading a relevant repository Skill improve issue-resolution outcomes,
correct tool use, or verification quality enough to justify the added context
and calls? A Skill being loaded is not itself a success.

## Reproduction protocol

Use the same clean task base twice, with fresh sessions and worktrees. Keep the
issue text, model/version, runtime executable, prompt, memory snapshot, Docker
image, turn/tool/token limits, and evaluator identical. Change only
`skills_enabled`.

- Control: [`skills-disabled.json`](configs/skills-disabled.json).
- Treatment: [`skills-enabled.json`](configs/skills-enabled.json).
  Copy any task-specific runtime fields into both configs; Sigma loads one
  config file per run.
- Stage the exact reviewed `SKILL.md` only in the treatment workspace. Record
  its SHA-256 and verify its name/description is discoverable before the run.
- Include both tasks that should match the Skill and near-miss tasks where it
  should not load. Ensure no Skill-specific guidance leaks into the control.
- Disable GitHub MCP, web search, and container networking; use the same issue
  only input and official evaluator in both arms.
- Record raw patch outcome, target and regression tests, Skill selection/load
  events, turns, calls, prompt/tool-result bytes, tokens, cost, latency, and
  all invalid/incomplete attempts.

Run setup and scoring from the shared
[benchmark protocol](../../benchmark-protocol.md). Use a fresh issue set for a
causal comparison; historical retests are not a substitute for the paired arms.

## Current evidence

- **Observation, n=1:** the matched go-port smoke verified that the Skill
  toggle and selection path worked, but the task result did not improve and the
  Skill-enabled arm used more tokens/calls. Record:
  `2026-10-30-skills-go-port-ablation-smoke` (local detailed report retained
  under `evals/results/`, which is not part of this public branch).
- **Observation:** several Sphinx retests load updated Skills but also differ in
  runtime version, budget, or retry conditions. For example, #11510's Skill
  retest changed the runtime and turn cap as well as Skill content and ended
  unresolved with target failures and regressions. This cannot isolate the
  Skill's effect. Archive record: `batch-20261107-sphinx-skill-retest-30`, task
  `sphinx-doc__sphinx-11510`.
- **Observation:** #7985's latest retest loaded the updated Sphinx Skill, then
  stopped on provider quota before a source edit or target test. It is
  incomplete/unscored, not a Skill failure. Archive record:
  `batch-20261107-sphinx-7985-minimax-skill-retest`, task
  `sphinx-doc__sphinx-7985`.
- **Hypothesis:** Skills may help when they encode reviewed, non-obvious
  project procedures or constraints. Test that with matched pairs and report
  outcome and resource cost together.

## Reporting rule

Publish all pairs and failures. Do not claim that Skills increase success rate
until a preselected multi-task set has matched runs under the same frozen
conditions. Keep Skill routing accuracy, task success, regression behavior,
and token/call cost as separate measures.
