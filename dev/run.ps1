<#
.SYNOPSIS
    Builds and starts Server Manager locally for development, without a real Assetto Corsa server.

.DESCRIPTION
    - builds the frontend (TypeScript/SCSS) if it hasn't been built yet, or when -Frontend is given
    - generates the embedded asset files (esc) and builds the Go binary
    - on first run, copies dev/config.yml and the fake AC install (dev/assetto-seed) into cmd/server-manager
    - starts Server Manager with templates/static files served from disk (FILESYSTEM_HTML=true),
      so changes to views/ only need a browser refresh.

    Login: http://localhost:8772 with admin / admin

.PARAMETER Frontend
    Force a rebuild of the frontend (npm install + gulp build).

.PARAMETER Reset
    Delete the local dev data (fake AC install, database, config.yml) and start from scratch.

.PARAMETER NoRun
    Only build, don't start the server.
#>
param(
    [switch]$Frontend,
    [switch]$Reset,
    [switch]$NoRun
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$appDir = Join-Path $repoRoot 'cmd/server-manager'
$tsDir = Join-Path $appDir 'typescript'

function Invoke-Checked([string]$what, [scriptblock]$block) {
    Write-Host "==> $what" -ForegroundColor Cyan
    & $block
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

foreach ($tool in 'go', 'npm') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        throw "'$tool' was not found in PATH. Install it first (e.g. 'scoop install go' / Node.js)."
    }
}

# esc is used by 'go generate' to embed static files, views and the changelog
$goBin = Join-Path (go env GOPATH) 'bin'
if ($env:PATH -notlike "*$goBin*") { $env:PATH = "$goBin;$env:PATH" }
if (-not (Get-Command esc -ErrorAction SilentlyContinue)) {
    Invoke-Checked 'Installing esc' { go install github.com/mjibson/esc@v0.2.0 }
}

# frontend
$bundle = Join-Path $appDir 'static/js/bundle.js'
if ($Frontend -or -not (Test-Path $bundle)) {
    Push-Location $tsDir
    try {
        Invoke-Checked 'Installing npm packages' { npm ci --no-audit --no-fund }
        Invoke-Checked 'Building frontend' { npx gulp build }
    } finally {
        Pop-Location
    }
}

# go
Push-Location $repoRoot
try {
    Invoke-Checked 'Generating embedded assets' { go generate ./... }

    $version = 'dev-' + (Get-Date -Format 'yy.MM.dd-HH-mm')
    $out = Join-Path $appDir 'server-manager.exe'
    Invoke-Checked 'Building server-manager' {
        go build -o $out -ldflags "-X github.com/JustaPenguin/assetto-server-manager.BuildVersion=$version" ./cmd/server-manager
    }
} finally {
    Pop-Location
}

# local dev data (all gitignored)
$assettoDir = Join-Path $appDir 'assetto'
$configFile = Join-Path $appDir 'config.yml'
$dbFile = Join-Path $appDir 'server_manager.db'

if ($Reset) {
    Write-Host '==> Resetting local dev data' -ForegroundColor Cyan
    foreach ($p in $assettoDir, $configFile, $dbFile) {
        if (Test-Path $p) { Remove-Item -Recurse -Force $p }
    }
}

if (-not (Test-Path $configFile)) {
    Write-Host '==> Creating config.yml from dev/config.yml' -ForegroundColor Cyan
    Copy-Item (Join-Path $PSScriptRoot 'config.yml') $configFile
}

if (-not (Test-Path $assettoDir)) {
    Write-Host '==> Creating fake Assetto Corsa install from dev/assetto-seed' -ForegroundColor Cyan
    Copy-Item -Recurse (Join-Path $PSScriptRoot 'assetto-seed') $assettoDir
}

if ($NoRun) { return }

Write-Host '==> Starting Server Manager on http://localhost:8772 (login: admin / admin)' -ForegroundColor Green
Push-Location $appDir
try {
    $env:FILESYSTEM_HTML = 'true'
    $env:DEBUG = 'true'
    & $out
} finally {
    Pop-Location
}
