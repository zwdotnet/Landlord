param([string]$WxmlCompiler = '')
$ErrorActionPreference = 'Stop'
$landlordMiniRoot = Split-Path $PSScriptRoot -Parent
Push-Location $landlordMiniRoot
try {
    if (-not (Test-Path 'miniprogram/config.local.js')) { throw 'Run python scripts/prepare-miniprogram.py first.' }
    foreach ($landlordMiniFile in (Get-ChildItem miniprogram -Recurse -Filter '*.js' | Where-Object { $_.FullName -notmatch '[\\/](node_modules|miniprogram_npm)[\\/]' })) {
        & node --check $landlordMiniFile.FullName
        if ($LASTEXITCODE -ne 0) { throw "JavaScript syntax check failed: $($landlordMiniFile.Name)" }
    }
    & node --test scripts/miniprogram-tests/client.test.cjs scripts/miniprogram-tests/landlord.test.cjs scripts/miniprogram-tests/meters.test.cjs scripts/miniprogram-tests/issue.test.cjs scripts/miniprogram-tests/payments.test.cjs scripts/miniprogram-tests/contact.test.cjs
    if ($LASTEXITCODE -ne 0) { throw 'Mini program client tests failed.' }
    # unittest writes normal progress to stderr; capture it without PS5 converting it
    # into a terminating NativeCommandError. The process exit code remains decisive.
    & python -c "import subprocess,sys; r=subprocess.run([sys.executable,'-m','unittest','discover','-s','scripts/miniprogram-tests','-p','test_*.py'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT);sys.stdout.buffer.write(r.stdout);sys.exit(r.returncode)"
    if ($LASTEXITCODE -ne 0) { throw 'Mini program configuration tests failed.' }
    if ($WxmlCompiler) {
        $landlordMiniEvidence = Join-Path $landlordMiniRoot ('.runtime/m5-checks/' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
        New-Item -ItemType Directory -Path $landlordMiniEvidence -Force | Out-Null
        $landlordMiniOutput = Join-Path $landlordMiniEvidence 'wxml.js'
        $landlordMiniTemplates = Get-ChildItem miniprogram -Recurse -Filter '*.wxml' | Where-Object { $_.FullName -notmatch '[\\/](node_modules|miniprogram_npm)[\\/]' } | ForEach-Object { $_.FullName }
        $landlordMiniCompilerText = (& python -c "import subprocess,sys; r=subprocess.run(sys.argv[1:],stdout=subprocess.PIPE,stderr=subprocess.STDOUT);sys.stdout.buffer.write(r.stdout);sys.exit(r.returncode)" $WxmlCompiler -o $landlordMiniOutput @landlordMiniTemplates | Out-String)
        $landlordMiniCompilerText | Out-File (Join-Path $landlordMiniEvidence 'compiler.txt') -Encoding utf8
        # This compiler can report a template error while returning exit code zero.
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path $landlordMiniOutput) -or (Get-Item $landlordMiniOutput).Length -lt 100 -or $landlordMiniCompilerText -match 'Bad attr|unexpected|[Ee]rror') {
            throw "WXML compilation failed. Evidence: $landlordMiniEvidence"
        }
        Write-Host "Native WXML compiled: $landlordMiniEvidence"
    }
} finally { Pop-Location }
