#!/bin/sh
#
# AniSync core embedding for macOS (GUI app).
#
# Called from the "Embed AniSync Core" shell-script build phase of the Runner
# target in Runner.xcodeproj. Runs after Flutter's own macos_assemble embed
# phase so the .app skeleton already exists.
#
# docs/ops-api.md §1.2 pins the macOS lookup order: <exe>/../Frameworks/ first,
# then the directory holding the executable. We embed into Frameworks/ so the
# .app is self-contained instead of depending on a file sitting next to it.
#
# The library itself is produced by scripts/build-core.ps1 (owned by t4) into
#   app/build/native/macos-<arch>/libanisync_core.dylib
# and the whole app/build tree is gitignored -- the dylib is a build product,
# never a committed file.
#
# Overrides:
#   ANISYNC_CORE_LIB=<abs path>        use this dylib instead of the default
#   ANISYNC_ALLOW_MISSING_CORE=1       warn instead of failing (UI-only work)
#
set -eu

FLUTTER_APP_FRAMEWORKS_DIR="${BUILT_PRODUCTS_DIR}/${FRAMEWORKS_FOLDER_PATH}"

# Map Xcode's arch names onto the directory names used under app/build/native/.
# ARCHS may hold several names (e.g. "arm64 x86_64"); take the first known one.
ARCH_DIR=""
for candidate in ${ARCHS:-} ${CURRENT_ARCH:-}; do
  case "${candidate}" in
    arm64)  ARCH_DIR="macos-arm64" ; break ;;
    x86_64) ARCH_DIR="macos-amd64" ; break ;;
  esac
done

DEFAULT_SRC="${SRCROOT}/../build/native/${ARCH_DIR}/libanisync_core.dylib"
SRC="${ANISYNC_CORE_LIB:-}"

if [ -n "${SRC}" ] && [ ! -f "${SRC}" ]; then
  echo "error: ANISYNC_CORE_LIB is set but does not exist: ${SRC}" >&2
  exit 1
fi

if [ -z "${SRC}" ]; then
  if [ -z "${ARCH_DIR}" ]; then
    echo "error: cannot map ARCHS='${ARCHS:-}' to a build/native/ directory." >&2
    echo "       Expected arm64 or x86_64. Set ANISYNC_CORE_LIB to override." >&2
    exit 1
  fi
  SRC="${DEFAULT_SRC}"
fi

if [ ! -f "${SRC}" ]; then
  echo "error: libanisync_core.dylib not found. Looked in:" >&2
  echo "         ${DEFAULT_SRC}" >&2
  echo "       ANISYNC_CORE_LIB is ${ANISYNC_CORE_LIB:-<not set>}" >&2
  echo "       Build the engine first: scripts/build-core.ps1 -OS macos -Arch <arch> (docs/build.md)." >&2
  if [ "${ANISYNC_ALLOW_MISSING_CORE:-0}" = "1" ]; then
    echo "warning: ANISYNC_ALLOW_MISSING_CORE=1 -- continuing without the engine." >&2
    exit 0
  fi
  exit 1
fi

mkdir -p "${FLUTTER_APP_FRAMEWORKS_DIR}"
cp -f "${SRC}" "${FLUTTER_APP_FRAMEWORKS_DIR}/libanisync_core.dylib"
echo "AniSync: embedded libanisync_core.dylib from ${SRC}"

# The dylib has to carry the same signature as the app, otherwise library
# validation rejects it at load time and the app dies on first FFI call. With
# no signing identity (plain local builds) there is nothing to re-sign against,
# so skip rather than fail.
if [ -n "${EXPANDED_CODE_SIGN_IDENTITY:-}" ] && [ "${CODE_SIGNING_REQUIRED:-NO}" != "NO" ]; then
  codesign --force --verbose --sign "${EXPANDED_CODE_SIGN_IDENTITY}" \
    "${FLUTTER_APP_FRAMEWORKS_DIR}/libanisync_core.dylib"
fi
