---
name: go-issue-workflow
description: Required workflow for investigating or fixing any Go issue or bug in this repository; load before repository analysis.
---

# Go issue workflow

## Required change-contract gate

For every issue, inspect the relevant source, callers, and tests before the first file-changing tool call. Then send a concise **Change Contract** in an assistant-visible message. Do not edit production code until it states:

Record the contract with `record_change_contract`, citing at least two paths successfully inspected with `read_file`. The runtime rejects source writes until the contract tool succeeds and a later assistant turn has reviewed its acknowledgement. If investigation changes the scope or interface, record the revised contract before the next edit.

- **Behavior and trigger:** what observable behavior changes, and what specifically opts into or triggers it.
- **Scope boundary:** whether it applies globally, per repository/service, per route/endpoint, per request, or per operation/caller. State the issue or code evidence for that boundary.
- **Existing contract:** the current API/configuration entry point and the callers/tests that depend on it. Prefer extending a matching existing abstraction over creating a parallel setting.
- **Execution path:** the ordered path from the outer entry point through dispatch and wrappers to the affected behavior and response. Name every relevant short-circuit point and which later layers run or are skipped. For HTTP routing, include route/method matching and whether an explicit handler or fallback runs.
- **Acceptance checks:** at least one case where the behavior must happen and one adjacent negative-control case where it must not happen, plus the relevant compatibility and test expectations.

If the issue is unclear about scope, first inspect existing usage and nearby API patterns. Do not silently choose a broader or narrower scope. If evidence still does not settle a choice that changes caller-visible behavior, ask the user; otherwise state the narrowest compatible assumption in the Change Contract and test both the selected and unselected paths.

Treat request metadata (such as headers), environment variables, and global configuration as selectors only when the requirement or existing API explicitly makes them selectors. A protocol marker may identify the kind of request without authorizing a policy change for every matching request. If callers need to opt in independently, expose or reuse an explicit option at that boundary instead of inferring the policy from unrelated global state.

## Implementation and verification

1. Translate the issue into acceptance checks for observable behavior, affected inputs, compatibility constraints, and expected test evidence. Before selecting the implementation layer, trace the failing request from the outer server/client entry through registration, dispatch or method matching, wrappers, middleware/auth, handler, and response. Ground each hop in inspected code; for each layer record its input, action, whether it can stop processing, and which next layer is reached. Do not infer the full path from the issue's named file or a pasted snippet. Compare a failing input with adjacent variants such as route match/miss, explicit handler/fallback, method, or authenticated/rejected request. If behavior depends on a request being intercepted before route dispatch, test both the interception response and whether a registered downstream handler/middleware ran; a status code alone does not prove short-circuiting. Choose the narrowest shared boundary reached by every failing path, then cover the affected path and an adjacent unaffected path. Inspect relevant interfaces, call sites, configuration structs, mocks/test doubles, and nearby patterns before designing a new type or API; update all affected callers and test doubles together when an API changes. When a new option changes an effective value, trace it through the public option, state carrier, canonical resolver/helper, and every consumer. Prefer updating that resolver to encode the option's semantics over conditionally bypassing one downstream consumer. Search for every resolver call site after changing its signature, and test both option states along with inherited/default behavior.
   - For an opt-out that changes a resolved value, do not implement it only by skipping one handler or consumer when a shared resolver already determines that value. Pass the option into the resolver and verify its direct result for both opted-in and default cases, then add an integration check for the selected path and an adjacent unaffected path.
2. Reproduce the behavior with the narrowest relevant existing test. If a regression test is appropriate, add it before changing production code. Distinguish a test that cannot compile or run because of the environment from a behavioral failure. Call a failure pre-existing only after running the same command on the clean starting commit; otherwise label it unclassified.
3. Choose the narrowest shared boundary that sees every failing path while preserving adjacent passing paths. Confirm the chosen layer is reached for each acceptance case; avoid changing lower layers that run only after an earlier dispatch/short-circuit, or globally reordering unrelated middleware, unless the traced flow requires it. Make the smallest complete fix that satisfies the Change Contract.
4. Before adding tests, inspect neighboring tests and use public types and methods that exist in this checkout; do not invent test helpers or server APIs. Name tests for the specific behavior and scenario rather than simply copying a new public option's name, reducing collisions when an external benchmark test patch is later combined with the candidate. Run focused Go tests for changed packages, then broader tests when the change crosses package boundaries or focused results are insufficient. Review compiler output and failures; do not infer success from a patch applying or from unrelated tests passing. After any later source or test edit, rerun the focused tests against the final patch.
5. Before finishing, compare the final diff with every Change Contract field: actual scope versus intended scope, public API/configuration, positive case, negative control, compatibility, and test results. If any item differs or is unverified, report it as incomplete and continue fixing it when possible. Do not claim completion based only on the Agent's chosen implementation or passing local tests.

For benchmark tasks, follow the benchmark's information boundary: when target-PR hints or diffs are prohibited, use the issue, current checkout, and permitted tests only. Do not retrieve or expose the target PR diff as a shortcut.

Use repository test instructions and configured test profiles. The skill loader exposes only this Markdown file; companion scripts are never executed as part of loading a skill.
