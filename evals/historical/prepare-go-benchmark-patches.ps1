[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$CandidatePatch,

    [Parameter(Mandatory = $true)]
    [string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'
$candidatePath = (Resolve-Path -LiteralPath $CandidatePatch).Path
$outputPath = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $outputPath -Force | Out-Null

$rawPatchPath = Join-Path $outputPath 'candidate.raw.patch'
$sourcePatchPath = Join-Path $outputPath 'candidate.source-only.patch'
$manifestPath = Join-Path $outputPath 'patch-manifest.json'

Copy-Item -LiteralPath $candidatePath -Destination $rawPatchPath -Force
$rawPatch = [System.IO.File]::ReadAllText($candidatePath)
$sections = [System.Text.RegularExpressions.Regex]::Split($rawPatch, '(?m)(?=^diff --git )')
$keptSections = [System.Collections.Generic.List[string]]::new()
$includedFiles = [System.Collections.Generic.List[string]]::new()
$excludedTestFiles = [System.Collections.Generic.List[string]]::new()

foreach ($section in $sections) {
    if ([string]::IsNullOrWhiteSpace($section)) {
        continue
    }

    if ($section -notmatch '(?m)^diff --git a/(.+?) b/(.+?)\r?$') {
        throw 'Input is not a git diff patch with diff --git sections.'
    }

    $oldPath = $Matches[1]
    $newPath = $Matches[2]
    if ($oldPath -match '(?:^|/)[^/]+_test\.go$' -or $newPath -match '(?:^|/)[^/]+_test\.go$') {
        $excludedTestFiles.Add($newPath)
        continue
    }

    $includedFiles.Add($newPath)
    $keptSections.Add($section)
}

if ($keptSections.Count -eq 0) {
    throw 'The candidate patch has no production-file changes after excluding Go test files.'
}

$sourcePatch = [string]::Concat($keptSections)
[System.IO.File]::WriteAllText($sourcePatchPath, $sourcePatch, [System.Text.UTF8Encoding]::new($false))

$manifest = [ordered]@{
    candidate_patch       = $candidatePath
    raw_patch             = $rawPatchPath
    raw_sha256            = (Get-FileHash -LiteralPath $rawPatchPath -Algorithm SHA256).Hash.ToLowerInvariant()
    source_only_patch     = $sourcePatchPath
    source_only_sha256    = (Get-FileHash -LiteralPath $sourcePatchPath -Algorithm SHA256).Hash.ToLowerInvariant()
    included_files        = @($includedFiles)
    excluded_test_files   = @($excludedTestFiles)
}
[System.IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 4), [System.Text.UTF8Encoding]::new($false))

[pscustomobject]@{
    RawPatch = $rawPatchPath
    SourceOnlyPatch = $sourcePatchPath
    Manifest = $manifestPath
    ExcludedTestFiles = @($excludedTestFiles)
}
