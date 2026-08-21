$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $root
try {
    & bash (Join-Path $PSScriptRoot 'verify.sh')
    exit $LASTEXITCODE
}
finally {
    Pop-Location
}
