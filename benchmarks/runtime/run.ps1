$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$go = Join-Path $root '.tools\go\bin\go.exe'
$runner = Join-Path $root '.tools\runtime-runner.exe'

if (-not (Test-Path -LiteralPath $go -PathType Leaf)) {
    throw 'Verified repository-local Go toolchain is missing; follow benchmarks/runtime/README.md.'
}

& $go -C (Join-Path $root 'benchmarks\runtime\runner') build -trimpath -o $runner .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

& $runner -output 'benchmarks/runtime/results/2026-08-21-windows-amd64.json'
exit $LASTEXITCODE
