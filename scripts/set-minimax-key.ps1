$ErrorActionPreference = 'Stop'

$projectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$secretDirectory = Join-Path $projectRoot '.secrets'
$secretPath = Join-Path $secretDirectory 'minimax-api-key'
$secureKey = Read-Host 'MiniMax API key (input is hidden)' -AsSecureString
$keyPointer = [IntPtr]::Zero

try {
    $keyPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureKey)
    $key = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($keyPointer)
    if ([string]::IsNullOrWhiteSpace($key)) {
        throw 'The API key cannot be empty.'
    }

    New-Item -ItemType Directory -Path $secretDirectory -Force | Out-Null
    [IO.File]::WriteAllText($secretPath, $key, [Text.UTF8Encoding]::new($false))
    Write-Output 'MiniMax API key saved locally. The key was not displayed.'
}
finally {
    if ($keyPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($keyPointer)
    }
    if ($null -ne $key) {
        Remove-Variable key -ErrorAction SilentlyContinue
    }
    if ($null -ne $secureKey) {
        $secureKey.Dispose()
    }
}
