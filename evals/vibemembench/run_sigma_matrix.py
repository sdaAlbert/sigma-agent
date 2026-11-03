"""Run a matched Sigma memory ablation matrix over prepared VibeMemBench tasks."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path
from typing import Any


REPO_ROOT = Path(__file__).resolve().parents[2]
RUNNER = Path(__file__).with_name("run_sigma.py")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--retrieval", required=True, type=Path)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--repeats", type=int, default=2)
    parser.add_argument("--max-turns", type=int, default=128)
    parser.add_argument("--max-tool-calls", type=int, default=256)
    parser.add_argument("--resume", action="store_true")
    return parser.parse_args()


def injected_task(issue: str, retrieved: str, memory_file: Path) -> str:
    match = re.search(r"memory_entry=\.sigma-agent/MEMORY\.md:(\d+)", retrieved)
    if match is None:
        return issue
    entries = memory_file.read_text(encoding="utf-8").splitlines()
    line_number = int(match.group(1))
    if line_number < 1 or line_number > len(entries):
        return issue
    entry = entries[line_number - 1].strip()
    return f"<relevant_experiences>\n{entry}\n</relevant_experiences>\n\n{issue}"


def main() -> int:
    args = parse_args()
    manifest: dict[str, Any] = json.loads(args.manifest.read_text(encoding="utf-8"))
    retrieval_rows = json.loads(args.retrieval.read_text(encoding="utf-8"))
    retrieval_by_id = {row["instance_id"]: row["raw_result"] for row in retrieval_rows}
    output = args.output_dir.resolve()
    output.mkdir(parents=True, exist_ok=True)
    tasks = manifest["targets"]

    # Counterbalance run order across repeat rounds while keeping each task's
    # three conditions close together in time.
    arms = ["no_memory", "memory", "injected_memory"]
    schedule: list[tuple[dict[str, Any], int, str]] = []
    for repeat in range(args.repeats):
        for task_index, task in enumerate(tasks):
            order = arms[:] if (repeat + task_index) % 2 == 0 else list(reversed(arms))
            for arm in order:
                schedule.append((task, repeat, arm))

    summary_path = output / "orchestration.jsonl"
    with summary_path.open("a", encoding="utf-8") as journal:
        for index, (task, repeat, arm) in enumerate(schedule, start=1):
            instance_id = task["instance_id"]
            seed_dir = output / instance_id / arm / f"seed{repeat}"
            result_path = seed_dir / "sigma_result.json"
            if args.resume and result_path.is_file():
                print(f"[{index}/{len(schedule)}] SKIP {instance_id} {arm} repeat={repeat}", flush=True)
                continue

            issue_file = Path(task["problem_statement_file"])
            repo = task["memory_repo"] if arm == "memory" else task["control_repo"]
            config_key = "native_memory" if arm == "memory" else ("injected_memory" if arm == "injected_memory" else "no_memory")
            config = Path(task["configs"][config_key])
            effective_task_file = issue_file
            if arm == "injected_memory":
                raw_retrieved = retrieval_by_id[instance_id]
                issue = issue_file.read_text(encoding="utf-8")
                injected_path = output / "injected_tasks" / f"{instance_id}.task.txt"
                injected_path.parent.mkdir(parents=True, exist_ok=True)
                injected_path.write_text(injected_task(issue, raw_retrieved, Path(task["memory_file"])) + "\n", encoding="utf-8")
                effective_task_file = injected_path

            command = [
                sys.executable, str(RUNNER),
                "--instance-id", instance_id,
                "--repo", str(repo),
                "--task-file", str(effective_task_file),
                "--config", str(config),
                "--output-dir", str(output),
                "--arm", arm,
                "--seed", str(repeat),
                "--max-turns", str(args.max_turns),
                "--max-tool-calls", str(args.max_tool_calls),
            ]
            env = os.environ.copy()
            # This experiment isolates the production BM25 path and keeps the
            # injected arm identical to an actual search_memory top-1 result.
            env.pop("SIGMA_AGENT_EMBEDDING_MODEL", None)
            started = time.time()
            print(f"[{index}/{len(schedule)}] START {instance_id} {arm} repeat={repeat}", flush=True)
            completed = subprocess.run(command, cwd=REPO_ROOT, env=env, text=True, capture_output=True)
            elapsed = time.time() - started
            terminal = None
            for line in reversed(completed.stdout.splitlines()):
                try:
                    candidate = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if candidate.get("type") in ("run_completed", "run_failed"):
                    terminal = candidate
                    break
            record = {
                "instance_id": instance_id, "arm": arm, "repeat": repeat,
                "exit_code": completed.returncode, "elapsed_seconds": round(elapsed, 2),
                "run_id": terminal.get("run_id") if terminal else None,
                "status": terminal.get("status") if terminal else "runner_error",
                "terminal_error": terminal.get("error", "") if terminal else completed.stderr[-2000:],
                "command": command,
            }
            journal.write(json.dumps(record, ensure_ascii=False) + "\n")
            journal.flush()
            print(f"[{index}/{len(schedule)}] END {instance_id} {arm} repeat={repeat} status={record['status']} exit={completed.returncode} elapsed={elapsed:.0f}s", flush=True)
            if completed.stdout:
                for line in completed.stdout.splitlines()[-2:]:
                    print(line, flush=True)
            if completed.stderr:
                print(completed.stderr[-1000:], file=sys.stderr, flush=True)

    (output / "matrix_config.json").write_text(json.dumps({
        "repeats": args.repeats,
        "max_turns": args.max_turns,
        "max_tool_calls": args.max_tool_calls,
        "conditions": ["no_memory", "memory", "injected_memory"],
        "injected_memory_method": "Sigma BM25 top-1 from the same pre-task MEMORY.md; VibeMemBench relevant_experiences block",
        "embedding_provider_enabled": False,
        "model_seed_control": "MiniMax API exposes no deterministic seed in this runner; repeat index is a label only",
        "schedule_order": "counterbalanced by task index and repeat parity",
    }, indent=2) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
