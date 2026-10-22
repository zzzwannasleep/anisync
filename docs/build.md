# 从源码构建

要求 Go 1.24 或更高版本。

```bash
go build -o AniSync ./cmd/anisync
```

产物是单个静态二进制，无运行时依赖。运行期文件（`config.yaml`、`data/cache.json`）生成在二进制所在目录。

## 注入版本号

```bash
go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o AniSync ./cmd/anisync
```

## macOS 菜单栏版本

菜单栏图标由 Objective-C 实现（依赖 `getlantern/systray`），必须开启 cgo 且在 macOS 上构建：

```bash
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o AniSync ./cmd/anisync
bash scripts/package-macos.sh ./AniSync 0.1.0 arm64
```

`package-macos.sh` 会生成 `AniSync.app` 并打包为 `dist/AniSync-<版本>-macos-<架构>.app.zip`。

必须以 `.app` 形式分发：systray 不会设置 `NSApplicationActivationPolicy`，裸二进制启动会在 Dock 里多出一个图标；只有 `Info.plist` 里声明 `LSUIElement=true` 才是纯菜单栏应用。

## 交叉编译全部目标

```bash
bash scripts/build-all.sh          # macOS / Linux
```

```powershell
powershell -File ./scripts/build-all.ps1   # Windows
```

覆盖 windows / darwin / linux × amd64 / arm64 六个目标。在非 macOS 主机上，darwin 目标为纯 Go 构建，不含菜单栏图标；需要菜单栏版本请在 macOS 上构建，或使用 CI 产物。

## 构建时校验

CI 在 darwin 目标上额外执行 `otool -L` 检查二进制是否链接了 `Cocoa.framework`。编译通过不代表菜单栏代码真的编进去了，这一步是硬性门禁。
