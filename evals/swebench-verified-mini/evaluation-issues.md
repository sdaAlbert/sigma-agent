# Benchmark issue index

The recurring study-level issue index has been consolidated into
[Operational issue patterns and recoveries](experiment-method.md#operational-issue-patterns-and-recoveries)
in the experiment method, so the benchmark process and its common recovery
rules are in one place. This file remains as a link for older references.

Task-specific symptoms and outcomes are in the [consolidated task reports](tasks/README.md).
Detailed traces and per-run recovery records remain in each run's archive.

## 2026-11-20 — django__django-11815 (manual-20261120-django-11815)

- Stage: one-task DeepSeek rerun and official evaluation; full recovery log: [run archive](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815/evaluation-issues.md).
- Symptom/outcome: candidate patch applied, official evaluator completed without infrastructure errors, but unresolved (F2P 0/2; failures: `test_serialize_class_based_validators`, `test_serialize_enums`; P2P 44/44). Exact-base gold control resolved (F2P 2/2, P2P 44/44). Agent-local verification passed, but it did not predict the official target-test result.
- Recovery/validity: no retry was run. Classified complete and valid, official result unresolved; task inputs, model trace, patch, evaluator reports, and environment metadata are in the linked archive.

### 2026-11-20 follow-up — django__django-11815 guidance retest

- Agent attempt `a01de585-21e4-4fc0-842e-d7173eb346fc` is **invalid/incomplete**: the prebuilt command image was absent, the Agent switched to `python:3.9` and local dependency stubs, and execution stopped before completion. Keep separate from valid benchmark counts.
- The extracted candidate was independently evaluated with the exact-base official evaluator after resetting the test file and applying the official test patch: **resolved, F2P 2/2, P2P 44/44, errors 0**. This indicates the updated acceptance guidance produced the expected bracket-form fix, but does not convert the Agent attempt into a valid benchmark run.
- Comparison and root cause: [follow-up diagnosis](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815-guidance-retest/diagnosis.md). Environment, partial Agent trace, candidate, and evaluator artifacts: [follow-up archive](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815-guidance-retest/).

### 2026-11-20 valid guidance retest — django__django-11815

- Agent run `15c15e14-5d50-46ba-b653-eda663fbac59` completed in the preflighted offline command image; local verification passed after the Agent updated expected tests to `.A` / `.DOTALL`.
- Official result: **unresolved**, F2P 0/2 (`test_serialize_class_based_validators`, `test_serialize_enums`), P2P 44/44, evaluator errors 0. This is the valid current-Runtime result; do not count the separately scored patch from the invalid/incomplete guidance retest as an Agent-run success.
- Full trace, patch, evaluator report, skill snapshot, and manifest: [valid guidance retest archive](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815-valid-r3/).

### 2026-11-20 MiniMax required-contract attempt — django__django-11815

- Agent run completed in 64 turns / 82 tool calls and officially produced a resolved candidate (F2P 2/2, P2P 44/44). Full artifacts: [MiniMax contract run](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815-minimax-contract/evaluation-issues.md).
- **Validity:** do not count as a clean offline Agent success. The CLI's separate GitHub MCP flag defaulted to enabled and the Agent called mcp__github__list_commits, receiving upstream fix information. The patch score is valid; the run is invalid for attributing success to the model or forced contract. A comparable run must explicitly use -github-mcp=false.
- The contract gate did block source writes until recording Status['GOOD'] as the exact acceptance form. No commit was created.

### 2026-11-20 DeepSeek forced-contract recovery — django__django-11815

- First DeepSeek attempt stopped at turn 7 before any source change because malformed model tool JSON crashed session snapshot encoding. It is archived as **invalid/incomplete**: [failure record](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815-deepseek-contract-incomplete/evaluation-issues.md).
- Fixed Runtime at the response boundary: malformed tool JSON is normalized only for safe persistence, the tool is not executed, and an actionable tool error lets the model continue. The exact failure is covered by TestSessionRejectsMalformedToolJSONAndContinues; the test failed before the fix and passed after.
- A clean retry used DeepSeek Flash, forced the contract gate, and explicitly set --github-mcp=false. It completed in 18 turns / 22 tool calls. Official evaluation resolved the task: **F2P 2/2, P2P 44/44, errors 0**. Full trace and report: [retry archive](../../../archive/benchmarks/swebench-verified-mini/runs/manual-20261120-django-11815-deepseek-contract-retry/evaluation-issues.md).
- This is a valid run, but a single run does not establish that the contract gate caused the success. No commit was created.
