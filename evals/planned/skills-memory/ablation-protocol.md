# Skills and repository memory evaluation

Implementation is available. One matched one-task smoke is recorded in [the result](../../historical/README.md#archive-access) (local detailed report not bundled); it verifies the path, but is too small to estimate general usefulness. This protocol records how to test without conflating results with model or task variance.

## Skill ablation

Run each task twice from the same clean commit with a fresh session. Keep the issue text, model, runtime prompt, memory file, Docker test image, and all limits fixed. Change only `skills_enabled`:

- Baseline: `--config evals/planned/skills-memory/configs/skills-disabled.json`
- Treatment: `--config evals/planned/skills-memory/configs/skills-enabled.json`

Include tasks that match `go-issue-workflow` and near-miss tasks where it should not load. Record the skill tool call, official test outcome, patch outcome, turns, model usage if available, and input/tool-result bytes. A skill call by itself is not a positive outcome. Do not use one task result to claim a success-rate change.

The loader discovers Skills only inside the task worktree. Stage the exact reviewed `SKILL.md` into `.agents/skills/go-issue-workflow/SKILL.md` in each clean worktree before the treatment run, and verify startup exposes its name/description. Do not let the Skill file or any Skill-specific prompt text leak into the disabled baseline.

## Memory ablation

Use the same clean repository state with the same `.sigma-agent/MEMORY.md` present in both arms. The paired configs keep Skills disabled in both arms and change only `memory_enabled`: `evals/planned/skills-memory/configs/memory-disabled.json` and `evals/planned/skills-memory/configs/memory-enabled.json`. Source browsing stays the same, while the treatment can call `search_memory`. Use repeated-work tasks whose answer depends on a fact in the reviewed memory. Record whether the fact was applied correctly, whether current source contradicted the note, and how many memory bytes entered the prompt. Keep a copy of each memory version and its reviewed source/date. The CLI loads one config file, so copy any task-specific runtime fields into both files instead of trying to stack them.

For coding tasks, memory should capture durable facts that are costly to rediscover and are not reliably derivable from the current checkout: project-specific test commands and environment constraints, reviewed architecture decisions, compatibility rules, and recurring non-obvious pitfalls. Do not use it as a second code index, a transcript archive, or a replacement for current source and repository instructions. Keep facts scoped to the repository, dated, sourced, and easy to remove when stale. This boundary follows the patterns and limits summarized in [the first-party project research](../../../research/2026-10-coding-agent-long-term-memory-patterns.md): OpenHands demonstrates project/user Markdown tiers, LangGraph supplies namespaced cross-thread storage primitives, while Continue and Aider focus on current-code context retrieval.

Use a small matched task set with four cases: (1) a new session must follow a remembered test command; (2) a task depends on a reviewed architecture decision; (3) an unrelated task should not be distracted by memory; and (4) source or a newer review contradicts an old fact, so current evidence must win. First verify retrieval coverage and provenance independently with the [`memory retrieval protocol`](memory-retrieval-evaluation.md), then compare task outcome against no-memory runs under the same model, prompt, and token budget. Report stale-fact conflicts, irrelevant retrieval, added tokens/latency, and task success. Do not infer coding gains from conversational-memory benchmark scores.

## Reporting

Publish all attempts, including no patch, failed tests, incorrect Skill selection, and stale-memory conflicts. Separate retrieval path metrics from issue-resolution outcomes. The initial implementation has not been evaluated against a no-Skill or no-memory arm yet.
