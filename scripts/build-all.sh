#!/usr/bin/env bash
# 交叉编译全部目标平台，并把每个平台打成一个自包含的 zip（macOS / Linux 上执行）。
# 在 macOS 上会为 darwin 目标打开 cgo 并额外产出 .app；在 Linux 上 darwin 目标为纯 Go（无菜单栏图标）。
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mkdir -p dist

host_os="$(uname -s)"
version="${VERSION:-$(tr -d '[:space:]' < VERSION)}"

build() {
  local os="$1" arch="$2"
  local suffix="" cgo="0"

  [[ "$os" == "windows" ]] && suffix=".exe"
  # darwin 的菜单栏图标是 Objective-C，必须 cgo，且只能在 macOS 上编译
  [[ "$os" == "darwin" && "$host_os" == "Darwin" ]] && cgo="1"

  local out="dist/AniSync-${os}-${arch}${suffix}"
  local start end size
  start=$(date +%s.%N)
  GOOS="$os" GOARCH="$arch" CGO_ENABLED="$cgo" \
    go build -trimpath -ldflags "-s -w -X main.version=${version}" -o "$out" ./cmd/anisync
  end=$(date +%s.%N)
  size=$(du -h "$out" | cut -f1)

  printf '%-16s OK  %5s  cgo=%s  %.1fs\n' "${os}/${arch}" "$size" "$cgo" "$(echo "$end - $start" | bc)"

  # 发布物一律是 zip：解压出来是一个 AniSync/ 文件夹
  local platform="$os"
  [[ "$os" == "darwin" ]] && platform="macos"

  if [[ "$os" == "darwin" && "$cgo" == "1" ]]; then
    bash scripts/package-macos.sh "$out" "$version" "$arch"
    go run ./scripts/pack -version "$version" -os "$platform" -arch "$arch" -app dist/AniSync.app
  else
    go run ./scripts/pack -version "$version" -os "$platform" -arch "$arch" -bin "$out"
  fi
}

for target in "windows amd64" "windows arm64" "darwin amd64" "darwin arm64" "linux amd64" "linux arm64"; do
  # shellcheck disable=SC2086
  build $target
done

echo
echo "产物在 dist/：每个平台一个 zip，解压出来是一个自包含的 AniSync/ 文件夹。"
if [[ "$host_os" != "Darwin" ]]; then
  echo "注意：本机不是 macOS，darwin 产物为纯 Go 构建，不含原生菜单栏图标。"
  echo "需要菜单栏版本请在 macOS 上执行本脚本，或使用 CI 产出的 AniSync-<版本>-macos-*.zip。"
fi
