# Dot-source this file. It changes only the current PowerShell process.
$landlordRoot = Split-Path $PSScriptRoot -Parent
$landlordTools = Join-Path $landlordRoot '.runtime/toolchains'
$landlordGo = Join-Path $landlordTools 'go/bin'
$landlordCC = Join-Path $landlordTools 'llvm-mingw-20261104-ucrt-x86_64/bin'
if (Test-Path (Join-Path $landlordGo 'go.exe')) {
    $env:PATH = "$landlordGo;" + $env:PATH
}
if (Test-Path (Join-Path $landlordCC 'gcc.exe')) {
    $env:PATH = "$landlordCC;" + $env:PATH
    $env:CC = Join-Path $landlordCC 'gcc.exe'
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw 'Go 1.26.5 is required. See README.md for the local toolchain paths.'
}
$env:CGO_ENABLED = '1'
$env:GOTOOLCHAIN = 'local'
$env:GOCACHE = Join-Path $landlordRoot '.runtime/go-cache'
$env:GOMODCACHE = Join-Path $landlordRoot '.runtime/go-mod-cache'
