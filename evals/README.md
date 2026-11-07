# Evaluation guide

This directory contains benchmark protocols, experiment plans, reusable
configs, fixtures, and helper scripts. Start here to find the right entry
point; the benchmark protocol defines the safety and validity rules shared by
issue-resolution runs.

## Start a benchmark run

1. Read [the issue benchmark protocol](benchmark-protocol.md) before preparing
Docker images or starting a model session.
2. Choose a study below and use its frozen task set, config, and budget. Keep
   model, runtime build, prompt, Skills/Memory state, and evaluator conditions
   fixed for comparisons.
3. From the `go-runtime` repository root, run
   [`scripts/prepare-swebench-task.ps1`](scripts/prepare-swebench-task.ps1).
   It pulls missing task images through the host Docker daemon and invokes the
   offline image and checkout check. A failed preflight must stop the run
   before model calls. To recheck an already prepared environment without
   pulling, run [`scripts/preflight-swebench-task.ps1`](scripts/preflight-swebench-task.ps1)
   directly.
4. Save the raw candidate and evaluator evidence. Keep invalid, incomplete,
   unresolved, and resolved outcomes distinct. The protocol explains when a
   run may be cleaned up or archived.

Do not put provider keys in configs, transcripts, task prompts, or tracked
files. Use environment variables or Sigma's local credential store.

## Study index

### SWE-bench and issue repair

- [SWE-bench Verified Mini protocol and run plan](swebench-verified-mini-50-evaluation-plan.md)
- [Zero fail-to-pass task analysis](swebench-verified-mini-50-zero-f2p-tasks.md)
- [Issue benchmark inventory](issue-benchmark-inventory.md)
- [Multi-SWE-Go task subset](multiswe-go-10-task-subset.md)
- [Paired Multi-SWE evidence set](multiswe-paired-evidence-set.md)
- [Multi-SWE subset naming research](multiswe-subset-naming-research.md)
- [Pi 50-task comparison plan](pi-50-task-evaluation-plan.md)

### Skills and repository memory

- [Skill and memory ablation protocol](skills-memory-evaluation.md)
- [Memory retrieval evaluation](memory-retrieval-evaluation.md)
- [Labeled retrieval queries](memory-retrieval-v1.json)
- [Public experiment summaries and current evidence](experiments/README.md)

The full config inventory, overlays, and studies without frozen configs are
listed in [`configs/README.md`](configs/README.md).

## Helper scripts

Scripts live in [`scripts/`](scripts/), separate from plans and result
summaries.

| Script | Use |
| --- | --- |
| [`prepare-swebench-task.ps1`](scripts/prepare-swebench-task.ps1) | Pull missing command/evaluator images using the host Docker daemon, then run the offline preflight. |
| [`preflight-swebench-task.ps1`](scripts/preflight-swebench-task.ps1) | Verify Docker, exact local images, checkout mount, and command/evaluator Python before model calls; never pulls images. |
| [`prepare-go-benchmark-patches.ps1`](scripts/prepare-go-benchmark-patches.ps1) | Preserve the raw Go patch and produce a separate source-only diagnostic patch plus checksums. |

Run the scripts from the repository root unless a study says otherwise. The
prepare script invokes its sibling preflight script from `scripts/`.

## Configs, fixtures, and outputs

- [`configs/`](configs/README.md) contains study configs, task overrides,
  paired ablation configs, and examples. Its README maps each file to the relevant
  study and records which planned studies still need a frozen config.
- [`fixtures/`](fixtures/) contains small test workspaces, including the
  labeled memory-retrieval fixture.
- [`experiments/`](experiments/) contains public experiment overviews,
  reproduction notes, and distilled findings.
- `results/` is local run output and is ignored by Git. Detailed benchmark
  transcripts, patches, and official evaluator artifacts live in the separate
  archive; the experiment summaries identify records by batch/task ID.
