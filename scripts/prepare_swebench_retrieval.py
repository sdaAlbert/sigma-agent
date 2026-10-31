#!/usr/bin/env python3
"""Download SWE-bench BM25 dev rows and convert them to Sigma Agent retrieval cases.

The gold paths are extracted from the official solution patch. This measures
whether retrieval finds files changed by the reference solution; it does not
claim that every changed file is necessary context.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import time
import urllib.parse
import urllib.request
from collections import Counter
from pathlib import Path


DATASET = "princeton-nlp/SWE-bench_bm25_13K"
CONFIG = "default"
SPLIT = "dev"
PAGE_SIZE = 100
PATCH_PATH = re.compile(r"^diff --git a/(.+?) b/(.+?)$", re.MULTILINE)


def fetch_rows(offset: int, length: int) -> tuple[list[dict], int]:
    params = urllib.parse.urlencode(
        {
            "dataset": DATASET,
            "config": CONFIG,
            "split": SPLIT,
            "offset": offset,
            "length": length,
        }
    )
    request = urllib.request.Request(
        f"https://datasets-server.huggingface.co/rows?{params}",
        headers={"User-Agent": "Sigma Agent retrieval-eval dataset preparer"},
    )
    for attempt in range(5):
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                payload = json.load(response)
            return [item["row"] for item in payload["rows"]], payload["num_rows_total"]
        except Exception:
            if attempt == 4:
                raise
            time.sleep(2**attempt)
    raise RuntimeError("unreachable")


def patch_paths(patch: str) -> list[str]:
    paths = []
    for old, new in PATCH_PATH.findall(patch):
        # Evaluate against the pre-fix base commit: label the a/ path and omit
        # newly added files, which do not exist in the retrieval corpus yet.
        if old == "/dev/null":
            continue
        path = old
        if path != "/dev/null" and path not in paths:
            paths.append(path)
    return paths


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--output",
        default="var/benchmark-data/swebench_bm25_13k_dev.json",
        help="output JSON path (default: %(default)s)",
    )
    parser.add_argument(
        "--exclude-repo",
        action="append",
        default=["sqlfluff/sqlfluff"],
        help="repository to exclude; repeatable (default excludes SQLFluff)",
    )
    args = parser.parse_args()
    excluded = set(args.exclude_repo)

    rows: list[dict] = []
    offset = 0
    total = None
    while total is None or offset < total:
        page, total = fetch_rows(offset, PAGE_SIZE)
        if not page:
            break
        rows.extend(page)
        offset += len(page)
        print(f"Fetched {offset}/{total} SWE-bench BM25 dev rows", file=sys.stderr)

    cases = []
    for row in rows:
        if row["repo"] in excluded:
            continue
        relevant = patch_paths(row.get("patch", ""))
        query = (row.get("problem_statement") or "").strip()
        if not query or not relevant:
            continue
        cases.append(
            {
                "id": row["instance_id"],
                "dataset": "SWE-bench BM25 13K dev (non-SQLFluff)",
                "repo": row["repo"],
                "base_commit": row["base_commit"],
                "query": query,
                "relevant_paths": relevant,
            }
        )

    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(cases, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    counts = Counter(case["repo"] for case in cases)
    print(f"Wrote {len(cases)} cases to {output}")
    print("Repositories:")
    for repo, count in sorted(counts.items()):
        print(f"  {repo}: {count}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
