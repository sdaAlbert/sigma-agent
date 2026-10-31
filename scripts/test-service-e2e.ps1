$ErrorActionPreference = 'Stop'
$previous = $env:SIGMA_AGENT_LIVE_SERVICE_TEST
try {
    $env:SIGMA_AGENT_LIVE_SERVICE_TEST = '1'
    & go test ./internal/server -run '^TestLiveTaskServiceLifecycle$' -count=1 -v
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    if ($null -eq $previous) {
        Remove-Item Env:SIGMA_AGENT_LIVE_SERVICE_TEST -ErrorAction SilentlyContinue
    } else {
        $env:SIGMA_AGENT_LIVE_SERVICE_TEST = $previous
    }
}
