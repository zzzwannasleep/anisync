# Cross-compile every target platform and pack each into a self-contained zip (run on Windows).
#   powershell -File ./scripts/build-all.ps1
#
# Kept ASCII-only on purpose: Windows PowerShell reads BOM-less UTF-8 files as ANSI.
# A stray non-ASCII comment does not fail loudly -- it silently mangles the NEXT line,
# which is how `$platform` below once ended up empty. There is a test guarding this.
$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
$packer = $null
try {
    New-Item -ItemType Directory -Force -Path dist | Out-Null

    $version = (Get-Content (Join-Path $root 'VERSION') -Raw).Trim()
    Write-Host "version $version"
    Write-Host ""

    # Build the packer up front, with host settings. The GOOS/GOARCH below are
    # process-wide, so a plain `go run ./scripts/pack` inside the loop would make Go
    # cross-compile the packer itself into the target platform's binary -- which then
    # cannot execute on this machine.
    $packer = Join-Path $root 'dist/pack-tool.exe'
    go build -o $packer ./scripts/pack

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
        # The zip name says "macos", matching what CI publishes.
        $platform = if ($target.os -eq 'darwin') { 'macos' } else { $target.os }

        $sw = [Diagnostics.Stopwatch]::StartNew()
        go build -trimpath -ldflags "-s -w -X main.version=$version" -o $out ./cmd/anisync
        $sw.Stop()

        $size = [math]::Round((Get-Item $out).Length / 1MB, 1)
        Write-Host ("{0,-16} OK  {1,5} MB  {2:N1}s" -f "$($target.os)/$($target.arch)", $size, $sw.Elapsed.TotalSeconds)

        # One zip per platform: unpacking it yields a self-contained AniSync/ folder.
        & $packer -version $version -os $platform -arch $target.arch -bin $out
    }

    Write-Host ""
    Write-Host "Artifacts in dist/: one zip per platform, each unpacking to a self-contained AniSync/ folder."
    Write-Host "NOTE: darwin artifacts built here are pure Go (no native menu bar icon),"
    Write-Host "      because the macOS menu bar requires cgo and must be built on macOS."
    Write-Host "      Use CI artifacts (AniSync-<version>-macos-*.zip) for the menu bar app."
}
finally {
    if ($packer -and (Test-Path $packer)) { Remove-Item $packer -Force }
    $env:GOOS = ''
    $env:GOARCH = ''
    $env:CGO_ENABLED = ''
    Pop-Location
}
