param(
    [string]$Project = (Join-Path (Split-Path $PSScriptRoot -Parent) 'miniprogram'),
    [Parameter(Mandatory = $true)][string]$DevToolsCli
)
$ErrorActionPreference = 'Stop'
$Project = (Resolve-Path -LiteralPath $Project).Path
$landlordTDesign = Join-Path $Project 'node_modules/tdesign-miniprogram/miniprogram_dist'
$landlordBuiltTDesign = Join-Path $Project 'miniprogram_npm/tdesign-miniprogram'
$landlordTslib = 'miniprogram_npm/tslib/index.js'
if (-not (Test-Path -LiteralPath (Join-Path $landlordTDesign $landlordTslib))) { throw 'Incomplete npm dependency. Run npm ci in the project directory.' }
& $DevToolsCli build-npm --project $Project --lang zh
if ($LASTEXITCODE -ne 0) { throw 'WeChat npm build failed.' }
if (-not (Test-Path -LiteralPath (Join-Path $landlordBuiltTDesign 'navbar/navbar.js'))) { throw 'TDesign was not built.' }
if ((Get-FileHash -LiteralPath (Join-Path $landlordTDesign $landlordTslib)).Hash -ne (Get-FileHash -LiteralPath (Join-Path $landlordBuiltTDesign $landlordTslib)).Hash) { throw 'Built npm dependency is incomplete or differs from the installed package.' }
Write-Host 'TDesign built; bundled tslib verified.'
