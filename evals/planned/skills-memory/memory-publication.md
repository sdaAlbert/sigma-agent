# Repository memory and retrieval: public record

This page describes the kinds of repository memory used in Sigma's coding-agent
experiments and the retrieval setup at a high level. It is not a memory dump or
a task-by-task catalog.

## What repository memory is for

Sigma's durable repository memory is a small, reviewed set of facts that can
save rediscovery across tasks in the same codebase. The experiments used broad
categories such as:

- repository architecture and component responsibilities;
- test layout, test-runner conventions, and environment requirements;
- compatibility constraints and recurring implementation pitfalls;
- repository-specific development and verification workflows.

These are categories, not a promise that every repository or run contained each
kind of fact. Memory is supplementary context: the current checkout, tests,
repository instructions, and the user's request remain authoritative.

## Retrieval configuration

- The canonical source is a reviewed `.sigma-agent/MEMORY.md` file. Sigma does
  not inject the full file into each prompt; it retrieves a small relevant
  subset when `search_memory` is useful.
- Local BM25 retrieval works without Qdrant. When configured, Sigma also
  retrieves semantic vector candidates from the `sigma_agent_memory` Qdrant
  collection and fuses lexical and vector ranks with reciprocal-rank fusion.
- Qdrant stores vectors and provenance for memory facts, not source-code
  chunks. Queries are scoped to the repository identity; stale vector entries
  are rejected when their source-memory hash no longer matches.
- If Qdrant or the embedding endpoint is unavailable, retrieval falls back to
  local BM25. Vector indexing is an explicit operation; it is not required for
  local memory search.
- Provider, embedding model, endpoint, Qdrant URL, and credentials are supplied
  by local configuration or environment variables. Credentials and private
  endpoint values are intentionally not part of this public record.

The runtime settings and setup instructions are documented in
[`docs/CONFIGURATION.md`](../../../docs/CONFIGURATION.md#repository-long-term-memory).

## Conditions in the experiments

Memory availability, indexing, retrieval, and use are separate events. The
Mini SWE records include runs with memory enabled and disabled; some enabled
runs indexed repository facts, while some did not call `search_memory`.
`memory_auto_write` was also not uniform across all historical runs. Therefore,
these records are not a matched memory-on/off study and do not establish that
memory improved issue-resolution scores. See the
[memory status and evidence limits](repository-memory-status.md).

For controlled comparisons, memory inputs must be fixed before the paired
runs, automatic writeback must be disabled, and only the memory-enabled setting
should change. Any run whose memory contains knowledge specific to its target
issue must be classified as answer-adjacent and excluded from an issue-only
comparison.

## What this public record does not publish

This page intentionally does not include raw Qdrant payloads, complete
repository memory files, per-issue memory entries, task-specific fix
instructions, patches, hidden tests, transcripts, credentials, or private
service endpoints. Such material can reveal benchmark solutions or private
configuration. The public record is limited to the memory's broad subject
areas, system behavior, and experiment controls; it does not provide answers
to benchmark tasks.
