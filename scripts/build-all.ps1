# Cross-compile every target platform (run on Windows).
#   powershell -File ./scripts/build-all.ps1
# Kept ASCII-only on purpose: Windows PowerShell reads BOM-less UTF-8 files as ANSI.
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    New-Item -ItemType Directory -Force -Path dist | Out-Null

    $version = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
    Write-Host "version $version"
    Write-Host ""

    $targets = @(
        @{ os = 'windows'; arch = 'amd64' },
        @{ os = 'windows'; arch = 'arm64' },
        @{ os = 'darwin';  arch = 'amd64' },
        @{ os = 'darwin';  arch = 'arm64' },
        @{ os = 'linux';   arch = 'amd64' },
        @{ os = 'linux';   arch = 'arm64' }
    )

    foreach ($target in $targets) {
        $env:GOOS = $target.os
        $env:GOARCH = $target.arch
        $env:CGO_ENABLED = '0'          # fully static, no external runtime
        $suffix = if ($target.os -eq 'windows') { '.exe' } else { '' }
        $out = "dist/AniSync-$($target.os)-$($target.arch)$suffix"

        $sw = [Diagnostics.Stopwatch]::StartNew()
        go build -trimpath -ldflags "-s -w -X main.version=$version" -o $out ./cmd/anisync
        $sw.Stop()

        $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-Host ("{0,-16} OK  {1,5} MB  {2:N1}s" -f "$($target.os)/$($target.arch)", $size, $sw.Elapsed.TotalSeconds)
    }

    Write-Host ""
    Write-Host "Artifacts in dist/."
    Write-Host "NOTE: darwin artifacts built here are pure Go (no native menu bar icon),"
    Write-Host "      because the macOS menu bar requires cgo and must be built on macOS."
    Write-Host "      Use CI artifacts (AniSync-<version>-macos-*.app.zip) for the menu bar app."
}
finally {
    $env:GOOS = ''
    $env:GOARCH = ''
    $env:CGO_ENABLED = ''
    Pop-Location
}
