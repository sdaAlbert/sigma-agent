# Pi Agent loop and Sigma finalization safeguards

Date: 2026-11-03

## What the Pi source shows

The Pi core agent loop keeps requesting model turns while tool calls remain or queued steering messages exist. Its loop continues after tool execution unless a `finishTurn` hook ends the turn; it exposes `prepareNextTurn`, `beforeToolCall`, and `finishTurn` lifecycle hooks. The inspected loop does not itself show a repeated-command or no-progress circuit breaker. That means Pi does not provide a magic solution to a model repeatedly exploring; an application can add policy at those lifecycle seams.

Sources:

- [Pi agent loop](https://github.com/earendil-works/pi/blob/main/packages/agent/src/agent-loop.ts), especially `runLoop`, `finishTurn`, and `prepareNextTurn`.
- [Pi agent types](https://github.com/earendil-works/pi/blob/main/packages/agent/src/types.ts), which define loop hooks and context transformation.

Pi's coding-agent compaction documentation describes proactive compaction near the model context limit, with recovery after provider context overflow. Its default compaction settings reserve 16,384 tokens and retain a recent tail of 20,000 tokens; these are context-management controls, separate from tool-loop stopping.

- [Pi compaction documentation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/compaction.md)
- [Pi compaction implementation](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/src/core/compaction/compaction.ts)

## What the 11848 trace showed

At turn 107, context compaction was within budget and the task image had no exposed `/testbed` history. The agent was still cycling through distinct inspection and shell-edit commands around a CRLF test file. It was not merely repeating one identical command, so a duplicate-string detector would not address the failure. The run ultimately reached 414 changed lines and was stopped at turn 122 without official evaluation.

The runtime's finalization phase already reserved the last half of the run, but continued to expose `read_file` and broad `run_command` calls. Prompt text said to stop exploring, but nothing in the runtime bounded those calls.

## Sigma change

Sigma now gives the finalization phase a budget of eight inspection calls per run. Inspection includes file reads, code/list/memory/web searches, and non-test `run_command` calls. Once exhausted, the model receives only contract/edit, test, and diff tools; the runtime also rejects inspection calls if the model attempts one anyway. Test commands, targeted edits, and diff review remain available. The budget resets for each new run.

This is a Sigma policy layered on top of the loop, inspired by Pi's explicit lifecycle hooks. It does not claim that Pi has the same guard or that a model will always produce a correct patch. The 11848 task should be rerun only after this change and its runtime tests are reviewed; stopped run artifacts remain unscored.

## Context budget decision

Sigma's configured maximum remains 64,000 estimated tokens. The default proactive compaction threshold is now 30,000 (previously 24,000), including the 11848 retry config. The earlier `24,049 > 24,000` edge case also has a regression test for measuring the final escaped summary and retained context. This margin reduces premature boundary failures; a single irreducibly oversized turn can still exceed the threshold.
