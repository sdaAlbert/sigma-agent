# VibeMemBench with Sigma Agent

> **Status: paused.** The local paired pilot was inconclusive and was explicitly
> abandoned; no memory benefit was established. Treat the commands below as a
> reproducibility record only. Do not resume a run without first deciding to
> restart the experiment and selecting/archiving the task environment.

This path runs **Sigma Agent itself** on VibeMemBench coding tasks. The official
VibeMemBench `run_agent.py` uses a pinned mini-SWE-agent solver, so it is not the
solver used for this experiment. We reuse the benchmark task data and its official
patch evaluator, while Sigma receives the issue and works in a prepared target
repository.

`run_sigma.py` launches one Sigma run and writes its patch as `preds.json` in the
directory layout consumed by the official evaluator. It also writes the small
trajectory marker required by the evaluator's run discovery, plus Sigma's
terminal result and log. The marker does not claim to be a mini-SWE trace. A
patch is preserved for scoring even when Sigma exits at its turn limit; the exit
status remains visible in `sigma_result.json`. GitHub MCP is disabled.

`run_sigma_matrix.py` supports three conditions for diagnostic studies:

- `no_memory`: issue only; memory is disabled.
- `memory`: issue only; Sigma can choose to call `search_memory`.
- `injected_memory`: the same top-1 result from Sigma's BM25 search is supplied
  in VibeMemBench's `<relevant_experiences>` block; memory search remains off.

The injected condition separates memory-content usefulness from the agent's
decision to invoke retrieval. `memory_preflight.go` records the exact BM25
result used for that condition. Do not treat retrieval calls alone as task
success, and do not weaken the official test-set comparison. The current
quota-aware pilot uses only `no_memory` and `memory`; run `injected_memory` only
as a follow-up when a completed pair shows that native retrieval did not occur.

## Superseded low-budget plan (historical record)

The original five-target, three-condition, two-repeat matrix was superseded.
Two agent runs consumed about 5.08 million reported input tokens and exhausted
the MiniMax Token Plan before the injected condition could run. This plan is
preserved to document the abandoned design; it is not a request or schedule to
resume the experiment. A restart requires a new explicit decision and a fresh
retention plan for its task environments.

1. Before selecting a target, verify that its pre-task memory contains a
   distinct, actionable experience and that the issue statement does not
   already reveal the same fix. `argoproj__argo-4735` is excluded because its
   top memory repeats nearly the same issue. `benbjohnson__litestream-522` is
   also excluded from a memory-benefit claim because its issue statement
   already names the intended fix.
2. Run conditions sequentially with a 48-turn / 96-tool-call ceiling and the
   same model, context limits, tools, image, and task commit.
3. Record official `resolved` / `passed_match`, memory retrieval events, turns,
   and token usage. If either run hits a limit or provider error, mark the pair
   incomplete and do not claim an effect.
4. Repeat the matched pair once only if the first pair completes within the
   available token budget. Expand to more targets only after that gate.

The proposed first pair was a smoke test, not statistical proof. If restarted,
keep Docker task containers sequential. Retain each task's image, container, worktree, and cache
until the Agent run, official evaluation, and result archive are complete. If a
run is interrupted, mark it incomplete and retain its environment until it is
resumed, archived, or explicitly abandoned. Remove the image only after that
point when no retained run depends on it.

## Paired run

Prepare two clean worktrees at the same target base commit. The memory worktree
may contain the target-specific, pre-task `.sigma-agent/MEMORY.md`; the control
worktree must not. Use the same model, issue text, task commit, tools, and budgets
for both arms. Keep automatic writeback disabled. If the memory config uses
Qdrant retrieval, index the memory worktree before its run with Sigma's
`--index-memory` command and use the same Qdrant URL when running the arm. For
example:

```powershell
$task = 'C:\bench\tasks\target-issue.txt'
$runs = 'C:\bench\sigma-runs'

python evals/vibemembench/run_sigma.py `
  --instance-id imdario__mergo-231 --arm no_memory --seed 0 `
  --repo C:\bench\mergo-control --task-file $task `
  --config evals/configs/memory-disabled.json --output-dir $runs

python evals/vibemembench/run_sigma.py `
  --instance-id imdario__mergo-231 --arm memory --seed 0 `
  --repo C:\bench\mergo-memory --task-file $task `
  --config evals/configs/memory-enabled.json --output-dir $runs
```

The runner defaults to 128 turns and 256 tool calls; pass `--max-turns` and
`--max-tool-calls` to override them. Keep the values identical between arms.
Run the benchmark's official evaluator against the generated directory:

```powershell
python C:\path\to\VibeMemBench\scripts\run_eval.py `
  --runs-dir $runs --dataset C:\path\to\test.parquet
```

Use only histories dated before each target task when preparing memory. Do not
include the target patch, test patch, or target solution in memory. Keep the
benchmark's exact `passed_match` result as the score; the Sigma runner does not
weaken or replace the official evaluator.

## Current evidence

The earlier local Go-subset pilot already used Sigma for both memory and control.
Those runs used a 32-turn ceiling and repeatedly stopped at that limit. The
runner also looked up `run.json` by `session_id`, although Sigma writes the run
under the separate `run_id`; terminal JSONL output contains the actual run and
patch paths. `run_sigma.py` reads that terminal record instead. The earlier
pilot is diagnostic and should not be counted as a successful VibeMemBench
result. A later pilot also failed to provide a usable paired score; the
experiment is paused pending a new explicit decision.

The 2026-11-01 `google__yamlfmt-42` paired pilot used 64 turns per arm. Both
patches failed compilation and both runs hit the turn limit. The trace showed
the runtime rejecting final-turn edits after the verification checks, and the
Windows diff collector mixed Git warnings into patch bytes. The runtime now
allows a targeted correction after verification and requires checks again after
that edit; patch collection now keeps Git stderr separate. The runner default is
128 turns/256 tool calls. These changes have not yet been validated by a rerun.

VibeMemBench sources: [benchmark repository](https://github.com/AlibabaResearch/DAMO-ConvAI/tree/main/VibeMemBench),
[paper](https://arxiv.org/abs/2609.23570),
[dataset](https://huggingface.co/datasets/Tongyi-ConvAI/VibeMemBench).
