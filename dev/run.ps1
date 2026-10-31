<#
.SYNOPSIS
    Builds and starts ACPitwall locally for development, without a real Assetto Corsa server.

.DESCRIPTION
    - builds the frontend (TypeScript/SCSS) if it hasn't been built yet, or when -Frontend is given
    - generates the embedded asset files (esc) and builds the Go binary
    - on first run, copies dev/config.yml and the fake AC install (dev/assetto-seed) into cmd/acpitwall
      (twice: server a in assetto, server b in assetto-b, whose content folder is a junction to that of a)
    - starts ACPitwall with templates/static files served from disk (FILESYSTEM_HTML=true),
      so changes to views/ only need a browser refresh.

    Login: http://localhost:8772 with admin / admin

.PARAMETER Frontend
    Force a rebuild of the frontend (npm install + gulp build).

.PARAMETER Reset
    Delete the local dev data (both fake AC installs, database, config.yml) and start from scratch.

.PARAMETER NoRun
    Only build, don't start the server.

.PARAMETER Seed
    Fill the local database and fake install with test data (drivers, results, championships, custom races, a race
    weekend) via cmd/utils/dev-seed, after the build and before starting the server. Does nothing if the data is already
    seeded; use together with -Reset to start from a fresh database. The server must not be running.
#>
param(
    [switch]$Frontend,
    [switch]$Reset,
    [switch]$NoRun,
    [switch]$Seed
)

$ErrorActionPreference = 'Stop'

$repoRoot = Split-Path -Parent $PSScriptRoot
$appDir = Join-Path $repoRoot 'cmd/acpitwall'
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
    $out = Join-Path $appDir 'acpitwall.exe'
    Invoke-Checked 'Building acpitwall' {
        go build -o $out -ldflags "-X github.com/wernersbacher/ACPitwall.BuildVersion=$version" ./cmd/acpitwall
    }
} finally {
    Pop-Location
}

# local dev data (all gitignored)
$assettoDir = Join-Path $appDir 'assetto'
$configFile = Join-Path $appDir 'config.yml'
$assettoBDir = Join-Path $appDir 'assetto-b'
$dbFile = Join-Path $appDir 'server_manager.db'

if ($Reset) {
    Write-Host '==> Resetting local dev data' -ForegroundColor Cyan
    # the content folder of server b is a junction to that of server a: remove the link, not the shared files
    if (Test-Path (Join-Path $assettoBDir 'content')) { cmd /c rmdir (Join-Path $assettoBDir 'content') | Out-Null }
    foreach ($p in $assettoDir, $assettoBDir, $configFile, $dbFile) {
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

if (-not (Test-Path $assettoBDir)) {
    Write-Host '==> Creating fake install of server b (content shared with server a)' -ForegroundColor Cyan
    Copy-Item -Recurse (Join-Path $PSScriptRoot 'assetto-seed') $assettoBDir
    # the sample results belong to server a only (the same file names would be hidden as duplicates)
    Get-ChildItem (Join-Path $assettoBDir 'results') | Remove-Item -Recurse -Force
    # content is managed through the first server, so the second one uses the same folder
    Remove-Item -Recurse -Force (Join-Path $assettoBDir 'content')
    New-Item -ItemType Junction -Path (Join-Path $assettoBDir 'content') -Target (Join-Path $assettoDir 'content') | Out-Null
}

if ($Seed) {
    Push-Location $repoRoot
    try {
        Invoke-Checked 'Seeding dev data' { go run ./cmd/utils/dev-seed -dir $appDir }
    } finally {
        Pop-Location
    }
}

if ($NoRun) { return }

Write-Host '==> Starting ACPitwall on http://localhost:8772 (login: admin / admin)' -ForegroundColor Green
Push-Location $appDir
try {
    $env:FILESYSTEM_HTML = 'true'
    $env:DEBUG = 'true'
    & $out
} finally {
    Pop-Location
}
