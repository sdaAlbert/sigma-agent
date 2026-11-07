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

Use a clean worktree and the same model, runtime executable (record its SHA-256), runtime limits, repository test image, and evaluator for comparisons. For a Skill-only comparison, change only the Skill snapshot; if the executable, turn/tool caps, or other run settings change, record it as a separate condition and do not attribute the score to the Skill change alone. Save the prompt, configuration, transcript, patch, evaluator report, and run metadata for every attempt, including failures.

## Shared instructions and memory across task checkouts

When benchmark tasks use independent clones of the same upstream repository,
use `project_instructions_file` and `memory_file` in the run config instead of
adding profile files to each source checkout. The external instruction file is
loaded alongside any task-root `AGENTS.md`; the external memory file is copied
into Sigma's active repository workspace. Set one stable
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

### Python and Sphinx Skill staging

The Python and Sphinx workflows live in this Sigma project at
`.agents/skills/python-issue-workflow/SKILL.md` and
`.agents/skills/python-sphinx-issue-workflow/SKILL.md`, not in the user's
shared `~/.agents/skills` directory. Sigma discovers project Skills from the
active task checkout, so stage the relevant file into that checkout under the
same `.agents/skills/<name>/SKILL.md` path. Add the staged Skill directory to
that checkout's local `.git/info/exclude` so it is available to Sigma but
cannot enter the candidate patch. Verify the Skill is listed and loaded in the
transcript before code exploration; record its path and SHA-256 in the batch
manifest. Do not modify archived checkouts to add a Skill retroactively.

The runtime currently reads at most 12 KiB (12,288 bytes) from one
`SKILL.md`. Keep staged Skill files below this limit with margin. After staging,
verify the exact file size and confirm `read_skill` returns the Skill body;
`unknown Agent Skill` can mean discovery skipped an oversized file, even when
the path exists. Do not spend model turns retrying a Skill that the runtime did
not discover; fix/split the Skill, create a fresh checkout, and record the
attempt as invalid/incomplete.

Before `--index-memory`, verify the configured Qdrant endpoint is healthy. If
the local endpoint refuses connections, inspect `docker ps -a` and restart the
existing task-independent Qdrant container with its existing persistent volume;
check `/readyz`, then retry indexing and save the successful entry count. Keep
the shared container available while subsequent tasks use Memory.

### Skill auto-match false positives

Sphinx issue #10435 exposed an earlier false positive: short Skill names were
matched as substrings, so `pdf` matched “pdf output” and `latexpdf` even though
the task only changed generated LaTeX. The runtime matcher now requires an
explicit Skill reference for short names and token boundaries for longer names.
Keep the focused matcher regression test. In a scored run, record the required
Skill name and trigger phrase, then check that the Skill's declared purpose
fits the task. Do not disable Skills or weaken the gate to bypass a mismatch.
If an irrelevant Skill requirement blocks a run, archive it as
`invalid/incomplete`, preserve the environment, and fix routing as a separate
runtime change. Keep failed attempts and continuations separate and report
combined resource use.

When validating generated LaTeX, treat `%` as syntax: it comments out the rest
of its line, so newline placement around inline content and closing delimiters
can change the rendered result. Inspect the generated `.tex` context around
both boundaries; do not relax an exact-output assertion until an alternative
is shown to preserve the same TeX behavior.

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
$repoPath = "C:\work\swb5\django__django-11880"
.\evals\preflight-swebench-task.ps1 `
  -RepoPath $repoPath `
  -ConfigPath evals\configs\full-sigma-5-django-11880-shared-profile.json `
  -EvaluatorImage swebench/sweb.eval.x86_64.django_1776_django-11880:latest `
  -ExpectedPython 3.6.13
```

Run only one Agent/evaluator task at a time on this workstation. Before each
launch, inspect the active benchmark processes and their current run logs; do
not assume a task is idle because its batch is from an earlier date. Do not
start another task's containers or cleanup while one task is active. After its patch,
evaluator output, and run manifest are archived, clean only resources unique
to that task, then preflight the next one. Reuse a local image only when its
repository/instance tag and digest match the task manifest.

Benchmark configs set `max_repeated_failed_commands: 3`. Sigma pauses after
three identical failed test/command calls without a workspace change; the
session, patch, logs, worktree, and images remain available for diagnosis or
resume. If `run_command` repeatedly reports Docker network creation failures such as
`all predefined address pools have been fully subnetted`, stop retrying the
same command. Check `docker network ls` and inspect candidate networks for
attached containers, then confirm no other Agent/evaluator task is active. Do
not prune Docker networks or remove names whose owning task is unknown; this
host may have shared or active task networks. Resume only after capacity is
recovered through a network known to belong to an archived task, or mark the
attempt invalid/incomplete and preserve its environment. Record the error and
recovery in the shared and batch issue logs.

A successful source write resets the guard. The full batch starts
proactive compaction at 24,000 estimated tokens, permits up to 256 estimated
tokens of soft-threshold overage, and prioritizes the original request, recent
source writes, test outcomes, diff reviews, and failures in its extractive
summary. If the newest complete turn still cannot fit the soft budget, Sigma
shrinks tool output and retains that turn up to the 64,000 hard context ceiling.
A small soft-threshold overage is not a run failure; stop only when the newest
complete turn cannot fit the hard ceiling.

When resuming a saved session whose last message is a tool result, prefer
`--resume` without `--task`. In the current OpenAI-compatible serializer, an
additional user prompt can be coalesced with that tool-result message and sent
before the tool reply, which providers reject as an invalid tool-call sequence.
If the continuation needs new guidance, preserve the failed attempt and use a
separately validated recovery path; do not repeatedly retry the same malformed
transcript. Record the condition and workaround in the batch issue log.

## Keep evaluator inputs fresh

Multi-SWE-Bench reuses Docker images by repository/PR tag, and the runner skips an instance when its work directory already contains a report. When the candidate patch or dataset changes, force-rebuild the image (or use a unique image tag), and use a fresh workdir, output directory, and log directory for that evaluation. Before interpreting a result, verify that the image's `/home/test.patch` matches the current benchmark test patch and that the candidate patch mounted at `/home/fix.patch` matches the saved Agent patch. If patch application fails or the fix stage captures no tests, label the run as an evaluator failure and do not count it as a code-resolution result.

Before starting an evaluator container, inspect the saved candidate's changed
paths and run a host-side patch-application preflight against a clean checkout
of the exact task base. If the benchmark applies a separate test patch, include
that patch in the same preflight sequence and test the candidate both before and
after it, matching the evaluator's application order. Resolve path collisions
and create the final scoring patch before launching the evaluator. Do not start
a second evaluator container just to discover a deterministic patch conflict;
start a fresh evaluator only when the validated candidate or evaluation
condition changes. Gold control and candidate scoring remain separate runs.

The SWE-bench wrapper `run_swebench_preserve_container.py` writes its per-run `logs/run_evaluation/<run-id>` directory relative to the current working directory, separately from `--report-dir`. Run it with a working directory under `var`, then move the completed task archive under `archive/benchmarks/swebench-verified-mini/` after evaluation. Keep active evaluator logs under `var` until the task archive is complete. Before closing the archive, verify that no run-specific evaluator logs remain under the project root.

For a local single-task dataset, pass a `.json` file containing a one-element
array (`[{...}]`) or a `.jsonl` file with one JSON object per line to
`--dataset`. Do not pass a bare JSON object: the installed SWE-bench loader
iterates it as keys and fails before creating an evaluator container. Parse the
file back before scoring and verify that it contains exactly the intended
`instance_id`. The `--dataset` value may also be an official dataset name; keep
that separate from the prediction JSONL passed through `--predictions`.

When Multi-SWE-Bench supplies a separate `test_patch`, save the Agent's patch unchanged as the raw candidate, then prepare a second scoring patch that excludes Agent-authored Go `*_test.go` changes. Run the same evaluator with that source-only patch so overlapping test edits do not prevent the benchmark tests from compiling. Keep both application results: a raw-patch conflict remains an evaluator/patch-integration failure, while target tests passing on the source-only patch is evidence about the production implementation. Do not replace the raw patch or report the two outcomes as the same score. Prepare both files and a checksum manifest with:

```powershell
.\evals\prepare-go-benchmark-patches.ps1 `
  -CandidatePatch $candidatePatch `
  -OutputDirectory $artifactDirectory
```

Sigma's `WorkspaceDiff` excludes the session-managed `.sigma-agent/MEMORY.md`.
Sigma does not provide its own Git commit tool; benchmark runners should save
the candidate diff from the task workspace and keep the memory sidecar out of
the submitted patch. For older runs that already contain it, preserve the raw
patch and evaluator error, then score a separate code-only patch with only this
sidecar removed. Record both outcomes instead of silently replacing the raw
candidate.

## Docker images and environment failures

Treat benchmark preparation and Agent egress as separate phases. Before starting
the model, use the host Docker daemon's network to pull the exact evaluator and
command images and, if needed, build a dependency-ready command image. Registry
access and host-side image preparation do not use `allow_container_network`.

During issue solving, keep both `allow_container_network: false` and
`web_search_enabled: false`, and set
`disable_configured_mcp_servers: true` plus
`disable_workflow_extensions: true`. The latter two prevent user-level MCP
servers and JavaScript extensions from adding network tools despite the web
search flag. This prevents the Agent's command container from downloading
dependencies or searching the web. It does not prevent the host from pulling
images before the run. Do not turn Agent networking on to repair a
Docker Hub, image-build, or dependency-preparation failure. Instead, prepare the
image outside the Agent run, then rerun the offline preflight. If dependencies
are still missing, mark the task `infra-invalid`/`invalid-incomplete` and retain
the environment until the run is resolved or archived.

With container networking disabled, Sigma command containers should use Docker network none and allocate no custom bridge subnet; verify the pinned runtime includes this behavior before a large offline batch.

Before starting Sigma, resolve the exact task-specific official evaluator image
and the command image. Run `evals/prepare-swebench-task.ps1` to pull missing
images through the host Docker daemon and then run the offline preflight. Build
the command image from that same task image when necessary. The command image should retain the
task's interpreter and preinstalled dependencies while removing the baked
`/testbed` checkout; mount only the sanitized base-commit workspace at
`/workspace`. Do not substitute another repository, task tag, or Python version
when the exact evaluator image is unavailable.

### Prepare a Windows checkout from an official task image

When a clean source tree must come from the official evaluator image, keep the
source container alive explicitly; the image's default entrypoint may exit as
soon as it starts:

```powershell
docker create --name <task>-source --entrypoint /bin/bash <exact-evaluator-image> -lc "sleep infinity"
docker start <task>-source
docker exec <task>-source /bin/bash -lc "cd /testbed && git rev-parse HEAD"
```

Inside `/testbed`, check that the dataset's exact `base_commit` exists and
checkout that commit detached. The image's current HEAD or
`environment_setup_commit` can differ from `base_commit`; never assume they are
interchangeable. Verify `git rev-parse HEAD` and a clean `git status` inside
Linux before copying.

For Windows-hosted Django checkouts, set local Git options before exposing the
tree to Sigma:

```sh
git config core.symlinks false
git config core.filemode false
git config core.autocrlf false
```

`docker cp` may fail with `A required privilege is not held by the client` when
it tries to create a Linux symlink on Windows. Find tracked symlinks by mode
`120000` and materialize each as its exact Git target-text blob in Linux before
copying. For paths without embedded newlines:

```sh
git ls-files -s | awk '$1 == "120000" { sub(/^[^\t]*\t/, ""); print }' |
while IFS= read -r path; do
  git show "HEAD:$path" > "$path.materialized"
  mv -f "$path.materialized" "$path"
done
```

Keep the host worktree at a short path such as
`go-runtime\var\worktrees\<date>\<short-task-id>`; deeply nested Windows paths
can make Git report `Filename too long`. After `docker cp`, set the three local
Git options again on the host and require all of the following before model
execution: exact base commit, empty `git status --porcelain`, empty
`git diff --stat`, and symlink target-text files matching the tracked blobs.
Record every affected path and recovery in the batch issue log.

When exporting official patches from a dataset on Windows, preserve their LF
bytes. Text APIs can silently rewrite the patch to CRLF, causing host
`git apply --check` to fail even when the official evaluator accepts the same
patch text. Write the original string as UTF-8 bytes without newline
translation, verify no `\r` bytes were introduced, and record the exact patch
hash before preflight.

Build the command image from the exact evaluator image, then compare interpreter
versions through the actual preflight shells. A plain `docker run ... python`
can select a different Conda interpreter from the evaluator's `/bin/bash -lc`
startup. If the evaluator prepends its task environment (for example
`/opt/miniconda3/envs/testbed/bin`), set the verified environment path first in
the command image's `PATH`. Rerun `prepare-swebench-task.ps1` with the exact
expected version; do not start Sigma until command and evaluator versions match
and the offline mount preflight passes.

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
2. Record the command, image reference, stage, exact error, impact on validity,
   and recovery in the batch's `runs/<batch>/evaluation-issues.md`, even if the
   run later recovers. Add or update a corresponding entry in the shared index
   in the central archive at `../archive/benchmarks/swebench-verified-mini/evaluation-issues.md` relative to the Sigma repository root. The archive is stored separately from this Git repository.
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

### Safe source edits after Django #12273 and #12276

These runs exposed a destructive edit path: the Agent can accidentally replace
an entire large source file, and shell redirection opens/truncates its
destination before the command on the left succeeds. In #12276, a failed
container-side git show was redirected into django/forms/fields.py; the file
was truncated and subsequent manual reconstruction caused 19 unrelated
pass-to-pass regressions. #12273 also ended with a source file truncated and a
syntax error before evaluator tests could run.

The runtime now limits full-file apply_patch replacement to files and content
at most 32 KiB, and blocks shell redirection to existing workspace files.
Use edit_file for localized changes to larger files. Redirect output only to a
new literal scratch path; check command status before using its output.
Review the final diff for unexpectedly deleted or dramatically shortened files
before evaluation. The shell-redirection check is a targeted guard for literal
paths; it is not a complete shell sandbox.

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

### Passing issue-only task text

The CLI `--task` flag takes the task text itself; it does not open a file path. When an issue-only prompt is stored outside the task checkout, read it on the host (for example, PowerShell `Get-Content -Raw`) and pass the resulting string to `--task`. Passing a host path literally causes the Agent to receive only that path, and command containers generally cannot read it unless separately mounted. Record this as an invalid launch and use a fresh session; do not score or resume it.

### Empty model finish reason

If the provider stream returns no finish reason, the runtime preserves the session and retries the original task with a short recovery prompt at most twice. A third consecutive empty finish reason ends the run with a specific error. Record such attempts as incomplete, retain the session and benchmark environment, and do not start an evaluator until the Agent produces a candidate patch. The raw provider SSE payload is not currently retained, so distinguish the observed empty runtime reason from an unverified claim about the provider's raw response.

### Windows shell and image setup checks

- When a command is sent through host PowerShell to `docker exec`, host-side `|` pipelines execute in PowerShell unless the entire pipeline is explicitly passed to a container shell. Do not append Linux commands such as `grep` to a bare `docker exec` invocation; either run a deliberate `/bin/bash -lc` command with simple quoting or capture the output and filter it with PowerShell.


- When passing shell snippets through PowerShell into `docker exec`, avoid nested command substitutions and quote-heavy `awk` expressions. Use simple separate commands or a script file, and verify the source container's exact Git HEAD/status before copying the checkout.
- Compare command/evaluator Python using the same probe shells as preflight. If `/bin/sh -lc` and `/bin/bash -lc` resolve different Python installations, explicitly set the sanitized command image PATH to the evaluator's testbed environment (for example, `/opt/miniconda3/envs/testbed/bin`) and rerun offline preflight. Never accept a version mismatch.
- Run Sigma and `--index-memory` from the `go-runtime` project root when relying on local `.secrets/embedding-config.json`; do not copy credentials into task configs or logs.
- For a one-row SWE-bench dataset created with PowerShell, ensure the JSON file is a one-element array (`[...]`) and parse it back to verify the expected `instance_id` before starting the evaluator.

- If copying a large checkout from a source container returns an error such as `unexpected EOF`, retain the source container and reject the partial host copy even if its HEAD matches. Create a tar archive inside the container, copy and extract it into a separate clean directory, and verify the base commit, tracked-file count, and clean Git status before preflight.
