#!/usr/bin/env bash
# 交叉编译全部目标平台（macOS / Linux 上执行）
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
mkdir -p dist

build() {
  local os="$1" arch="$2"
  local suffix=""
  [[ "$os" == "windows" ]] && suffix=".exe"
  local out="dist/assm-${os}-${arch}${suffix}"

  local start end
  start=$(date +%s.%N)
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o "$out" ./cmd/assm
  end=$(date +%s.%N)

  local size
  size=$(du -h "$out" | cut -f1)
  printf '%-16s OK  %5s  %.1fs\n' "${os}/${arch}" "$size" "$(echo "$end - $start" | bc)"
}

for target in "windows amd64" "windows arm64" "darwin amd64" "darwin arm64" "linux amd64" "linux arm64"; do
  # shellcheck disable=SC2086
  build $target
done

echo
echo "产物在 dist/；macOS 原生菜单栏图标需 cgo + Mac 构建，当前走 Web 设置页。"
