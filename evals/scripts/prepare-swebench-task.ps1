param(
    [Parameter(Mandatory = $true)]
    [string]$RepoPath,

    [Parameter(Mandatory = $true)]
    [string]$EvaluatorImage,

    [string]$ConfigPath = "evals/configs/swebench-verified-mini/full-sigma-5.json",
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

function Ensure-Image {
    param([Parameter(Mandatory = $true)][string]$Image)

    $inspect = & docker image inspect $Image --format "{{.Id}} {{.Size}}" 2>&1
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace(($inspect -join "`n"))) {
        Write-Host "Image already present: $Image ($($inspect -join ' '))"
        return
    }

    Write-Host "Pulling missing image through the host Docker daemon: $Image"
    $pullOutput = & docker pull $Image 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Host-side docker pull failed for '$Image':`n$($pullOutput -join "`n")`nNo Agent model run was started."
    }
    Write-Host ($pullOutput -join "`n")
}

$repo = (Resolve-Path -LiteralPath $RepoPath).Path
$configFile = if ([System.IO.Path]::IsPathRooted($ConfigPath)) {
    $ConfigPath
} else {
    Join-Path (Get-Location).Path $ConfigPath
}
$configFile = (Resolve-Path -LiteralPath $configFile).Path
$config = Get-Content -LiteralPath $configFile -Raw | ConvertFrom-Json

if ($config.allow_container_network -ne $false) {
    throw "Benchmark config must explicitly set allow_container_network=false for the Agent run."
}
if ($config.web_search_enabled -ne $false) {
    throw "Benchmark config must explicitly set web_search_enabled=false for the Agent run."
}
if ($config.disable_configured_mcp_servers -ne $true) {
    throw "Benchmark config must explicitly set disable_configured_mcp_servers=true to prevent user MCP egress."
}
if ($config.disable_workflow_extensions -ne $true) {
    throw "Benchmark config must explicitly set disable_workflow_extensions=true to prevent user-installed extensions from adding tools."
}
$commandImage = [string]$config.command_execution_image
if ([string]::IsNullOrWhiteSpace($commandImage)) {
    throw "Config '$configFile' has no command_execution_image."
}

$null = Invoke-DockerText -Arguments @("info", "--format", "{{.ServerVersion}}")
Ensure-Image -Image $commandImage
Ensure-Image -Image $EvaluatorImage

$preflight = Join-Path $PSScriptRoot "preflight-swebench-task.ps1"
& $preflight -RepoPath $repo -EvaluatorImage $EvaluatorImage -ConfigPath $configFile -ExpectedPython $ExpectedPython
if ($LASTEXITCODE -ne 0) {
    throw "Offline task preflight failed. Do not start the Agent until the environment is ready."
}
