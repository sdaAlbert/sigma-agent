[CmdletBinding()]
param(
    [switch]$Delete,
    [switch]$RunsArchivedOrAbandoned
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$expectedRoot = [IO.Path]::GetFullPath((Join-Path $repoRoot 'var\sessions')).TrimEnd('\')
if (-not (Test-Path -LiteralPath $expectedRoot -PathType Container)) {
    throw "Session directory does not exist: $expectedRoot"
}

$sessionRoot = (Resolve-Path -LiteralPath $expectedRoot).Path.TrimEnd('\')
if (-not $sessionRoot.Equals($expectedRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Resolved session directory differs from the expected path: $sessionRoot"
}

$candidates = @(
    foreach ($directory in Get-ChildItem -LiteralPath $sessionRoot -Directory -Force) {
        $metadataPath = Join-Path $directory.FullName 'metadata.json'
        if (-not (Test-Path -LiteralPath $metadataPath -PathType Leaf)) {
            continue
        }

        try {
            $metadata = Get-Content -LiteralPath $metadataPath -Raw | ConvertFrom-Json
        }
        catch {
            Write-Warning "Skipping session with unreadable metadata: $($directory.Name)"
            continue
        }

        $repo = [string]$metadata.repo
        if ($repo -notmatch '(?i)(?:^|[/\\])(?:vbm-pilot|vibemembench)(?:[/\\]|$)') {
            continue
        }

        $resolvedDirectory = (Resolve-Path -LiteralPath $directory.FullName).Path.TrimEnd('\')
        $expectedParent = $sessionRoot + '\'
        if (-not $resolvedDirectory.StartsWith($expectedParent, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Refusing a session outside var\sessions: $resolvedDirectory"
        }
        if (($directory.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) {
            Write-Warning "Skipping reparse-point session directory: $($directory.Name)"
            continue
        }

        $nestedReparsePoint = Get-ChildItem -LiteralPath $resolvedDirectory -Recurse -Force -ErrorAction Stop |
            Where-Object { ($_.Attributes -band [IO.FileAttributes]::ReparsePoint) -ne 0 } |
            Select-Object -First 1
        if ($nestedReparsePoint) {
            Write-Warning "Skipping session containing a reparse point: $($directory.Name)"
            continue
        }

        $files = Get-ChildItem -LiteralPath $resolvedDirectory -File -Recurse -Force -ErrorAction Stop
        $bytes = ($files | Measure-Object -Property Length -Sum).Sum
        [PSCustomObject]@{
            Path       = $resolvedDirectory
            Session    = $directory.Name
            Repo       = $repo
            Modified   = $directory.LastWriteTime
            FileCount  = $files.Count
            SizeBytes  = [long]$bytes
            SizeMB     = [math]::Round(([double]$bytes / 1MB), 2)
        }
    }
)

if ($candidates.Count -eq 0) {
    Write-Output 'No VibeMemBench session directories found.'
    exit 0
}

$totalBytes = ($candidates | Measure-Object -Property SizeBytes -Sum).Sum
$candidates |
    Sort-Object Modified |
    Format-Table Session, Modified, SizeMB, Repo -AutoSize
Write-Output ("Found {0} session directories totaling {1:N2} MB." -f $candidates.Count, ([double]$totalBytes / 1MB))

if (-not $Delete) {
    Write-Output 'Dry run only. To delete, pass -Delete -RunsArchivedOrAbandoned after the runs are archived or explicitly abandoned.'
    exit 0
}

if (-not $RunsArchivedOrAbandoned) {
    throw 'Refusing deletion until every matching Agent run and official evaluation is archived or the run is explicitly abandoned. Review the inventory, then pass -RunsArchivedOrAbandoned.'
}

$confirmation = "DELETE $($candidates.Count) ARCHIVED OR ABANDONED VIBEMEMBENCH SESSIONS"
$answer = Read-Host "Type exactly '$confirmation' to continue"
if ($answer -cne $confirmation) {
    Write-Output 'Confirmation did not match. Nothing was deleted.'
    exit 0
}

foreach ($candidate in $candidates) {
    $currentPath = (Resolve-Path -LiteralPath $candidate.Path).Path.TrimEnd('\')
    if (-not $currentPath.Equals($candidate.Path, [StringComparison]::OrdinalIgnoreCase) -or
        -not $currentPath.StartsWith($sessionRoot + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to remove a path that changed after inventory: $($candidate.Path)"
    }

    $metadata = Get-Content -LiteralPath (Join-Path $currentPath 'metadata.json') -Raw | ConvertFrom-Json
    if ([string]$metadata.repo -notmatch '(?i)(?:^|[/\\])(?:vbm-pilot|vibemembench)(?:[/\\]|$)') {
        throw "Refusing to remove a session whose metadata changed: $currentPath"
    }

    Remove-Item -LiteralPath $currentPath -Recurse -Force
    if (Test-Path -LiteralPath $currentPath) {
        throw "Session directory still exists after removal: $currentPath"
    }
}

Write-Output ("Removed {0} VibeMemBench session directories ({1:N2} MB)." -f $candidates.Count, ([double]$totalBytes / 1MB))
