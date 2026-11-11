param([switch]$Focused)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'use-local-toolchain.ps1')
$landlordEvidence = Join-Path $landlordRoot ('.runtime/verification/' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory -Path $landlordEvidence -Force | Out-Null
Push-Location (Join-Path $landlordRoot 'server')
try {
    $landlordTestArgs = @('test', './...', '-count=1', '-json')
    if ($Focused) {
        $landlordTestArgs = @('test', './rental', '-count=1', '-json', '-run', 'TestM3|TestM4|TestM2|TestLandlord|TestBillUpdatePartial|TestContractOwnershipIsolated|TestTenantRenew|TestPortalBills|TestPortalBillDetail|TestPortalPayments')
    }
    & go @landlordTestArgs | Out-File -LiteralPath (Join-Path $landlordEvidence 'backend.jsonl') -Encoding utf8
    if ($LASTEXITCODE -ne 0) { throw "Backend tests failed. Evidence: $landlordEvidence" }
} finally { Pop-Location }
Push-Location (Join-Path $landlordRoot 'web')
try {
    & npm run build | Tee-Object -FilePath (Join-Path $landlordEvidence 'web-build.txt')
    if ($LASTEXITCODE -ne 0) { throw 'Frontend build failed.' }
    & node --test tests/lease.test.mjs tests/pwa.test.mjs
    if ($LASTEXITCODE -ne 0) { throw 'Frontend lease date tests failed.' }
} finally { Pop-Location }
Write-Host "Verification passed. Evidence: $landlordEvidence"
