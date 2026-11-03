# Sigma Agent project status

**As of:** 2026-11-02
**Scope:** Go coding-agent runtime in this directory. Pi Agent is an architecture reference

## What is implemented

| Area | Current state | Evidence / limit |
|---|---|---|
| Agent runtime | MiniMax and Anthropic Messages adapters plus OpenAI Chat Completions-compatible adapters for OpenAI, DeepSeek, and OpenRouter; tool-call loop, bounded retries/cancellation, lifecycle events, before/after tool hooks, per-tool allow/deny policy, parallel safe reads and path-locked concurrent file writes | Mixed read/write batches and shell/tests stay serialized. A denied tool returns an error to the model; there is no interactive human approval prompt. |
| Sessions | Durable JSON snapshots, resume, recovery of interrupted tool calls, append-only session entries with parent links, branching API, model-change entries, CLI model selection and in-session switching | Interactive `/model` supports listing, switching between turns, and saving the default. History navigation and the full-screen Pi TUI are not implemented. |
| Context | Canonical session history is retained while the provider request uses a bounded extractive window and tool-result compaction | This is not model-generated summarization. |
| Workspace tools | Scoped search/read, `create_file`, hash-checked edits, patching, Docker Go test profiles, arbitrary Docker image selection and reusable prepared environments for `run_command`, session-persistent background commands, diffs, and large-output artifacts | One background job per session, 30-minute limit. Package caches persist under the worktree; containers are offline by default. `prepare_environment` needs network access to install OS packages; sibling service stacks are not provisioned. |
| Delegation | One read-only investigation subagent per run, with a separate transcript and bounded turns/tools/context | It cannot edit files or run tests; the parent owns implementation and verification. |
| GitHub and publishing | Optional GitHub MCP issue/repository/PR context; direct CLI can opt in to commit/push a new named feature branch and `--github-write` can create a PR | PR creation is opt-in and restricted to the local `origin` repository and configured feature branch. Push is non-forced; no merge or issue-write tools are exposed. |
| Source browsing | File listing, literal search, and bounded reads; no source-code embeddings or Qdrant index | This remains the code-navigation path available to the Agent. |
| Skills | Discovers `.agents/skills/**/SKILL.md` and loads matching skill instructions on demand | One matched smoke did not show a quality gain; broader effect is unmeasured. |
| Repository memory | Markdown facts are retrieved on demand with BM25; optional embeddings/Qdrant add exact-repository-scoped vector search and RRF. Optional post-run capture can append up to three evidence-backed notes after a successful non-interactive task. | The six-query fixture and automatic writer have not been evaluated in matched coding runs; keep auto-write disabled for controlled A/B tests. VibeMemBench runs use Sigma itself as solver; both arms of the latest #42 pair hit 64 turns and failed compilation. Runtime corrections are in place but not yet rerun. See [the Sigma VibeMemBench run guide](evals/vibemembench/README.md). |
| Interfaces | One-shot CLI, interactive terminal session, JSON-lines output, and loopback HTTP/SSE task service with durable messages/resume | The HTTP service is a local prototype without authentication; do not expose it as-is. |

## Recent task evidence

The issue-level run inventory is summarized in [the benchmark table](evals/issue-benchmark-inventory.md). It distinguishes clean issue-only results from hinted runs, guided reference reproductions, evaluator diagnostics, and unfinished attempts. At present, only go-zero issue #1729 / benchmark PR #1783 is a documented clean issue-only success; the other resolved evaluator reports used extra guidance or target-PR access.

The active evaluation plan is the [Pi 50-task baseline](evals/pi-50-task-evaluation-plan.md): all 15 go-zero instances, all 16 verified grpc-go instances, and 19 seeded cli/cli instances. It includes the previous Sigma go-zero tasks, with historical assistance conditions labeled; Pi is to be run fresh on all 50. No runs have started. For a valid Pi/Sigma head-to-head, rerun Sigma on the same 50 tasks under the same issue-only protocol; do not combine historical Sigma outcomes with the fresh Pi score. The earlier 7-task demo plan and 10-task reserve list are superseded.

- **PR #964, etcd authentication:** 1/1 resolved in a strongly guided run. The prompt included a public PR link and required paired `User/Pass` validation, `EnableAuth`, credential propagation through internal etcd clusters and authenticated Registry/DialClient methods, authenticated Publisher/keepalive, zrpc server selection of the auth path, mock updates, and focused tests; it also explicitly ruled out TLS and a global endpoint credential map. The Agent did not receive the PR diff and made no GitHub tool call, but this is detailed-specification-assisted evidence, not independent issue repair. The evaluator also needed a test-environment workaround for `gomonkey` and two unrelated interceptor calls; the product patch did not add `gomonkey`.
- **PR #2537 / issue #2508, UUID unmarshalling:** 1/1 resolved, but this was a reference-patch reproduction: the task included explicit acceptance cases and the Agent called GitHub MCP `pull_request_read(get_diff)` for PR #2537. It therefore received the target PR diff. Do not treat this as independent repair or ordinary hints-assisted evidence. MiniMax returned HTTP 500 after the patch had been saved, and the persisted patch passed the evaluator.

- **Issue #1729 / benchmark PR #1783, JSON unmarshalling:** 1/1 resolved under an issue-only prompt containing the official issue title/body. The run explicitly disabled GitHub MCP, web search, Skills, and repository memory; the target PR text and patch were not provided. Both fail-to-pass tests passed, and the Agent patch left exactly the same three failing tests as the base. This is the first clean issue-only success, but only one sample. The Agent hit its 32-turn cap after saving the patch and consumed 550,688 input tokens, so efficiency and generalization remain unproven. Full controls and evaluator notes are in [the issue-only report](evals/issue-only-go-zero-1783.md).

- **Issue #964, issue-only retry:** 0/1 resolved. GitHub MCP, web search, Skills, and memory were disabled, and the Agent received only the official issue title/body. The initial `corrupt patch at line 316` came from a stale cached evaluator image; after rebuilding it and using a fresh workdir, the Agent's candidate patch applied but left all 7 test-patch failures unchanged. Compiler errors showed mismatches between the introduced `Auth` API and the tests' expected config/Registry contract; the evaluator also lacked `gomonkey` for one test package. The 32-turn run was resumed for 64 more turns, and both segments reached their limits. This is a real unresolved implementation attempt, not an invalid patch-format result. Details are in [the attempt report](evals/issue-only-go-zero-964-attempt-2026-11-01.md).

The #964 run is a detailed-specification-assisted task-completion result. The #2537 run is a reference-patch reproduction because the Agent fetched the target PR diff. The #1729 run is one successful issue-only result, not a reliable estimate of repair rate. The older issue-only #2537 failure remains separate evidence: its task input did not state the tested value/pointer and map-byte cases, and its patch was incomplete.

Detailed commands, evaluator adjustments, and per-stage counts are recorded in [the guided PR evaluation](evals/results/2026-10-31-guided-pr-964-and-2537.md). The earlier BM25 and memory experiment results remain in `evals/results/`.

## Main gaps

1. **Coding reliability and cost:** the only documented clean issue-only success is #1729 / benchmark PR #1783; the clean issue-only attempts on #964, #2537, #2032, and #2283 remain unresolved. Several later runs were diagnostic replays, not independent samples. Use a preselected held-out task set before reporting a rate. Keep issue-only, hinted, detailed-specification-assisted, reference-patch, and memory/Skill-assisted results separate; verify `github_mcp_enabled: false` in every issue-only run's `run.json`.
2. **Memory RAG value:** run the labeled six-query corpus with a real embedding endpoint and Qdrant; then run a few fresh, leakage-controlled Go issue pairs with repository memory on/off. Retrieval metrics alone do not prove improved coding.
3. **Tool completion:** continue reducing unproductive exploration and make sure the final budget covers focused tests, a complete diff review, and unresolved acceptance items. Raising turn limits alone did not fix the previous incomplete #2537 patch.
4. **Subagent value:** compare the current read-only investigation tool against a matched no-delegation run; the implementation exists, but coding-quality/context savings are not measured.
5. **Pi parity:** add CLI-visible history navigation/model controls and continue testing interrupted-session recovery. The current implementation is a useful subset, not full Pi session/TUI/extension parity.
6. **Service readiness:** add authentication and deployment safeguards before using the HTTP/SSE service beyond loopback development.

## Recommended order

1. Freeze the task set, prompt, model, budget, and evaluator protocol before the next score. Do not reuse tasks already used to tune the Agent as held-out results.
2. On a fresh issue, run a separately labeled memory/Skill-assisted attempt using only reviewed, non-target repository facts. Keep the issue prompt limited to its official title/body, disable GitHub MCP and web search, and verify whether `read_skill` and `search_memory` were invoked. The current #1456 attempt was interrupted before completion and is not a baseline.
3. Follow [the benchmark protocol](evals/benchmark-protocol.md): use clean worktrees and consistent evaluator images, never expose target PR diffs in issue-only runs, and keep evaluator test-environment workarounds separate from the candidate patch.
4. Use matched, fresh issue runs to test memory utility separately from Skills or subagent delegation, with the same source browsing tools, model, and budget.
5. Use matched multi-task ablations to decide whether Skills or subagent delegation improves results enough to justify added tokens and complexity.
6. Only then expand Pi-compatible CLI/session operations or harden the task service, based on observed user need.

## Model errors and quota

The latest MiniMax HTTP 500 occurred while the account panel showed substantial remaining quota. Record a 500 as a provider/request failure, not as quota exhaustion by default. Preserve the run artifacts, retry with bounded backoff, and only change providers if retries continue failing or a specific quota response is returned.

## Latest benchmark and environment diagnosis — 2026-11-02

The #2032 issue-only attempts chose a global SSE switch, while the gold patch implements route-level timeout opt-out. A later 128-turn hinted run produced a source-only patch that passed the target `rest` package tests after excluding overlapping Agent-authored tests, but the raw patch did not apply cleanly with the benchmark test patch; it is not an official resolved result. The gold-patch control on the same image passed all 23 N2P tests. See the [128-turn run](evals/results/2026-11-02-multiswe-go-zero-2032-hinted-128-resolver-skill.md) and [issue inventory](evals/issue-benchmark-inventory.md).

The `run_tests` Docker isolation now uses a temporary `--internal` network with no external egress. This fixes network-interface tests that fail under `--network none`. The official go-zero benchmark image uses Go 1.24.1 while the repository declares Go 1.16; three baseline failures remain, including two `rest/router` tests. This does not explain #2032: the official gold patch passes all 23 target N2P tests in the same image. `go test ./internal/sigmaagent` and `go build ./...` pass after the runner change.

The `google__yamlfmt-42` VibeMemBench paired run used Sigma as solver, with up to 256 cumulative turns per arm. The no-memory arm stopped after 165 turns; the memory arm reached 256. Neither official score is interpretable: Sigma's patch and VibeMemBench's test patch both append to `formatters/basic/formatter_test.go`, so the evaluator's second `git apply --3way` conflicted before `go test` ran. The memory patch also replaced two existing factory test cases. This run does not establish whether memory helped. Keep Sigma's own tests for the feature in a separate test file and preserve existing baseline cases before repeating the pair.

The latest #42 attempt also hit a 64K context preparation limit during resume; raising the configured request budget to 96K allowed continuation. This is evidence that the current extractive context manager can still fail on very large accumulated turns.
