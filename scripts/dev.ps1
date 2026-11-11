param([int]$Port = 18920, [switch]$NoBuild, [ValidatePattern('^[a-zA-Z0-9-]+$')][string]$Instance = 'dev-m2')
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'use-local-toolchain.ps1')
$landlordRun = Join-Path $landlordRoot ".runtime/$Instance"
$landlordWeb = Join-Path $landlordRoot 'web/dist'
$landlordVersion = (Get-Content -LiteralPath (Join-Path $landlordRoot 'VERSION') -Raw).Trim()
$landlordExe = Join-Path $landlordRun "landlord-$landlordVersion.exe"
if (Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue) {
    throw "Port $Port is already in use. Use a different -Port and -Instance, or stop your existing instance first."
}
New-Item -ItemType Directory -Path $landlordRun -Force | Out-Null
if (-not $NoBuild) {
    Push-Location (Join-Path $landlordRoot 'web')
    try {
        & npm run build
        if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    } finally { Pop-Location }
    Push-Location (Join-Path $landlordRoot 'server')
    try {
        & go build -ldflags "-X smallgo/server/version.Version=$landlordVersion -X smallgo/server/version.AppName=landlord" -o $landlordExe .
        if ($LASTEXITCODE -ne 0) { throw 'Backend build failed.' }
    } finally { Pop-Location }
}
if (-not (Test-Path $landlordExe)) { throw 'Run without -NoBuild once to build the backend.' }
if (-not (Test-Path (Join-Path $landlordWeb 'index.html'))) { throw 'Frontend build is missing.' }
Write-Host "Landlord: http://127.0.0.1:$Port (Ctrl+C to stop)"
Push-Location $landlordRoot
try {
    & $landlordExe -port $Port -data-dir (Join-Path $landlordRun 'data') -web-dir $landlordWeb -disable-stats -log-console -rate-limit 0
} finally { Pop-Location }
