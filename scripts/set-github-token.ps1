$ErrorActionPreference = 'Stop'

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$secretDirectory = Join-Path $projectRoot '.secrets'
$secretPath = Join-Path $secretDirectory 'github-pat'
$secureToken = Read-Host 'GitHub PAT (input is hidden)' -AsSecureString
$tokenPointer = [IntPtr]::Zero

try {
    $tokenPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)
    $token = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($tokenPointer)
    if ([string]::IsNullOrWhiteSpace($token)) {
        throw 'The token cannot be empty.'
    }

    New-Item -ItemType Directory -Path $secretDirectory -Force | Out-Null
    [IO.File]::WriteAllText($secretPath, $token, [Text.UTF8Encoding]::new($false))
    Write-Output 'GitHub PAT saved locally. The token was not displayed.'
}
finally {
    if ($tokenPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($tokenPointer)
    }
    if ($null -ne $token) {
        Remove-Variable token -ErrorAction SilentlyContinue
    }
    if ($null -ne $secureToken) {
        $secureToken.Dispose()
    }
}
