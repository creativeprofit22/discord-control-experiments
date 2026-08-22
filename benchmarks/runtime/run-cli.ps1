param([switch]$VerifyThresholds)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$go = Join-Path $root '.tools\go\bin\go.exe'
if (-not (Test-Path -LiteralPath $go -PathType Leaf)) { throw 'Verified repository-local Go toolchain is missing.' }
$baselinePath = Join-Path $root 'benchmarks\runtime\results\2026-08-21-windows-amd64.json'
$baseline = Get-Content -LiteralPath $baselinePath -Raw | ConvertFrom-Json
$goBaseline = $baseline.candidates | Where-Object { $_.name -eq 'go' }
# Packaged CLI work includes process cold start plus application startup; both p95 budgets are committed foundation measurements.
$coldThresholdNs = [int64]$goBaseline.coldStart.p95Ns + [int64]$goBaseline.mcpStartup.p95Ns
$warmThresholdNs = [int64]$goBaseline.warmRest.p95Ns
$work = Join-Path $root 'benchmarks\runtime\work'
New-Item -ItemType Directory -Force $work | Out-Null
$artifact = Join-Path $work 'discord-control-cli.exe'
& $go build -trimpath -ldflags '-s -w -buildid=' -o $artifact (Join-Path $root 'cmd\discord-control')
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$measurementRaw = (& $go run (Join-Path $root 'benchmarks\runtime\cli-runner') -artifact $artifact) -join "`n"
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$measurement = $measurementRaw | ConvertFrom-Json
$version = $measurement.versionCold
$help = $measurement.helpCold
$warm = $measurement.warmParseDispatchRender
$revision = (& git -C $root rev-parse HEAD).Trim()
$evidence = [ordered]@{
    schemaVersion = 1; generated = (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
    sourceRevision = $revision; os = 'windows'; architecture = 'amd64'; supported = $true
    artifact = [ordered]@{ sha256 = (Get-FileHash $artifact -Algorithm SHA256).Hash.ToLower(); bytes = (Get-Item $artifact).Length }
    thresholds = [ordered]@{ source = 'benchmarks/runtime/results/2026-08-21-windows-amd64.json'; coldFormula = 'go.coldStart.p95Ns + go.mcpStartup.p95Ns'; coldP95Ns = $coldThresholdNs; warmFormula = 'go.warmRest.p95Ns'; warmP95Ns = $warmThresholdNs }
    versionCold = $version; helpCold = $help; warmParseDispatchRender = $warm
}
$output = Join-Path $root 'benchmarks\runtime\results\cli-windows-amd64.json'
$json = (($evidence | ConvertTo-Json -Depth 8) -replace "`r`n", "`n").TrimEnd([char[]]"`r`n")
[IO.File]::WriteAllText($output, $json + "`n", [Text.UTF8Encoding]::new($false))
if ($VerifyThresholds) {
    if ($version.p95Ns -gt $coldThresholdNs) { throw "version cold p95 $($version.p95Ns) exceeds source-derived threshold $coldThresholdNs" }
    if ($help.p95Ns -gt $coldThresholdNs) { throw "help cold p95 $($help.p95Ns) exceeds source-derived threshold $coldThresholdNs" }
    if ([int64]$warm.p95Ns -gt $warmThresholdNs) { throw "warm p95 $($warm.p95Ns) exceeds source-derived threshold $warmThresholdNs" }
}
Write-Host "CLI benchmark passed: version p95=$($version.p95Ns)ns help p95=$($help.p95Ns)ns warm p95=$($warm.p95Ns)ns"
