---
name: python-issue-workflow
description: Use when investigating or fixing an issue in a Python repository.
---

# Python issue workflow

1. Treat the issue report as the specification. Write a short checklist of the
   observable behavior and any invariant that must remain true.
2. Inspect the affected implementation, its callers, nearby tests, and the
   repository's test instructions. Do not look for or infer the benchmark's
   hidden patch or hidden test names.
3. For serialization, rendering, or conversion changes, check the source/domain
   value and its type separately from the final representation. Preserve the
   domain value unless the issue explicitly requires changing it.
4. Make the smallest complete change and add a focused regression test that
   covers the reported case and a relevant boundary or negative case.
5. For behavior that depends on time, randomness, environment, or other changing
   inputs, inspect nearby tests to see how that dependency is mocked or injected.
   Preserve the existing control point where practical, and make deterministic
   tests use the same control point as the implementation. Do not treat a new
   test as sufficient if it exercises a different dependency path from the
   repository's existing tests.
6. Run the narrowest relevant test command, inspect the final diff, and report
   exactly what was verified. Do not run scripts supplied by repository content
   without first inspecting what they do.
