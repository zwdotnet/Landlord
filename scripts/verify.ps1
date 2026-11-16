param([switch]$Focused)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'use-local-toolchain.ps1')
$landlordVerifyArgs = @((Join-Path $PSScriptRoot 'verify.py'))
if ($Focused) { $landlordVerifyArgs += '--focused' }
& python @landlordVerifyArgs
if ($LASTEXITCODE -ne 0) { throw 'Verification failed. See the evidence path above.' }
