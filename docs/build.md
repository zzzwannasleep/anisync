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
  01-… 02-… 03-… 04-… 05-… samples/ 里的文件摊平到根目录
  subs.yaml raws.yaml all.yaml deny.yaml
```

样例摊平而不是放进 `samples/` 子目录，是因为样例之间按相对路径互相引用 ——
01 引用 `raws.yaml` + `deny.yaml`；02 引用 `subs.yaml` + `deny.yaml`；03 三个都引用
（`primaryFiles: [raws.yaml, subs.yaml]`，raws 在前）；04 引用 `all.yaml` + `deny.yaml`；
05 四个都引用（两套方案合起来用到的）。
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

---

# 跨平台 GUI 应用（`app/`）

`app/` 是 Flutter 写的图形客户端，和上面那个 CLI 二进制是**两个独立产物**：CLI 仍是
`AniSync-<版本>-<平台>-<架构>.zip`，GUI 是 `AniSync-GUI-<版本>-<平台>-<架构>.zip`。
GUI 自己**不实现任何业务逻辑**：它通过 `dart:ffi` 调用 Go 编出来的 c-shared 动态库，
或者通过 HTTP 调远端 `AniSync serve` 节点。两条路共用同一份 op 契约（`docs/ops-api.md`）。

## 工具链版本

| 组件 | 版本 | 说明 |
| --- | --- | --- |
| Flutter | **3.47.9**（stable 通道） | 钉死。`app.yml` 里写的是精确版本，不是 `channel: stable` —— stable 会漂移，某天 CI 换了版本而本地没换，`flutter analyze` 红一片却查不出来 |
| Dart | 3.13.7 | 随 Flutter 3.47.9 |
| Go | **1.24** | 与 CLI 的 `build.yml` 一致，不许单独升级 |

国内网络下建议固定镜像源，否则拉 engine 零件会很慢：

```powershell
$env:FLUTTER_STORAGE_BASE_URL = 'https://storage.flutter-io.cn'
$env:PUB_HOSTED_URL           = 'https://pub.flutter-io.cn'
```

## 第一步：编 Go 引擎（c-shared 动态库）

GUI 用的库不是 `go build` 出来的可执行文件，而是 `-buildmode=c-shared` 的动态库：

```powershell
pwsh ./scripts/build-core.ps1 -OS <windows|macos|linux|android> -Arch <amd64|arm64> [-Ndk <ndk 根目录>]
```

产出固定落在 `app/build/native/<os>-<arch>/` 下。这个目录在 `.gitignore` 里
（`app/.gitignore` 有 `/build/`）——**动态库是构建产物，不进仓库**。

## 第二步：库放哪、叫什么（平台接线）

`docs/ops-api.md` §1.2 定义了运行期的查找位置，各平台的工程接线就是照着它落地的：

| 平台 | 库文件名 | 查找位置 | 工程里在哪接的 |
| --- | --- | --- | --- |
| Windows | `anisync_core.dll` | 与 GUI 可执行文件同目录 | `app/windows/CMakeLists.txt`：`POST_BUILD` 拷进构建树 + `install()` 拷进安装树 |
| macOS | `libanisync_core.dylib` | `<exe>/../Frameworks/`，回退到与可执行文件同目录 | `app/macos/embed_core.sh`，由 `Runner.xcodeproj` 的 “Embed AniSync Core” 阶段调用，落点是 `Contents/Frameworks/` |
| Android | `libanisync_core.so` | `android/app/src/main/jniLibs/<abi>/`，装进 APK 后由 `dlopen` 解析 | `app/android/app/build.gradle.kts` 声明 `jniLibs.srcDirs("src/main/jniLibs")`，AGP 负责打包 |

**产物名与 zip 顶层目录（打包的唯一权威口径）。**

| 平台 | GUI 产物 | 引擎库 |
| --- | --- | --- |
| Windows | `AniSync.exe` | `anisync_core.dll`（与 exe 同目录） |
| macOS | `AniSync.app` | `libanisync_core.dylib`（`Contents/Frameworks/`） |
| Android | APK，`applicationId = com.zzzwannasleep.anisync` | `libanisync_core.so`（`jniLibs/<abi>/`） |

打包脚本**必须按精确名查找**（`AniSync.exe` / `AniSync.app`），找不到就报错。
**禁止**写成 `*.exe` / `*.app` 通配去蒙——通配会把「名字被改错了」这件事**静默吞掉**，
等用户拿到一个打不开的包才发现。

**GUI 的 zip 顶层目录是 `AniSync-GUI/`，不是 `AniSync/`。** CLI 的 zip 解出来是
`AniSync/`；如果 GUI 也叫 `AniSync/`，用户把两个 zip 解到同一个父目录时 Windows 会
**合并**这两个文件夹，然后 `AniSync.exe` 同名互相覆盖——一次静默的数据丢失。
换个顶层目录名就断了这条路。

**macOS 的产物名是约定，不是实现细节。** `PRODUCT_NAME` 在
`app/macos/Runner/Configs/AppInfo.xcconfig` 里定成 `AniSync`，所以 GUI 的 bundle 是
`AniSync.app`——与 CLI 在 macOS 上产出的那个同名，也与 `README.md` 里写的一致。
改这个名字要同时改 `project.pbxproj` 里的 product 引用、`Runner.xcscheme` 的
`BuildableName`、以及 RunnerTests 的 `TEST_HOST`，**只改 xcconfig 会留下半拉子**。
打包时（`scripts/package.ps1` 及以后任何脚本）**必须按 `AniSync.app` 精确查找**，
找不到就报错；**不要**写成 `*.app` 通配去蒙——通配会把「名字被改错了」这件事静默吞掉，
等用户拿到一个打不开的包才发现。

**Windows 一定要两处都拷。** 只写 `POST_BUILD`，本地 `flutter run -d windows` 能跑，
但装出来的包缺 `anisync_core.dll`；只写 `install()` 则反过来。这正是
「本地跑得好、装出来找不到库」的成因，所以 `app/windows/CMakeLists.txt` 里两处都显式写了。
`ANISYNC_REQUIRE_CORE=ON` 可以在库缺失时直接把配置阶段打失败（CI 的发包 job 该开）。

**macOS 的 dylib 必须跟着 app 一起签。** `embed_core.sh` 在签名身份存在时会用同一个
`EXPANDED_CODE_SIGN_IDENTITY` 重新签一遍嵌入的 dylib；否则 library validation 会在
第一次 FFI 调用时拒绝加载，表现为应用一启动就崩。
Debug 构建则靠 `DebugProfile.entitlements` 里的
`com.apple.security.cs.disable-library-validation`（仅 debug，release 不放开）。

**Android 的 `applicationId` 是契约的一部分。** 它是 `com.zzzwannasleep.anisync`，
Go 侧据此推导默认工作区，配置与数据落在
`/data/data/com.zzzwannasleep.anisync/files/anisync/`（`docs/ops-api.md` §5 的 `core.init`
示例用的就是这个路径）。改它等于把每个用户的数据搬一次家，不要随手动。
`minSdk` 是 24（Android 7.0）。

## 第三步：编 GUI

```bash
cd app
flutter pub get
flutter analyze
flutter test          # 需要 app/test 里有测试；为空时返回 exit 1，这是刻意的硬门禁
flutter build windows --release
flutter build macos --release
flutter build apk --release
```

`flutter test` 不打包任何原生产物：它靠环境变量 `ANISYNC_CORE_LIB` 指向刚编出来的库，
跑真实的 FFI 回环。没设这个变量时，加载失败要抛出**能看懂的错误**（列出去找过的全部路径），
不许静默降级成假实现 —— 这一条写在 `docs/ops-api.md` §1.2。

**`app/build/native/**` 下的产物是派生物，它会随时过期。跑回环之前永远先用当前源码重编一次：**

```powershell
pwsh scripts/build-core.ps1 -OS windows -Arch amd64
$env:ANISYNC_CORE_LIB = (Resolve-Path app/build/native/windows-amd64/anisync_core.dll).Path
cd app; flutter test test/ffi/ffi_roundtrip_test.dart
```

**回环失败时，第一件事是比对库的 mtime 与源码的 mtime**，而不是先去怀疑契约或 Dart 侧。
这已经真实发生过一次：`app/build/native/windows-amd64/anisync_core.dll` 拿着一份
**已经修好的 use-after-free**（它的 mtime 早于事件字符串所有权修复），谁用它跑事件路径
都会 4/4 解码失败。那是个**已经不存在的缺陷**，拿陈旧产物去查只会把人往错的方向带。

**但 mtime 比对只是启发式，构建图才是事实。** 源码比库新，不等于库过期——先看那个文件
**在不在 c-shared 的构建图里**：

```powershell
$env:CGO_ENABLED='1'; go list -deps ./cmd/anisync-core | Select-String 'anisync/'
```

`CGO_ENABLED=1` 不能省：不带它时 `cmd/anisync-core` 选中的是 `main_nocgo.go` 那个
`//go:build !cgo` 占位文件，**它什么都不 import**，依赖图会缩到只剩 main 包本身——于是
任何包都「不在图里」，而这个假阴性看起来和真阴性一模一样。**所以跑这个检查时必须带阳性
对照**（`internal/bridge` 必须出现在列出的包里），否则你量的是另一个程序。

**不要试图在 Windows 上编 Windows 或 Android 产物**：`flutter build windows` 需要
Visual Studio 的 “Desktop development with C++” 工作负载（MSVC），`flutter build apk`
需要 JDK + Android SDK。这两样都不在开发机的必装清单里，产物归 CI（`.github/workflows/app.yml`）。
`flutter analyze` / `flutter test` 不需要它们，本地就能跑。

### 出包脚本与调用形状（唯一权威）

任何人手工出包、以及 CI 里跑 GUI 构建的作业矩阵，**一律按本节这两个脚本的形状调用**。
参数名与产物名以本节为唯一权威；脚本不改名，改名就要同步这一节。

```powershell
# 1) 编 GUI：内部先调 scripts/build-core.ps1 备好引擎库，再 flutter build
pwsh scripts/build-app.ps1 -OS <windows|macos|android> [-Arch <amd64|arm64>] [-SkipCore]

# 2) 出包：版本号默认读仓库根 VERSION
pwsh scripts/package.ps1  -OS <windows|macos|android> [-Arch <amd64|arm64>] [-Version <版本>]
```

| OS | `-Arch` | 传给 flutter 的 `--target-platform` | 精确产物路径 |
| --- | --- | --- | --- |
| windows | amd64（只此一个，其余报错） | `windows-x64` | `app/build/windows/x64/runner/Release/AniSync.exe` |
| macos | arm64 / amd64 | `darwin-arm64` / `darwin-x64` | `app/build/macos/Build/Products/Release/AniSync.app` |
| android | arm64 / amd64 | `android-arm64` / `android-x64` | `app/build/app/outputs/flutter-apk/app-release.apk` |

产物落 `dist/`（与 CLI 的 `build-all.ps1` 同一个目录，两边不重名）：

| 平台 | 文件名 | 顶层目录 | 内含 |
| --- | --- | --- | --- |
| Windows | `AniSync-GUI-<版本>-windows-x64.zip` | `AniSync-GUI/` | `AniSync.exe`、`anisync_core.dll`、`README.md` |
| macOS | `AniSync-GUI-<版本>-macos-<arch>.zip` | `AniSync-GUI/` | `AniSync.app/`（引擎在 `Contents/Frameworks/`）、`README.md` |
| Android | `AniSync-GUI-<版本>-android-<abi>.apk` | —— | `<abi>` 是 `arm64-v8a` 或 `x86_64`；APK 原样拷出，**不再套一层 zip** |

**Windows 的架构写作 `x64`，不是 CLI 的 `amd64`。** 这是 Flutter `-target-platform` 与构建
目录的叫法（`windows-x64` / `build/windows/x64`）。两者不同是有意的，别"统一"掉。
Android 出的是 `.apk` 而不是 zip：APK 本身就是 zip，再套一层只会让用户多解一次。

**三个平台只要不止一个架构，就把架构写进产物名。** Windows 只有一个（`x64`），macOS 两个
（`arm64` / `amd64`），Android 两个 ABI。Android 最初漏了这一步 —— 名字里没有架构，于是两个
ABI 的产物同名，「发布时挂哪个」变成一条必须记住的隐含知识。现在补齐。

**Android 用 ABI 名（`arm64-v8a` / `x86_64`），不是我们内部的 `-Arch` 名。** 这两个字符串要
和 `app/android/app/src/main/jniLibs/<abi>/` 的目录名**逐字一致**，这样「这个 APK 里的 `.so`
是从哪来的」读文件名就能对上。映射关系如下，**下一个人别以为名字打错了**：

| 脚本参数 | jniLibs 目录名 = 产物名里的 ABI | flutter `--target-platform` |
| --- | --- | --- |
| `-Arch arm64` | `arm64-v8a` | `android-arm64` |
| `-Arch amd64` | `x86_64` | `android-x64` |

所以**同一次构建里 `-Arch amd64` 产出的是 `…-android-x86_64.apk`**：不是笔误，ABI 名与内部
架构名本来就是两套叫法，而产物名要跟 ABI 走。

**发布：两个 ABI 各自一个产物，名字自带 ABI，两个都挂。** 没有理由只挂一个 —— 两个 ABI 的
确都构建、也都各自校验过（`lib/<abi>/libanisync_core.so` 真的在 APK 里）。下载页写清用途即可：
`arm64-v8a` 是**真机**那份（现在的手机几乎都是它），`x86_64` 是**模拟器**那份。

`build-app.ps1` 里两步的顺序不能颠倒：**引擎库必须在 GUI 构建之前存在**。
`app/windows/CMakeLists.txt` 在 `POST_BUILD` 阶段把 `anisync_core.dll` 拷到 exe 旁边，
`app/macos/embed_core.sh` 把 dylib 塞进 `Contents/Frameworks/`。反过来先编 GUI，
拿到的是一个**启动后第一次 FFI 调用才炸**的包 —— 本地看不出问题，用户那儿才现形。

zip 一律由 `scripts/pack -gui` 写，**不要**改用 `Compress-Archive`：后者不写 Unix 权限位，
打出来的 `AniSync.app` 到 macOS 上是不可执行的。GUI 模式的顶层目录名与
`Contents/MacOS/` 的可执行位有测试钉住（`scripts/pack/package_test.go`）。

## 为什么是 `c-shared` + `dart:ffi`，不是 gomobile

`gomobile bind` 只产 Android AAR 和 iOS XCFramework —— **桌面端没有对应物**。
而 AniSync 的 GUI 要覆盖 Windows / macOS / Android 三个平台，且要求「同一份绑定代码」。
`-buildmode=c-shared` 在三个平台都产出一个普通动态库，`dart:ffi` 在三个平台都用同一套
`DynamicLibrary.open` + 符号查找，于是 Dart 侧只有一份绑定层。

代价是**要自己管动态库的落位**（就是上面那张表），而 gomobile 会替你打包 —— 
但那份「替你打包」只对 Android/iOS 生效，换不来桌面端。这个交换是划算的。

## CI

`.github/workflows/app.yml` 管 GUI 这一摊，和管 CLI 发布的 `build.yml` 严格分开：

- `analyze-test`：`flutter pub get` → `flutter analyze` → `flutter test`，
  先编一份宿主平台（linux/amd64）的库给测试用。
- `core`：六格矩阵 —— windows amd64/arm64、macos arm64/amd64、android arm64/amd64，
  每格产出 `AniSync-GUI-core-<平台>-<架构>.zip`（**产物一律 zip**，与 CLI 一致）。

两件事容易踩：

1. **`flutter create` 没有 `--project-dir` 选项。** 实测返回 exit 64 /
   `Could not find an option named "--project-dir"`。要用工作目录定位
   （`working-directory:`），别把这个参数抄进脚本。
2. **`flutter test` 在 `app/test` 为空时返回 exit 1**（`Test directory "test" not found.`），
   因为 `flutter create --empty` 不生成测试目录。CI 里这一步是故意的硬门禁：
   GUI 契约测试没落地之前它就该红。

## 构建红了先查哪里

这类排查知识如果只留在对话里，下一轮就丢了。所以落成文档：**按顺序查，别跳。**

### macOS（`flutter build macos` / `xcodebuild test` 失败）

1. **`Runner.xcscheme` 的 `BuildableName`（头号嫌疑）** —— 5 处必须全是 `AniSync.app`
   （`PreActions/EnvironmentBuildable`、`BuildActionEntry`、`TestAction/MacroExpansion`、
   `LaunchAction`、`ProfileAction`）。漏一处 Xcode 就在 scheme 里找不到产物，直接构建失败。
   ```bash
   grep -n BuildableName app/macos/Runner.xcodeproj/xcshareddata/xcschemes/Runner.xcscheme
   ```
2. **`project.pbxproj` 里 product 的三处一致性** —— `PBXFileReference` 的
   `path = "AniSync.app"`、Products group 成员、`productReference` 的注释与 UUID
   必须互相对得上。
   ```bash
   grep -n 'AniSync.app' app/macos/Runner.xcodeproj/project.pbxproj
   ```
3. **RunnerTests 的 `TEST_HOST`** ——
   `"$(BUILT_PRODUCTS_DIR)/AniSync.app/$(BUNDLE_EXECUTABLE_FOLDER_PATH)/AniSync"`，
   包名与**包内可执行文件名两处**都得是 `AniSync`。`xcodebuild test` 红了先看这里。
4. **`PRODUCT_NAME` 是否被覆盖** —— `AppInfo.xcconfig` 必须仍被 `338D0CEA`(Profile) /
   `33CC10FC`(Debug) / `33CC10FD`(Release) 三个块引用，且这三个块里**不能**出现
   `PRODUCT_NAME`。pbxproj 里的设置优先于 xcconfig，一旦有人加了
   `PRODUCT_NAME = "$(TARGET_NAME)"`，产物会变成 `Runner.app`，第 3 步会先炸。
5. **`embed_core.sh` 的落点** —— 它用 `$FRAMEWORKS_FOLDER_PATH`，随 `PRODUCT_NAME`
   自动变。若报找不到 dylib，问题在 `app/build/native/macos-<arch>/` 里没有库，
   不是产物名的问题。

### Windows（`flutter build windows` 失败 / 产物名不对）

1. **`BINARY_NAME`** —— `app/windows/CMakeLists.txt` 里的 `set(BINARY_NAME "AniSync")`。
   它**是**产物名的唯一决定者：Flutter 工具链只认这一行
   （`packages/flutter_tools/lib/src/cmake.dart` 的 `getCmakeExecutableName()`，
   正则 `^\s*set\(BINARY_NAME\s*"(.*)"\s*\)\s*$`），**从不读 `project()`**。
   写法必须保持 `set(BINARY_NAME "名字")` 这一行形态，否则工具链解析不到。
2. **`runner/Runner.rc` 的四条字面量** —— `FileDescription` / `InternalName` /
   `ProductName` 应为 `AniSync`，`OriginalFilename` 应为 `AniSync.exe`。
   它们是**静态文本不是 CMake 变量**，改 `BINARY_NAME` 不会带着它们走，必须手改。
3. **两处拷贝路径** —— `app/windows/CMakeLists.txt` 里 `install(TARGETS ...)` 与
   `add_custom_command(TARGET ${BINARY_NAME} POST_BUILD ...)` 都必须是
   `${BINARY_NAME}` / `$<TARGET_FILE_DIR:${BINARY_NAME}>`，**不许出现硬编码的名字**。
   少一处就会出现「本地跑得好、装出来找不到库」。
4. **core dll 是否真的在产物目录** —— 构建完看
   `build/windows/x64/runner/<Debug|Release>/anisync_core.dll` 是否存在。
   没有的话先确认 `app/build/native/windows-<arch>/anisync_core.dll` 存在
   （`ANISYNC_REQUIRE_CORE=ON` 能让配置阶段直接失败，比事后发现好）。

## 明文 HTTP 实例的平台策略与我们的取舍

**问题**：AniSync 的全部功能都是「连到用户自己填的 ANI-RSS 实例」，而自建实例绝大多数是
局域网里的明文 http（`http://192.168.1.5:7777`）。Android 从 `targetSdk 28` 起
`usesCleartextTraffic` 默认 false；macOS 有 App Transport Security。
**如果这类请求真被拦，用户看到的不是「明文被拦」，而是「实例连不上」——一个毫无线索的报错。**

下面是核查结论。**先给事实，再给取舍和不确定性。**

### 事实：我们的出站请求走的是哪条路

| 客户端 | 实现 | 是否经过平台策略检查 |
| --- | --- | --- |
| FFI 内嵌后端 | Go `net/http`，cgo 编进 `libanisync_core` | **否**（见下） |
| 远端节点后端 | Dart `dart:io` 的 `HttpClient` | **否**（见下） |

**Go 侧**：`net` 包直接建裸 socket。`$(go env GOROOT)/src/net/sock_posix.go:18-19` 里
`socket()` 的第一步就是 `s := sysSocket(family, sotype, proto)`，而 `net/sys_cloexec.go:20-35`
的 `sysSocket` 是 `socket(2)` 系统调用的薄封装（`socketFunc` + `syscall.SetNonblock`）。
cgo 编成 c-shared 后这套 runtime 一起进 `.so`，行为不变。**Go 不经过 Java，也不经过 CFNetwork**。

**Dart 侧（Android）** 四处独立证据，全部指向「不检查」：
1. `dart-sdk/lib/io/**` 的 Dart 层源码里**没有**任何 `NetworkSecurityPolicy` /
   `isCleartextTrafficPermitted` / `usesCleartextTraffic` 逻辑。
2. 官方 Android 引擎二进制 `libflutter.so`（arm64，395,872,560 字节，从
   `bin/cache/artifacts/engine/android-arm64/flutter.jar` 取出）里，
   `isCleartextTrafficPermitted` / `CleartextTraffic` / `NetworkSecurityPolicy` /
   `usesCleartextTraffic` / `android.security` **全部搜不到**；而对照串
   `libflutter.so` / `FlutterJNI` / `DartVM` / `JNI_OnLoad` **全部搜得到**
   （证明扫描本身有效，不是全空导致的假阴性）。
3. 同一个 `flutter.jar` 里解出的 **448 个** Java 嵌入类，**没有任何一个**引用上述符号
   （对照：`FlutterJNI` 出现在 36 个类里）。
4. Dart SDK 的 `runtime/bin/` 里**不存在** `network_policy_*.cc` 这类文件（逐个探过，全 404）。

**Dart 侧（macOS）**：`dart:io` 自己实现 TLS，不经过 CFNetwork。
`dart-sdk/lib/_internal/vm/bin/secure_socket_patch.dart:309-312` 原文写着
`_X509CertificateImpl wraps an X509 certificate object held by the BoringSSL library`；
且 `dart-sdk/lib/io/**` 里 `CFNetwork` / `NSURLSession` / `NSURLConnection` /
`SecureTransport` / `kCFAllocator` 的引用数**全是 0**。ATS 是 CFNetwork 层的策略，
裸 socket + BoringSSL 不经过它。

**结论（关于明文拦截）**：`dart:io` 与 Go 都不过平台的明文/ATS 策略，
**明文 HTTP 请求本身不会被这两条策略拦掉**。这一条是**证据支持的**，不是推测。

### 但 macOS 上有一个真会拦住全部请求的东西（已被证实）

**`Release.entitlements` 缺 `com.apple.security.network.client`。**
沙箱化的 macOS 应用没有这个 entitlement 就**完全无法发起出站连接**：不是「明文被拦」，
而是**所有**请求失败（`Operation not permitted, errno = 1`），用户读到的同样是「实例连不上」。

这不是推测，是配置事实：

```
app/macos/Runner/Release.entitlements   （改前）只有 app-sandbox
project.pbxproj:508  Profile  -> Runner/DebugProfile.entitlements
project.pbxproj:640  Debug    -> Runner/DebugProfile.entitlements
project.pbxproj:660  Release  -> Runner/Release.entitlements
```

Flutter 的 macos 模板默认就是 `DebugProfile.entitlements` 带 `network.client`、
`Release.entitlements` 只带 `app-sandbox`，所以**调试能连、发版连不上**，
属于只有真机/正式包才会暴露的问题。已在 `Release.entitlements` 补上
`com.apple.security.network.client`（**不加** `network.server`：GUI 从不监听，
监听的是 CLI 的 `serve`）。

### 我们做了什么、为什么

| 位置 | 改动 | 性质 |
| --- | --- | --- |
| `app/macos/Runner/Release.entitlements` | 加 `com.apple.security.network.client` | **硬需求**，由配置事实证实；不加则 Release 版 100% 连不上 |
| `app/android/.../AndroidManifest.xml` | `android:usesCleartextTraffic="true"` | **防御性**（证据说不会拦，决断理由见下） |
| `app/macos/Runner/Info.plist` | `NSAppTransportSecurity` / `NSAllowsArbitraryLoads = true` | **防御性**（证据说不会拦） |

**为什么 Android 用全局开关而不是 `network_security_config.xml`**：
Android 的网络安全配置**只按主机名字面量匹配，没有 CIDR/网段语法**。我们的目标是
「用户自己填的任意地址」，列不出这样一份主机名清单，所以全局开关是唯一能覆盖真实用法的形式。

**为什么 macOS 用 `NSAllowsArbitraryLoads` 而不是 `NSAllowsLocalNetworking`**：
`NSAllowsLocalNetworking` 只覆盖 `.local` / 非限定主机名 / 私有网段，比产品需要窄，
用户可能指向一个 DDNS 或域名形式的 http 主机。而且按 Apple 的文档，两者同时存在时前者会
**压过** `NSAllowsArbitraryLoads`，也就是**同时写两个键反而会把策略收窄成「只允许局域网」**。
只写 `NSAllowsArbitraryLoads` 语义上严格更宽，且不会被兄弟键覆盖，
**无论那个优先级规则在某个具体系统版本上怎么表现，这个选择都是安全的**。

**为什么明知是防御性仍然加**：这是**不对称风险**。加了的代价是「放宽一条本来就由用户自己
指定目标的明文许可」；不加的代价是「主功能在移动端静默不可用，且报错毫无线索」。量级不对等。

### 不确定性与残余风险（请连同结论一起读）

- **改完是否真的生效，本机无法验证**：没有 JDK/Android SDK、没有 macOS，
  全部是静态证据（源码、二进制字符串、配置文件与 pbxproj 接线）。**属未本地验证项。**
- 「`dart:io` 不查平台策略」是基于 **Flutter 3.47.9 的引擎二进制**得出的。
  将来 Flutter 若让 `dart:io` 开始遵守 cleartext 策略，`usesCleartextTraffic="true"`
  就成了必需品（这也是保留它的理由之一）。
- `NSAllowsArbitraryLoads` 对**未来**走平台栈的代码路径（WebView、
  `cupertino_http` / `NSURLSession` 类客户端）是真实有效的，那时它不再是 no-op。
- `NSAllowsLocalNetworking` 与 `NSAllowsArbitraryLoads` 的优先级交互规则，
  **本机无法抓取 Apple 文档原文逐字核对**；上面的选择之所以不受该规则影响，已在「为什么」里说明。

### 怎么才算真的验证过

**唯一能证伪的办法：让用户在手机上装 GUI，连一个 LAN 内的 `http://` 实例试一次。**
其余都只是间接证据。

1. **Android**：真机装 APK → 填一个 `http://192.168.x.x:port` 的实例 → 点「测试连接」，
   期望 `instance.test` 返回 `ok: true`。若失败，看日志里是
   `CLEARTEXT communication not permitted`（说明策略确实生效、我们的开关没起作用）
   还是 `Connection refused` / 超时（网络层问题，与策略无关）。
2. **macOS**：装 **Release** 构建的 `AniSync.app`（别用 Debug，Debug 的 entitlement 本来就够）
   → 连同一台 `http://` 实例。若报 `Operation not permitted, errno = 1`，
   说明 entitlement 没进签名，用 `codesign -d --entitlements - AniSync.app` 核对。
3. **回归**：确认 `https://` 实例仍然正常（这两项改动都不该影响 TLS 路径）。

