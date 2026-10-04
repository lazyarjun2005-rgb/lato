# install.ps1 — prebuilt binary installer for Lato (Windows).
#
# Downloads the official native lato.exe for this architecture from the
# pinned GitHub release, verifies its SHA-256 checksum, and installs it
# to %LOCALAPPDATA%\Programs\Lato\lato.exe (per-user, no administrator
# rights). The user PATH is updated only if the directory is missing.
#
# Usage:
#   irm <installer URL> | iex
#   .\scripts\install.ps1
#
# The script never executes the downloaded binary during installation.

$ErrorActionPreference = 'Stop'

$Version = 'v1.2.1'
$Repo = 'lazyarjun2005-rgb/lato'
$BaseUrl = "https://github.com/$Repo/releases/download/$Version"
$ChecksumsUrl = "$BaseUrl/checksums.txt"
$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\Lato'
$Target = Join-Path $InstallDir 'lato.exe'

# --- Architecture detection ----------------------------------------------

# A 32-bit PowerShell process on 64-bit Windows reports its native arch in
# PROCESSOR_ARCHITEW6432; prefer that when present.
$procArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }

$Arch = switch ($procArch) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default {
        Write-Error "unsupported architecture: $procArch. Download manually from https://github.com/$Repo/releases/tag/$Version"
    }
}

$Asset = "lato-windows-$Arch.exe"
$AssetUrl = "$BaseUrl/$Asset"

Write-Host "Installing Lato $Version (windows-$Arch)..."

if (Test-Path $Target) {
    Write-Host "Replacing existing Lato installation at $Target"
}

# --- Download --------------------------------------------------------------

$tempFile = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
$checksumsFile = $null

try {
    Write-Host "Downloading $AssetUrl"
    Invoke-WebRequest -Uri $AssetUrl -OutFile $tempFile -UseBasicParsing

    # --- Checksum verification ------------------------------------------

    Write-Host 'Verifying SHA-256 checksum...'

    $checksumsFile = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $checksumsFile -UseBasicParsing

    $expected = $null
    foreach ($line in Get-Content $checksumsFile) {
        $parts = $line -split '\s+', 2
        if ($parts.Count -eq 2 -and $parts[1].Trim() -eq $Asset) {
            $expected = $parts[0].Trim().ToLowerInvariant()
            break
        }
    }
    if (-not $expected) {
        Write-Error "no checksum found for $Asset in checksums.txt"
    }

    $actual = (Get-FileHash -Path $tempFile -Algorithm SHA256).Hash.ToLowerInvariant()

    if ($actual -ne $expected) {
        Write-Error "checksum mismatch for $Asset`nExpected: $expected`nActual:   $actual`nThe download may be corrupted or tampered with. Aborting."
    }

    Write-Host 'Checksum OK'
}
finally {
    if ($checksumsFile -and (Test-Path $checksumsFile)) { Remove-Item $checksumsFile -Force -ErrorAction SilentlyContinue }
}

# --- Install ---------------------------------------------------------------

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

try {
    if (Test-Path $Target) {
        Remove-Item $Target -Force
    }
    Move-Item -Path $tempFile -Destination $Target
}
catch {
    if (Test-Path $tempFile) { Remove-Item $tempFile -Force -ErrorAction SilentlyContinue }
    throw
}

Write-Host ""
Write-Host "Installed: $Target"

# --- PATH --------------------------------------------------------------------

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')

if ($userPath -split ';' -notcontains $InstallDir) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$InstallDir", 'User')
    Write-Host "Added $InstallDir to your user PATH."
    Write-Host "Open a new terminal for the change to take effect."
}
else {
    Write-Host "$InstallDir is already on your user PATH."
}

# Also make it available in this session, if possible.
if ($env:PATH -split ';' -notcontains $InstallDir) {
    $env:PATH = "$env:PATH;$InstallDir"
}

Write-Host ""
Write-Host "Verify with: lato --version   (should print: lato $Version)"
Write-Host "Then start it from any project directory: cd ~\some-project; lato"
