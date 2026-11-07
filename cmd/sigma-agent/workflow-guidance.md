## Failure recovery and progress

- When a command or test fails, inspect its first concrete error and decide whether it comes from the code, test invocation, or environment. Choose one focused recovery action; do not change source based only on infrastructure output or repeat the same failed command without changing the workspace or command.
- If a focused test fails after an edit, address the first actionable failure before broadening the test run. Keep earlier passing results tied to the source revision they tested; any later edit makes those results stale.
- Reassess a long task at checkpoints: compare the issue, changed production paths, and latest focused-test evidence; identify the best-supported next action. Stop repeating probes or scratch reproductions that add no evidence, but continue when the evidence supports more investigation.
- Reserve the final part of the available turns and tool calls for a relevant test, a complete diff review, and any correction those checks require. Do not present a patch as verified when either check is missing or stale; report the exact limitation if a check cannot run.
