"""Run Sigma Agent on one VibeMemBench task and write an official eval prediction."""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--instance-id", required=True)
    parser.add_argument("--repo", required=True, type=Path, help="prepared target repository")
    task = parser.add_mutually_exclusive_group(required=True)
    task.add_argument("--task", help="benchmark issue text")
    task.add_argument("--task-file", type=Path, help="UTF-8 file containing benchmark issue text")
    parser.add_argument("--config", required=True, type=Path, help="Sigma runtime config for this arm")
    parser.add_argument("--output-dir", required=True, type=Path, help="official runs directory")
    parser.add_argument("--arm", required=True, choices=("memory", "no_memory", "injected_memory"))
    parser.add_argument("--seed", type=int, default=0)
    parser.add_argument("--max-turns", type=int, default=128)
    parser.add_argument("--max-tool-calls", type=int, default=256)
    parser.add_argument("--session-id", default="")
    parser.add_argument("--qdrant-url", default=os.environ.get("SIGMA_AGENT_QDRANT_URL", "http://127.0.0.1:6333"))
    return parser.parse_args()


def final_result(log_path: Path) -> dict[str, Any] | None:
    """Find Sigma's terminal JSONL record, which includes the actual run/patch paths."""
    for line in reversed(log_path.read_text(encoding="utf-8", errors="replace").splitlines()):
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if isinstance(row, dict) and row.get("type") in ("run_completed", "run_failed"):
            return row
    return None


def clean_git_diff(patch: str) -> str:
    """Drop Git's Windows line-ending warnings interleaved with diff sections."""
    noise = "The file will have its original line endings in your working directory"
    lines = [
        line
        for line in patch.splitlines(keepends=True)
        if not line.startswith("warning: LF will be replaced by CRLF in ")
        and line.rstrip("\r\n") != noise
    ]
    return "".join(lines)


def diff_from_workspace(run_dir: Path, session_id: str) -> str | None:
    """Rebuild the patch with stderr kept separate from diff bytes."""
    run_json = run_dir / "run.json"
    workspace = REPO_ROOT / "var" / "sessions" / session_id / "workspace"
    if not run_json.is_file() or not (workspace / ".git").exists():
        return None
    metadata = json.loads(run_json.read_text(encoding="utf-8"))
    base = metadata.get("starting_commit", "")
    if not base:
        return None

    index_result = subprocess.run(
        ["git", "rev-parse", "--git-path", "index"],
        cwd=workspace,
        capture_output=True,
        text=True,
        check=True,
    )
    index_path = Path(index_result.stdout.strip())
    if not index_path.is_absolute():
        index_path = workspace / index_path
    with tempfile.NamedTemporaryFile(prefix="sigma-vbm-index-", delete=False) as index_file:
        temporary_index = Path(index_file.name)
    try:
        shutil.copyfile(index_path, temporary_index)
        env = os.environ.copy()
        env["GIT_INDEX_FILE"] = str(temporary_index)
        untracked = subprocess.run(
            ["git", "ls-files", "--others", "--exclude-standard", "-z"],
            cwd=workspace,
            capture_output=True,
            check=True,
        ).stdout.split(b"\0")
        paths = [
            entry.decode("utf-8", errors="surrogateescape")
            for entry in untracked
            if entry
            # Go caches created by benchmark agents are build artifacts, not
            # candidate files. Exclude them before constructing the Git argv;
            # Windows cannot pass thousands of cache paths in one invocation.
            and entry.split(b"/", 1)[0] not in (b".gocache", b".gotmp", b".sigma-agent")
            and entry not in (b".test.log",)
        ]
        if paths:
            subprocess.run(["git", "add", "-N", "--", *paths], cwd=workspace, env=env, capture_output=True, check=True)
        diff = subprocess.run(
            ["git", "diff", "--no-ext-diff", "--binary", "--unified=3", base],
            cwd=workspace,
            env=env,
            capture_output=True,
            check=True,
        )
        return diff.stdout.decode("utf-8", errors="replace")
    finally:
        temporary_index.unlink(missing_ok=True)


def main() -> int:
    args = parse_args()
    repo = args.repo.resolve()
    config = args.config.resolve()
    if not repo.is_dir() or not (repo / ".git").exists():
        raise SystemExit(f"--repo must be a Git worktree: {repo}")
    if not config.is_file():
        raise SystemExit(f"--config does not exist: {config}")
    config_data = json.loads(config.read_text(encoding="utf-8"))
    memory_enabled = bool(config_data.get("memory_enabled", False))
    if memory_enabled != (args.arm == "memory"):
        raise SystemExit(f"--arm {args.arm} does not match memory_enabled in {config}")
    if config_data.get("memory_auto_write", False):
        raise SystemExit("memory_auto_write must be disabled during controlled benchmark runs")
    memory_file = repo / ".sigma-agent" / "MEMORY.md"
    if args.arm == "memory" and not memory_file.is_file():
        raise SystemExit(f"memory arm requires a prepared {memory_file}")
    if args.arm == "no_memory" and memory_file.exists():
        raise SystemExit(f"control arm must not contain {memory_file}")

    task_text = args.task if args.task is not None else args.task_file.read_text(encoding="utf-8")
    if not task_text.strip():
        raise SystemExit("benchmark task text is empty")

    seed_dir = args.output_dir.resolve() / args.instance_id / args.arm / f"seed{args.seed}"
    seed_dir.mkdir(parents=True, exist_ok=True)
    log_path = seed_dir / "sigma.jsonl"
    command = [
        "go", "run", "./cmd/sigma-agent",
        "--task", task_text,
        "--repo", str(repo),
        "--config", str(config),
        "--github-mcp=false",
        "--json",
        "--keep-workspace",
        "--max-turns", str(args.max_turns),
        "--max-tool-calls", str(args.max_tool_calls),
    ]
    if args.session_id:
        command.extend(("--session-id", args.session_id))

    env = os.environ.copy()
    env["SIGMA_AGENT_QDRANT_URL"] = args.qdrant_url
    with log_path.open("w", encoding="utf-8") as log:
        process = subprocess.run(command, cwd=REPO_ROOT, env=env, stdout=log, stderr=subprocess.STDOUT)

    result = final_result(log_path)
    if result is None:
        raise SystemExit(f"Sigma produced no terminal result; inspect {log_path}")

    patch_path = Path(str(result.get("patch_path", "")))
    if patch_path.is_file():
        patch = clean_git_diff(patch_path.read_text(encoding="utf-8", errors="replace"))
        rebuilt = diff_from_workspace(Path(str(result.get("run_dir", ""))), str(result.get("session_id", "")))
        if rebuilt is not None:
            patch = rebuilt
        if patch.strip():
            (seed_dir / "preds.json").write_text(
                json.dumps({args.instance_id: {"model_patch": patch}}, ensure_ascii=False, indent=2) + "\n",
                encoding="utf-8",
            )

    summary = {
        **result,
        "arm": args.arm,
        "seed": args.seed,
        "process_exit_code": process.returncode,
        "log": str(log_path),
        "prediction_written": (seed_dir / "preds.json").is_file(),
    }
    (seed_dir / "sigma_result.json").write_text(
        json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    # The benchmark's run discovery scans for *.traj.json before it reads
    # preds.json. This small marker lets its official evaluator discover a
    # Sigma prediction without pretending that Sigma emitted mini-SWE's trace.
    exit_status = "Submitted" if result.get("type") == "run_completed" else "LimitsExceeded"
    (seed_dir / f"{args.instance_id}.traj.json").write_text(
        json.dumps(
            {"instance_id": args.instance_id, "info": {"exit_status": exit_status, "submission": ""}},
            ensure_ascii=False,
            indent=2,
        ) + "\n",
        encoding="utf-8",
    )
    print(json.dumps(summary, ensure_ascii=False))
    return process.returncode


if __name__ == "__main__":
    sys.exit(main())
