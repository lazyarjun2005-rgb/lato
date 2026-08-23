# install.ps1 — user-local global installation of Lato (Windows).
#
# Installs the `lato` executable so it can be started from any terminal
# and any project directory with plain `lato` - no absolute paths.
#
# The binary is built with Go into $GOBIN, or $GOPATH\bin, or
# $HOME\go\bin (the standard `go install` location). Set PREFIX to
# target another user-local directory instead:
#
#   .\scripts\install.ps1
#   $env:LATO_PREFIX = "$HOME\.local\bin"; .\scripts\install.ps1
#
# The script is idempotent: re-running it simply refreshes the binary.
# It never requires administrator rights and never edits your shell
# configuration; if PATH needs updating it prints the exact command.

$ErrorActionPreference = "Stop"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error "Go is not installed or not on PATH. Install Go from https://go.dev/dl/ and re-run."
}

$repo = Split-Path -Parent $PSScriptRoot
Set-Location $repo

if ($env:LATO_PREFIX) {
    $binDir = $env:LATO_PREFIX
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    Write-Host "Building lato into $binDir ..."
    go build -o (Join-Path $binDir "lato.exe") .
} else {
    Write-Host "Installing lato via go install . ..."
    go install .
    $goBin = go env GOBIN
    if ($goBin) {
        $binDir = $goBin
    } else {
        $gopath = (go env GOPATH).Split(";")[0]
        $binDir = Join-Path $gopath "bin"
    }
}

Write-Host "Installed: $(Join-Path $binDir 'lato.exe')"

$pathEntries = $env:PATH -split ";"
$onPath = $false
foreach ($entry in $pathEntries) {
    if ([string]::Equals([System.IO.Path]::GetFullPath($entry), [System.IO.Path]::GetFullPath($binDir), [System.StringComparison]::OrdinalIgnoreCase)) {
        $onPath = $true
        break
    }
}

if (-not $onPath) {
    Write-Host ""
    Write-Host "NOTE: $binDir is not on your PATH."
    Write-Host "To make 'lato' available in every terminal, run:"
    Write-Host ""
    Write-Host ("  setx PATH `"%PATH%;{0}`"" -f $binDir)
    Write-Host ""
    Write-Host "(then open a new terminal), or add the directory through"
    Write-Host "Settings > System > About > Advanced system settings > Environment Variables."
    Write-Host "This script deliberately does not change your PATH for you."
}

Write-Host ""
Write-Host "Verify with:"
Write-Host "  lato doctor"
Write-Host ""
Write-Host "Then, from any project directory:"
Write-Host "  cd ~\some-project; lato"
