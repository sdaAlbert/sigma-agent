---
name: python-issue-workflow
description: Use when fixing a Python repository issue that depends on API validation, serialization, time-dependent behavior, or compatibility with existing call paths.
---

# Python issue workflow

Use this with the repository's own tests and component-specific guidance. Keep
the change at the layer shown by the failing behavior.

## Trace contracts through the full path

1. Start from the failing test or public entry point. Follow input through
   parsing, validation, model/domain values, and output serialization. Check
   every caller that applies the same contract; fixing one path is not enough
   if another path still enforces the old rule.
2. Before choosing a dependency or value source, look for the repository's
   existing seam. Search nearby tests for patched, frozen, or substituted
   dependencies (for example, mocks, monkeypatches, fake clocks, temporary
   settings, or injected clients); then trace the production code to see how
   that dependency is imported and read. Prefer the established seam when it
   already represents the behavior being changed. A test double is evidence
   about how the project exposes that dependency, but do not copy its setup
   blindly if the issue requires a different public contract. Verify the
   change through the same call path and with the same substitution mechanism.
3. Preserve the repository's canonical classification structures. When an
   existing parser distinguishes positional, keyword-only, required, and
   optional inputs, extend the established membership check instead of
   deriving a parallel list from defaults; parallel classifications can make
   the validator and its duplicate/error-precedence checks disagree.
4. When selecting among related objects, verify that lookup keys and compared
   values use the same representation. A runtime object and a normalized
   index key (for example, an object and a tuple key) are not interchangeable
   under identity comparison; use the repository's canonical conversion
   helper or compare canonical values. Test supported candidate orderings and
   confirm the selected object passes downstream validation.
5. Apply conversion at the layer that owns the value. For model fields and
   serializers, inspect the field's storage/preparation API before reaching
   for a form or display adapter. Preserve existing fallback behavior for
   values that cannot be encoded, and inspect both the in-memory value and the
   final serialized output.
6. When an explicit marker determines which candidates are eligible, enforce
   that rule while building the candidate collection; do not collect ineligible
   candidates and only prefer the marked one later. Audit downstream validation
   for guards that still reject the newly valid selection. Preserve a rejection
   only for the no-eligible-candidate case when the contract requires it. Test
   both declaration orders and the unmarked-only boundary.
7. For overlapping invalid inputs, check which error should win. Keep specific
   structural errors such as a duplicated binding distinguishable from a
   generic unknown-name error. Add cases for both errors separately and for
   inputs that trigger both conditions.
8. For time-dependent code, follow the dependency-seam check above all the way
   from the test patch target to the production read. Confirm that the patched
   object is the one the implementation actually consults; a real system clock
   can bypass frozen-time tests even when its output looks plausible.

## Django model-backed forms

- For a `ModelForm` or model-backed field issue, follow the model field's
  `formfield()` path into the generated form field, then follow that field's
  choice construction and widget rendering. Check where metadata is passed or
  dropped at each boundary.
- Keep model `blank`, form `required`, a choice field's `empty_label`, and a
  widget's rendering behavior distinct. Similar names do not make them
  interchangeable; inspect the concrete values the failing test creates.
- If the failure concerns whether an option exists, first inspect the field
  that builds the choices. Avoid changing a shared widget's rendering rule
  unless a focused test shows the behavior is wrong for that widget generally.
  Run a neighboring ordinary `Select` case to catch changes that leak across
  widgets.

## Change and verify

- Make one localized production change, then run the narrow test that exercises
  the reported path. Add a boundary case and a neighboring compatibility case
  when validation order, value types, or serialization boundaries are involved.
- Read the assertion and test setup before changing expected behavior. Verify
  the final candidate through the same public path the test uses, including
  mocks, adapters, and serializers.
- Keep lifecycle changes at the narrowest owner of the reported contract. Do
  not expand a local setter/parser behavior into constructor, persistence, or
  metadata changes unless a failing test demonstrates that those paths need to
  change too. Remove debug prints and scratch instrumentation before final diff
  review.
- After the final edit, rerun the focused tests and inspect the full tracked
  diff. Keep test fixtures separate from production changes and exclude scratch,
  generated, and cache files from the patch.
- At a continuation checkpoint, compare the tracked diff and focused-test
  evidence with the previous checkpoint. If neither improved, stop extending
  the run and report the unresolved cause instead of repeating exploration.
