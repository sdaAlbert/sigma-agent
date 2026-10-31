# Skills and repository memory evaluation

Implementation is available. One matched one-task smoke is recorded in [the result](results/2026-10-30-skills-go-port-ablation-smoke.md); it verifies the path, but is too small to estimate general usefulness. This protocol records how to test without conflating results with model or task variance.

## Skill ablation

Run each task twice from the same clean commit with a fresh session. Keep the issue text, model, runtime prompt, memory file, Docker test image, and all limits fixed. Change only `skills_enabled`:

- Baseline: `--config evals/configs/skills-disabled.json`
- Treatment: use the normal config with `skills_enabled: true` (the default)

Include tasks that match `go-issue-workflow` and near-miss tasks where it should not load. Record the skill tool call, official test outcome, patch outcome, turns, model usage if available, and input/tool-result bytes. A skill call by itself is not a positive outcome. Do not use one task result to claim a success-rate change.

## Memory ablation

Use the same clean repository state with the same `.sigma-agent/MEMORY.md` present in both arms. Keep Skills disabled for both arms. Change only `memory_enabled`, using `evals/configs/memory-disabled.json` and `evals/configs/memory-enabled.json`; code retrieval and its Qdrant collection remain identical. Use repeated-work tasks whose answer depends on an entry in the reviewed memory. Record whether the fact was applied correctly, whether current source contradicted the note, and how many memory bytes entered the prompt. Keep a copy of each memory version and its reviewed source/date.

For coding tasks, memory should capture durable facts that are costly to rediscover and are not reliably derivable from the current checkout: project-specific test commands and environment constraints, reviewed architecture decisions, compatibility rules, and recurring non-obvious pitfalls. Do not use it as a second code index, a transcript archive, or a replacement for current source and repository instructions. Keep facts scoped to the repository, dated, sourced, and easy to remove when stale. This boundary follows the patterns and limits summarized in [the first-party project research](../research/2026-10-coding-agent-long-term-memory-patterns.md): OpenHands demonstrates project/user Markdown tiers, LangGraph supplies namespaced cross-thread storage primitives, while Continue and Aider focus on current-code context retrieval.

Use a small matched task set with four cases: (1) a new session must follow a remembered test command; (2) a task depends on a reviewed architecture decision; (3) an unrelated task should not be distracted by memory; and (4) source or a newer review contradicts an old fact, so current evidence must win. First verify retrieval coverage and provenance independently, then compare task outcome against no-memory runs under the same model, prompt, and token budget. Report stale-fact conflicts, irrelevant retrieval, added tokens/latency, and task success. Do not infer coding gains from conversational-memory benchmark scores.

## Reporting

Publish all attempts, including no patch, failed tests, incorrect Skill selection, and stale-memory conflicts. Separate retrieval path metrics from issue-resolution outcomes. The initial implementation has not been evaluated against a no-Skill or no-memory arm yet.
