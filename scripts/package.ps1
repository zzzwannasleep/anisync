# Assemble one GUI release artifact for AniSync and drop it in dist/.
#
#   pwsh scripts/package.ps1 -OS <windows|macos|android> [-Arch <amd64|arm64>] [-Version <v>]
#
# Output (dist/, same directory the CLI release uses):
#   AniSync-GUI-<version>-windows-x64.zip     top level AniSync-GUI/
#       AniSync.exe, anisync_core.dll, README.md
#   AniSync-GUI-<version>-macos-<arch>.zip    top level AniSync-GUI/
#       AniSync.app/ (with Contents/Frameworks/libanisync_core.dylib), README.md
#   AniSync-GUI-<version>-android-<abi>.apk  copied, NOT re-zipped
#
#   <arch> is amd64 or arm64 for macOS; the Windows name is pinned to x64 because
#   that is the Flutter -target-platform name for the only contracted Windows GUI
#   build. The android artifact is the APK itself: an APK already is a zip, and
#   wrapping it in another zip would only make users unpack twice.
#
#   Android's <abi> is the jniLibs directory name, NOT our -Arch spelling:
#     -Arch arm64 -> arm64-v8a      -Arch amd64 -> x86_64
#   Keeping those two literally equal is what makes "which engine is inside this
#   APK" readable from the file name. Every platform that builds more than one
#   architecture puts the architecture in the artifact name; android used to hide
#   it, which made two ABIs collide on one release name (docs/build.md).
#
# Rules this script exists to enforce (docs/build.md is the authority):
#   * gui artifacts are looked up by EXACT name. There is no *.exe / *.app
#     wildcard anywhere in here on purpose: a wildcard swallows the fact that the
#     artifact got renamed, and the user finds out when the download will not open.
#   * the top level directory is AniSync-GUI/, never AniSync/. Unpacking the GUI
#     and CLI zips into one parent otherwise merges both folders and silently
#     overwrites AniSync.exe with AniSync.exe.
#   * zipping goes through scripts/pack -gui, not Compress-Archive: the latter
#     writes no Unix permission bits, so a macOS .app zipped with it arrives
#     non-executable. See the comment at the top of scripts/pack/main.go.
#
# ASCII only, comments included (scripts/pack/scripts_test.go guards this).
#
# Environment overrides (machine-local facts, never hard-coded paths):
#   ANISYNC_GO        full path to go, when it is not on PATH
#   ANISYNC_ROOT      repository root, when this script is run from elsewhere

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('windows', 'macos', 'android')]
    [string]$OS,

    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',

    [string]$Version = ''
)

$ErrorActionPreference = 'Stop'
$OS = $OS.ToLowerInvariant()
$Arch = $Arch.ToLowerInvariant()

$repoRoot = if ($env:ANISYNC_ROOT) { $env:ANISYNC_ROOT } else { Split-Path -Parent $PSScriptRoot }
$appDir = [IO.Path]::Combine($repoRoot, 'app')
$distDir = [IO.Path]::Combine($repoRoot, 'dist')

if (-not $Version) {
    $versionFile = Join-Path $repoRoot 'VERSION'
    if (-not (Test-Path -LiteralPath $versionFile)) {
        throw "no -Version given and $versionFile does not exist"
    }
    $Version = (Get-Content -LiteralPath $versionFile -Raw).Trim()
}
if (-not $Version) { throw 'version is empty' }

if ($OS -eq 'windows' -and $Arch -ne 'amd64') {
    throw ("windows/$Arch is not a contracted GUI target: the release name is " +
        "AniSync-GUI-<version>-windows-x64.zip. Use -Arch amd64.")
}

function Resolve-Go {
    if ($env:ANISYNC_GO) {
        if (-not (Test-Path -LiteralPath $env:ANISYNC_GO)) {
            throw "ANISYNC_GO points at '$env:ANISYNC_GO', which does not exist."
        }
        return (Resolve-Path -LiteralPath $env:ANISYNC_GO).Path
    }
    $cmd = Get-Command go -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($cmd) { return $cmd.Source }
    throw ("go was not found, and scripts/pack is a Go program. Put the Go bin " +
        "directory on PATH, or set ANISYNC_GO. (Looked for: 'go' on PATH.)")
}

# Exact-name lookup with a diagnostic that shows what IS there. Never widens to a
# wildcard: see the header.
function Resolve-Exact {
    param([string]$Path, [string]$What)
    if (Test-Path -LiteralPath $Path) { return (Resolve-Path -LiteralPath $Path).Path }
    $parent = Split-Path -Parent $Path
    $listing = if (Test-Path -LiteralPath $parent) {
        (Get-ChildItem -LiteralPath $parent -Force | ForEach-Object { $_.Name }) -join ', '
    }
    else {
        'the directory does not exist'
    }
    throw ("$What not found at the exact expected path:`n  $Path`n" +
        "  actual contents of ${parent}: $listing`n" +
        "  Paths and names are a contract (docs/build.md). Do not widen this to a wildcard.")
}

$exitCode = 0
try {
    New-Item -ItemType Directory -Force -Path $distDir | Out-Null

    if ($OS -eq 'android') {
        # The release name carries the ABI, and it is the **jniLibs directory name**,
        # not our -Arch spelling: -Arch amd64 is jniLibs x86_64. Keeping the two
        # literally equal is what makes "which engine is inside this APK" readable
        # from the file name. Two ABIs used to collide on one name (docs/build.md).
        $abi = if ($Arch -eq 'arm64') { 'arm64-v8a' } else { 'x86_64' }
        $apk = Resolve-Exact ([IO.Path]::Combine($appDir, 'build', 'app', 'outputs', 'flutter-apk', 'app-release.apk')) 'the release APK'
        $target = Join-Path $distDir "AniSync-GUI-$Version-android-$abi.apk"
        if (Test-Path -LiteralPath $target) { Remove-Item -LiteralPath $target -Force }
        Copy-Item -LiteralPath $apk -Destination $target
        $length = (Get-Item -LiteralPath $target).Length
        if ($length -le 0) { throw "$target is empty" }
        Write-Host ("package: OK  android/{0} (abi {1})  {2} bytes  ->  {3}" -f $Arch, $abi, $length, $target)
        Write-Host "package: the APK is copied as-is; an APK already is a zip."
        exit 0
    }

    # Staging is rebuilt from scratch every run: a leftover file from an earlier
    # target must never end up inside this target's archive.
    $stageRoot = [IO.Path]::Combine($distDir, "gui-stage-$OS-$Arch")
    if (Test-Path -LiteralPath $stageRoot) { Remove-Item -LiteralPath $stageRoot -Recurse -Force }
    New-Item -ItemType Directory -Force -Path $stageRoot | Out-Null

    if ($OS -eq 'windows') {
        $relDir = [IO.Path]::Combine($appDir, 'build', 'windows', 'x64', 'runner', 'Release')
        $exe = Resolve-Exact ([IO.Path]::Combine($relDir, 'AniSync.exe')) 'AniSync.exe'
        # The engine library must sit NEXT TO the exe. Missing here means the app
        # would start and then fail on its first FFI call, so this is a hard stop.
        $dll = Resolve-Exact ([IO.Path]::Combine($relDir, 'anisync_core.dll')) 'anisync_core.dll (must sit next to AniSync.exe)'
        Copy-Item -LiteralPath $exe -Destination (Join-Path $stageRoot 'AniSync.exe')
        Copy-Item -LiteralPath $dll -Destination (Join-Path $stageRoot 'anisync_core.dll')
        $zipBase = "AniSync-GUI-$Version-windows-x64"
    }
    else {
        $bundle = Resolve-Exact ([IO.Path]::Combine($appDir, 'build', 'macos', 'Build', 'Products', 'Release', 'AniSync.app')) 'AniSync.app'
        # Bundle id and product name are a contract; a renamed or re-idd bundle is
        # a silent break, so check the two facts that carry user-visible meaning.
        $exeInside = Join-Path $bundle 'Contents/MacOS/AniSync'
        if (-not (Test-Path -LiteralPath $exeInside)) {
            throw ("$bundle does not contain Contents/MacOS/AniSync -- the product name " +
                "is not AniSync (see docs/build.md; the name lives in " +
                "app/macos/Runner/Configs/AppInfo.xcconfig).")
        }
        $dylib = Join-Path $bundle 'Contents/Frameworks/libanisync_core.dylib'
        if (-not (Test-Path -LiteralPath $dylib)) {
            throw ("$bundle has no Contents/Frameworks/libanisync_core.dylib. The app " +
                "would build and then fail to load its engine. Run " +
                "scripts/build-core.ps1 -OS macos -Arch $Arch before the app build.")
        }
        Copy-Item -LiteralPath $bundle -Destination (Join-Path $stageRoot 'AniSync.app') -Recurse
        $zipBase = "AniSync-GUI-$Version-macos-$Arch"
    }

    $readme = Resolve-Exact (Join-Path $repoRoot 'README.md') 'README.md'
    Copy-Item -LiteralPath $readme -Destination (Join-Path $stageRoot 'README.md')

    # Build the packer as a HOST binary. GOOS/GOARCH are process-wide: inheriting
    # a target value here would cross-compile the packer itself and then fail to
    # run it on this machine.
    $goExe = Resolve-Go
    $packer = [IO.Path]::Combine([IO.Path]::GetTempPath(), "anisync-pack-$PID.exe")
    $savedOs = $env:GOOS
    $savedArch = $env:GOARCH
    $savedCgo = $env:CGO_ENABLED
    Push-Location $repoRoot
    try {
        $env:GOOS = ''
        $env:GOARCH = ''
        $env:CGO_ENABLED = ''
        & $goExe build -o $packer ./scripts/pack
        if ($LASTEXITCODE -ne 0) { throw "go build ./scripts/pack failed with exit code $LASTEXITCODE" }
    }
    finally {
        $env:GOOS = $savedOs
        $env:GOARCH = $savedArch
        $env:CGO_ENABLED = $savedCgo
        Pop-Location
    }

    try {
        # -gui keeps the CLI layout out of this zip (no config.example.yaml, no
        # samples/) and -top pins the documented top level directory.
        & $packer -version $Version -os $OS -arch $Arch -gui -staging $stageRoot -top 'AniSync-GUI' -name $zipBase -out $distDir
        if ($LASTEXITCODE -ne 0) { throw "pack failed with exit code $LASTEXITCODE" }
    }
    finally {
        if (Test-Path -LiteralPath $packer) { Remove-Item -LiteralPath $packer -Force }
    }

    $zip = Join-Path $distDir "$zipBase.zip"
    if (-not (Test-Path -LiteralPath $zip)) { throw "pack reported success but $zip does not exist" }
    $length = (Get-Item -LiteralPath $zip).Length
    if ($length -le 0) { throw "pack reported success but $zip is empty" }

    Write-Host ("package: OK  {0}/{1}  {2}  {3} bytes  ->  {4}" -f $OS, $Arch, $zipBase, $length, $zip)
    Write-Host "package: top level directory is AniSync-GUI/ (never AniSync/)."
    Write-Host "package: staging kept at $stageRoot for inspection."
}
catch {
    Write-Host ""
    Write-Host "ERROR: $($_.Exception.Message)"
    $exitCode = 1
}

exit $exitCode
