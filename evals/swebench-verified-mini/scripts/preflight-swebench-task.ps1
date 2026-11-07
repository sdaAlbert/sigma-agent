param(
    [Parameter(Mandatory = $true)]
    [string]$RepoPath,

    [Parameter(Mandatory = $true)]
    [string]$EvaluatorImage,

    [string]$ConfigPath = "evals/swebench-verified-mini/configs/full-sigma-5.json",
    [string]$ExpectedPython = ""
)

$ErrorActionPreference = "Stop"

function Invoke-DockerText {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    $output = & docker @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed:`n$($output -join "`n")"
    }
    return ($output -join "`n").Trim()
}

function Assert-LocalImage {
    param([Parameter(Mandatory = $true)][string]$Image)

    $imageInfo = Invoke-DockerText -Arguments @("image", "inspect", $Image, "--format", "{{.Id}} {{.Size}}")
    if ([string]::IsNullOrWhiteSpace($imageInfo)) {
        throw "Docker returned no metadata for local image '$Image'."
    }
    Write-Host "Image ready: $Image ($imageInfo)"
}

$repo = (Resolve-Path -LiteralPath $RepoPath).Path
$configFile = if ([System.IO.Path]::IsPathRooted($ConfigPath)) {
    $ConfigPath
} else {
    Join-Path (Get-Location).Path $ConfigPath
}
$configFile = (Resolve-Path -LiteralPath $configFile).Path
$config = Get-Content -LiteralPath $configFile -Raw | ConvertFrom-Json
$allowCommandNetwork = $config.allow_container_network
$allowWebSearch = $config.web_search_enabled
if ($allowCommandNetwork -ne $false) {
    throw "Benchmark config must explicitly set allow_container_network=false for the Agent run. Prepare/download dependencies before starting the Agent."
}
if ($allowWebSearch -ne $false) {
    throw "Benchmark config must explicitly set web_search_enabled=false for the Agent run."
}
if ($config.disable_configured_mcp_servers -ne $true) {
    throw "Benchmark config must explicitly set disable_configured_mcp_servers=true to prevent user MCP egress."
}
if ($config.disable_workflow_extensions -ne $true) {
    throw "Benchmark config must explicitly set disable_workflow_extensions=true to prevent user-installed extensions from adding tools."
}
if ($config.command_filesystem_scope -ne "workspace") {
    throw "Benchmark config must set command_filesystem_scope=workspace so run_command rejects explicit reads outside /workspace."
}
$commandImage = [string]$config.command_execution_image
if ([string]::IsNullOrWhiteSpace($commandImage)) {
    throw "Config '$configFile' has no command_execution_image."
}

$dockerVersion = Invoke-DockerText -Arguments @("info", "--format", "{{.ServerVersion}}")
Write-Host "Docker daemon ready: $dockerVersion"
$repoRootOutput = & git -C $repo rev-parse --show-toplevel 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "Task checkout is not a Git repository:`n$($repoRootOutput -join "`n")"
}
$repoRoot = ($repoRootOutput -join "`n").Trim()
Write-Host "Task checkout: $repoRoot"

# Inspect first and run with --pull=never: a missing task image fails before any
# model call instead of triggering a slow, implicit Docker Hub pull.
Assert-LocalImage -Image $commandImage
Assert-LocalImage -Image $EvaluatorImage

$mount = "type=bind,source=$repo,target=/workspace,readonly"
$commandProbe = 'set -eu; echo "Command image Python:"; if command -v python >/dev/null 2>&1; then python --version; elif command -v python3 >/dev/null 2>&1; then python3 --version; else echo "python/python3 missing"; exit 21; fi; echo "Mounted checkout:"; test -d /workspace; test -n "$(ls -A /workspace)"; echo "source files are mounted at /workspace"'
$commandResult = Invoke-DockerText -Arguments @(
    "run", "--pull=never", "--rm", "--network", "none", "--memory", "1g", "--cpus", "1",
    "--mount", $mount, "--workdir", "/workspace", "--entrypoint", "/bin/sh",
    $commandImage, "-lc", $commandProbe
)
Write-Host "Command image preflight:`n$commandResult"
if ($ExpectedPython -and $commandResult -notmatch [regex]::Escape($ExpectedPython)) {
    throw "Command image Python version did not match expected '$ExpectedPython'."
}

$evaluatorProbe = 'set -eu; echo "Evaluator Python:"; if command -v python >/dev/null 2>&1; then python --version; elif command -v python3 >/dev/null 2>&1; then python3 --version; else echo "python/python3 missing"; exit 21; fi; test -d /testbed; echo "Evaluator checkout:"; git -c safe.directory=* -C /testbed rev-parse --show-toplevel'
$evaluatorResult = Invoke-DockerText -Arguments @(
    "run", "--pull=never", "--rm", "--network", "none", "--memory", "2g", "--cpus", "1",
    "--entrypoint", "/bin/bash", $EvaluatorImage, "-lc", $evaluatorProbe
)
Write-Host "Evaluator image preflight:`n$evaluatorResult"
$commandVersion = [regex]::Match($commandResult, 'Python\s+(\d+\.\d+\.\d+)').Groups[1].Value
$evaluatorVersion = [regex]::Match($evaluatorResult, 'Python\s+(\d+\.\d+\.\d+)').Groups[1].Value
if (-not $commandVersion -or -not $evaluatorVersion) {
    throw "Could not determine both command and evaluator Python versions."
}
if ($commandVersion -ne $evaluatorVersion) {
    throw "Command/evaluator Python mismatch: command image is $commandVersion; evaluator image is $evaluatorVersion. Prepare a matching command image before starting the Agent."
}
Write-Host "Command/evaluator Python match: $commandVersion"

Write-Host "PREFLIGHT PASSED. No image was pulled and the task checkout was mounted read-only."
