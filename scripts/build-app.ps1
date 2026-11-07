# Build the AniSync Flutter GUI for one target.
#
#   pwsh scripts/build-app.ps1 -OS <windows|macos|android> [-Arch <amd64|arm64>] [-SkipCore]
#
# Step order is the whole point of this script:
#   1. scripts/build-core.ps1 -OS <os> -Arch <arch>    the Go c-shared engine
#   2. app/ : flutter build <windows|macos|apk> --release --target-platform <...>
#
# The engine library must exist BEFORE the app build. app/windows/CMakeLists.txt
# copies anisync_core.dll next to the exe in POST_BUILD, and app/macos/embed_core.sh
# embeds libanisync_core.dylib into Contents/Frameworks/. Build the app first and
# you get a bundle that cannot load its own engine -- which only shows up at the
# first FFI call, on the user's machine.
#
# ASCII only, comments included: Windows PowerShell 5.1 reads a BOM-less UTF-8
# file as ANSI and can swallow the line AFTER a non-ASCII one, with no error.
# scripts/pack/scripts_test.go guards this.
#
# The artifact paths below are derived from the Flutter tool source, not guessed:
#   build_info.dart getWindowsBuildDirectory() -> build/windows/<simpleName> and
#   the simpleName of windows-x64 is "x64" (build_info.dart, simpleName switch);
#   the exe name comes from set(BINARY_NAME ...) in app/windows/CMakeLists.txt;
#   getMacOSBuildDirectory() -> build/macos, then Build/Products/Release;
#   android getApkDirectory() -> build/app/outputs/flutter-apk, and without
#   --split-per-abi the file is app-release.apk (gradle.dart _apkFilesFor).
#
# This machine has no MSVC / JDK / Android SDK / macOS, so only argument handling
# and the diagnostics are verifiable locally. The real builds are the CI matrix's
# job (t18). Nothing here claims to have been built.
#
# Environment overrides (machine-local facts, never hard-coded paths):
#   ANISYNC_FLUTTER   full path to flutter (or flutter.bat) when it is not on PATH

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('windows', 'macos', 'android')]
    [string]$OS,

    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',

    [switch]$SkipCore
)

$ErrorActionPreference = 'Stop'
$OS = $OS.ToLowerInvariant()
$Arch = $Arch.ToLowerInvariant()

$repoRoot = Split-Path -Parent $PSScriptRoot
$appDir = [IO.Path]::Combine($repoRoot, 'app')

# Only windows/amd64 has a contracted GUI release name
# (AniSync-GUI-<version>-windows-x64.zip), so refuse the rest instead of emitting
# an artifact nobody agreed to publish.
if ($OS -eq 'windows' -and $Arch -ne 'amd64') {
    throw ("windows/$Arch is not a contracted GUI target: the release name is " +
        "AniSync-GUI-<version>-windows-x64.zip. Use -Arch amd64, or add the " +
        "windows-$Arch artifact to docs/build.md first.")
}

function Get-FlutterExe {
    if ($env:ANISYNC_FLUTTER) {
        if (-not (Test-Path -LiteralPath $env:ANISYNC_FLUTTER)) {
            throw "ANISYNC_FLUTTER points at '$env:ANISYNC_FLUTTER', which does not exist."
        }
        return (Resolve-Path -LiteralPath $env:ANISYNC_FLUTTER).Path
    }
    foreach ($name in @('flutter', 'flutter.bat')) {
        $cmd = Get-Command $name -CommandType Application -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($cmd) { return $cmd.Source }
    }
    throw ("flutter was not found. Put the Flutter bin directory on PATH, or set " +
        "ANISYNC_FLUTTER to the full path of flutter/flutter.bat. " +
        "(Looked for: 'flutter' and 'flutter.bat' on PATH.)")
}

# Exact-name check, never a wildcard: a wrong name must fail loudly here rather
# than be silently tolerated (docs/build.md).
function Assert-Artifact {
    param([string]$Path, [string]$What)
    if (Test-Path -LiteralPath $Path) { return }
    $parent = Split-Path -Parent $Path
    $listing = if (Test-Path -LiteralPath $parent) {
        (Get-ChildItem -LiteralPath $parent -Force | ForEach-Object { $_.Name }) -join ', '
    }
    else {
        "the directory does not exist"
    }
    throw ("$What was not produced at the expected path:`n  $Path`n" +
        "  actual contents of $(Split-Path -Parent $Path): $listing`n" +
        "  The name is a contract (docs/build.md). Do not widen this to a wildcard.")
}

$flutter = Get-FlutterExe
$version = (Get-Content -LiteralPath (Join-Path $repoRoot 'VERSION') -Raw).Trim()

switch ($OS) {
    'windows' {
        $flutterArgs = @('build', 'windows', '--release', '--target-platform', 'windows-x64')
        $artifact = [IO.Path]::Combine($appDir, 'build', 'windows', 'x64', 'runner', 'Release', 'AniSync.exe')
        $what = 'AniSync.exe'
    }
    'macos' {
        $darwinArch = if ($Arch -eq 'arm64') { 'darwin-arm64' } else { 'darwin-x64' }
        $flutterArgs = @('build', 'macos', '--release', '--target-platform', $darwinArch)
        $artifact = [IO.Path]::Combine($appDir, 'build', 'macos', 'Build', 'Products', 'Release', 'AniSync.app')
        $what = 'AniSync.app'
    }
    'android' {
        $androidArch = if ($Arch -eq 'arm64') { 'android-arm64' } else { 'android-x64' }
        $flutterArgs = @('build', 'apk', '--release', '--target-platform', $androidArch)
        $artifact = [IO.Path]::Combine($appDir, 'build', 'app', 'outputs', 'flutter-apk', 'app-release.apk')
        $what = 'app-release.apk'
    }
}

Write-Host "app: $OS/$Arch  version $version"
Write-Host "app: flutter $flutter"

$exitCode = 0
try {
    if (-not $SkipCore) {
        $coreScript = Join-Path $PSScriptRoot 'build-core.ps1'
        if (-not (Test-Path -LiteralPath $coreScript)) {
            throw "missing $coreScript (the engine build script)"
        }
        Write-Host "app: engine first -- scripts/build-core.ps1 -OS $OS -Arch $Arch"
        # Invoked in this process. build-core.ps1 ends with `exit`, which exits
        # only that script and leaves the exit code in $LASTEXITCODE (verified),
        # so no child interpreter and no -ExecutionPolicy juggling is needed.
        & $coreScript -OS $OS -Arch $Arch
        if ($LASTEXITCODE -ne 0) {
            throw "scripts/build-core.ps1 failed for $OS/$Arch with exit code $LASTEXITCODE"
        }
    }
    else {
        Write-Host "app: -SkipCore given; assuming the engine library is already in place"
    }

    Push-Location $appDir
    try {
        Write-Host ("app: flutter " + ($flutterArgs -join ' '))
        & $flutter @flutterArgs
        if ($LASTEXITCODE -ne 0) {
            throw "flutter $($flutterArgs -join ' ') failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        Pop-Location
    }

    Assert-Artifact -Path $artifact -What $what
    $item = Get-Item -LiteralPath $artifact
    Write-Host ("app: OK  {0}/{1}  {2}" -f $OS, $Arch, $artifact)
    if ($item.PSIsContainer) {
        Write-Host "app: bundle contents:"
        Get-ChildItem -LiteralPath $artifact -Recurse -File |
            ForEach-Object { Write-Host ("      " + $_.FullName.Substring($artifact.Length + 1)) }
    }
    else {
        Write-Host ("app: size {0} bytes" -f $item.Length)
    }
}
catch {
    Write-Host ""
    Write-Host "ERROR: $($_.Exception.Message)"
    $exitCode = 1
}

exit $exitCode
