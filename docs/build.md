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

## 打发布包

**发布物一律是 zip**，解压出来是一个自包含的 `AniSync/` 文件夹。打包由 `scripts/pack` 负责：

```bash
go run ./scripts/pack -version 0.1.0 -os linux -arch amd64 -bin ./AniSync
# -> dist/AniSync-0.1.0-linux-amd64.zip
```

包里的内容：

```
AniSync/
  AniSync              程序本体（windows 是 AniSync.exe，macOS 是 AniSync.app）
  config.example.yaml
  README.md
  01-… 02-… 03-… 04-…  samples/ 里的文件摊平到根目录
  subs.yaml raws.yaml all.yaml deny.yaml
```

样例摊平而不是放进 `samples/` 子目录，是因为样例之间按相对路径互相引用 ——
01 引用 `raws.yaml` + `deny.yaml`；02 引用 `subs.yaml` + `deny.yaml`；03 三个都引用
（`primaryFiles: [raws.yaml, subs.yaml]`，raws 在前）；04 引用 `all.yaml` + `deny.yaml`。
摊平之后「把 03 复制成 config.yaml」这一步才直接可用。

用 Go 写而不是 `zip` / `Compress-Archive`，有两个原因：

- `archive/zip` 按规范给非 ASCII 文件名置 UTF-8 标志位（bit 11），而样例文件名是中文；
- 权限位由 `scripts/pack` 显式给，不读源文件系统 —— 否则在 Windows 上打出来的包
  解压到 Linux/macOS 会不可执行。

CI 里有一道硬性门禁：发布目录里除了 `.zip` 不许有别的东西。

## macOS 菜单栏版本

菜单栏图标由 Objective-C 实现（依赖 `getlantern/systray`），必须开启 cgo 且在 macOS 上构建：

```bash
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=0.1.0" -o AniSync ./cmd/anisync
bash scripts/package-macos.sh ./AniSync 0.1.0 arm64        # 只组装 dist/AniSync.app
go run ./scripts/pack -version 0.1.0 -os macos -arch arm64 -app dist/AniSync.app
```

`package-macos.sh` 只负责组装 `.app`，打成 zip 交给 `scripts/pack` —— 这样各平台的发布物结构完全一致。

必须以 `.app` 形式分发：systray 不会设置 `NSApplicationActivationPolicy`，裸二进制启动会在 Dock 里多出一个图标；只有 `Info.plist` 里声明 `LSUIElement=true` 才是纯菜单栏应用。

## 交叉编译全部目标

```bash
bash scripts/build-all.sh          # macOS / Linux
```

```powershell
powershell -File ./scripts/build-all.ps1   # Windows
```

覆盖 windows / darwin / linux × amd64 / arm64 六个目标，每个目标产出一个 zip。
在非 macOS 主机上，darwin 目标为纯 Go 构建，不含菜单栏图标；需要菜单栏版本请在 macOS 上构建，或使用 CI 产物。

## 构建时校验

CI 在 darwin 目标上额外执行 `otool -L` 检查二进制是否链接了 `Cocoa.framework`。编译通过不代表菜单栏代码真的编进去了，这一步是硬性门禁。
