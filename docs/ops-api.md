# AniSync Ops API（schema 1）

> ## 变更记录
>
> **读者必看：这份文件会被改。你读完之后它可能已经不是最新的了。**
> 动笔实现前先看一眼这里的最后一条；发现你的实现与文档不符时，**先假定是自己读了旧版**，再核对。
>
> | 时间 | 改了哪 | 为什么 |
> | --- | --- | --- |
> | 建队时 | 初版 | op 清单、信封、视图、事件模型 |
> | 建队后 +2min | §6.0 新增；`RuleFileInfo` 示例由 `{owner,file,primary,deny}` 修正为 `{path,kind,count}`；§6.5 字段名补全 | 初版里的 `RuleFileInfo` 形状是我凭印象写的，与 `config` 包真实结构不符 |
> | 契约悬置点修复 | §7.9 新增 `journal.clear`（原只在 §8 闸门清单里出现）；`lease.takeover` payload 补 `confirm:true` | flutter-app 发现 §7 与 §8 不一致；顺带发现 `lease.takeover` 同样不一致。**op 总数 36 → 37** |
> | 去歧义 | §6 `RuleFileInfo` 增加「唯一形状、禁止超集、owner 属于外层对象」；`profile.ruleFile` 的 `file` 改名 `path` | core-go 提议「同时输出两套字段让两边都能过」——那等于取消契约 |
> | 事件所有权 | §1.1 新增：事件字符串所有权归调用方（Go 分配不释放，Dart 读完 `AniSyncFree`）；读失败时**不得释放**；生产者两条义务；不可达路径要配计数器 | flutter-app 用真库回环抓到 use-after-free（偶发 `Utf8Codec.decode` 失败） |
> | 错误码判别 | §3 新增：op 名不存在 → `not_found` **且 `detail` 恰好等于该 op 名**；`not_found` 的两种含义与判别规则 | captain 先前给 verifier 的口径（`bad_request`）是错的；且 `not_found` 混用两种含义会让客户端只能写含糊提示 |
> | `config.save` 语义 | §7.2 改成「总是落盘 + 返回 `{path, valid, errors, warnings}`」 | 旧行为下空工作区**连一个 config.yaml 都建不出来** |
> | 新增 `log.write` | §7.12 新增 `log.write {level,text}`；**op 总数 37 → 38**。§1.1 的计数器改指向它 | captain 要求「把计数放进引擎日志」，而 flutter-app 核实后指出：计数在客户端、`log.recent`/`status.get` 由引擎产出，**按当时契约根本做不到**。补齐这条客户端 → 引擎日志的通道 |

一份操作契约，两种落地。**Go 引擎是唯一的实现，Dart 只是它的一个客户端。**

```
                        ┌──────────────────────────────────────┐
   Windows / macOS ───► │  libanisync_core.{dll,dylib,so}      │
   Android         ───► │  c-shared，进程内直调（dart:ffi）     │
                        │                                      │
   另一台电脑/手机 ───► │  AniSync serve  →  POST /api/v1/call │
                        └──────────────────────────────────────┘
                                    全部落在 internal/app
```

- **内嵌（FFI）**：Go 编成 `c-shared` 动态库，Flutter 用 `dart:ffi` 在**同一个进程内**调用。没有端口、没有子进程、没有序列化之外的中间层。三个平台都走这一条。
- **节点（HTTP）**：电脑上跑 `AniSync serve`，同一组 op 通过 HTTP 暴露，手机或另一台电脑用它做遥控。**手机不需要一台服务器**，但需要「电脑上的常驻进程持有租约 + 定时跑」时走这条。

两条路共用同一份 op 名、同一份 payload、同一份响应形状。**Flutter 侧只有一个 `Backend` 接口，两个实现。**

> 为什么不是 gomobile：`gomobile bind` 只产 Android AAR / iOS XCFramework，桌面端没有对应物。要让 Windows / macOS / Android 用同一套绑定代码，只有 `c-shared` + `dart:ffi`。

---

## 1. 传输层 A：FFI

动态库只导出 **5 个契约符号**。不做「一个函数一个导出」——所有能力收敛到一个 op 路由器，新增功能不需要动 Dart 的绑定层。

**别用「导出表恰好 5 条」做断言。** cgo 会附带自己的辅助符号（实测至少会多一个 `_cgo_dummy_export`，名字表共 6 条），**那些不是契约的一部分**。正确的断言是「这 5 个名字**都在**导出表里」，而不是「导出表**只有** 5 条」——后者会被 cgo 的实现细节搞红，而那种红不指向任何真实问题。

```c
/* 生成的 anisync_core.h 里追加这一段；符号名固定，不许改 */
typedef void (*AniSyncEventFn)(const char* eventJson);

/* 返回 UTF-8 JSON，调用方用 AniSyncFree 释放 */
char*       AniSyncCall(const char* op, const char* payloadJson, const char* callId);
/* 取消一次进行中的调用；返回 1=已取消，0=没有这个 callId */
int         AniSyncCancel(const char* callId);
/* 设置全局事件回调；fn 为 NULL 时清除 */
int         AniSyncSetEventCallback(AniSyncEventFn fn);
/* 释放 AniSyncCall 返回的字符串 */
void        AniSyncFree(char* p);
/* 静态字符串，不需要释放 */
const char* AniSyncCoreVersion(void);
```

### 1.1 线程模型

- **`AniSyncCall` 会阻塞**，直到该 op 完成或返回 `jobId`。快 op 几十毫秒，慢 op（`plan.build`）几十秒。
- Dart 侧**永远不在 UI isolate 上调用**：一个常驻 worker isolate 串行执行 FFI 调用，UI isolate 只发请求、收回结果。这样即使阻塞，界面照样跟手。
- **取消**：Dart 为每次调用生成一个 `callId`（UUID v4），需要中断时从 UI isolate 调 `AniSyncCancel(callId)`。Go 侧维护 `callId → context.CancelFunc`。
  - **多久生效**：毫秒级（实测 `instance.test` 被取消后 3ms 返回）。
  - **怎么表现：不统一，取决于那个 op 自己的错误模型。**
    - `plan.build` 这类没有自身错误字段的 op → 返回 `{"ok":false,"error":{"code":"canceled"}}`
    - `instance.test` 这类**自带 `error?` 字段**的 op → 返回 `ok:true`，把原因放进 `data.error`（形如 `context canceled`）。这是对的：「这个实例连不上」对那个 op 来说是一次**正常完成的结果**，不是协议失败——UI 正好可以把这句话显示在那一行里，而不是让整个调用失败。
  - **两种都算取消成功。客户端不许把 `ok:true` 一律当成「没取消」**——要按该 op 自己的契约去读它的错误字段。
- **事件**：Go 侧任何 goroutine 都可以推事件。Dart 用 `NativeCallable<...>.listener` 注册回调，回调里只做一件事：把字符串丢进 `SendPort`。**回调必须立即返回**，Go 侧推送是非阻塞的（channel 满了就丢，绝不阻塞业务）。
- **事件字符串的所有权归调用方（这是硬规定，不是实现细节）。** `NativeCallable.listener` 的语义是：**回调体在回调返回之后才执行**（它把参数投进 isolate 的消息队列）。所以 Go 侧**绝不能**在回调返回后释放那个字符串——那时 Dart 还没读它。表现是一个极难查的竞态：`Utf8Codec.decode` 抛出「不是合法 UTF-8」，而且只在负载下偶发、重跑就没了。

  规则与 `AniSyncCall` 完全一致：**Go 返回的任何 `char*` 都由调用方用 `AniSyncFree` 释放。** 事件也一样——事件字符串由 Go 用 `CString` 分配、**永不自行释放**，Dart 在读成 Dart 字符串之后调用 `AniSyncFree`。

  ```dart
  // 正确：先拷贝成 Dart 字符串，再释放
  final text = ptr.toDartString();
  AniSyncFree(ptr);
  ```

  Dart 侧还必须**防御读取失败**：即便规则如此，读失败时也要降级成一条 warn 事件，**不许让事件流把整个应用带走**（事件是旁路信息，不是业务数据）。

- **读失败时，调用方不得释放（这条对每个语言绑定都成立，别各自赌一次）。**

  | 情形 | 调用方该做什么 |
  | --- | --- |
  | 读成功 | 读成自有字符串后 `AniSyncFree(ptr)` |
  | **读失败** | **不释放**，降级成 warn 事件，记一次失败计数 |

  理由是失败后果的量级不对等：
  - **不释放的代价**：泄漏一帧字符串（几十字节），而且已经通过 warn 事件如实报出来了，不是无声吞掉。
  - **释放的代价**：读失败意味着**这个指针的合法性已经不再被保证**。对来路不明的指针调 `free` 不是「清一次内存」，而是 **`free()` 一个非 malloc 地址 —— 结果是堆破坏**。堆破坏比崩溃更糟：**它不在这里崩，它在很远处、很久之后崩**，现场和病因完全脱钩。

  **宁可泄漏一帧，不换一次堆破坏。**

- **但重点是让这条路径根本不该到达。** 生产者侧有两条硬义务，谁实现都成立：
  1. **事件字符串一律由 Go 的 `encoding/json` 产出**。`encoding/json` 会把非法 UTF-8 字节**替换成 U+FFFD**，所以「Go 给的 JSON 不是合法 UTF-8」在结构上就不可能。
  2. **绝不交出一个自己不再拥有的指针。**

  也就是说：**一个按契约实现的调用方永远不该走到「读失败」这一支。** 它在契约里只是为了「万一你走到了这里，别把堆搞坏」。

- **推论：这条路径上要放一个计数器。** 既然它按设计不可达，那**它一旦非零，就是「设计外的某件事发生了」的唯一证据**。**一个不可达路径上的计数器，是发现它其实可达的唯一办法。** 没有计数器的「防御性代码」，等于一层没人知道的免责声明。

计数发生在**调用方**（读取失败抛在调用方语言里），而 `log.recent` / `status.get` 由**引擎**产出——所以调用方必须有一条**写入引擎日志**的路径，否则这个计数只存在于客户端内存里，进程一重启就没了，而「半年后有人问有没有触发过」时没人查得到。这就是 `log.write`（§7.12）存在的理由。**它不只是为这个计数器加的**：在它之前，客户端侧的任何诊断信息都没有办法进入引擎日志，而引擎日志恰恰是排查时唯一会被人翻的地方。
- Go 侧同一个 `callId` 只允许一次在飞；重复使用同一 `callId` 返回 `bad_request`。

### 1.2 动态库在哪

| 平台 | 库文件名 | 查找位置 |
| --- | --- | --- |
| Windows | `anisync_core.dll` | 与 GUI 可执行文件同目录 |
| macOS | `libanisync_core.dylib` | `<exe>/../Frameworks/`，回退到与可执行文件同目录 |
| Android | `libanisync_core.so` | `android/app/src/main/jniLibs/<abi>/`，装进 APK 后由 `dlopen` 解析 |
| 测试 / 开发 | 同名 | 环境变量 `ANISYNC_CORE_LIB` 指向绝对路径则优先用它 |

`ANISYNC_CORE_LIB` 是**测试的命脉**：`flutter test` 不打包任何原生产物，靠它加载 `build/native/<platform>-<arch>/` 下刚编出来的库，跑真实的 FFI 回环。没设这个变量时，加载失败必须抛出**能看懂的错误**（列出去找过的全部路径），不许静默降级成假实现。

---

## 2. 传输层 B：HTTP 节点

```
POST /api/v1/call          {"op":"...","payload":{...}}   → 信封（见 §3）
GET  /api/v1/events        text/event-stream，每行 data: <事件 JSON>
GET  /api/v1/health        {"ok":true,"data":{"version":...,"schema":1}}
```

鉴权：设了 `--token`（或配置 `node.token`）时，所有请求必须带
`X-AniSync-Token: <token>`。不匹配返回 401。

**绑定规则（不许放宽）**：

- 默认仍然只绑 `127.0.0.1:51837`。
- 要绑非回环地址（`0.0.0.0`、局域网 IP）时**必须**同时给 `--token`，否则直接拒绝启动并打印原因。
- 没有 token 的节点**永远**不监听非回环地址——这个 API 能改写订阅，裸奔在网络上等于把实例交出去。

Dart 侧 `NodeBackend` 与 `FfiBackend` 实现同一个 `Backend`：

```dart
abstract class Backend {
  Future<Map<String, Object?>> call(String op, Map<String, Object?> payload);
  Stream<CoreEvent> get events;
  Future<void> dispose();
}
```

`NodeBackend` 的 `events` 就是把 SSE 拆成 `CoreEvent`。**UI 层不区分自己接的是本地库还是远端节点。**

---

## 3. 信封与错误

请求：`{"op":"<name>","payload":{...}}`。`payload` 省略等价于 `{}`。

响应**永远是**下面两者之一：

```json
{"ok": true,  "data": { ... }}
{"ok": false, "error": {"code": "invalid_config", "message": "人话错误描述", "detail": "可选的原始错误/字段名"}}
```

| `code` | 含义 | UI 该怎么显示 |
| --- | --- | --- |
| `bad_request` | payload 字段缺失或非法 | 表单校验错误，指到具体字段 |
| `not_found` | 实例名/方案名/文件/任务不存在，**以及 op 名根本不存在** | 提示刷新；op 不存在时说明**版本不匹配**（判别见下） |
| `invalid_config` | 配置不合法（`config.Validate` 拦下的；配置保存不走这个码——校验问题落盘并以 `valid:false` 返回，见 §7.2） | 红条 + 原文，跳到配置页 |
| `network` | 连不上实例/上游 | 可重试提示 |
| `busy` | 已有一个写操作在飞 | 「正在同步中」 |
| `lease_held` | 单飞租约被别人持有 | 显示持有者 pid/用途，给 `--force` 入口 |
| `canceled` | 调用被 `AniSyncCancel` 取消（另一种形态见 §1.1：自带 `error?` 的 op 以 `ok:true` + `data.error` 报告取消） | 静默 |
| `internal` | 其它 | 可复制粘贴的错误文本 |

**op 名不存在一律回 `not_found`，不是 `bad_request`。** 两者的区别对客户端是有意义的：`bad_request` = 「你的 payload 形状不对」，改 payload 重试即可；`not_found` = 「你叫的这件事这个内核没有」，**说明客户端与内核版本对不上**。把后者混进 `bad_request`，会让版本偏斜（新的 GUI 连旧的节点、或反之）看起来像「参数写错了」，排查方向从一开始就是歪的。

这不是风格问题：本契约明确允许**加 op**（§10），所以「客户端认识而节点不认识」是一种正常的、必须能被识别的状态。

**但 `not_found` 同时承载两种含义，所以必须给出判别方法，否则客户端只能给一句含糊的提示：**

| 情况 | `code` | `detail` |
| --- | --- | --- |
| op 名不存在（版本偏斜） | `not_found` | **恰好等于请求里那个 op 名**（逐字，不加前缀） |
| 实例名/方案名/文件/任务不存在 | `not_found` | 那个资源的名字 |

判别规则：**`code == "not_found"` 且 `detail` 与你刚发出的 op 名逐字相同 → 版本偏斜**（不是资源缺失）。

```dart
final skew = res.code == 'not_found' && res.detail == op;
```

客户端本来就知道自己发了哪个 op，所以这条判别**不需要新字段、不需要额外请求**，而且不会误判——资源名不会长得和一个 op 名逐字相同（op 名一律 `组.动作` 形状）。**不许**用 `message` 的措辞去判别，那是一条会被翻译和改写的东西。

之所以不新造一个 `unknown_op` 错误码：`not_found` 对「叫了一个不存在的名字」是准确的，多一个码会让客户端多一条必须穷举的分支；而**判别方法是判别方法，不该混进错误码枚举里**。

> **这条判别规则要求本文档里的 op 名一个都不能写错。** 名字写错时，调用方拿到的会是 `not_found` + `detail` 等于那个错名字，按上面的规则**会被读成「版本偏斜」**——一个错字伪装成版本不匹配，排查方向立刻歪掉。本文档曾把 `instance.subscriptions` 误写成 `subscription.list`，就是这么一类。

`data.jobId` 出现在异步 op 上（见 §4）。**没有 `jobId` 的 op 完成后就没有后续事件**，不要等。

---

## 4. 同步 vs 异步

- **默认同步**：`AniSyncCall` 阻塞到出结果。慢的（`plan.build`、`instance.subscriptions`、`discover.list`）靠 worker isolate + `callId` 取消保证界面不卡。
- **异步（返回 `jobId`）**：**只在「会写实例、且可能很久」时用**。当前是这四个：
  `sync.run`、`plan.apply`、`backup.restore`、`discover.autoAdd`。
  立即返回 `{"ok":true,"data":{"jobId":"..."}}`，结果通过 `job.done` 事件送出，也可以用 `job.get` 取回；`job.cancel` 取消。

异步 op 同时发这几类事件：`job.start` / `progress` / `log` / `job.done` / `job.failed`。其中 `job.*` 三个带 `jobId`；`progress` / `log` / `instance.progress` 的 `jobId` **可能为 `null`**（§8 的信封示例里就有一个 `jobId:null` 的 `progress`；`log` 也可能来自 job 外，那时 `callId` 可能非 `null`）。

---

## 5. 路径与工作区

桌面端沿用「单一文件夹就是全部」的承诺：不调用 `core.init` 时，配置文件与 `data/` 都在**可执行文件同目录**（`workdir.Dir()` 的现有语义，逐字节不变）。

移动端没有这个前提（APK 目录只读），所以：

```json
// core.init
{"configPath": "/data/data/com.zzzwannasleep.anisync/files/anisync/config.yaml",
 "dataDir":    "/data/data/com.zzzwannasleep.anisync/files/anisync/data"}
```

`dataDir` 省略时取 `dirname(configPath)/data`。**`core.init` 可以重复调用**——Flutter 侧靠它切换工作区（换一份 config.yaml = 换一个工作区）。第二次调用会释放上一个工作区持有的租约并关闭缓存。

`workdir` 包新增 `SetRoot(dir string)` / `Root() (string, bool)`：设了 root 之后 `Dir()` 返回它。**默认（没人调 `SetRoot`）行为必须与现在完全一致**——有一条测试专门钉死这件事。

---

## 6. 数据模型

以下形状在 Go 与 Dart 两侧**字段名逐字一致**。空值一律用 `null` / 省略，不用空字符串占位。

`RuleFileInfo`（`config` 包里已有，**别另造**）是唯一一个不带 yaml 标签的结构：

```json
{"path": "raws.yaml", "kind": "primary", "count": 4}
```

`kind` ∈ `primary` / `deny`。**一个文件一行**，不要合并成「某方案引用了几个文件、各几条」的聚合结构。

**这个形状只有这一种，不许输出超集。** 曾经有人提议「同时输出 `owner` / `file` / `primary` / `deny`，这样 Dart 侧按哪种读法写都能过」——**那条路是错的**：契约一旦允许两种读法，就等于没有契约。Dart 侧只能猜哪个字段是权威的，而对拍测试再也抓不出漂移（多出来的字段永远「合法」）。`bridge` 输出的字段集合必须与本文档**逐字相同**，多一个字段算契约破坏（§9 的对拍会查这个）。

**owner 不在每一行里，它是外层对象。** 一个 `ruleFiles` 数组属于某个持有者：

| 数组 | 持有者 |
| --- | --- |
| `SchemeView.ruleFiles` | 同一对象里的 `instance` |
| `ProfileView.ruleFiles` | 同一对象里的 `name` |
| `Config.fileRules` | 「顶层 rules」，没有名字 |

所以 `ruleFiles` 里**同一个 `path` 重复出现是正常的**（两套方案引用同一份文件，各自读一次、各自生效）——那是两个持有者各自的加载结果，不是重复数据。UI 按持有者分组展示，不要试图把同名文件合并。

### 6.0 `Config`（原样回传，不许改名）

`config` 包的结构体已经带齐 json 标签，**直接用它们，不要另建一套 DTO**。
Dart 侧要建镜像模型，字段名与下面逐字一致（这才是 GUI 能读能写的唯一形状）：

```json
{
  "listen": "127.0.0.1:51837",
  "proxy": "",
  "sources": {
    "instance": "", "order": ["anibt","nyaa"],
    "stopOnFirstMatch": false, "timeoutSeconds": 25, "cacheMinutes": 720
  },
  "rules": {
    "primary": [ {…Rule} ], "deny": [ {…DenyRule} ],
    "primaryFiles": ["raws.yaml"], "denyFiles": ["deny.yaml"],
    "maxStandby": 1
  },
  "instances": [
    {"name":"主库","baseUrl":"https://ass.example.com/","apiKey":"${ANISYNC_KEY_MAIN}",
     "profile":"追番", "enabled":true}
  ],
  "profiles": [
    {"name":"追番",
     "rules": {"primary":[…],"deny":[…],"primaryFiles":[…],"denyFiles":[…],"maxStandby":1},
     "sources": {"order":["anibt","animegarden"]},
     "fileRules": [ {"path":"raws.yaml","kind":"primary","count":4} ]}
  ],
  "targets": [
    {"title":"冰之城墙","jpTitle":"氷の城壁","bgmId":"665390","mikanId":"",
     "instances":["主库"],"profile":"收藏","enable":false,
     "season":2,"offset":-12,"standbyOffset":-12}
  ],
  "discover": {
    "enabled":true,"sources":["yuc"],"bgmUser":"","days":180,"limit":50,
    "enable":false,"exclude":[],"japanOnly":true
  },
  "sync": {"intervalMinutes":60,"preview":false,"addMissing":false,
           "season":null,"offset":null,"standbyOffset":null},
  "fileRules": [ {"path":"raws.yaml","kind":"primary","count":4} ]
}
```

`Rule` = `{name, match, priority, site?, scope?, in?}`；`DenyRule` = `{match, why?}`。

**三态字段必须当成三态处理，这是最容易翻车的一处。** 下面这些字段在 Go 侧是 `*bool` / `*int`，`null`／缺失和「写了 0 / 写了 false」是**完全不同的两件事**：

| 字段 | `null` / 缺失 | 写了值 |
| --- | --- | --- |
| `instances[].enabled` | 启用（默认） | 按写的值 |
| `targets[].enable` | 新订阅不启用（默认） | 按写的值 |
| `discover.japanOnly` | 只列日番（默认） | 按写的值 |
| `sync.season` / `offset` / `standbyOffset` | **不碰订阅里已有的值** | 覆盖 |
| `targets[].season` / `offset` / `standbyOffset` | 沿用 `sync` 的全局值 | 覆盖 |

Dart 侧一律用 `bool?` / `int?` 建模，**不许用 `bool` / `int` 加默认值**：把 `null` 读成 `false` 会让「保存一次配置」把所有订阅的偏移洗成 0。`toJson` 时 `null` 必须**显式写 `null` 或整个键省略**，不许写成 `false` / `0`。

### 6.1 `InstanceView`

```json
{
  "name": "追番库",
  "baseUrl": "https://ass-a.example.com/",
  "apiKey": "abcdef…wx",          // 遮蔽：前 6 后 2；长度 <10 时全遮蔽
  "hasApiKey": true,
  "enabled": true,
  "profile": "追番",               // 空 = 用顶层 rules
  "profileLabel": "追番",          // 空 = "默认（顶层 rules）"
  "scheme": SchemeView
}
```

### 6.2 `SchemeView`（字段与 `planner.SchemeSummary` 对齐）

```json
{
  "instance": "追番库",
  "profile": "追番",
  "maxStandby": 1,
  "siteOrder": ["anibt", "animegarden"],
  "ruleCount": 7,
  "denyCount": 8,
  "ruleFiles": [{"path":"raws.yaml","kind":"primary","count":4}]
}
```

### 6.3 `SubscriptionView`

```json
{
  "key": "bgm:665390",
  "id": "1234",
  "title": "冰之城墙 (2026) [tmdbid=283428]",
  "bgmId": "665390",
  "bgmUrl": "https://bgm.tv/subject/665390",
  "image": "https://lain.bgm.tv/pic/cover/m/xx.jpg",
  "season": 2,
  "offset": -12,
  "standbyOffset": -12,
  "enable": true,
  "profile": "收藏",
  "main":    {"url":"https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=x","group":"猎户发布组","title":"…"},
  "standby": [{"url":"https://…","group":"ANi","title":"…","offset":-12}],
  "issues": ["副 RSS 第 2 条不是对象，已跳过"],
  "raw": { ... }                   // 实例返回的原始对象，逐字不改
}
```

- `key` 是**稳定标识**，规则（按序）：
  1. 实例对象的 `id` 非空且非 `"0"` → `"id:<id>"`
  2. `bgmId` 非空 → `"bgm:<bgmId>"`
  3. 否则 → `"title:<去掉全部空白并转小写后的标题>"`
  **UI 只认 `key`**，不要拿 title 当身份。
- `season` / `offset` / `standbyOffset` 在实例里可能是**任意值**（对象、字符串、null）。读出不是数字时给 `null` 并把原因追加进 `issues`，**不要猜**。
- `raw` 必须原样保留：`setAni` 是全量覆盖，UI 改完一个字段要把整份对象交回去。

### 6.4 `ProfileView`

```json
{
  "name": "追番",
  "rules": {"maxStandby":1, "primary":[{...Rule}], "deny":[{...DenyRule}],
            "primaryFiles":["raws.yaml","subs.yaml"], "denyFiles":["deny.yaml"]},
  "sources": {"order":["anibt","animegarden"]},
  "ruleFiles": [{"path":"raws.yaml","kind":"primary","count":4}]
}
```

`Rule` / `DenyRule` 沿用 `config` 包的结构体与 yaml 标签，**不要另造一套字段名**。

### 6.5 `PlanView`

直接用 `planner.Plan` 的 JSON（已有 json tag）：`actions[] / sites[] / skipped[] / siteInstance / discovered[] / discoverNotes[] / schemes[]`。
`actions[]` 的 `kind` ∈ `update` / `add` / `noop` / `error`；`changes[]` 给 `{field, from, to}`；`actions[]` 还带 `instance` / `aniId` / `title` / `reason?` / `target?` / `profile?`。

### 6.6 `RunEntry` / `LeaseView`

`RunEntry` = `journal.Entry` 原样（json tag 已有）。
`LeaseView` = `{"held":true,"pid":1234,"note":"tray","owner":"…","ageSeconds":12.5,"expired":false}`；空闲时 `{"held":false}`。

### 6.7 `JobView`

`job.list` / `job.get` 用的形状：

```json
{"id":"...","kind":"sync.run","state":"running","startedAt":"RFC3339","finishedAt":null,
 "message":"正在取站点数据 3/8","error":null,"result":{...}}
```

---

## 7. op 清单

分组顺序即**实现顺序**。每组都要有 Go 侧测试。

### 7.1 core

| op | payload | data |
| --- | --- | --- |
| `core.init` | `{configPath?, dataDir?}` | `{version, schema, platform, configPath, dataDir, goVersion}` |
| `core.version` | `{}` | `{version, schema, platform, goVersion}` |
| `core.ops` | `{}` | `{schema:1, ops:[{name, group, summary, async:bool}]}` |

### 7.2 config

| op | payload | data |
| --- | --- | --- |
| `config.get` | `{}` | `{path, config, ruleFiles:[RuleFileInfo], views:{instances:[InstanceView], profiles:[ProfileView]}, warnings:[]}` |
| `config.save` | `{config}` | `{path, valid, errors:[{field,message}], warnings:[]}` — 走 `Config.Save()`，**必须保住注释与键序** |
| `config.validate` | `{config}` | `{ok, errors:[{field,message}], warnings:[]}` |
| `config.default` | `{}` | `{config}` |
| `config.sample` | `{}` | `{yaml}` — `config.example.yaml` 的内容 |

**`config.save` 为什么是「总是落盘」而不是「校验不过就拒绝」**：拒绝的话，**零实例的新工作区连一个 `config.yaml` 都建不出来**，而「先设代理 / 先改规则、还没加实例」是交互式界面的正常中间态。校验该在 `check` / `plan` / `sync` 上拦（那些真的会碰实例），不该拦在保存上。

**但有一条线不能跨：落盘可以落一个「不完整」的配置，不能落一个「行为与它文字不符」的配置。**

| 情况 | 行为 |
| --- | --- |
| 校验类问题（没配实例、名字非法、字段越界…） | **落盘**，`valid:false` + `errors` 如实返回 |
| **引用的外置规则文件读不出来 / 解析不了** | **硬错误，不落盘** |

区别在于「不完整」与「会静默变形」：前者用户看得见（`errors` 摆在那里，同步会被拦）；后者会让后续的 `plan` / `sync` **少一套规则地跑**，而配置文本看上去毫无问题——**正是本项目一路在防的那种静默回落**。前者可恢复，后者会让你在不知情的情况下改变了挑选行为。

### 7.3 instance

| op | payload | data |
| --- | --- | --- |
| `instance.list` | `{}` | `{instances:[InstanceView]}` |
| `instance.test` | `{name}` | `{name, ok, latencyMs, about:{}, subscriptionCount, error?}` |
| `instance.subscriptions` | `{name, refresh?}` | `{name, total, items:[SubscriptionView], error?}` |

`instance.test` 走 `assapi.About` + `ListAni`；`refresh=false` 时允许吃缓存（`internal/cache`）。

### 7.4 subscription

| op | payload | data |
| --- | --- | --- |
| `subscription.set` | `{name, items:[{key, raw?, patch:{season?,offset?,standbyOffset?,enable?,profile?}}], dryRun?}` | `{updated, skipped, results:[{key, changed:[{field,from,to}], error?}]}` |
| `subscription.add` | `{name, bgmId?, title?, rssUrl?, season?, offset?, standbyOffset?, enable?, profile?, override?}` | `{added:true, ani:SubscriptionView}` |
| `subscription.delete` | `{name, keys:[...], deleteFiles?, confirm:true}` | `{deleted, errors:[]}` |
| `subscription.resolve` | `{name, rssUrl}` | `{ani}` — 即 `assapi.RssToAni` |

- `subscription.set` 的 `raw` 缺省时按 `key` 重新读一次 `listAni` 定位。`raw` 给了就用给的那份（UI 拿到的就是最新的一份）。
- `patch` 里**没写的字段一律不动**。`profile: ""` 表示**清除**该订阅的覆盖（显式用 `null` 与省略区分）。
- `dryRun:true` 只算 `results`，不调 `setAni`。
- 一次最多 200 条；超出 → `bad_request`。**没有 `confirm` 就不删**（见 §8）。

### 7.5 profile

| op | payload | data |
| --- | --- | --- |
| `profile.upsert` | `{profile}` | `{config, views}` |
| `profile.delete` | `{name}` | `{config, views, affected:[{instance}]}` |
| `profile.ruleFile` | `{owner, path}` | `{owner, path, kind, rules:[Rule], denyRules:[DenyRule]}` |

`ruleFile` 是**只读**的：外置规则文件的内容仍然只能在文件里改（这是现有承诺，不许在 GUI 里破例）。查看是刚需，编辑不是。

### 7.6 discover

| op | payload | data |
| --- | --- | --- |
| `discover.list` | `{japanOnly?, force?}` | 与现有 `/api/season` 响应同形：`{items,total,added,filtered,source,warnings,unmatched?,instanceError?,subscribed?}` |
| `discover.apply` | `{bgmIds, asTargets:true}` | `{config, added}` |
| `discover.autoAdd` | `{dryRun?}` | `{jobId}` — 异步；结果 `{planned, added, errors}` |

`discover.list` 必须**复用** `webui` 里已经写好的那套判定（`subscribedBgmIDs` / `bgmIDOfAni` / `foldTitle` / `listAniWithRetry`）——把这几个人从 `webui` 提到 `internal/app`，`webui` 改为调 `internal/app`，**不许复制一份**。

### 7.7 plan

| op | payload | data |
| --- | --- | --- |
| `plan.build` | `{discover?, limit?, instance?}` | `{plan:PlanView}` — `instance` 给了**一个不存在的实例名 → `not_found`**（不是空计划，见 §7.7 末尾） |
| `plan.apply` | `{discover?, force?}` | `{jobId}` — 异步；结果 `{updated, added, errors, plan}` |

`plan.build` 在飞行中发 `progress {done,total,label}` 事件，支持 `callId` 取消。

### 7.8 sync / job

| op | payload | data |
| --- | --- | --- |
| `sync.run` | `{discover?, addMissing?, preview?, force?}` | `{jobId}` — 异步；结果 `{writable, updated, added, skipped, errors, busy, note, plan}` |
| `job.list` | `{}` | `{jobs:[JobView]}` |
| `job.get` | `{id}` | `{job:JobView}` |
| `job.cancel` | `{id}` | `{ok}` — **幂等**：未知或已进入终态的 job 回 `{ok:false}`，**这不是错误**（见下） |

`JobView` 的形状定义在 §6.7。`state` ∈ `running` / `done` / `failed` / `canceled`。最近 20 个 job 留在内存里。

**异步 op 的 `result` 形状**（`job.done` 事件的 `data.result`，以及 `job.get` 里的 `job.result`）——契约原先只说了「返回 jobId」，没说结果长什么样，导致 Dart 侧只能猜。现在补齐：

```json
// sync.run
{"writable": 12, "updated": 9, "added": 3, "skipped": 41,
 "errors": ["追番库/冰之城墙：setAni 超时"],
 "busy": false, "note": "更新 9 条，新增 3 条", "plan": {PlanView}}

// plan.apply
{"updated": 9, "added": 3, "errors": [...], "plan": {PlanView}}

// discover.autoAdd
{"planned": 5, "added": 5, "errors": [...]}

// backup.restore
{"restored": 71, "pruned": 4, "errors": [...]}
```

`sync.run` **同时给数字摘要与完整 `plan`**：界面既要回答「改了几条」也要回答「改了哪几条」。`errors` 是逐条失败原因（`internal/journal` 的 `Clip` 会裁剪长度，最多留前 5 条）。

**租约的持有期（重要，别照自己的直觉实现）**：`sync.run` 与 `plan.apply` 采用**「拿到租约 → 跑完交还」**，**不是**长期持有。理由：内嵌 FFI 这一层不是常驻进程，跑完还攥着租约会把用户紧接着敲的命令行 `AniSync sync -y` 挡在 `lease_held` 外面——而那个进程其实什么都没在写。只有 `lease.takeover` 长期持有租约。常驻持有租约的仍然是 `tray` 与 `serve`（既有行为不变）。

**但这条有一个用户会撞上的后果，必须写在这里**：`serve` / `sync` 常驻或运行时持有租约，**如果那个进程是被硬杀的（任务管理器 / `Stop-Process -Force` / 断电），租约不会被交还**，最长要等 TTL 走完（**10 分钟**）。在那之前：

- 后续的 `sync` 会回 `lease_held`
- **连 `serve` 都起不来** —— 于是「打开设置页看一眼」这件只读的事也做不了

这是**单写者设计的必然代价**（设置页里带着「立即同步」，不给它租约就等于开了一个不受控的写入口），不是缺陷。但**用户看到的是一句「被占用」而不知道要等 10 分钟**，所以：

- 客户端遇到 `lease_held` **必须显示持有者的 `pid` / `note` / `ageSeconds` / 剩余时间**（`LeaseView` 有这些字段），并给出 `--force` / `lease.takeover` 的入口
- **硬杀之后的恢复路径是 `--force` 接管**，不是等待。UI 要说清这一句。

### 7.9 status / journal / lease

| op | payload | data |
| --- | --- | --- |
| `status.get` | `{limit?}` | `{version, platform, configPath, dataDir, instances:[SchemeView], lease:LeaseView, runs:[RunEntry], nextRunAt?}` |
| `journal.tail` | `{limit}` | `{entries:[RunEntry], path}` |
| `journal.clear` | `{confirm:true}` | `{ok, cleared}` |
| `lease.takeover` | `{force:true, confirm:true}` | `{ok, lease:LeaseView}` |

`status.get` **不发任何网络请求**（现有承诺）。

### 7.10 backup

| op | payload | data |
| --- | --- | --- |
| `backup.create` | `{}` | `{instances:[{name,count,file}], dir}` |
| `backup.list` | `{}` | `{dir, backups:[{file, path, createdAt, size, instances:[{name,count}]}]}` |
| `backup.restore` | `{file, prune?, instance?}` | `{jobId}` — 异步；结果 `{restored, pruned, errors}`；**`instance?` 的语义与别处不同，见下** |

原 `cmd/anisync/backup.go` 的业务逻辑已**提取到 `internal/backup`**（该文件已随提取删除），`cmd` 与 bridge 都调它。

**⚠️ 备份类 op 的 `instance?` 与其它「具名实例」不是一回事，别顺手统一掉。**

`backup.create` / `backup.restore` 的 `instance?` 指的是**存档里那些实例名**——而存档可能来自另一份配置，**它里面装着一个当前配置里没有的实例是正常的**（那正是备份的用途之一）。

| op | 实例名不在当前配置里时 |
| --- | --- |
| `plan.build` / `probe.run` / `instance.*` / `subscription.*` | **`not_found`**——具名资源必须存在。否则一笔 typo 会换来一份**看起来完全正常**的「无事可做」：`plan.build` 修复前的实际输出里 `actions` 是空的，但 `schemes` / `siteInstance` **照旧报着真实例名**（它们从配置算出、不受过滤器影响），所以整份响应看不出任何异常 |
| **`backup.restore`** | **跳过该实例 + 记一条 warn 日志**，其余实例照常还原 |

理由：一份旧备份里少一个实例，**不该让整次还原失败**——那会把「一个实例对不上」升级成「什么都恢复不了」。而跳过是安全的：还原可重跑，警告在引擎日志里。

**如果哪天要让这个跳过更显眼，正确方向是把它放进返回值**（`errors` 里，或加一个 `skipped` 列表），**不是把它变成 `not_found`**。

### 7.11 probe / bgm

| op | payload | data |
| --- | --- | --- |
| `probe.run` | `{site, bgmId?, title?, instance?}` | `{site, candidates:[{group,title,rss,weight,aliases,matched,rule,rank}], errors:[]}` |
| `bgm.search` | `{q, limit?}` | `{items:[{id,name,nameCN,image,summary,date,score}]}` |

### 7.12 log / events

| op | payload | data |
| --- | --- | --- |
| `log.recent` | `{limit?}` | `{lines:[{ts, level, text}]}` — 环形缓冲，最近 500 行 |
| `log.write` | `{level, text}` | `{ok, dropped?}` — 往同一个环形缓冲追加一行 |

`log.write` 是**唯一一个由客户端写入引擎状态的 op**，所以约束必须写清楚：

- `level` 先去掉首尾空白并转小写再判定——**大小写不敏感**；规整后不属于 `debug` / `info` / `warn` / `error` 的一律 `bad_request`。**写进日志缓冲的是规整后的小写形式**（客户端发 `INFO`，`log.recent` 里看到的是 `info`）。**拼写差异容忍，名字错误拒绝**——为拼写差异丢一条诊断信息不划算，而诊断信息丢了就没人知道它存在过。
- `text` 超过 **2000 字符截断**（不是报错——诊断信息宁可被截断也不要丢整条）
- 引擎在写入前加一行 `[client] ` 前缀，**让客户端写的行与引擎自己写的行在日志里可区分**
- **只进内存环形缓冲，不落盘、不进 `data/runs.jsonl`**。`runs.jsonl` 是每轮同步的结果记录，不是通用日志；混进去会让「最近 200 轮」被诊断行淹没
- 引擎侧**限速 20 行/秒**（唯一的那个上限，别把它读成占位符）：超出丢弃，回 `{"ok":true,"dropped":N}`，其中 `N` 是**这一秒被丢掉的行数**——它是返回值里的字段，与上面的 20 不是同一个量。**这是被设计过的背压，不是错误**，客户端不该重试。
  缓冲里只留**一条**「客户端日志被限速」告警（**不用噪音解释噪音**）。——**一个客户端不该能把引擎的日志缓冲刷掉**。日志是排查资产，不是可以让调用方随意覆盖的资源
- 它**不需要 `confirm`**：§8 的确认清单是给「不可逆或夺权」的动作用的（删订阅 / 清日志 / 接管租约），写一行日志既不会不可逆地破坏什么，也不夺谁的权

**与 `log.recent` 配套的意义**：`log.recent` 是「事件流断了之后的兜底视图」（SSE 重连 / App 切前台 / isolate 重启）。客户端在**读失败**这种「设计外状态」下写的那一行，正是断线期间唯一不会丢的记录——因为事件是旁路、会丢，而它进了缓冲。

`log.recent` 是**给「事件流断了」兜底**用的：SSE 重连、App 切前台、isolate 重启之后，UI 仍要能看到刚才发生了什么。

---

## 8. 事件模型

事件 JSON（FFI 回调参数 / SSE 的一行 data）：

```json
{"type":"progress","ts":"2026-11-08T00:31:02Z","callId":"…","jobId":null,"data":{...}}
```

| `type` | `data` | 谁发 |
| --- | --- | --- |
| `log` | `{level, text}` | `daemon.Options.Log`、planner 的逐条失败 |
| `progress` | `{done, total, label}` | `plan.build` / `sync.run` |
| `job.start` | `{kind}` | 异步 op |
| `job.done` | `{result}` | 异步 op 成功 |
| `job.failed` | `{error:{code,message}}` | 异步 op 失败 |
| `lease.lost` | `{holder:{pid,note}}` | `lease.KeepAlive` 的 `onLost` |
| `instance.progress` | `{name, done, total}` | 多实例遍历 |

**危险动作的确认**。下列 op 在 payload 里没有 `confirm:true` 时直接 `bad_request`，不执行：

- `subscription.delete`（删订阅）
- `journal.clear`
- `lease.takeover`

理由：这些动作的代价是「实例里的东西没了」或「抢了别人的写权」，不是 UI 的一层点击就能替代的。Dart 侧对应地必须弹二次确认，且确认文案要写清**影响几个实例、多少条订阅**。

---

## 9. 契约门禁（不靠自觉，靠对拍）

Go 侧 `internal/bridge` 有一个 `Ops()` 注册表，**每个 op 的名字与所属分组只在这一个地方声明**。

```
go test ./internal/bridge -run TestOpsSnapshot -update   # 生成 app/test/fixtures/ops.json
```

- `internal/bridge/ops_snapshot_test.go` 把注册表 dump 成 `app/test/fixtures/ops.json`（排序稳定，便于 diff）。
- `app/test/contract/ops_contract_test.dart` 读同一份 `ops.json`，断言：
  1. Dart 侧 `Ops` 常量表的每个名字都在 `ops.json` 里；
  2. `ops.json` 里除 `async:true` 外的每个 op，`FfiBackend` 与 `NodeBackend` 都能构造出请求；
  3. 没有任何一边单方面多出/少了 op。

**这条门禁的意义**：Go 侧加了 op 忘了告诉 Dart、Dart 写了个不存在的 op —— 两种漂移都在 `flutter test` 这一步炸出来，而不是等到用户点了按钮才发现「没反应」。

`app/test/contract/` 里还有一支**形状对拍**：Go 侧把 `core.ops` 之外，再用 `TestGoldenShapes` 输出 `config.get` / `instance.subscriptions` / `plan.build` / `status.get` 四个 op 的**样例响应**到 `app/test/fixtures/shapes/*.json`（对着 `httptest` 假实例跑出来），Dart 侧用它们跑 model 的 `fromJson` 反序列化。**字段名改了就红。**

---

## 10. 兼容规则

- `schema` 现在是 `1`。**加 op、加可选字段**= 兼容，不算破坏。**改字段名、改语义、删字段**= 破坏，必须同时升 `schema` 并同步改两侧。
- 每个 op 的响应里 `data` 是对象（可以为空对象 `{}`），不用裸数组——将来加字段不用改类型。
- Go 侧任何 op 里都不许 `panic`：`internal/bridge` 的 op 派发外层统一 `recover`，转成 `internal` 错误 + 事件，**进程不许死**（这是现有 daemon 的纪律，扩展到这里）。
