# Issue-resolution benchmark protocol

Use this protocol when measuring whether Sigma Agent can resolve repository issues. Keep reference patches outside the Agent's context until the candidate patch and run have been saved.

For Docker image preparation, environment failures, interrupted runs, or cleanup,
follow the operational rules below before using `docker pull`, `docker build`,
`docker rm`, or `docker image rm`.

## Disable GitHub MCP for the run

The CLI enables GitHub MCP by default when credentials are present. Every benchmark invocation must explicitly disable it:

```powershell
$repoPath = "C:\path\to\clean-worktree"
$issueTask = "Paste the official issue title and body here"
go run ./cmd/sigma-agent --repo $repoPath --task $issueTask --github-mcp=false
```

`run.json` records `github_mcp_enabled`. Accept the run for the benchmark only when it is `false`, and inspect `transcript.json` for unexpected reference access. If the MCP was enabled or the run metadata is missing, label the attempt as unverified and exclude it from the issue-only score.

## Separate assistance levels

- **Issue-only:** provide the official issue title and body. Do not include a target PR link, diff, commit hash, or acceptance details copied from the reference patch.
- **Human-guided:** add a human-authored hint or acceptance criteria, record the exact extra information, and still pass `--github-mcp=false` so the Agent cannot fetch the gold PR itself.
- **Reference reproduction:** deliberately provide the target PR diff. Label this separately; it checks whether Sigma Agent can apply and validate a known solution, not whether it can discover one.

Use a clean worktree and the same model, runtime limits, repository test image, and evaluator for comparisons. Save the prompt, configuration, transcript, patch, evaluator report, and run metadata for every attempt, including failures.

## Shared instructions and memory across task checkouts

When benchmark tasks use independent clones of the same upstream repository,
use `project_instructions_file` and `memory_file` in the run config instead of
adding profile files to each source checkout. The external instruction file is
loaded alongside any task-root `AGENTS.md`; the external memory file is copied
only into Sigma's isolated session worktree. Set one stable
`memory_repository_id` for that upstream repository so BM25 and Qdrant use the
same scope across clones. Run `--index-memory` with this config before the
batch, and record the profile paths, hashes, memory scope, Skills state, and
effective config for each run. `run.json` records external instruction and
starting-memory hashes.

Keep shared profile instructions generic: do not include issue-specific
acceptance details, target tests, PR links/diffs, or proposed fixes. If the
profile or its memory changes during a benchmark series, treat subsequent runs
as a new condition; do not combine their scores with runs that used the old
profile. If `memory_auto_write` is enabled, successful runs append durable
facts to the external source and refresh its index; preserve each pre-run hash
so the memory state used by each task can be reconstructed.

## Preflight and serial task execution

Before starting a model session, run `evals/preflight-swebench-task.ps1` with
the clean task checkout, the task's command-image config, and the official
evaluator image. It checks Docker, verifies both images are already local,
mounts the checkout read-only at `/workspace` in the command image, and checks
the command-image Python version plus the evaluator's `/testbed` Git checkout.
The script uses `--pull=never`; a missing image fails before consuming model
tokens. Preflight containers are disposable checks; retain the task images,
worktree, session data, and benchmark inputs/results until the Agent run,
official evaluator, and archive are complete.

Example for the retained `django__django-11880` task:

```powershell
.\evals\preflight-swebench-task.ps1 `
  -RepoPath C:\dev\Agent\swb5\django__django-11880 `
  -ConfigPath evals\configs\full-sigma-5-django-11880-shared-profile.json `
  -EvaluatorImage swebench/sweb.eval.x86_64.django_1776_django-11880:latest `
  -ExpectedPython 3.6.13
```

Run only one Agent/evaluator task at a time on this workstation. Do not start
another task's containers or cleanup while one task is active. After its patch,
evaluator output, and run manifest are archived, clean only resources unique
to that task, then preflight the next one. Reuse a local image only when its
repository/instance tag and digest match the task manifest.

Benchmark configs set `max_repeated_failed_commands: 3`. Sigma pauses after
three identical failed test/command calls without a workspace change; the
session, patch, logs, worktree, and images remain available for diagnosis or
resume. A successful source write resets the guard. The full batch starts
proactive compaction at 24,000 estimated tokens, permits up to 256 estimated
tokens of soft-threshold overage, and prioritizes the original request, recent
source writes, test outcomes, diff reviews, and failures in its extractive
summary. If the newest complete turn still cannot fit the soft budget, Sigma
shrinks tool output and retains that turn up to the 64,000 hard context ceiling.
A small soft-threshold overage is not a run failure; stop only when the newest
complete turn cannot fit the hard ceiling.

## Keep evaluator inputs fresh

Multi-SWE-Bench reuses Docker images by repository/PR tag, and the runner skips an instance when its work directory already contains a report. When the candidate patch or dataset changes, force-rebuild the image (or use a unique image tag), and use a fresh workdir, output directory, and log directory for that evaluation. Before interpreting a result, verify that the image's `/home/test.patch` matches the current benchmark test patch and that the candidate patch mounted at `/home/fix.patch` matches the saved Agent patch. If patch application fails or the fix stage captures no tests, label the run as an evaluator failure and do not count it as a code-resolution result.

When Multi-SWE-Bench supplies a separate `test_patch`, save the Agent's patch unchanged as the raw candidate, then prepare a second scoring patch that excludes Agent-authored Go `*_test.go` changes. Run the same evaluator with that source-only patch so overlapping test edits do not prevent the benchmark tests from compiling. Keep both application results: a raw-patch conflict remains an evaluator/patch-integration failure, while target tests passing on the source-only patch is evidence about the production implementation. Do not replace the raw patch or report the two outcomes as the same score. Prepare both files and a checksum manifest with:

```powershell
.\evals\prepare-go-benchmark-patches.ps1 `
  -CandidatePatch $candidatePatch `
  -OutputDirectory $artifactDirectory
```

## Docker images and environment failures

Treat benchmark preparation and Agent egress as separate phases. Before starting
the model, use the host Docker daemon's network to pull the exact evaluator and
command images and, if needed, build a dependency-ready command image. Registry
access and host-side image preparation do not use `allow_container_network`.

During issue solving, keep both `allow_container_network: false` and
`web_search_enabled: false`. This prevents the Agent's command container from
downloading dependencies or searching the web. It does not prevent the host
from pulling images before the run. Do not turn Agent networking on to repair a
Docker Hub, image-build, or dependency-preparation failure. Instead, prepare the
image outside the Agent run, then rerun the offline preflight. If dependencies
are still missing, mark the task `infra-invalid`/`invalid-incomplete` and retain
the environment until the run is resolved or archived.

Before starting Sigma, resolve the exact task-specific official evaluator image
and the command image. Run `evals/prepare-swebench-task.ps1` to pull missing
images through the host Docker daemon and then run the offline preflight. Build
the command image from that same task image when necessary. The command image should retain the
task's interpreter and preinstalled dependencies while removing the baked
`/testbed` checkout; mount only the sanitized base-commit workspace at
`/workspace`. Do not substitute another repository, task tag, or Python version
when the exact evaluator image is unavailable.

Use the configured task command image first. In particular, do not let
`prepare_environment` default to building a generic image such as
`python:3.11-slim` with `apt`/`pip` installs before checking the configured image.
The 2026-11-03 Django #11880 run showed this failure mode: Docker Hub token
authorization was unreachable, although the pre-pulled task image already had
Python 3.6.13 and the Django checkout worked in it. An earlier issue-log entry
said Python 3.11; direct inspection of the configured image corrected that
record. Record the failed build and
continue only if the exact task image can safely run the needed commands.

When a pull/build fails:

1. Stop repeated attempts against the same unreachable registry.
2. Record the command, image reference, stage, exact error, and recovery in the
   evaluation issue log, even if the run later recovers.
3. Inspect whether the exact task image is already local and whether it has the
   needed runtime/dependencies. Do not infer availability from a similarly
   named image.
4. If the exact evaluator environment is unavailable, mark the task
   `infra-invalid` or `invalid/incomplete`; do not score it or silently use a
   different image. Keep the Agent's no-egress setting unchanged while
   diagnosing host-side image access.

Before running Sigma, verify the command image's interpreter/version, expected
test dependencies, `/workspace` working directory, and absence of `/testbed`.
Also verify that the configured `command_execution_image` matches that image,
and that both Agent egress settings are explicitly false. Preflight uses
`--pull=never` and `--network none` after preparation, so a missing image or an
Agent-network-enabled eval config fails before consuming a model call.
Never put benchmark `patch`, `test_patch`, or evaluator fixtures in the Agent
workspace.

The command runner starts a fresh `--rm` container per invocation and bind-mounts
the task checkout at `/workspace`. Changes to the container root filesystem and
`/tmp` disappear between calls. Sigma also mounts
`/workspace/.sigma-agent/container-home` as persistent `HOME`/cache storage, so
files there, Python cache entries, and configured Go module/build caches can
survive. Other files written under `/workspace` also persist and can enter the
candidate patch. Read the repository's documented test runner
instead of assuming `pytest`; for Django, use its `tests/runtests.py` unless the
task image explicitly provides another supported runner. If dependencies are
missing, inspect the image and its provided wheelhouse once; do not repeatedly
scan caches or try network installs in a no-egress container. Keep setup and a
test in the same command when setup is ephemeral. Put scratch scripts in `/tmp`
and create/run/remove them in one invocation, or remove any workspace scratch
files before collecting the patch. Check `git status` and `git_diff` for harness
files before scoring.

Repository-native tests must be called with `purpose=test` so Sigma can track
whether tests passed after the latest edit. Keep the test command direct and
preserve its exit status; avoid shell pipes or semicolon-separated commands
that hide a test failure. Record the exact command, image, exit status, and
failure stage in the trace or issue log.

## Run retention and cleanup

A task is complete only after the Agent run has ended, the official evaluator
has completed (or a documented gold-control failure establishes an invalid
environment), and the result archive has been verified. Until then, retain the
task's images, worktree, session, containers that still exist, and build caches.

| State | Required action |
|---|---|
| Agent or evaluator still running | Do not remove task resources. |
| Interrupted, quota-limited, or otherwise incomplete | Mark `invalid/incomplete`; retain the environment so the run can be resumed or deliberately re-evaluated. Do not clean it just to save space. |
| Agent finished, evaluation/archive pending | Keep all task resources. |
| Evaluation and archive verified | Remove only task-specific stopped containers/worktrees and images that are no longer needed by the planned next tasks. Preserve reusable base layers and shared services. |

Before cleanup, verify the archive contains the exact issue-only input, effective
config, model/runtime metadata, Skills/Memory state, raw patch, scoring patch if
different, full run trace, evaluator logs/report, image references, and issue
log entries. Confirm the run is no longer active and no later task in the
current batch reuses the image. Remove resources by exact task name or image
reference; avoid broad `docker system prune` or deleting shared layers/cache.
One-shot containers launched with `--rm` may already be absent after a command
finishes; record that as normal container lifecycle, not as manual cleanup.
Per-session `.sigma-agent/container-home` caches are task-workspace data: retain
them while a run may resume, and remove them only with that retired task
workspace after archive verification. Do not delete a shared cache path by a
wildcard.

## Issue log

Append each benchmark execution problem to the batch's `evaluation-issues.md`
when it occurs, including recoverable problems and incomplete attempts. Record
the task, stage, command/image, observed error, impact on validity, workaround,
and whether the workaround succeeded. After a recurring issue is understood,
keep the general rule here and leave the detailed per-run evidence in the issue
log.
