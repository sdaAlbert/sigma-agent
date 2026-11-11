# Agent context and reliability design notes

This document records the design choices behind Sigma's Skills, repository memory, retrieval, context handling, and completion workflow. It distills the repository's design research into maintained documentation. Current behavior and configuration are documented in [Architecture](ARCHITECTURE.md) and [Configuration](CONFIGURATION.md); this page explains the trade-offs and what available evidence does and does not establish.

## Keep context sources separate

Sigma has several kinds of context, each with a different lifetime and authority:

| Context | Lifetime | Best use | Authority |
| --- | --- | --- | --- |
| Session transcript and snapshot | One resumable task/session | Conversation history, tool results, recovery and continuation | Record of the session; may be compacted in model requests |
| Project instructions (`AGENTS.md`) | Every task in a repository | Stable conventions and links to maintained guidance | User/project instruction |
| Skill | Loaded for a matching task | Reusable procedures and domain workflows | Guidance; verify against current code and tests |
| Repository memory | Across tasks in one repository | Reviewed facts that are expensive to rediscover | Reference data; verify against current source |
| Source and tests | Current checkout/revision | Actual implementation and behavior | Primary authority for code changes |

Do not turn session history into project memory automatically. Session state is for resuming work; memory is a small set of durable, reviewed facts. Keep repository memory scoped to an explicit repository identity, preserve provenance and dates, and make the Markdown source the canonical record. A vector index is derived data that can be rebuilt. Current instructions, source, and tests take precedence over retrieved memory.

Skills use progressive disclosure: expose short names and descriptions first, then load a matching procedure on demand. This keeps unrelated workflows out of the stable prompt prefix. Skills should contain reusable methods and boundaries, not hidden task answers. A Skill being available or loaded does not prove it influenced the result; record whether the Agent selected it and compare matched task outcomes before claiming a benefit.

## Development iteration record

Use this document for durable Sigma design decisions and the reasoning behind changes. It is not a transcript of every coding session. Sigma's per-run event journal, run metadata, transcript, and session snapshots record work performed by Sigma; they do not observe Codex or other external development sessions. Repository memory is for short, reviewed project facts, not a development diary.

The benchmark-driven coding-agent changes are documented separately in [Sigma coding-agent iteration history](SIGMA_CODING_AGENT_ITERATION_HISTORY.md). That record distinguishes Sigma runtime/tool code from Skill content, Codex's benchmark operations, provider configuration, and Pi-side behavior, and links each claim to code or scored runs.

### 2026-11-07 — Align tool context and completion workflow with Pi

- Raised file-reading and visible tool-result limits, added offset-based reads and chunked retrieval for longer outputs, and expanded command-output capture. Sigma keeps its own per-item and per-session artifact storage quotas; matching user-visible limits does not imply identical internals.
- Made verification guidance conditional on the task and removed the retry loop that required tests or a diff review even when they were inapplicable. The runtime still reports verification state, including whether the diff was reviewed.
- Treat these as compatibility and usability changes, not evidence that Sigma now matches Pi's task success rate. Compare equivalent tasks and inspect actual trajectories before claiming parity.

## Retrieval should be layered and measurable

Use exact repository search and source reads alongside lexical retrieval. Embeddings can help with paraphrased concepts, but they are not a replacement for exact names, symbols, error text, or call-path inspection. A compact symbol/dependency map can provide repository orientation; lexical and semantic retrieval can then find task-specific passages. These are complementary signals, not one universal search mechanism.

For any derived index:

- Keep code retrieval and durable memory in separate collections and policies.
- Scope every memory query by exact repository identity; do not rely on a loose namespace prefix for isolation.
- Index stable content hashes, update incrementally, and validate retrieved paths/content against the live checkout before returning code context.
- Return source/path/date with memory facts. Drop stale or missing records rather than trusting index text as current code.
- Rank and deduplicate before applying a small context/token budget. A bounded second retrieval after the Agent forms a concrete hypothesis is a possible design, but measure it against the existing single-query flow.

BM25 and path-level Recall@K/MRR are useful retrieval diagnostics. They do not show that retrieved context improves code-task success, reduces stale-context errors, or justifies its token and latency costs. Evaluate no retrieval, lexical retrieval, and hybrid retrieval on the same held-out queries and context budget; include exact identifier queries and paraphrases, and measure both retrieval quality and downstream task decisions.

## Treat memory writes as a lifecycle

Start with human-reviewed memory. If automatic proposals are introduced, preserve the evidence/source, date, repository scope, and a way to review, amend, supersede, or delete the record. Avoid append-only accumulation of contradictory facts without a current/superseded state. Do not store secrets, raw transcripts, or task-specific patches as durable memory.

Evaluate memory separately from Skills and other agent changes. Compare memory off, reviewed Markdown with lexical search, and hybrid search under the same model, issue input, repository state, and context/token budget. Measure whether the right fact is retrieved, whether it is stale or contradicted, irrelevant context, latency, and whether the later coding task improves. Conversational-memory benchmark scores and a successful storage smoke do not establish coding-agent gains.

## Preserve a stable model-request prefix

When a provider caches matching prompt prefixes, keep stable system instructions and tool definitions—including their ordering—byte-for-byte stable across turns where possible. Put task-specific issue text, retrieved context, and changing workflow state after that stable prefix. Avoid appending changing progress summaries or reminders to the system prompt each turn; deliver them through a separate message or runtime/tool result when needed.

Treat context compaction as a possible cache-prefix change. Keep a compact, executable task state in the summary: objective, current hypothesis, files changed, latest focused-test result, and the next concrete action. Before tuning compaction thresholds, inspect provider cache-read/cache-write counters. Total prompt tokens alone cannot establish cache hits; if an adapter does not decode provider cache usage fields, report cache accounting as unavailable rather than zero.

## Put workflow safeguards at the runtime boundary

Prompt guidance is useful for explaining the workflow, but runtime policy should own checks that must hold regardless of whether the model remembers a reminder. Keep those checks outside a growing, per-turn system prompt: track the revision tested and diff-reviewed; reopen checks after a write; provide a clear recovery path after a rejected operation; and reserve enough time for final tests and diff review. A rejection should say what failed and the next safe action.

Long-task checkpoints should preserve actionable state instead of a transcript recap. At each checkpoint, compare the current tracked diff and focused-test evidence with the previous checkpoint. Continue when a relevant change improves evidence or one isolated next edit remains; stop when the same patch and failure state repeat. A turn cap is not evidence of convergence, and adding turns alone does not fix a loop.

These controls are workflow safeguards, not a guarantee that the model chooses the right algorithm or patch. Keep scope checks narrow enough to avoid blocking legitimate work, and evaluate both task outcomes and the cost of added runtime policy.

## Evidence and references

The patterns below are design inputs, not claims that Sigma matches another product or that a particular architecture improves issue-resolution rates:

- Pi documents Skill metadata discovery with on-demand loading, resumable sessions, and context compaction. Its agent-loop hooks are extension points; they do not by themselves provide a no-progress breaker. [Skills](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/skills.md) · [Sessions](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sessions.md) · [Compaction](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/compaction.md) · [Agent loop](https://github.com/earendil-works/pi/blob/main/packages/agent/src/agent-loop.ts)
- OpenHands documents project/user Markdown memory; LangGraph separates thread checkpoints from cross-thread Store records. These are useful lifecycle patterns, not coding-task quality results. [OpenHands memory](https://github.com/OpenHands/docs/blob/main/sdk/guides/persistent-memory.mdx) · [LangGraph Store](https://github.com/langchain-ai/docs/blob/main/src/oss/langgraph/stores.mdx)
- Continue documents incremental, multi-artifact code indexing; Aider documents a token-bounded symbol/dependency map; Sourcegraph describes keyword search and code-graph context. These approaches show that useful code context can combine lexical, structural, and semantic signals. [Continue indexing](https://github.com/continuedev/continue/blob/main/core/indexing/README.md) · [Aider repo map](https://github.com/Aider-AI/aider/blob/main/aider/website/docs/repomap.md) · [Sourcegraph context](https://sourcegraph.com/docs/cody/core-concepts/context)
- RepoCoder evaluates iterative retrieval for repository-level code completion. That setting differs from multi-step issue repair, so it motivates a bounded experiment rather than a performance claim. [Paper](https://arxiv.org/abs/2303.12570)

Product documentation describes intended mechanisms; it is not a controlled Sigma evaluation. Keep implementation facts in [Architecture](ARCHITECTURE.md), settings in [Configuration](CONFIGURATION.md), and experimental results in the evaluation records.
