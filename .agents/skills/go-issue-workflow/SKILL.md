---
name: go-issue-workflow
description: Use when investigating or fixing a Go bug or issue in this repository.
---

# Go issue workflow

1. Restate the observable failure from the issue or user request. Find the affected package and one nearby implementation or test pattern.
2. Reproduce the behavior with the narrowest relevant existing test. If a regression test is appropriate, add it before changing production code.
3. Trace the failing input through the relevant code path. Make the smallest complete fix and keep unrelated behavior unchanged.
4. Run focused Go tests for the changed package. Run the broader suite when the change crosses package boundaries or the focused result is insufficient.
5. Inspect the final diff and report the behavior changed, the test command and result, and any remaining uncertainty.

Use repository test instructions and configured test profiles. The skill loader exposes only this Markdown file; companion scripts are never executed as part of loading a skill.
