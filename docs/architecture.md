# 架构

## 目录职责

```
cmd/anisync          命令行入口：init / check / discover / probe / plan / sync / serve / tray / status
internal/workdir     运行期文件位置（程序同目录）
internal/config      配置读写：YAML、环境变量展开、校验与默认值；方案解析（profiles → Scheme）
internal/daemon      常驻调度循环：每轮重读配置、间隔热取、睡眠切片、崩溃兜住、失败退避
internal/lease       单飞租约：data/sync.lock 心跳式独占，防止两个进程同时写同一批实例
internal/journal     运行日志：data/runs.jsonl，保留最近 200 轮结果，status 读它
internal/cache       站点数据磁盘缓存（TTL + 原子落盘）
internal/assapi      ANI-RSS API 客户端
internal/discover    本季新番：读 Yuc 当季番剧表，用 Bangumi 补 bgmId，产出待新增的番剧
internal/source      站点适配层（anibt / nyaa 默认；animegarden / mikan 兼容保留）
internal/rules       规则编译、打分、去重
internal/planner     计划生成与执行
internal/webui       本地设置页（内嵌资源，无前端构建步骤）
internal/tray        托盘：Windows 通知区域、macOS 菜单栏
app/                 跨平台 GUI（Flutter）：Windows / macOS / Android 的唯一图形界面
samples/             开箱即用的配置样例
scripts/             构建与打包脚本
docs/                文档
```

依赖方向单向：`cmd → planner / webui / tray / daemon → discover / rules / source / cache / journal / lease / config → assapi / httpx`。

## 数据流

配置先被解析成「每个实例实际生效的那一套」，然后才进规划：

```
config.profiles ──┐
                  ├──► config.SchemeFor(instanceName) ──► config.Scheme
instance.profile ─┘        （规则合并 / 站点顺序 / 副 RSS 上限都解析完）
                                     │
                                     ▼
                           planner.newBuilder
                             ├─ engines[方案名]        按方案名复用规则引擎
                             └─ regists[站点顺序签名]   按 orderKey 复用注册表
                                     │
                                     ▼
         collect() 的候选缓存键 = 站点顺序签名 | bgmId | mikanId | title
```

`SchemeFor` 是这条链上的分叉点：它按实例拿到 `Scheme`，planner 之后只认 `Scheme`，
不再关心「这个实例挂没挂方案」。没写 `profile` 的实例拿到的是顶层 `rules` / `sources`
的视图，与分方案之前逐字一致。（番剧级的第二道分叉见「关键设计」里的 `targets[].profile`。）

然后才是每一轮同步的主链路：

```
discover（可选）──► 现在在放的番剧 ──► 自动生成的 targets
      │                                      │
listAni（每个实例）                           │
      │  订阅列表                            │
      ▼                                      │
queryOf ──► source.Registry ──► 候选 RSS（含字幕组名与最近的资源标题）
      │                              │
      │                              ▼
      │                        rules.Rank（主 RSS 排序 + 去重）
      │                              │
      │                              ├─► 首项 = 主 RSS
      │                              └─► rules.Standby（副 RSS 排序）→ 副 RSS 列表
      ▼
diff（与实例现状比对）──► plan（update / add / noop）◄──┘
      │
      ▼
setAni（update） / rssToAni + addAni（add）
```

## 关键设计

**方案在 config 层解析完，planner 只认 `Scheme`。** `profiles` 是给人看的写法，
`Scheme` 是给代码用的视图：`config.SchemeFor(instanceName)` 把「实例挂的方案」展开成
规则（内联 `primary` / `deny` 在前，外置规则文件按声明顺序追加）、生效的 `maxStandby`
与站点顺序。方案没写的字段继承顶层（`maxStandby` 为 0 时取顶层，`sources` 整段缺省时
取顶层的 `order`），没挂方案的实例则直接拿顶层那一套 —— 于是「用默认规则」和「用某个方案」
在调用侧没有分支，planner 不必知道 `profiles` 存在。
`targets[].profile` 是同一条链上的第二个入口：命中某条番剧配置的订阅走
`SchemeForProfile(target.Profile)` 而不是实例那套，实例挂的方案对这一条不参与；季度与偏移
仍由 `offsetsOf` 按 target 自己的值处理，两件事互不影响。`Action.Profile` 记下每条订阅实际
生效的方案名，所以计划里能回答「这一条为什么全收了」，而不只是「实例挂的是哪套」。

**候选缓存键必须带站点顺序签名。** planner 按方案名复用规则引擎，按站点顺序签名
（`orderKey`，即 `anibt>animegarden` 这样的稳定字符串）复用注册表，而候选缓存（`memo`）
的键是「签名 | bgmId | mikanId | title」。少了签名这一步，两套**站点不同的**方案会互相复用候选
（缓存只按番剧记）：规则看着按方案生效了，候选却来自另一个方案的站点集合 ——
表现为「收藏档拿了追番档那两家的候选，nyaa 的组一个都没进来」，
而且只在两套方案站点不同的配置下出现，最难查。

**同一份配置只允许一个进程写。** ANI-RSS 的 `setAni` 是全量覆盖，两个进程各算一份目标状态
再各自整份写回，结果会在两组之间反复横跳且不报错。`internal/lease` 用 `data/sync.lock`
的心跳租约（TTL 10 分钟、每 2 分钟续约）做单飞，而不是操作系统文件锁：发布物是单文件静态
二进制，为它引平台 API 不划算；而且进程被强杀时没人来释放锁，租约靠「多久没心跳」自动过期，
语义反而更贴。代价是过期判断看时间不看进程存活，所以留了 `--force` 给「明确知道旧进程已经死了」的场合。

**常驻循环每轮重读配置。** `internal/daemon` 不缓存启动时的 `*Config`：Web 设置页保存时构造的是
一份全新的对象，抓着旧指针的调度器会永远跑旧规则，表现为「改完没生效」。间隔同理每轮重取，
长睡眠切成 ≤30 秒的片段轮询，于是「改成 0 就停、改回非 0 立刻跑一轮」不需要重启。

**发现链路不猜番剧，靠 `bgmId` 走到底。** `discover` 只负责「当季有什么番剧在放」，
产出的 `config.Target` 和用户手写的 `targets` 走同一条路：同样的站点爬取、同样的规则排序、
同样的 `rssToAni` → `addAni`。这样发现不需要理解 ANI-RSS 的字段规则，也不会绕过任何校验。
去重按 `bgmId` 精确比对（只比标题会因为中/日文名不同而漏），手写的 `targets` 优先于发现结果，
避免用户为某部番剧规定的季度与偏移被自动生成的空值顶掉。

**番剧清单来自 Yuc，`bgmId` 来自 Bangumi。** 默认来源是 `yuc`：Yuc（yuc.wiki）的季度页
本身就是一份人工整理过的日本番剧表，`OpenAniBT/Yuc` 每 6 小时把它的公开页面抓成结构化 JSON。
拿它定「这一季有哪些番」，再让 Bangumi 只负责补 `bgmId` —— 两边各干各擅长的事。

不用 `bangumi-calendar` 当默认，是因为放送表把国漫、美漫、OVA、网络短片全混在一起，
而且**不带地区与平台**，要筛干净只能逐条查 `/v0/subjects/{id}`（一季 110 次请求）。
它仍保留可用，配合 `discover.japanOnly`（默认开）按 `platform ∈ {TV, 剧场版}` 且
`meta_tags` 含「日本」过滤。

**匹配走日文原名。** Yuc 的 `titles.original.text` 对 Bangumi 的 `name`，比对前先归一化：
全角折半角、罗马数字折 ASCII（`Ⅱ` → `II`，与 NFKC 行为对齐）、去标点、季度写法统一
（`Season2` / `第2クール` / `Part 2` / `第2部分` 都折成 `第2期`）。实测日文原名命中 88%，
中文译名只有 46% —— 中文译名是各家自己的写法。剩下 12% 用 Bangumi 检索兜底，
且要在**全部**结果里挑最像的而不是取第一条：续作搜索经常同时返回「X 第2期」和「X」，
取第一个会把续作认成第一季（实测踩过：「转生就是剑 第二季」已经订了，却又冒出一条
「转生就是剑」）。认不出来的计入 `unmatched` 报到界面上，不静默吞掉。

**setAni 是整对象覆盖语义。** ANI-RSS 服务端用 `BeanUtil.copyProperties` 拷贝，null 也会覆盖过去。因此 `applyDesired` 从不构造局部对象，而是把 `listAni` 拿到的原始 map 改完再整份回传，避免把未修改的字段洗掉。

**备用 RSS 列表整体重建，但偏移必须继承。** ANI-RSS 会按每个 RSS 的实际集数自动推断 `StandbyRss.offset`（`AniUtil.autoOffset`）。AniSync 每次都重建 `standbyRssList`，所以先按 URL 把订阅里已有的 `offset` 继承回来，只有配置显式写了 `standbyOffset` 才覆盖。`diff` 比较备用列表时把 `url#offset` 一起比，否则「只改了偏移」会被误判成无需变更。季度与主 RSS 偏移同理：配置留空（指针为 nil）就不写这两个字段，绝不把 ANI-RSS 的推断结果洗掉。

**季度与偏移按番剧给，偏移按 RSS 分。** 与 ANI-RSS 的 `Ani.season` / `Ani.offset` / `StandbyRss.offset` 对应：`targets` 的一条配置就是「一个番剧」，`season` 给一个值（该番剧的所有订阅共用），`offset` 管主 RSS、`standbyOffset` 管副 RSS。`matchTarget` 先按 `bgmId` 精确匹配（订阅的 `bgmUrl` 或 RSS 参数里的 `bgmId`），再按标题包含匹配 —— 订阅标题带年份与 tmdbid（如「冰之城墙 (2026) [tmdbid=283428]」），只能包含匹配，标题同时看 `title` / `jpTitle` / `mikanTitle`。`offsetsOf` 的优先级是「命中的 target > 全局 `sync` 值 > 不碰」，所以番剧级配置对**已有订阅**同样生效，不只是新增。命中的番剧名会记在 `Action.Target` 里，计划和 Web 设置页都据此说明偏移是谁规定的。

**`${ENV}` 只在发请求时展开。** 展开发生在 `Instance.Resolve()` 与 `Config.ResolvedProxy()` 里，配置结构里始终保留占位符原文，`Validate` 也按展开后的值判断。Web 设置页是「读配置 → 改 → 存回」的闭环：如果在 `Load` 时就把密钥展开进内存，用户点一次保存就会把明文写进 `config.yaml`。同理由 `ResolvedProxy()` 兜底 `ANISYNC_PROXY`，避免环境变量被固化进配置文件。

**站点数据来自实例。** AniBT 不自己爬网页，而是调用实例的 `aniBTGroup` 接口。实例怎么认这个站点，
AniSync 就怎么认，拿到的是实例自己拼好的 RSS 地址，站点改版不会导致各写一份解析。
`sources.instance` 指定借用哪个实例，留空取第一个启用的实例。

**默认只跑 AniBT + Nyaa。** 两家覆盖面互补：AniBT 字幕组维度、API 齐全、带热度权重；
Nyaa 资源最广，补 AniBT 的盲区。`animegarden` / `mikan` 走的是和 AniBT 同一类实例接口，
因此不再进 `DefaultSourceOrder`——但**仍然可配且照常工作**，老配置不会因为这次调整失效。
注意 `animegarden` 不是 AniBT 的一层备份，而是**另一批组**：实测 121 部番剧里，只在它那边出现的
「番剧 × 字幕组」组合有 111 个（喵萌奶茶屋 16、猎户发布组 9 …），只在 AniBT 出现的多是放流组
（ToonsHub 60 部）。想按版本挑中文字幕就得显式写进 `sources.order`（样例 02/03/04 都这么配，
只追放流的 01 可以不开）。`SiteType` 的映射表也原样保留，避免下游按 `site` 字段分支的地方踩空。

**Mikan 需要番剧页地址而非 RSS 地址。** `mikanGroup` 的入参是 `https://mikanani.me/Home/Bangumi/<id>`。优先从订阅现有 RSS 的 `bangumiId` 推导；推不出来时用实例的 `mikan` 搜索接口按标题反查，并用 `bgmId` 精确匹配结果，而不是按标题猜。

**Nyaa 例外。** ANI-RSS 本身不集成 Nyaa，Nyaa 也没有字幕组维度。AniSync 按关键字搜一次 RSS，从标题里提取 `[放流组]` 聚合，为每个组生成一个限定搜索词的 RSS（`?page=rss&q=…&c=0_0&f=0`，与站点搜索页的 RSS 按钮一致）。这种搜索型 RSS 的 `type` 为 `other`，ANI-RSS 可以直接消费。

**规则分两个轴。** ANI-RSS 那边组名是一个**身份**（订阅的 `subgroup` / `standbyRssList[].label`，存的就是选组界面上的显示名），字幕类型才是标题里的**文字**。AniSync 照抄这个划分：`in: group` 只看组名，`in: title` 只看资源标题，`in: any`（默认）两者拼在一起看。分轴解决了两件事 —— 一是短组名不会再被作品名误伤（`Ani ni Tsukeru Kusuri wa Nai` 里的 `Ani` 不会把别的组认成 ANi，于是 `^ani\b|\[ani\]` 这种位置锚定可以退回 `^ani\b`）；二是组名那一侧同时含显示名与**种子标题方括号里的名字**（`GroupAliasesFromTitles`，与 ANI-RSS 的 `ItemsUtil.getSubgroup()` 同源），所以 `猎户发布组` 与 `猎户压制部` 写哪个都能命中，用户不必维护对照表。`in: any` 保持分轴之前的语义，已有配置的挑选结果不变。

**`scope` 决定规则参与哪一侧。** `main` 只挑主 RSS、`standby` 只挑副 RSS、`both` 两边都参与，因此「主 RSS 按版本档挑、副 RSS 用放流组」就是 `subs.yaml`（`scope: main`）+ `raws.yaml`（`scope: both`）两份文件。

**缓存只存需要的部分。** 缓存键为 `站点:番剧标识`，只保留每个字幕组最近 12 条标题样本，热度单独用 `Total` 字段承载。缓存写入是「临时文件 + rename」的原子替换，读不出来就退化成无缓存，不影响同步结果。

**macOS 托盘需要 cgo。** 托盘实现按构建约束分流：`windows || (darwin && cgo)` 走 systray，其余走 stub（功能由 CLI 与 Web 设置页承担）。darwin 未开 cgo 时不会编译失败，只是没有菜单栏图标。

## GUI：一份 op 契约，两种落地

`app/` 里的 Flutter 应用是 Windows / macOS / Android 上的唯一图形界面。它**不含业务逻辑** ——
所有能力都由 Go 引擎提供，而引擎有两种接法，Dart 侧对二者无感：

```
                        ┌──────────────────────────────────────────┐
   Windows / macOS  ──► │  dart:ffi  → libanisync_core.{dll,dylib} │
   Android          ──► │  c-shared，同一个进程内直调                │
                        └──────────────────────────────────────────┘
   另一台电脑 / 手机 ──►  HTTP → POST /api/v1/call（远端 AniSync serve 节点）
```

- **进程内 FFI**：Go 用 `-buildmode=c-shared` 编成动态库，`dart:ffi` 在同一进程里调用。
  没有端口、没有子进程、没有额外的序列化层。三个平台都走这一条，也是默认路径。
- **远端节点 HTTP**：电脑上跑 `AniSync serve`，同一组 op 通过 HTTP 暴露，手机或另一台电脑
  用它做遥控。需要「电脑上的常驻进程持有租约并定时跑」时走这条。

**两条路共用同一份 op 名、同一份 payload、同一份响应信封。** Dart 侧只有一个 `Backend`
接口，两个实现（`FfiBackend` / `NodeBackend`），UI 层不区分自己接的是本地库还是远端节点。
完整契约（5 个导出符号、信封形状、错误码、op 清单、事件模型、兼容规则）见
**[`ops-api.md`](ops-api.md)** —— 那里是唯一权威，本文不复述。

契约靠**对拍**而不是靠自觉守住：Go 侧 `internal/bridge` 的 `Ops()` 注册表是 op 名字的
唯一声明处，dump 成 `app/test/fixtures/ops.json` 后由 Dart 侧的契约测试读同一份文件断言
两边一致。字段名改了、某一边多加了 op，都会在 `flutter test` 这一步红掉，而不是等用户
点了按钮才发现「没反应」。形状对拍（`config.get` / `instance.subscriptions` /
`plan.build` / `status.get` 的样例响应）同理，见 `ops-api.md` §9。

**为什么不是 gomobile**：`gomobile bind` 只产 Android AAR 与 iOS XCFramework，桌面端
没有对应物；而 GUI 要覆盖 Windows / macOS / Android 三个平台且要求同一份绑定代码。
`c-shared` + `dart:ffi` 是唯一能三个平台通吃的组合（`docs/build.md` 有展开）。

**GUI 与 CLI 是两个独立产物。** CLI 仍是单静态二进制的
`AniSync-<版本>-<平台>-<架构>.zip`（既有发布流程由 `build.yml` 管，不受 GUI 影响）；
GUI 是 `AniSync-GUI-<版本>-<平台>-<架构>.zip`，里面带着动态库（`build.yml` 的
「发布物一律 zip」承诺对两者都成立，但从不在同一个包里混装）。
