#!/usr/bin/env bash
# 把 darwin 二进制打成 .app（菜单栏应用）。
#
# 为什么必须打 .app：macOS 上裸二进制启动会被当成普通 GUI 程序，
# 在 Dock 里多出一个图标；只有 Info.plist 里声明 LSUIElement=true
# 才是纯粹的菜单栏应用。systray 本身不会设置激活策略。
#
# 用法：package-macos.sh <二进制路径> <版本号> <架构>
set -euo pipefail

binary="${1:?用法: package-macos.sh <二进制路径> <版本号> <架构>}"
version="${2:-0.0.0}"
arch="${3:-$(uname -m)}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
app="$work/assm.app"
trap 'rm -rf "$work"' EXIT

mkdir -p "$app/Contents/MacOS"
cp "$binary" "$app/Contents/MacOS/assm"
chmod +x "$app/Contents/MacOS/assm"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>assm</string>
    <key>CFBundleDisplayName</key><string>assm</string>
    <key>CFBundleIdentifier</key><string>io.github.zzzwannasleep.assm</string>
    <key>CFBundleExecutable</key><string>assm</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleVersion</key><string>${version}</string>
    <key>CFBundleShortVersionString</key><string>${version}</string>
    <key>LSMinimumSystemVersion</key><string>11.0</string>
    <key>NSHighResolutionCapable</key><true/>
    <key>LSUIElement</key><true/>
</dict>
</plist>
PLIST

mkdir -p "$root/dist"
out="$root/dist/assm-${version}-macos-${arch}.app.zip"
rm -f "$out"
(cd "$work" && zip -qry "$out" assm.app)

echo "已生成 $out"
echo "提示：未签名的 .app 首次打开会被 Gatekeeper 拦截，右键「打开」或在终端执行："
echo "  xattr -dr com.apple.quarantine /Applications/assm.app"
