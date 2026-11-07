# Build the AniSync Go engine as a c-shared dynamic library for ONE target.
#
#   pwsh scripts/build-core.ps1 -OS <windows|macos|linux|android> -Arch <amd64|arm64> [-Ndk <path>]
#
# Output (pinned by the contract comment in .github/workflows/app.yml):
#
#   app/build/native/<os>-<arch>/<library>
#     windows -> anisync_core.dll
#     macos   -> libanisync_core.dylib
#     linux   -> libanisync_core.so
#     android -> libanisync_core.so
#
# The <os> in that path is the CONTRACT name, always "macos" -- never the Go name
# "darwin". The macos -> darwin mapping happens below and nowhere else.
#
# Exit code 0 only when the library really exists and is non-empty. A missing
# NDK, a non-macOS host or a missing cross gcc is a hard failure with the list of
# places that were tried -- never a silent skip and never a placeholder. A green
# build that produced no artifact is worse than a red one.
#
# ASCII only, comments included: Windows PowerShell 5.1 reads a BOM-less UTF-8
# file as ANSI and can swallow the line AFTER a non-ASCII one, with no error at
# all. scripts/pack/scripts_test.go guards this.
#
# Two traps this script is written around:
#   1. GOOS/GOARCH are PROCESS-WIDE. Everything that must run on THIS machine
#      (tool lookups, the go tool itself) happens before they are set, and they
#      are restored in the finally block. Do not add a `go run` in here: it would
#      cross-compile that tool into the target platform and then fail to execute.
#   2. On darwin, cmd/go passes -arch on its own (cmd/go/internal/work/exec.go,
#      gccArchArgs). Do not append -arch to CC: a second -arch can override the
#      one Go chose and silently emit the wrong architecture for the same
#      "successful" build.
#
# Optional environment overrides (machine-local facts, kept out of the repo):
#   ANISYNC_GO          full path to the go executable, when it is not on PATH
#   ANISYNC_W64DEVKIT   root of a w64devkit/mingw tree used for windows targets
#   CC                  honoured first for every target, exactly like go itself

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('windows', 'macos', 'linux', 'android')]
    [string]$OS,

    [Parameter(Mandatory = $true)]
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch,

    [string]$Ndk = ''
)

$ErrorActionPreference = 'Stop'

# ValidateSet is case-insensitive, but the output path is not: normalize so that
# -OS Windows still lands in app/build/native/windows-amd64, which is what CI
# looks for verbatim.
$OS = $OS.ToLowerInvariant()
$Arch = $Arch.ToLowerInvariant()

$repoRoot = Split-Path -Parent $PSScriptRoot
$outDir = [IO.Path]::Combine($repoRoot, 'app', 'build', 'native', "$OS-$Arch")

# The library name per docs/ops-api.md 1.2.
$libName = switch ($OS) {
    'windows' { 'anisync_core.dll' }
    'macos' { 'libanisync_core.dylib' }
    default { 'libanisync_core.so' }
}

# The only place the Go platform name is derived from the contract name.
$goos = if ($OS -eq 'macos') { 'darwin' } else { $OS }

# Android API level for the clang wrapper. 24 is minSdkVersion in
# app/android/app/build.gradle.kts: the .so must not require a newer runtime.
$androidApi = 24

function Get-HostName {
    # $IsWindows / $IsLinux / $IsMacOS only exist in PowerShell 6+. Windows
    # PowerShell 5.1 runs on Windows only, so that fallback is exact, not a guess.
    if (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue) {
        if ($IsWindows) { return 'windows' }
        if ($IsMacOS) { return 'macos' }
        if ($IsLinux) { return 'linux' }
    }
    return 'windows'
}

function Resolve-GoExe {
    if ($env:ANISYNC_GO) {
        if (-not (Test-Path -LiteralPath $env:ANISYNC_GO)) {
            throw "ANISYNC_GO points at '$env:ANISYNC_GO', which does not exist."
        }
        return (Resolve-Path -LiteralPath $env:ANISYNC_GO).Path
    }
    $cmd = Get-Command go -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if ($cmd) { return $cmd.Source }
    throw ("go was not found. Put the Go bin directory on PATH, or set ANISYNC_GO " +
        "to the full path of the go executable. (Looked for: 'go' on PATH.)")
}

function Get-MingwRoots {
    $roots = New-Object 'System.Collections.Generic.List[string]'
    if ($env:ANISYNC_W64DEVKIT) { $roots.Add($env:ANISYNC_W64DEVKIT) }
    foreach ($candidate in @(
            'C:\w64devkit',
            'C:\msys64\mingw64',
            'C:\mingw64',
            'C:\tools\mingw64',
            (Join-Path $env:ProgramFiles 'mingw64'),
            (Join-Path $env:ProgramData 'chocolatey\bin')
        )) {
        if ($candidate) { $roots.Add($candidate) }
    }
    return $roots
}

# Returns @{ Path = <cc executable>; Prepend = <dir to put on PATH, or ''> }.
function Resolve-CC {
    param([string]$HostName)

    $notes = New-Object 'System.Collections.Generic.List[string]'

    # An explicit CC always wins, for every target -- same precedence as go.
    if ($env:CC) {
        if (Test-Path -LiteralPath $env:CC) {
            return @{ Path = (Resolve-Path -LiteralPath $env:CC).Path; Prepend = (Split-Path -Parent (Resolve-Path -LiteralPath $env:CC).Path) }
        }
        $notes.Add("`$env:CC is set to '$env:CC', which does not exist")
    }
    else {
        $notes.Add('$env:CC is unset')
    }

    # macOS is a hard host requirement unless CC points at a real darwin cross
    # toolchain: the artifact is a Mach-O dylib linked against the Apple SDK.
    # Refuse up front rather than let a stray clang on PATH die inside the linker.
    if ($OS -eq 'macos' -and $HostName -ne 'macos') {
        $notes.Add("this host is $HostName, so neither the Xcode toolchain nor the macOS SDK is here")
        throw ((@(
                    "cannot compile macos/$Arch on a $HostName host.",
                    '  why: the artifact is a Mach-O .dylib linked against the Apple SDK, and only a',
                    '       macOS host with the Xcode command line tools (clang + SDK) can produce one.',
                    '       CI builds this target on macos-latest.',
                    '  looked for:'
                ) + @($notes | ForEach-Object { "    - $_" }) +
                @(
                    '  escape hatch: with a real darwin cross toolchain (osxcross and friends), point',
                    '       $env:CC at its clang and this script will use it. Nothing of the sort is',
                    '       present here, and no placeholder artifact is written.'
                )) -join "`n")
    }

    function Find-OnPath {
        param([string]$Name)
        $cmd = Get-Command $Name -CommandType Application -ErrorAction SilentlyContinue |
            Select-Object -First 1
        if ($cmd) { return $cmd.Source }
        return ''
    }

    function Find-InRoots {
        param([string[]]$Names)
        foreach ($root in (Get-MingwRoots)) {
            if (-not (Test-Path -LiteralPath $root)) { continue }
            foreach ($name in $Names) {
                foreach ($rel in @("bin\$name", $name)) {
                    $full = Join-Path $root $rel
                    if (Test-Path -LiteralPath $full) { return $full }
                }
            }
        }
        return ''
    }

    switch ("$HostName/$OS/$Arch") {
        'windows/windows/amd64' {
            foreach ($name in @('x86_64-w64-mingw32-gcc', 'gcc')) {
                $found = Find-OnPath $name
                if ($found) { return @{ Path = $found; Prepend = (Split-Path -Parent $found) } }
                $notes.Add("'$name' not on PATH")
            }
            foreach ($name in @('gcc.exe', 'x86_64-w64-mingw32-gcc.exe')) {
                $found = Find-InRoots @($name)
                if ($found) { return @{ Path = $found; Prepend = (Split-Path -Parent $found) } }
                $notes.Add("'$name' not found under: " + ((Get-MingwRoots) -join '; '))
            }
        }
        'windows/windows/arm64' {
            # The mingw gcc shipped by w64devkit is x86_64-w64-mingw32 ONLY. Using
            # it for arm64 would emit amd64 objects and the build would still look
            # like a success, so it is deliberately not a candidate here.
            $found = Find-OnPath 'aarch64-w64-mingw32-gcc'
            if ($found) { return @{ Path = $found; Prepend = (Split-Path -Parent $found) } }
            $notes.Add("'aarch64-w64-mingw32-gcc' not on PATH")
            $found = Find-InRoots @('aarch64-w64-mingw32-gcc.exe')
            if ($found) { return @{ Path = $found; Prepend = (Split-Path -Parent $found) } }
            $notes.Add("'aarch64-w64-mingw32-gcc.exe' not found under: " + ((Get-MingwRoots) -join '; '))
            $notes.Add('note: the x86_64-w64-mingw32 gcc of w64devkit cannot emit arm64 objects, so it is not used')
        }
        'linux/linux/amd64' {
            foreach ($name in @('cc', 'gcc')) {
                $found = Find-OnPath $name
                if ($found) { return @{ Path = $found; Prepend = '' } }
                $notes.Add("'$name' not on PATH")
            }
        }
        'linux/linux/arm64' {
            $found = Find-OnPath 'aarch64-linux-gnu-gcc'
            if ($found) { return @{ Path = $found; Prepend = '' } }
            $notes.Add("'aarch64-linux-gnu-gcc' not on PATH (cross toolchain)")
        }
        'macos/macos/amd64' {
            $found = Find-OnPath 'clang'
            if ($found) { return @{ Path = $found; Prepend = '' } }
            $notes.Add("'clang' not on PATH (comes with the Xcode command line tools)")
        }
        'macos/macos/arm64' {
            $found = Find-OnPath 'clang'
            if ($found) { return @{ Path = $found; Prepend = '' } }
            $notes.Add("'clang' not on PATH (comes with the Xcode command line tools)")
        }
    }

    # The host cannot build this target on its own. Before giving up, look for a
    # genuine cross gcc, so the message reports what was really tried.
    $crossNames = switch ("$OS/$Arch") {
        'linux/amd64' { @('x86_64-linux-gnu-gcc') }
        'linux/arm64' { @('aarch64-linux-gnu-gcc') }
        'windows/amd64' { @('x86_64-w64-mingw32-gcc') }
        'windows/arm64' { @('aarch64-w64-mingw32-gcc') }
        default { @() }
    }
    foreach ($name in $crossNames) {
        $found = Find-OnPath $name
        if ($found) { return @{ Path = $found; Prepend = (Split-Path -Parent $found) } }
        $notes.Add("'$name' not on PATH (the cross gcc for this target)")
    }

    $needs = switch ($OS) {
        'macos' {
            @(
                'clang from the Xcode command line tools on a macOS host.',
                'install or repair with: xcode-select --install'
            )
        }
        'windows' {
            @(
                "a mingw-w64 gcc that targets windows/$Arch.",
                'for windows/amd64: gcc, or x86_64-w64-mingw32-gcc',
                'for windows/arm64: aarch64-w64-mingw32-gcc (the w64devkit gcc is x86_64-mingw32 only, so it cannot emit arm64 objects and is deliberately not a candidate here)'
            )
        }
        'linux' {
            @(
                "a C compiler that targets linux/$Arch.",
                "on a linux/$Arch host the host cc/gcc is enough; from another host you need the matching <triple>-linux-gnu-gcc cross compiler."
            )
        }
        default { @('a C compiler for the requested target.') }
    }

    throw ((@(
                "cannot compile $OS/$Arch on this host: no usable C compiler was found.",
                "  host OS: $HostName",
                '  needs:'
            ) + @($needs | ForEach-Object { "    - $_" }) +
            @('  looked for:') +
            @($notes | ForEach-Object { "    - $_" }) +
            @('  fix: set CC to a working compiler, or build this target on its own host.')) -join "`n")
}

# Returns the NDK clang wrapper path, or throws with every location tried.
function Resolve-AndroidClang {
    param([string]$ExplicitNdk, [string]$HostName, [string]$GoArch)

    $notes = New-Object 'System.Collections.Generic.List[string]'
    $roots = New-Object 'System.Collections.Generic.List[object]'

    if ($ExplicitNdk) {
        $roots.Add([pscustomobject]@{ Source = "-Ndk '$ExplicitNdk'"; Path = $ExplicitNdk })
    }
    else {
        $notes.Add('-Ndk was not passed')
    }

    foreach ($name in @('ANDROID_NDK_HOME', 'ANDROID_NDK_LATEST_HOME', 'ANDROID_NDK_ROOT')) {
        $value = [Environment]::GetEnvironmentVariable($name)
        if ($value) {
            $roots.Add([pscustomobject]@{ Source = "`$env:$name"; Path = $value })
        }
        else {
            $notes.Add("`$env:$name is unset")
        }
    }

    foreach ($sdkName in @('ANDROID_HOME', 'ANDROID_SDK_ROOT')) {
        $sdk = [Environment]::GetEnvironmentVariable($sdkName)
        if (-not $sdk) {
            $notes.Add("`$env:$sdkName is unset, so $sdkName/ndk/<version> cannot be searched")
            continue
        }
        $ndkParent = Join-Path $sdk 'ndk'
        if (-not (Test-Path -LiteralPath $ndkParent)) {
            $notes.Add("$ndkParent does not exist")
            continue
        }
        $versions = @(Get-ChildItem -LiteralPath $ndkParent -Directory -ErrorAction SilentlyContinue |
                Sort-Object -Property Name -Descending)
        if ($versions.Count -eq 0) {
            $notes.Add("$ndkParent exists but holds no NDK version directory")
            continue
        }
        foreach ($version in $versions) {
            $roots.Add([pscustomobject]@{ Source = "$sdkName/ndk/$($version.Name)"; Path = $version.FullName })
        }
    }

    if ($roots.Count -eq 0) {
        throw (@(
                "Android NDK not found for android/$GoArch -- searched in this order:",
                '  -Ndk -> ANDROID_NDK_HOME -> ANDROID_NDK_LATEST_HOME -> ANDROID_NDK_ROOT -> ANDROID_HOME/ndk/<version> -> ANDROID_SDK_ROOT/ndk/<version>',
                '  results:'
            ) + @($notes | ForEach-Object { "    - $_" }) +
            @('  fix: install the NDK, or pass -Ndk <ndk root>.') -join "`n")
    }

    $triple = if ($GoArch -eq 'arm64') { 'aarch64-linux-android' } else { 'x86_64-linux-android' }
    # The clang wrapper name carries the API level: <triple><api>-clang.
    $wrapper = "$triple$androidApi-clang"
    $hostTags = switch ($HostName) {
        'windows' { @('windows-x86_64') }
        'macos' { @('darwin-arm64', 'darwin-x86_64') }
        default { @('linux-x86_64') }
    }
    $extensions = if ($HostName -eq 'windows') { @('.cmd', '') } else { @('') }

    $tried = New-Object 'System.Collections.Generic.List[string]'
    foreach ($root in $roots) {
        if (-not (Test-Path -LiteralPath $root.Path)) {
            $tried.Add("$($root.Source) = '$($root.Path)' -> not a directory")
            continue
        }
        foreach ($tag in $hostTags) {
            $binDir = Join-Path $root.Path "toolchains/llvm/prebuilt/$tag/bin"
            if (-not (Test-Path -LiteralPath $binDir)) {
                $tried.Add("$($root.Source) -> missing prebuilt host directory: $binDir")
                continue
            }
            foreach ($ext in $extensions) {
                $full = Join-Path $binDir "$wrapper$ext"
                if (Test-Path -LiteralPath $full) { return $full }
            }
            $tried.Add("$($root.Source) -> $binDir has no '$wrapper'")
        }
    }

    throw (@(
            "no usable Android NDK clang wrapper for android/$GoArch.",
            "  needs: $wrapper  (API $androidApi, matching minSdkVersion in app/android/app/build.gradle.kts)",
            '  searched in this order:',
            '  -Ndk -> ANDROID_NDK_HOME -> ANDROID_NDK_LATEST_HOME -> ANDROID_NDK_ROOT -> ANDROID_HOME/ndk/<version> -> ANDROID_SDK_ROOT/ndk/<version>',
            '  results:'
        ) + @($notes | ForEach-Object { "    - $_" }) +
        @($tried | ForEach-Object { "    - $_" }) +
        @('  fix: install a complete NDK (with toolchains/llvm/prebuilt), or pass -Ndk <ndk root>.') -join "`n")
}

function Save-Env {
    $names = @('GOOS', 'GOARCH', 'CGO_ENABLED', 'CC', 'CXX', 'PATH')
    $saved = @{}
    foreach ($name in $names) { $saved[$name] = [Environment]::GetEnvironmentVariable($name) }
    return $saved
}

function Restore-Env {
    param([hashtable]$Saved)
    foreach ($name in $Saved.Keys) {
        [Environment]::SetEnvironmentVariable($name, $Saved[$name])
    }
}

$hostName = Get-HostName
$exitCode = 0
$saved = $null

Push-Location $repoRoot
try {
    $goExe = Resolve-GoExe

    if ($OS -eq 'android') {
        $ccPath = Resolve-AndroidClang -ExplicitNdk $Ndk -HostName $hostName -GoArch $Arch
        $ccPrepend = ''
    }
    else {
        $cc = Resolve-CC -HostName $hostName
        $ccPath = $cc.Path
        $ccPrepend = $cc.Prepend
    }

    $versionFile = Join-Path $repoRoot 'VERSION'
    $version = if (Test-Path -LiteralPath $versionFile) {
        (Get-Content -LiteralPath $versionFile -Raw).Trim()
    }
    else {
        '0.0.0'
    }

    New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    $target = Join-Path $outDir $libName
    # cgo writes the generated header next to -o, named after its base name.
    $header = Join-Path $outDir ([IO.Path]::GetFileNameWithoutExtension($libName) + '.h')

    # Delete BOTH outputs before building. A stale library from an earlier run
    # must never be mistaken for this run's output, and a failed build must not
    # leave something that reads like a half-success: that was observed for real
    # while another task was mid-edit, leaving an orphan header and no library.
    foreach ($stale in @($target, $header)) {
        if (Test-Path -LiteralPath $stale) { Remove-Item -LiteralPath $stale -Force }
    }

    Write-Host "core: $OS/$Arch  (GOOS=$goos GOARCH=$Arch CGO_ENABLED=1)"

    $saved = Save-Env
    [Environment]::SetEnvironmentVariable('GOOS', $goos)
    [Environment]::SetEnvironmentVariable('GOARCH', $Arch)
    [Environment]::SetEnvironmentVariable('CGO_ENABLED', '1')
    [Environment]::SetEnvironmentVariable('CC', $ccPath)
    if ($ccPrepend) {
        $env:PATH = $ccPrepend + [IO.Path]::PathSeparator + $env:PATH
    }

    Write-Host "core: CC=$ccPath"

    $sw = [Diagnostics.Stopwatch]::StartNew()
    # -buildmode=c-shared is what produces the dynamic library plus the generated
    # C header. A non-zero exit is checked explicitly: with $ErrorActionPreference
    # = 'Stop' a native command's exit code still does not throw.
    & $goExe build -buildmode=c-shared -trimpath `
        -ldflags "-s -w -X main.version=$version" `
        -o $target ./cmd/anisync-core
    $buildExit = $LASTEXITCODE
    $sw.Stop()

    if ($buildExit -ne 0) {
        throw "go build failed for $OS/$Arch with exit code $buildExit."
    }

    # A "successful" build that produced nothing is the failure mode this whole
    # script is written against, so verify the artifact instead of trusting it.
    if (-not (Test-Path -LiteralPath $target)) {
        throw "go build reported success but $target does not exist."
    }
    $length = (Get-Item -LiteralPath $target).Length
    if ($length -le 0) {
        throw "go build reported success but $target is empty (0 bytes)."
    }

    Write-Host ("core: OK  {0}/{1}  {2}  {3} bytes  {4:N1}s" -f $OS, $Arch, $libName, $length, $sw.Elapsed.TotalSeconds)
    Write-Host "core: out $target"
}
catch {
    Write-Host ""
    Write-Host "ERROR: $($_.Exception.Message)"
    $exitCode = 1
}
finally {
    if ($saved) { Restore-Env -Saved $saved }
    Pop-Location
}

exit $exitCode
