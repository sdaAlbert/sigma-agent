# Sigma Agent project status

**As of:** 2026-10-31
**Scope:** Go coding-agent runtime in this directory. Pi Agent is an architecture reference; Sigma Agent is not a complete Pi clone.

## What is implemented

| Area | Current state | Evidence / limit |
|---|---|---|
| Agent runtime | MiniMax-compatible streaming adapter, tool-call loop, bounded retries/cancellation, lifecycle events, before/after tool hooks, per-tool allow/deny policy | A denied tool returns an error to the model; there is no interactive human approval prompt yet. |
| Sessions | Durable JSON snapshots, resume, recovery of interrupted tool calls, append-only session entries with parent links, branching API, and model-change entries | Branching and model changes exist in the session API; the interactive CLI does not yet expose full Pi-style history/model controls. |
| Context | Canonical session history is retained while the provider request uses a bounded extractive window and tool-result compaction | This is not model-generated summarization. |
| Workspace tools | Scoped search/read, `create_file`, hash-checked edits, patching, Docker Go test profiles, diffs that include new files, and session-backed large tool-output artifacts with bounded range reads | Results over the inline threshold are stored separately and referenced from the transcript; artifacts have per-file and per-session caps. Execution is an allowlist. Test reliability depends on the selected image having the repository dependencies. |
| Delegation | One read-only investigation subagent per run, with a separate transcript and bounded turns/tools/context | It cannot edit files or run tests; the parent owns implementation and verification. |
| GitHub integration | Optional read-only GitHub MCP tools; issue, repository, and pull-request context | This does not provide general-purpose remote tool execution. |
| Retrieval | BM25 works without services; optional Qdrant vector search and reciprocal-rank fusion use an embedding endpoint | SWE-bench BM25 dev baseline: 175 cases, Recall@5 0.4751, MRR 0.3485. Hybrid quality has not been measured end-to-end. |
| Skills | Discovers `.agents/skills/**/SKILL.md` and loads matching skill instructions on demand | One matched smoke did not show a quality gain; broader effect is unmeasured. |
| Repository memory | Human-reviewed Markdown facts are retrieved on demand with local BM25; optional embedding/Qdrant search adds vector retrieval with repo scoping and provenance | Whole memory files are no longer put into prompts. The vector retrieval path has a smoke result; a coding A/B did not establish repair benefit. |
| Interfaces | One-shot CLI, interactive terminal session, JSON-lines output, and loopback HTTP/SSE task service with durable messages/resume | The HTTP service is a local prototype without authentication; do not expose it as-is. |

## Recent task evidence

Two targeted go-zero tasks have now passed their official per-instance evaluator:

- **PR #964, etcd authentication:** 1/1 resolved. The task prompt included a public PR link and a detailed implementation checklist (configuration, authenticated registry/client path, publisher, and zrpc server); it did not include the PR patch text, and the transcript shows no tool fetching that diff. This is useful evidence that Sigma Agent can implement and debug a specified change, but not that it independently discovered the full design. The evaluator also needed a test-environment workaround for `gomonkey` and two unrelated interceptor calls; the product patch did not add `gomonkey`.
- **PR #2537 / issue #2508, UUID unmarshalling:** 1/1 resolved, but this was a reference-patch reproduction: the task included explicit acceptance cases and the Agent called GitHub MCP `pull_request_read(get_diff)` for PR #2537. It therefore received the target PR diff. Do not treat this as independent repair or ordinary hints-assisted evidence. MiniMax returned HTTP 500 after the patch had been saved, and the persisted patch passed the evaluator.

The #964 run is a detailed-specification-assisted task-completion result. The #2537 run is a reference-patch reproduction because the Agent fetched the target PR diff. Neither is an issue-only generalization result or a clean estimate of repair rate. The older issue-only #2537 failure remains separate evidence: its task input did not state the tested value/pointer and map-byte cases, and its patch was incomplete.

Detailed commands, evaluator adjustments, and per-stage counts are recorded in [the guided PR evaluation](evals/results/2026-10-31-guided-pr-964-and-2537.md). The earlier BM25 and memory experiment results remain in `evals/results/`.

## Main gaps

1. **Coding reliability:** evaluate the planned five-task go-zero set under a clearly named assistance protocol, with one frozen model/config, clean worktrees, and a consistent evaluator image. The existing #964/#2537 runs count as guided task-completion evidence, but not as issue-only independent-repair evidence. Keep those categories separate; neither is inherently the only useful measure. Explicitly pass `--github-mcp=false` on benchmark runs and verify `github_mcp_enabled: false` in `run.json`.
2. **RAG value:** measure hybrid retrieval against the 175-case BM25 baseline, then run matched issue-resolution tasks with retrieval on/off. Retrieval metrics alone do not prove improved coding.
3. **Tool completion:** continue reducing unproductive exploration and make sure the final budget covers focused tests, a complete diff review, and unresolved acceptance items. Raising turn limits alone did not fix the previous incomplete #2537 patch.
4. **Subagent value:** compare the current read-only investigation tool against a matched no-delegation run; the implementation exists, but coding-quality/context savings are not measured.
5. **Pi parity:** add CLI-visible history navigation/model controls and continue testing interrupted-session recovery. The current implementation is a useful subset, not full Pi session/TUI/extension parity.
6. **Service readiness:** add authentication and deployment safeguards before using the HTTP/SSE service beyond loopback development.

## Recommended order

1. Decide and freeze the five-task assistance protocol (issue only, issue plus acceptance criteria, or issue plus design references). The stored `var/benchmark-data/qwen-five-prs/selection.json` is still Qwen-configured; preserve it as provenance and create a separate MiniMax config. Follow [the benchmark protocol](evals/benchmark-protocol.md): explicitly disable GitHub MCP and verify run metadata. Do not expose target PR diffs in either track unless explicitly measuring reference-patch reproduction. If measuring independent repair, rerun #964 issue-only and run the remaining tasks the same way. If measuring collaborative usefulness, give each task consistent, logged human guidance and report that track separately. Record every attempt, including failures and model-server errors.
2. Make the #964 test image reproducible before that rerun. Keep test-only dependencies and evaluator-fixture corrections in the evaluation environment, separate from the product patch.
3. Run the BM25-versus-hybrid retrieval evaluation on the same 175 cases. Report Recall@5/MRR and indexing failures separately from issue-resolution outcomes.
4. Use matched multi-task ablations to decide whether Skills, memory, or subagent delegation improves results enough to justify added tokens and complexity.
5. Only then expand Pi-compatible CLI/session operations or harden the task service, based on observed user need.

## Model errors and quota

The latest MiniMax HTTP 500 occurred while the account panel showed substantial remaining quota. Record a 500 as a provider/request failure, not as quota exhaustion by default. Preserve the run artifacts, retry with bounded backoff, and only change providers if retries continue failing or a specific quota response is returned.
