# Issue-resolution benchmark protocol

Use this protocol when measuring whether Sigma Agent can resolve repository issues. Keep reference patches outside the Agent's context until the candidate patch and run have been saved.

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
