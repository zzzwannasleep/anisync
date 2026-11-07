# 本机网络路由表（实测）

**为什么有这份文件**：这台机器上**不同 host 的最佳路由是相反的**。全局开代理会让 Flutter SDK 的下载慢一百倍，
全局不开代理会让 GitHub 与 Go module 直接连不上。每人各摸一遍的代价是重复踩坑，而且**探针会抢下载带宽、
把别人的正常下载误判成死连接**（已经真实发生过一次，详见文末）。

**纪律：任何人在跑大文件下载时，其他人不许跑测速探针。** 要测就等当前下载报完 `DONE`。

## 路由表

| host / 用途 | 路由 | 依据（谁测的） |
| --- | --- | --- |
| `storage.flutter-io.cn`（Flutter SDK 镜像） | **直连**，**本机最快** | native-build 实测 **33.7 MB/s** |
| `storage.googleapis.com`（Flutter SDK 官方） | 直连 + `--noproxy '*'` | native-build 实测 11.85–19.6 MB/s；走 7890 代理仅 ~0.12 MB/s |
| `github.com` / `api.github.com` / `objects.githubusercontent.com` | **必须走代理** `-x http://127.0.0.1:7890` | 直连超时；代理返回 200 |
| `proxy.golang.org` / `sum.golang.org` | **必须走代理** | 直连 12s 超时（curl 28）；代理返回 module 列表 |
| `pub.dev` | 直连可达（返回 200 + 完整 JSON） | captain 探测；**吞吐未测**，等带宽空闲再补 |
| `pub.flutter-io.cn` | 直连可达，响应里把 archive 地址改写成 `storage.flutter-io.cn/...` | captain 探测；**吞吐未测** |
| `dl.google.com`（Android SDK / cmdline-tools） | 直连 200 | captain 探测；**吞吐未测** |
| `api.adoptium.net`（JDK） | 直连 200 | captain 探测；**吞吐未测** |

> 「未测」就是未测。要在这台机器上定它的路由，跑一次 20 MB 的 range 采样，把字节数与耗时记进本表。

## 命令模板

```powershell
# 直连（并强制忽略任何 *_PROXY 环境变量）
curl.exe -L --noproxy '*' --retry 8 --retry-all-errors --retry-delay 3 -C - -o <目标> <url>

# 走本地代理
curl.exe -L -x http://127.0.0.1:7890 --retry 8 --retry-all-errors --retry-delay 3 -C - -o <目标> <url>
```

- **大文件一律用 `curl.exe`，不要用 `Invoke-WebRequest`**：后者在 1.5 GB 量级上出过中途死掉（`Invoke-WebRequest` 那次停在 33,841,152 字节）。Windows 自带 `C:\Windows\System32\curl.exe`（8.21.0），支持断点续传与自动重试。
- **`-C -` 续传的前提是「同一个 URL + 同一个文件」**。换镜像换 URL 时不要续传（分片可能不同），从头下。
- **下载到唯一临时名再改名**，避免与他人写同一路径。
- **下完先校验再解压**：`tar -tf <zip>` 能列出条目（Windows 自带 bsdtar 认 zip），或对已知大小的文件比对字节数。
  - `flutter_windows_3.47.9-stable.zip` = **1,939,451,626** 字节（官方与镜像一致，已用 HEAD `Content-Length` 双向确认）
  - `w64devkit-x64-2.11.0.7z.exe` = **68,801,240** 字节（v2.11.0 的 release **没有** `.zip` asset，只有 `7z.exe` 自解压包与 `source.tar`）

## 测速探针的正确写法（别重蹈覆辙）

```powershell
# 先说清两条已踩过的坑：
# 1) PowerShell 里 `-o $null` 会把 $null 当空参数丢掉，于是 -o 吃掉后面的 URL →
#    报 "URL rejected: Malformed input to a URL function"，同时响应体整个漏到 stdout。
#    要丢弃输出就用 `-o NUL`（Windows）或 `-o` 一个真实临时文件。
# 2) 探针用 `-r 0-31457279` 只取前 30 MB 时，若同时有别的下载在跑，读出来的数字毫无意义。
#    一次只跑一个。
Remove-Item Env:HTTP_PROXY,Env:HTTPS_PROXY,Env:http_proxy,Env:https_proxy -ErrorAction SilentlyContinue
$o = "$env:TEMP\probe.bin"
curl.exe -sS -L -m 15 -r 0-20971519 -o $o --noproxy '*' <url>
$mb = (Get-Item $o).Length/1MB; "{0:N1} MB" -f $mb; Remove-Item $o -Force
```

## 已知噪声（看到红字别去查不存在的故障）

| 现象 | 真相 |
| --- | --- |
| **`scripts/build-core.ps1` 在本机开箱即失败**：`$env:CC is unset` / `'gcc' not on PATH` / 默认搜索目录里都没有 | 它搜索的是 `C:\w64devkit`、`C:\msys64\mingw64`、`C:\mingw64`、`C:\tools\mingw64`、`choco\bin` 这些**常规**位置，而**本机的 w64devkit 装在 `D:\WorkProgram\_toolchain\w64devkit`**。脚本设计是对的（支持 `ANISYNC_W64DEVKIT` / `CC` 环境变量覆盖，没有硬编码机器路径），**但没人给它设过**。<br>**先设再跑**：`$env:ANISYNC_W64DEVKIT='D:\WorkProgram\_toolchain\w64devkit'`（或直接设 `$env:CC='...\bin\gcc.exe'`）。<br>**注意脚本自身报的错会指向「去装个编译器」，而真正的问题是「它不知道编译器在哪」**——不要照它的话去装第二份 w64devkit。 |
| **以 `git` 为依据的检查，在这个仓库里会系统性失真** | 本仓库有**大片未跟踪路径**（`app/**`、`scripts/*.ps1`、`.github/workflows/app.yml`、`docs/` 下的新文件……）。于是：**`git diff` 对它们必然空输出**（有人据此做过越界自证，等于自证了个空）；**`git grep` 只搜已跟踪文件**（有人清点旧产物名时，`git grep` 找到 3 行、漏了 5 行，漏掉的全在未跟踪文件里）。<br>**规矩**：需要「有没有引用了 X」这类证据时，**文件系统扫描 + `git grep` 两遍都要跑**；需要「我有没有改到它」时，用 **mtime** 或 `git status --porcelain` 的未跟踪条目，**不要用 `git diff`**。 |
| 命令里的 shell **实际是 Windows PowerShell 5.1（Desktop），不是 pwsh 7** | 本机没有 `pwsh.exe`，`powershell.exe` 落在 `System32\WindowsPowerShell\v1.0\`。CI 用的是 `shell: pwsh`，所以**脚本必须两边都能跑**：`$IsWindows`/`$IsMacOS`/`$IsLinux` 存在就用、否则精确回退，不要用 PS7 专有语法。**这也是 `scripts/pack` 那条 ASCII 守卫存在的根本原因**——PS 5.1 把无 BOM 的 UTF-8 当 ANSI 读，会**静默吞掉非 ASCII 注释的下一行**。 |
| `-o $null` 之类的写法行为怪 | 同上：PS 5.1 会把 `$null` 当空参数丢掉，于是 `-o` 吃掉后面那个 URL，报 `URL rejected: Malformed input to a URL function`，同时响应体整个漏到 stdout。**要丢弃输出就用 `-o NUL` 或一个真实临时文件。** |
| 中文输出在某些 console 里显示成乱码 | 同上：PS 5.1 的编码链路。**读和写两个方向都要防**：写文件永远用 write/edit 工具、**不要用 `Set-Content`**（它把含中文的文件写成 ANSI，中文全烂）；**读**也一样——用 PS 5.1 的默认编码去读 UTF-8 的 Go 源会得到满屏乱码，**那是读取方解码错了，不是文件损坏**。**别据此下「文件坏了」的结论**（已经有人在 t12 里差点把队友的正常重构报成文件损坏），先去读原始字节再判断。 |
| `flutter` **首次运行**打印 `git fetch --tags` 失败、`Command exited with code 128` | 正常。`github.com` 直连超时（见上表），但 `flutter --version` 整体仍 **exit 0**。设 `$env:HTTPS_PROXY='http://127.0.0.1:7890'` 就能消掉，不设也不影响任何功能。 |
| `flutter create --project-dir <dir>` 报 `Could not find an option named "--project-dir"`，exit 64 | 这个选项**不存在**。用 cwd 定位目标目录。 |
| `flutter test` 在空工程上报 `Test directory "test" not found.`，exit 1 | `flutter create --empty` **不生成 `test/` 目录**。不是工具链坏了——补一个测试文件它就 exit 0。 |
| `flutter doctor -v` 报 `[X] Android toolchain` / `[X] Visual Studio` / `[X] Chrome` | 本机**故意**不装这些大体积组件。Android 与 Windows 原生产物归 CI，本机只跑 `analyze` / `test` 与 Go 的 c-shared 构建。 |

## 统一环境变量（建议导出）

```powershell
$env:FLUTTER_STORAGE_BASE_URL='https://storage.flutter-io.cn'   # 镜像 33.7 MB/s，本机最快
$env:PUB_HOSTED_URL='https://pub.flutter-io.cn'
$env:GOTOOLCHAIN='go1.24.0'                                     # 仓库 go.mod 声明 1.24
$env:HTTPS_PROXY='http://127.0.0.1:7890'                        # 仅 Go module / GitHub 需要；大文件下载要反过来 --noproxy '*'
```


## 事故记录（避免重犯）

**2026-11-08，Flutter SDK 下载被 captain 误杀。** 时间线：

1. captain 用 40 秒的 range 探针（且当时 native-build 正在跑同一份文件的下载）读到 0.15 MB/s，
   据此判断「直连被限速到要 162 分钟」。
2. captain 于是 `Stop-Process` 掐掉了 native-build 那条 curl（pid 33784）。
3. **事后证据表明该判断是错的**：被掐掉的那条连接已经把文件推进到 793,178,112 字节，
   从 4.29 MB 到 756 MB 只用了 54 秒（≈14 MB/s）。所谓「162 分钟」是探针被污染读出来的假数字。

**教训**：拿一个**与其他工作并发**的短探针去判「这条连接死了」，会误杀健康的连接。
判定「连接是否已死」要用**该连接自己的字节数随时间的变化**（它自己的进度），不是旁路的探针。
另：captain 当时还宣称「两个写入方并发写同一文件」，也是错的——那个 9 字节文件是 native-build
在 GitHub 上试 `.zip` asset 时 404 的残渣。**归因要看「谁写的」，不能看时间戳就编故事。**
