#!/usr/bin/env bash
# 把 darwin 二进制组装成 .app（菜单栏应用）。**只组装，不打包** ——
# 打成 zip 交给 scripts/pack，这样各平台的发布物结构完全一致。
#
# 为什么必须打 .app：macOS 上裸二进制启动会被当成普通 GUI 程序，
# 在 Dock 里多出一个图标；只有 Info.plist 里声明 LSUIElement=true
# 才是纯粹的菜单栏应用。systray 本身不会设置激活策略。
#
# 用法：package-macos.sh <二进制路径> <版本号> <架构> [输出目录]
set -euo pipefail

binary="${1:?用法: package-macos.sh <二进制路径> <版本号> <架构> [输出目录]}"
version="${2:-0.0.0}"
arch="${3:-$(uname -m)}"
out_dir="${4:-dist}"

app="$out_dir/AniSync.app"
rm -rf "$app"
mkdir -p "$app/Contents/MacOS"
cp "$binary" "$app/Contents/MacOS/AniSync"
chmod +x "$app/Contents/MacOS/AniSync"

cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>AniSync</string>
    <key>CFBundleDisplayName</key><string>AniSync</string>
    <key>CFBundleIdentifier</key><string>io.github.zzzwannasleep.anisync</string>
    <key>CFBundleExecutable</key><string>AniSync</string>
    <key>CFBundlePackageType</key><string>APPL</string>
    <key>CFBundleVersion</key><string>${version}</string>
    <key>CFBundleShortVersionString</key><string>${version}</string>
    <key>LSMinimumSystemVersion</key><string>11.0</string>
    <key>NSHighResolutionCapable</key><true/>
    <key>LSUIElement</key><true/>
</dict>
</plist>
PLIST

# 变量后面跟非 ASCII（全角括号、中文标点）时必须加大括号：bash 的变量名一直读到
# 「第一个不是变量名字符的字节」，C locale 下 $app（ 会被当成读一个叫 "app（" 的变量，
# set -u 直接 unbound variable。scripts/pack 里有测试盯着这条。
echo "已生成 ${app}（${arch}，v${version}）"
