# AniSync Flutter GUI — T3 施工图

> 本文件是 T3 的实现蓝图，不是交付物。交付物是能过 `flutter analyze` / `flutter test` 的代码。
> 契约唯一来源 = `docs/ops-api.md`（schema 1）。本文件与它冲突时，以 `docs/ops-api.md` 为准。
> 路径：本文件位于 `app/docs/design.md`。`flutter create` 不会碰 `docs/`，所以它可以先落地。

---

## 0. 铁律（写在最前面，违反任何一条 = 返工）

1. **字段名逐字照抄 `docs/ops-api.md`**，不发明、不改名、不加「更好听的」别名。
2. **三态字段一律 `bool?` / `int?`**，`toJson` 时 `null` 必须显式为 `null` 或省略键，**绝不许落成 `false` / `0`**。
   `sync.offset` 为 `null` = 「不碰订阅里已有的偏移」；写成 `0` = 「把所有订阅的偏移洗成 0」。这两件事差着整个实例的数据。
3. **`Ops` 常量表与 `app/test/fixtures/ops.json` 双向一致**：Dart 多的、`ops.json` 少的，都算红。
4. **FFI 加载失败绝不许静默降级成假实现**，必须抛出列全「找过哪些路径」的错误。
5. 文案全部走 `lib/src/ui/strings.dart`；页面里不写裸中文字符串。
6. 依赖只有四个：`ffi` / `http` / `path_provider` + dev `flutter_lints`。不引 `provider` / `riverpod` / `bloc` / `window_manager` / `shared_preferences` / `file_picker` / `google_fonts`。

---

## 1. 目录结构

```
app/
├── docs/design.md              ← 本文件
├── lib/
│   ├── main.dart               ← 入口：建 AppState → runApp
│   └── src/
│       ├── core/
│       │   ├── backend.dart          abstract Backend {call / events / dispose}
│       │   ├── ops.dart              op 名常量表（对拍 ops.json）
│       │   ├── errors.dart           CoreError / CoreException / 错误码常量
│       │   ├── core_event.dart       CoreEvent 模型 + 解析
│       │   ├── ffi_backend.dart      dart:ffi + 常驻 worker isolate
│       │   ├── ffi_library.dart      动态库查找（ANISYNC_CORE_LIB → 平台默认）
│       │   └── node_backend.dart     POST /api/v1/call + SSE /api/v1/events
│       ├── model/
│       │   ├── json.dart             安全取值助手（asString / asInt / asBool / asList / asMap）
│       │   ├── config.dart           Config / Sources / Rules / Rule / DenyRule / Instance /
│       │   │                         Profile / ProfileSources / Target / Discover / Sync / RuleFileInfo
│       │   ├── instance_view.dart    InstanceView / SchemeView
│       │   ├── subscription.dart     SubscriptionView / RssRef
│       │   ├── profile_view.dart     ProfileView / RuleFileDetail
│       │   ├── plan.dart             PlanView / PlanAction / Change / PlanSiteSummary /
│       │   │                         PlanSkipped / SchemeSummary
│       │   ├── season_item.dart      SeasonItem / DiscoverListResult
│       │   ├── run_entry.dart        RunEntry
│       │   ├── lease.dart            LeaseView
│       │   ├── job.dart              JobView / LogLine
│       │   └── backup.dart           BackupEntry / BackupCreateResult / BackupListResult
│       ├── state/
│       │   ├── app_state.dart        AppState + CoreState / ConfigState / InstancesState /
│       │   │                         SubscriptionsState / ProfilesState / DiscoverState /
│       │   │                         JournalState / JobsState / StatusState / BackupState
│       │   ├── async_state.dart      AsyncState<T> 判别联合 + AsyncController
│       │   └── prefs.dart            PrefsStore（app-prefs.json，原子写）
│       └── ui/
│           ├── app_shell.dart        自适应导航壳（NavigationBar / NavigationRail，断点 900dp）
│           ├── strings.dart          全部中文文案
│           ├── tokens.dart           设计 token（间距/色/字号/圆角）
│           ├── theme.dart            ThemeData（亮/暗）
│           ├── widgets/              common.dart（AsyncView / EmptyState / ErrorPane /
│           │                         SkeletonRows / ConfirmDialog / TriStateIntField /
│           │                         TriStateBoolField / HScroll / NumText / CodeText …）
│           │                         run_row.dart（RunEntryRow，概览与日志页共用）
│           ├── dialogs/
│           │   ├── confirm_dialog.dart      危险操作二次确认（写清影响面）
│           │   └── bgm_search_dialog.dart   「添加番剧」：搜 Bangumi → 选条目 → 选去向
│           └── pages/
│               ├── overview_page.dart
│               ├── instances_page.dart
│               ├── subscriptions_page.dart
│               ├── profiles_page.dart       顶层规则 + 命名方案（两级）
│               ├── targets_page.dart        番剧配置（targets 是一等公民）
│               ├── discover_page.dart
│               ├── journal_page.dart
│               └── settings_page.dart
└── test/
    ├── contract/ops_contract_test.dart
    ├── contract/shapes_test.dart
    ├── ffi/ffi_roundtrip_test.dart
    ├── model/*_test.dart
    └── widget/*_test.dart
```

---

## 2. 设计系统（tokens，参照 ui-craft-dense-dashboard）

锁定的旋钮：**CRAFT_LEVEL=7 / MOTION_INTENSITY=3 / VISUAL_DENSITY=9**，DESIGN_VARIANCE 取 4（管理工具，安全布局）。

| 维度 | 取值 |
| --- | --- |
| 间距 | 只用 4 / 8 / 12 / 16 / 24，绝不出现 5/7/9/10 |
| 表格单元格 | `EdgeInsets.symmetric(horizontal: 12, vertical: 8)`；超密态 8/6 |
| 字号 | 正文 13–14，密集表 12，页标题 18–20，分节 14–15，`w500` |
| 图表数字 | 全部 `FontFeature.tabularFigures()`；ID/版本/时间戳用 `FontFeature.slashedZero()` 的等宽族 |
| 字体 | 系统字体栈（`Segoe UI`/`PingFang SC`/`Noto Sans CJK SC`/`Roboto`），不联网拉字体 |
| 圆角 | 输入 6 / 卡片 8 / 弹层 14（分级，不统一） |
| 色 | 中性 90% + 1 个主色；语义色 subdued：success / warning / danger / info。禁止高饱和 |
| 阴影 | 1px 描边优先于阴影；卡片用 `p-4` + 8 圆角 |
| 动效 | 仅微动：行 hover 80ms、行选中强调色 6% 底 + 2px 主色左边、内联保存 150ms。表格区域**没有**入场动画 / 滚动揭示 |
| 强调位预算 | 一个视口内主色出现 3–5 处（主按钮、选中态、关键指标），不刷屏 |

数字规范：集数 / 偏移 / 条数 / 耗时 / 端口一律等宽 + 右对齐；低于 0 的偏移（`-12`）也要走同一数字组件。

---

## 3. 导航与页面清单

**自适应壳**：`LayoutBuilder` 取 `constraints.maxWidth`，`>= 900` 用 `NavigationRail`（宽屏，窄侧栏 200–240px，浅色底不做全暗），`< 900` 用 `NavigationBar`（窄屏，底部）。

| # | 页面 | 导航标签 | 路由键 | 一句话职责 |
| --- | --- | --- | --- | --- |
| 1 | 概览 | 概览 | `overview` | 工作区 + 各实例健康 + 租约 + 最近几轮 + 「立即同步」 |
| 2 | 实例 | 实例 | `instances` | 实例增删改 + 测试连接 |
| 3 | 订阅 | （实例详情，push 到 2 之上） | `instances/:name/subs` | 列表/搜索/过滤 + 逐条编辑 + 批量规划应用 + 删除 |
| 4 | 规则 | 规则 | `profiles` | **顶层规则（默认口味）** + 各命名方案；两级，见 4.4 |
| 5 | 番剧配置 | 番剧 | `targets` | `targets` 作为一等公民：番剧级 season/offset/enable/实例限定/按番剧覆盖方案 |
| 6 | 本季新番 | 新番 | `discover` | 封面网格 + 已添加标记 + 勾选加入 targets + 「添加番剧」手工闭环 |
| 7 | 运行日志 | 日志 | `journal` | journal 列表 + 「只看失败」+ 事件流尾巴 |
| 8 | 设置 | 设置 | `settings` | 工作区 / 后端 / 同步默认值 / 代理 / 备份 / 版本 |

全局常驻元素：
- **顶部状态条**：当前工作区路径（等宽截断）、后端类型（本机 FFI / 远端节点 host）、租约状态点、全局进行中的 job 数。
- **事件抽屉**：`Backend.events` 的 `log` / `progress` / `job.*` / `lease.lost` / `instance.progress` 全量落进一个环形缓冲（上限 500 行），任何页面可拉开；这也是 SSE 断线后的兜底视图（配合 `log.recent`）。
- **危险操作统一走 `ConfirmDialog`**，文案必须写清影响面（几个实例、多少条订阅）。

---

## 4. 每页用哪些 op、拿到什么形状

### 4.1 概览
| op | payload | 用到的 data 字段 |
| --- | --- | --- |
| `status.get` | `{limit:20}` | `version, platform, configPath, dataDir, instances:[SchemeView], lease:LeaseView, runs:[RunEntry], nextRunAt?` |
| `log.recent` | `{limit:200}` | `lines:[{ts,level,text}]` |
| `sync.run` | `{discover?, addMissing?, preview?, force?}` | `{jobId}`（异步） |
| `job.get` / `job.cancel` | `{id}` | `job:JobView` / `{ok}` |
| `lease.takeover` | `{force:true, confirm:true}` | `{ok, lease}`（二次确认） |
| `core.version` | `{}` | `{version, schema, platform, goVersion}` |

健康表每行 = 一个 `SchemeView`：实例名 / 方案（`profile` 空 → 「默认（顶层 rules）」）/ 站点顺序（`siteOrder` join）/ `ruleCount` / `denyCount` / `maxStandby`；`ruleFiles` 非空时行尾给一个「+N 外置文件」角标。

### 4.2 实例
| op | payload | data |
| --- | --- | --- |
| `config.get` | `{}` | `config.instances`（**可写**、apiKey 是原文/`${VAR}`）+ `views.instances`（**只读派生**、apiKey 已遮蔽） |
| `instance.list` | `{}` | `{instances:[InstanceView]}` |
| `instance.test` | `{name}` | `{name, ok, latencyMs, about:{}, subscriptionCount, error?}` |
| `config.save` | `{config}` | `{path, warnings:[]}` |
| `instance.subscriptions` | `{name, refresh?}` | 见 4.3 |

> **apiKey 的双形状陷阱（必须遵守）**：`InstanceView.apiKey` 是**遮蔽值**（前 6 后 2，长度 <10 全遮蔽），`Config.instances[].apiKey` 才是原文（可能是 `${ANISYNC_KEY_A}`）。
> **编辑实例写的是 `Config.instances`**，绝不允许把遮蔽值写回去——那会把用户的密钥当场毁掉。列表展示用 view，编辑表单绑定 `Config.instances`，「显示切换」打开的是原文，不是遮蔽值。

### 4.3 订阅（实例详情）
| op | payload | data |
| --- | --- | --- |
| `instance.subscriptions` | `{name, refresh?}` | `{name, total, items:[SubscriptionView], error?}` |
| `subscription.set` | `{name, items:[{key, raw?, patch:{season?,offset?,standbyOffset?,enable?,profile?}}], dryRun?}` | `{updated, skipped, results:[{key, changed:[Change], error?}]}` |
| `subscription.add` | `{name, bgmId?, title?, rssUrl?, season?, offset?, standbyOffset?, enable?, profile?, override?}` | `{added:true, ani:SubscriptionView}` |
| `subscription.delete` | `{name, keys:[...], deleteFiles?, confirm:true}` | `{deleted, errors:[]}` |
| `subscription.resolve` | `{name, rssUrl}` | `{ani}` |
| `plan.build` | `{discover?, limit?, instance?}` | `{plan:PlanView}` |
| `plan.apply` | `{discover?, force?}` | `{jobId}`（异步） |

要点：
- **`key` 是唯一身份**，界面一切勾选、比较、去重都按 `key`（`id:` / `bgm:` / `title:` 三选一），禁止用 `title` 当身份。
- `raw` 必须原样留着并**原样回传**（`setAni` 是全量覆盖）。UI 改字段走 `patch`，不改 `raw`；`raw` 只在「重新读一次定位」时省略。
- `patch` 里没写的字段不动；`profile: ""` 表示**清除**覆盖（GUI 的下拉要有「清除覆盖」这一项，它发的是空串而不是省略）。
- 一次最多 200 条，超出前端直接拦下来（`bad_request` 预检）；`dryRun:true` 先算 diff。
- 删除必须 `confirm:true`，确认文案写「将从实例 X 删除 N 条订阅，其中 M 条带副 RSS」。
- 列表列：`title` / `key`（等宽小字）/ `season` / `offset` / `standbyOffset` / `enable` / `profile` / main 组名 / standby 组名+条数 / `issues` 角标。

### 4.4 规则页（★ 两级：顶层规则 + 命名方案）

**为什么必须两级**：README 的既有承诺是「实例不写 `profile` → 用顶层 `rules` + `sources.order`，老配置一个字段都不用改」。
也就是说**绝大多数存量用户吃的是顶层那套**。如果 App 只能编辑 `profiles[]`，这些用户在 GUI 里根本改不了规则，只能回去手抠 YAML。这是功能缺口，不是取舍。

| op | payload | data |
| --- | --- | --- |
| `config.get` | `{}` | `config.rules` / `config.sources.order`（顶层，**可写**）+ `config.profiles`（可写）+ `views.profiles:[ProfileView]` |
| `config.save` | `{config}` | 顶层规则的保存路径（顶层不是方案，没有 `profile.upsert`） |
| `profile.upsert` | `{profile}` | `{config, views}` — 命名方案走这条 |
| `profile.delete` | `{name}` | `{config, views, affected:[{instance}]}` |
| `profile.ruleFile` | `{owner, file}` | `{owner, file, kind, rules:[Rule], denyRules:[DenyRule]}`（**只读**） |

**界面结构**（左列表 + 右详情，窄屏改成分段列表 + 详情页）：
1. 列表首项固定为 **「默认（顶层规则）」**，**不可删除**，不可改名；副标题写「没挂方案的实例用的就是这套」。
2. 下面是各命名方案，副标题写「被这些实例挂着：A / B」，删方案时列出 `affected` 实例并要求二次确认。
3. 详情里的子块：**规则** / **排除** / **外置规则文件** / **站点顺序**。

**编辑目标随选中项切换**：

| 子块 | 顶层（默认）写这里 | 命名方案写这里 |
| --- | --- | --- |
| 规则 | `config.rules.primary` | `profiles[i].rules.primary` |
| 排除 | `config.rules.deny` | `profiles[i].rules.deny` |
| 外置文件 | `config.rules.primaryFiles` / `denyFiles` | `profiles[i].rules.primaryFiles` / `denyFiles` |
| 副 RSS 上限 | `config.rules.maxStandby` | `profiles[i].rules.maxStandby` |
| 站点顺序 | `config.sources.order` | `profiles[i].sources.order`（空 = 继承顶层） |

规则编辑字段：`name` / `match` / `priority` / `scope?(main|standby|both)` / `in?(any|group|title)`；deny 行只有 `match` / `why?`。
站点顺序用 `ReorderableListView` 勾选排序；方案的站点顺序要能一键「回到继承顶层」（把 `sources` 整个置空 → `ProfileSources` 为 `null`）。
外置规则文件**只读展示**（`path` + `kind` + `count`，一个文件一行不聚合），点击可看内容（`profile.ruleFile`）。**编辑仍然只能在文件里改**——这是仓库既有承诺，只读展示是对的，不算缺口。

### 4.5 番剧配置（★ `targets` 是一等公民）

**为什么单独一页**：`targets` 是**番剧级配置**（规定季度与偏移、决定新订阅是否启用、可以按番剧覆盖方案），和「实例里真实存在的订阅」**不是一回事**。
实例里手动加的订阅根本不在 `targets` 里；反过来，一条 `targets` 也可能一条订阅都没命中。
把两者混在一个界面里，用户会以为「删了 target 就删了订阅」——这是要出事的误判。

| op | payload | data |
| --- | --- | --- |
| `config.get` | `{}` | `config.targets`（可写）+ `config.profiles`（给 `profile` 下拉） |
| `config.save` | `{config}` | 增删改 target 的落地路径（`targets` 属于 Config） |
| `plan.build` | `{discover?, limit?, instance?}` | `{plan:PlanView}` → 用 `actions[].target` 反查「每条 target 现在命中了哪些订阅」 |
| `config.validate` | `{config}` | 保存前预检（`title` 与 `bgmId` 不能同时为空、`profile` 必须存在） |

每条 target 的字段与控件：

| 字段 | 控件 | 说明 |
| --- | --- | --- |
| `title` | TextField | `title` 与 `bgmId` 至少有一个（`Validate` 会拦） |
| `jpTitle` | TextField | Nyaa 只认日文/罗马音，自动发现会填这里 |
| `bgmId` | TextField | **接受 bgm 链接**（`https://bgm.tv/subject/123`）；保存时后端会归一成数字 |
| `mikanId` | TextField | |
| `instances` | 多选（`config.instances` 的名字） | 留空 = 全部启用实例 |
| `profile` | 下拉（含「未设置」） | 按番剧覆盖方案；`""` 与省略等价，UI 上就是「未设置」 |
| `enable` | **三态开关**：未设置 / 开 / 关 | `null` = 新订阅不启用（默认） |
| `season` / `offset` / `standbyOffset` | **三态数字输入** | 见下 |

**三态在视觉上必须可区分**（这是验收会专门看的一条）：
- 每个三态字段用一个「启用覆盖」的勾选 + 数字框。未勾选 → 值 `null`，输入框 `disabled` 且显示占位文案 **「未设置（不碰订阅里已有的值）」**；勾选后输入框可编辑，`0` 是合法值。
- 每个字段旁有**「回到未设置」**按钮（清空 = 写回 `null`），不怕用户填错回不去。
- `offset` 与 `standbyOffset` 分开：主 RSS 与副 RSS 的集偏移本来就是两件事。
- 底部一句常驻说明：「`season` / `offset` 留空 = 交给 ANI-RSS 按每个 RSS 的实际集数自己推断」。这句就是防「一次同步把全部订阅偏移洗成 0」的唯一护栏。

**「命中几条订阅」**：调 `plan.build` 后用 `actions[].target` 分组计数，在每行右侧显示 `命中 N 条`（点击展开看订阅名与 `changes`）。
`plan.build` 失败（网络 / 无实例）时显示 **「无法计算：{原因}」** 并给重试，**不许编数字**。

### 4.6 本季新番 + 手工「添加番剧」闭环

| op | payload | data |
| --- | --- | --- |
| `discover.list` | `{japanOnly?, force?}` | `{items, total, added, filtered, source, warnings, unmatched?, instanceError?, subscribed?}` |
| `discover.apply` | `{bgmIds, asTargets:true}` | `{config, added}` |
| `discover.autoAdd` | `{dryRun?}` | `{jobId}`（异步）→ 结果 `{planned, added, errors}` |
| `bgm.search` | `{q, limit?}` | `{items:[{id,name,nameCN,image,summary,date,score}]}` |
| `subscription.add` | `{name, bgmId?, title?, rssUrl?, season?, offset?, standbyOffset?, enable?, profile?, override?}` | `{added:true, ani}` |
| `config.save` | `{config}` | 写进 `targets` 的那条路 |

- 边角信息必须显示：`filtered`（被 japanOnly 过滤掉几条）、`unmatched`（来源有但 Bangumi 认不出）、`instanceError`（读实例失败，此时「已添加」判定不可信，要显式说明）。
- 封面直接加载 `item.cover`（原生端无 CORS 限制；老 webui 的 `/api/bgm/cover` 中转是浏览器才需要的）。
- `japanOnly` 的开关可以**不先保存配置**就预览：`discover.list` 的 `japanOnly` 参数就是干这个的。

**「添加番剧」弹窗**（`bgm_search_dialog.dart`，与网页版的弹窗对等）：
1. 搜 `bgm.search`；卡片给封面 / `name` / `nameCN` / `date` / `score` / `summary` 首行。
2. 选中条目后**必须选去向**，两条路都提供，且后果在按钮上就写清：

| 去向 | 调用 | 落点 | 按钮文案与后果说明 |
| --- | --- | --- | --- |
| 加进配置的 `targets` | `config.get` → 往 `config.targets` 追加 `{title, jpTitle, bgmId}` → `config.save` | 只有配置变了；实例里的订阅要等下一次 `sync.run` / `plan.apply` 才会出现 | 「加进番剧配置」+ 副文案「写进 config.yaml 的 targets，下次同步才会进实例」 |
| 直接加进某个实例 | `subscription.add{name, bgmId, title, ...}` | 实例里立刻多一条订阅 | 「直接加进实例」+ 实例下拉 + 副文案「立刻写进实例，不经过 targets」 |
| 两边都要 | 先 `config.save` 再 `subscription.add` | — | 「两边都加」，操作完成后**逐条回报**实际结果 |

3. **操作完必须告诉用户「加到哪里了」**：结果条给出「已写入 targets（1 条）」/「已在实例 A 新建订阅（key=bgm:665390）」/ 失败原因。这两件事的后果完全不同，不能一个 toast 糊过去。
4. 本季新番页的勾选批量走 `discover.apply`（等价于第一条路，批量版）；「添加番剧」弹窗是补那条手工输入的闭环。

### 4.7 运行日志
| op | payload | data |
| --- | --- | --- |
| `journal.tail` | `{limit:100}` | `{entries:[RunEntry], path}` |
| `journal.clear` | `{confirm:true}` | `{ok, cleared}`（危险 op，二次确认，文案写「清空 N 条运行记录，文件 X 会被清空，不可恢复」） |
| `log.recent` | `{limit?}` | `{lines:[{ts,level,text}]}` |

`RunEntry` 逐条展开显示 `mode / ok / ms / writable / updated / added / skipped / errors[] / error / panic / busy / nextAt / attempt / note`。
「只看失败」= `!ok || error != null || panic != null || errors.isNotEmpty`。`busy:true` 单独一档（不是失败，是「这轮让开了」）。

### 4.8 设置
| op | payload | data |
| --- | --- | --- |
| `core.init` | `{configPath?, dataDir?}` | `{version, schema, platform, configPath, dataDir, goVersion}`（**可重复调用 = 换工作区**） |
| `core.version` / `core.ops` | `{}` | 版本 / op 表 |
| `config.get` / `config.save` / `config.validate` / `config.default` / `config.sample` | 见 §7.2 表 | — |
| `backup.create` | `{}` | `{instances:[{name,count,file}], dir}` |
| `backup.list` | `{}` | `{dir, backups:[{file, path, createdAt, size, instances:[{name,count}]}]}` |
| `backup.restore` | `{file, prune?, instance?}` | `{jobId}`（异步，二次确认） |

后台偏好（不走 Go，不进 config.yaml）：工作区历史、上次后端、主题，落 `dataDir/app-prefs.json`，由手写 `PrefsStore` 读写（原子写：先写 `.tmp` 再 rename）。

---

## 5. `lib/src/core/ops.dart` 常量表（初稿，照 §7 清单）

按 `docs/ops-api.md` §7 的 12 组、共 **37 个 op**（§7.9 已补 `journal.clear`）。分组顺序即实现顺序，组内顺序照表。
**注意：op 清单只从 §7 抄，不从 §8 反推。** §8 只是给 §7 里已有的危险条目加 `confirm` 闸门，不是 op 来源。

```dart
class Ops {
  // 7.1 core
  static const coreInit = 'core.init';
  static const coreVersion = 'core.version';
  static const coreOps = 'core.ops';
  // 7.2 config
  static const configGet = 'config.get';
  static const configSave = 'config.save';
  static const configValidate = 'config.validate';
  static const configDefault = 'config.default';
  static const configSample = 'config.sample';
  // 7.3 instance
  static const instanceList = 'instance.list';
  static const instanceTest = 'instance.test';
  static const instanceSubscriptions = 'instance.subscriptions';
  // 7.4 subscription
  static const subscriptionSet = 'subscription.set';
  static const subscriptionAdd = 'subscription.add';
  static const subscriptionDelete = 'subscription.delete';
  static const subscriptionResolve = 'subscription.resolve';
  // 7.5 profile
  static const profileUpsert = 'profile.upsert';
  static const profileDelete = 'profile.delete';
  static const profileRuleFile = 'profile.ruleFile';
  // 7.6 discover
  static const discoverList = 'discover.list';
  static const discoverApply = 'discover.apply';
  static const discoverAutoAdd = 'discover.autoAdd';
  // 7.7 plan
  static const planBuild = 'plan.build';
  static const planApply = 'plan.apply';
  // 7.8 sync / job
  static const syncRun = 'sync.run';
  static const jobList = 'job.list';
  static const jobGet = 'job.get';
  static const jobCancel = 'job.cancel';
  // 7.9 status / journal / lease
  static const statusGet = 'status.get';
  static const journalTail = 'journal.tail';
  static const journalClear = 'journal.clear';   // payload 必须带 confirm:true
  static const leaseTakeover = 'lease.takeover'; // payload: {force:true, confirm:true}
  // 7.10 backup
  static const backupCreate = 'backup.create';
  static const backupList = 'backup.list';
  static const backupRestore = 'backup.restore';
  // 7.11 probe / bgm
  static const probeRun = 'probe.run';
  static const bgmSearch = 'bgm.search';
  // 7.12 log / events
  static const logRecent = 'log.recent';

  /// 表驱动清单，对拍 ops.json 用（顺序无关，比较用 Set）。
  static const all = <String>[ /* 上面 37 个，逐个列全 */ ];

  /// §4：只有「会写实例、且可能很久」的 op 是异步的。
  static const asyncOps = <String>{syncRun, planApply, backupRestore, discoverAutoAdd};
}
```

**对拍规则**（`test/contract/ops_contract_test.dart`）：
- `ops.json` 的 `data.ops[].name` 集合 与 `Ops.all` 集合 **互为子集**（双向）。
- `async:true` 的集合必须等于 `Ops.asyncOps`（这条也查，防止「四个异步 op」走样）。
- `ops.json` 不存在时该测试**失败**（不是跳过），消息写明「快照未生成」+ 生成命令，防止「以为对拍过了其实根本没比」。

**§7 与 §8 的一致性**（已由 captain 修文档）：§7.9 现为 `journal.clear {confirm:true} → {ok, cleared}`；`lease.takeover {force:true, confirm:true} → {ok, lease}`。
`Ops.all` 因此是 **37** 个。§8 不是 op 来源，只是危险闸门清单。

危险 op 的三个（§8）：`subscription.delete` / `journal.clear` / `lease.takeover` —— 没有 `confirm:true` 一律 `bad_request`，App 侧对应三次二次确认。

---

## 6. 数据模型与 JSON 契约（本节是全篇最容易翻车的地方）

### 6.1 总体规则

1. **只有 `Config` 家族需要「逐字保真」的 `toJson`**——因为只有它会被 `config.save` 整份回传。
   其它 View（InstanceView / SubscriptionView / PlanView / …）是**只读派生**，`fromJson` 要宽容（缺字段不炸），`toJson` 只服务局部构造（如 `subscription.set` 的 `patch`），不承担回传保真责任。
2. **列表的可空性照抄 Go 的 json tag**：
   - Go 侧**没有** `omitempty` 的 slice → Dart 用 `List<T>?`，`null` 要**显式写成 `null`**（Go 的 nil slice 序列化成 `null`，不是 `[]`）。
   - Go 侧**有** `omitempty` 的 slice → Dart 用 `List<T>?`，`null` 或空列表时**整键省略**。
   - 每个可空列表配一个 `xxxOrEmpty` 便利 getter 给 UI 用（`config.rules.primaryOrEmpty`），但 `toJson` 用的是**原始可空值**。
   - 理由：`config.get → toJson → config.save → config.get` 走一圈，若把 `null` 写成 `[]`，第二个 `config.get` 就会回 `[]`，逐字比对立刻红。
3. **JSON 键顺序** = Go 结构体字段顺序（Dart 的 `Map` 保序），这样即使做文本级 diff 也对得上。
4. 空值一律 `null` / 省略，**不用空字符串占位**（Go 侧只有少数不带 `omitempty` 的 string，比如 `listen` / `proxy` / `sources.instance` / `targets[].title`，它们保留 `""`）。

**两条硬规则（Captain 会专门查）**：

- **无条件输出**：`listen` / `proxy` / `sources.instance` 在 Go 侧**刻意没有** `omitempty`（空值也要写出来，否则保存一次这个键就没了，挂在上面的注释跟着消失——`config.go` 里有原话注释）。所以 Dart 侧 `toJson` 里这三个键**必须无条件输出，哪怕是空串**。同理 `targets[].title` / `targets[].bgmId` / `rules.maxStandby` / `*.priority` / `sources.order` 等所有不带 omitempty 的字段。
- **`fileRules` 只读不写回**：`Config.fileRules` 与 `profiles[].fileRules` 在 Go 上是 `yaml:"-"` 的**只读派生**（外置规则文件的加载摘要）。Dart 侧 `fromJson` 要读它（界面要显示），但 **`toJson` 不许把它写回去**——写回去语义就是错的。实现上：`toJson` 里显式跳过这两个键。

同理只读派生的还有 `Profile.fileRules`、`SchemeView.*`、`RuleFileInfo` 本身（它是 `config.get` 的 `ruleFiles` 里的展示结构，不是可写配置）。

### 6.2 `Config` 家族字段表（照 `internal/config/config.go` 的 json tag 逐个核对）

| Go 结构 | 字段 | Dart 类型 | 是否省略 | 备注 |
| --- | --- | --- | --- | --- |
| `Config` | `listen` | `String` | 否 | |
| | `proxy` | `String` | 否 | 空串也写出来 |
| | `sources` | `Sources` | 否 | |
| | `rules` | `Rules` | 否 | |
| | `instances` | `List<Instance>?` | 否 | nil → `null` |
| | `profiles` | `List<Profile>?` | **是** | |
| | `targets` | `List<Target>?` | 否 | nil → `null` |
| | `discover` | `Discover` | 否 | 结构体上的 `omitempty` 在 encoding/json 下**无效**，永远写出（已用 `TestSaveKeepsLegacyKeys -v` 实测确认） |
| | `sync` | `Sync` | 否 | |
| | `fileRules` | `List<RuleFileInfo>?` | **是** | 只读派生，`yaml:"-"` 不落盘 |
| `Sources` | `instance` | `String` | 否 | |
| | `order` | `List<String>?` | 否 | nil → `null` |
| | `stopOnFirstMatch` | `bool` | 否 | |
| | `timeoutSeconds` | `int` | 否 | |
| | `cacheMinutes` | `int` | 否 | |
| `Rules` | `primary` | `List<Rule>?` | 否 | nil → `null` |
| | `deny` | `List<DenyRule>?` | 否 | nil → `null`（05 样例的「收藏」方案就没写 deny） |
| | `primaryFiles` | `List<String>?` | **是** | |
| | `denyFiles` | `List<String>?` | **是** | |
| | `maxStandby` | `int` | 否 | |
| `Rule` | `name` / `match` | `String` | 否 | |
| | `priority` | `int` | 否 | 0 也要写出来 |
| | `site` / `scope` / `in` | `String` | **是** | `""` 时省略 |
| `DenyRule` | `match` | `String` | 否 | |
| | `why` | `String` | **是** | |
| `RuleFileInfo` | `path` / `kind` / `count` | `String` / `String` / `int` | 否 | 唯一不带 yaml 标签的结构；**一个文件一行，不聚合** |
| `Instance` | `name` / `baseUrl` / `apiKey` | `String` | 否 | apiKey 原文，可能是 `${VAR}` |
| | `profile` | `String` | **是** | |
| | `enabled` | **`bool?`** | **是** | 三态，见 6.3 |
| `Profile` | `name` | `String` | 否 | |
| | `rules` | `Rules` | 否 | |
| | `sources` | `ProfileSources?` | **是** | |
| | `fileRules` | `List<RuleFileInfo>?` | **是** | |
| `ProfileSources` | `order` | `List<String>?` | **是** | 空时整个 `sources` 对象省略 |
| `Target` | `title` | `String` | 否 | `""` 也写出来 |
| | `jpTitle` / `mikanId` / `profile` | `String` | **是** | |
| | `bgmId` | `String` | 否 | |
| | `instances` | `List<String>?` | **是** | |
| | `enable` | **`bool?`** | **是** | 三态 |
| | `season` / `offset` / `standbyOffset` | **`int?`** | **是** | 三态 |
| `Discover` | `enabled` | `bool` | 否 | |
| | `sources` / `bgmUser` | `String` / `List<String>?` | **是** | |
| | `days` / `limit` | `int` | **是** | `0` 会被省略；但 `Normalize` 会补成 180 / 50，正常不会是 0 |
| | `enable` | **`bool?`** | **是** | 三态 |
| | `exclude` | `List<String>?` | **是** | |
| | `japanOnly` | **`bool?`** | **是** | 三态 |
| `Sync` | `intervalMinutes` / `preview` / `addMissing` | `int` / `bool` / `bool` | 否 | |
| | `season` / `offset` / `standbyOffset` | **`int?`** | **是** | 三态 |

### 6.3 三态字段（验收专门查这一条）

| 字段 | `null` / 键缺失的含义 | 写了值的含义 | Dart 类型 |
| --- | --- | --- | --- |
| `instances[].enabled` | 启用（默认） | 按写的值 | `bool?` |
| `targets[].enable` | 新订阅不启用（默认） | 按写的值 | `bool?` |
| `discover.japanOnly` | 只列日番（默认） | 按写的值 | `bool?` |
| `sync.season` / `offset` / `standbyOffset` | **不碰订阅里已有的值** | 覆盖 | `int?` |
| `targets[].season` / `offset` / `standbyOffset` | 沿用 `sync` 的全局值 | 覆盖 | `int?` |

实现纪律：
- 模型里**不出现** `bool` / `int` 的默认值回退（写完 `dart analyze` 之后我会额外 grep 一遍 `?? false` / `?? 0`，只允许出现在**展示层**，且展示层必须另写「未设置」的文案）。
- `toJson` 里这些键一律走 `omitNull` 助手：值为 `null` → **不写键**。
  （Go 侧收到缺失键 = `nil` 指针，与收到 `null` 等价；两种都能保住语义。选择省略键是为了同时匹配 `omitempty` 的字符串/列表字段。）
- 编辑表单必须有**「未设置」这一档**：`sync.offset` 的空输入框 = `null`（不碰），输入 `0` = 覆盖成 0。UI 上这两者视觉可区分（空 vs `0`），文案写「留空 = 不碰订阅里已有的值」。

### 6.4 其它视图字段

**`InstanceView`**（只读派生，apiKey 已遮蔽）
`name` / `baseUrl` / `apiKey` / `hasApiKey:bool` / `enabled:bool`（**这里是生效值，不是三态**）/ `profile`（空 = 用顶层 rules）/ `profileLabel`（空 = 「默认（顶层 rules）」）/ `scheme:SchemeView`。

**`SchemeView`**（= `planner.SchemeSummary`）
`instance` / `profile` / `maxStandby` / `siteOrder` / `ruleCount` / `denyCount` / `ruleFiles:[RuleFileInfo]`。

**`SubscriptionView`**
`key` / `id` / `title` / `bgmId` / `bgmUrl` / `image` / `season:int?` / `offset:int?` / `standbyOffset:int?` / `enable:bool` / `profile` / `main:{url,group,title}?` / `standby:[{url,group,title,offset?}]` / `issues:[String]` / `raw:Map`。
`season`/`offset`/`standbyOffset` 在实例里可能是对象、字符串、`null`——**读出不是数字就给 `null`**，原因由 Go 追加进 `issues`；Dart 不猜。
`raw` 原样保留（`setAni` 是全量覆盖）。

**`ProfileView`**
`name` / `rules:{maxStandby,primary:[Rule],deny:[DenyRule],primaryFiles,denyFiles}` / `sources:{order}` / `ruleFiles:[RuleFileInfo]`。

**`PlanView`**（直接用 `planner.Plan` 的 json tag）
`actions:[{instance,aniId,title,kind,reason?,changes:[{field,from,to}],target?,profile?}]` / `sites:[{site,candidates,requests,failures,error?}]` / `skipped:[{instance,title,reason}]` / `siteInstance?` / `discovered?` / `discoverNotes?` / `schemes?`。
`kind ∈ update|add|noop|error`。

**`RunEntry`** = `journal.Entry`：`at`(RFC3339) / `mode?` / `ok` / `ms?` / `writable?` / `updated?` / `added?` / `skipped?` / `errors?` / `error?` / `panic?` / `busy?` / `nextAt?` / `attempt?` / `note?`。

**`LeaseView`** = `{held, pid?, note?, owner?, ageSeconds?, expired?}`；空闲时只有 `{"held":false}`——所有字段除 `held` 外都可空。

**`JobView`** = `{id, kind, state, startedAt, finishedAt?, message, error?, result?}`；`state ∈ running|done|failed|canceled`。

**`SeasonItem`** = `discover.Item` + `added:bool`：`bgmId` / `title` / `jpTitle?` / `source` / `airAt?` / `cover?` / `summary?` / `doing?` / `score?` / `weekday?` / `added`。

**`CoreEvent`** = `{type, ts, callId?, jobId?, data:Map}`；
`type ∈ log{level,text} | progress{done,total,label} | job.start{kind} | job.done{result} | job.failed{error} | lease.lost{holder} | instance.progress{name,done,total}`。

### 6.5 错误信封

`{"ok":true,"data":{...}}` / `{"ok":false,"error":{"code","message","detail"}}`。
`code` 常量表 + UI 映射（照 §3 表）：

| code | UI 表现 |
| --- | --- |
| `bad_request` | 表单字段级错误，指到具体字段（`detail` 里的字段名） |
| `not_found` | 「已不存在，请刷新」+ 刷新按钮 |
| `invalid_config` | 红条 + 原文 + 「去配置页」 |
| `network` | 可重试提示（不含指责用户的话术） |
| `busy` | 「正在同步中」+ 查看进度 |
| `lease_held` | 显示持有者 pid/用途 + 「强制接管」入口（二次确认） |
| `canceled` | 静默（不弹任何东西） |
| `internal` | 可复制全文的错误面板 |

`canceled` 有一个易错点：批量操作里被取消的条目不算失败，结果面板里单独一列。

---

## 7. 状态设计（unhappy：先设计非快乐路径）

CRAFT_LEVEL=7 ⇒ 每页必须齐备：**idle / loading / empty / error / success**，并对数据多来源页面额外覆盖 **partial**；节点后端场景额外覆盖 **offline**（远端节点断开）；**conflict** 在「租约被抢 / 保存时配置已被改动」两处出现。

统一用 `AsyncState<T>` 判别联合，**禁止** `isLoading && !error && data` 这种可以表达出不可能状态的布尔组：

```dart
sealed class AsyncState<T> {}
class Idle<T>    extends AsyncState<T> {}
class Loading<T> extends AsyncState<T> { final T? previous; }      // 保留旧数据，重试不闪白
class Data<T>    extends AsyncState<T> { final T value; }
class Empty<T>   extends AsyncState<T> { final String why; }        // 为什么空，由页给
class Failed<T>  extends AsyncState<T> { final CoreError error; }
class Partial<T> extends AsyncState<T> { final T value; final List<CoreError> degradations; }
```

| 页面 | idle | loading | empty | error | partial | conflict | offline |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 概览 | 未 `core.init`：居中「先选工作区」+ 按钮 | 顶栏进度 + 健康表骨架行（列结构一致） | 没有实例：「去实例页添加第一个 ANI-RSS 实例」+ 直达按钮 | 错误面板：`code` + 人话 + 复制原文 + 重试 | `status.get` 成功但 `runs` 为空 → 只标那一段 | `lease_held` 横幅：持有者 + 接管入口 | 节点后端断开横幅「已离线，改动会在恢复后同步」 |
| 实例 | 「+ 添加实例」主按钮 | 列表骨架 3 行 | 零实例：同上 | 同上 | `instance.test` 部分失败：逐行标红 + 单独重试 | — | 断开时禁止保存，按钮 disabled + 原因 |
| 订阅 | 列表 + 工具栏 | 表头先画，行骨架 8 行 | 三档：①实例里本来就没订阅 → 「在实例里添加，或去本季新番批量加入」②被过滤空了 → 「清空筛选」③当前实例禁用 → 「先启用该实例」 | 错误面板 + 保留筛选条件 | 有 `issues` 的行给角标，能展开看逐条 | 应用计划时 `busy`/`lease_held` → 横幅 + 进度 | 断开：列表只读，「应用」禁用 |
| 方案 | 方案列表 | 骨架 | 无方案：「默认（顶层 rules）也能跑；想要两套口味就新建方案」+ 新建按钮 | 同上 | 外置规则文件读失败：方案卡片局部红条，不挡其它方案 | 删除方案时列出 `affected` 实例 → 二次确认 | 只读 |
| 番剧配置 | 「+ 添加番剧」主按钮 | 列表骨架 | 零 target：「没有番剧级配置也能跑；这里用来给个别番剧定季度与集偏移」+ 「从本季新番挑」跳转 | 同上 | 「命中 N 条」算不出来时显式写「无法计算：{原因}」，不编数字 | 磁盘上的 `targets` 已被外部改动 → 重载 / 覆盖二选一 | 断开时只读 |
| 本季新番 | 「刷新列表」 | 封面网格骨架（同尺寸占位，防 CLS） | 过滤后为空 → 「清空筛选」；来源全失败 → 错误面板 | 错误面板 + 重试 | `warnings` / `unmatched` / `instanceError` 全部显示在顶部信息条 | — | 只读 |
| 运行日志 | 列表 | 骨架行 | 「还没跑过同步」+ 「立即同步」 | 同上 | `errors[]` 逐条展开 | — | 用 `log.recent` 兜底 |
| 设置 | 表单预填当前值 | 保存按钮内嵌 spinner，标签保留不消失 | — | 字段级 + 表单级双层 | 备份还原部分失败：逐实例结果 | 保存时配置已被外部改动（`config.get` 的 `path`/mtime 变化）→ 提示「磁盘上的配置已变，重新加载 / 覆盖」 | 断开：只允许改本地偏好 |

落地细节（照 `state-design.md`）：
- 骨架 **200ms 后**才出现（快响应不闪），单次等待 **5s 上限**，超时转「仍在进行 + 取消」。
- 按钮 loading：**保留标签**、内嵌 spinner、disabled 不隐藏。
- 错误面板必有：具体原因、`code`、一键重试、复制原文按钮、以及 `internal` 时的原始文本块。
- 空态必有：**为什么是空的** + **下一步做什么**（按钮）+ 一个低调图标。
- 乐观 UI 只用在低风险本地交互（勾选、过滤）；任何写实例的动作都要等真实结果。

---

## 8. 后端抽象

```dart
abstract class Backend {
  Future<Map<String, Object?>> call(String op, Map<String, Object?> payload, {String? callId});
  Stream<CoreEvent> get events;
  Future<void> dispose();
}
```

### 8.1 `FfiBackend`（三平台进程内 FFI）
- 5 个符号：`AniSyncCall` / `AniSyncCancel` / `AniSyncSetEventCallback` / `AniSyncFree` / `AniSyncCoreVersion`。
- **加载顺序**：`ANISYNC_CORE_LIB` → Windows `FlutterNativeLibraryDir / exe 同目录` → macOS `<exe>/../Frameworks/` 再回退同目录 → Android `libanisync_core.so`（由 jniLibs 装进 APK，`DynamicLibrary.open` 解析）。
- 失败错误必须列出**所有找过的绝对路径**，不静默降级。
- **常驻 worker isolate**：UI isolate 只发请求/收结果；每次调用生成 `callId`（UUID v4，自实现，不引 uuid 包）；`AniSyncCall` 在 worker 上跑，慢 op 由 `AniSyncCancel(callId)` 中断。
- 事件：`NativeCallable.listener` 注册 `AniSyncSetEventCallback`，回调只做一件事——把字符串丢进 `SendPort`，**立即返回**。
- `AniSyncFree`：每次 `AniSyncCall` 返回的指针都要释放（worker isolate 内 `try/finally`）。
- 同一个 `callId` 只允许一次在飞。

### 8.2 `NodeBackend`（远端节点）
- `POST {base}/api/v1/call`，体 `{"op":..,"payload":..}`；带 `X-AniSync-Token`。
- `GET {base}/api/v1/events` SSE：每行 `data: <event JSON>`；断线指数退避重连（1s→2s→4s→…上限 30s），重连成功后调 `log.recent` 补回缺口。
- 非 200 / 无 `ok` 字段 → 归一到 `CoreError`；401 → `unauthorized`（UI 提示 token 不对）。

### 8.3 工作区（`core.init`）
- 桌面默认 = `File(Platform.resolvedExecutable).parent`（GUI 与 CLI 同目录即共用 `config.yaml`）；Android = `getApplicationSupportDirectory()`。
- 切工作区 = 再调一次 `core.init`；切换前先 `dispose` 旧的订阅流。

---

## 9. 测试计划

| 文件 | 断言 |
| --- | --- |
| `test/contract/ops_contract_test.dart` | 读 `test/fixtures/ops.json`，`Ops.all` ↔ `ops.json` 双向相等；`async` 集合 == `Ops.asyncOps`；**文件不存在 → 失败**（消息写明「快照未生成」+ 生成命令），不静默跳过 |
| `test/contract/shapes_test.dart` | 读 `test/fixtures/shapes/*.json`（`config.get` / `instance.subscriptions` / `plan.build` / `status.get`），跑对应 model 的 `fromJson`，断言关键字段（含 `RuleFileInfo` 的 `path/kind/count`、`Plan.actions[].changes[]`）；**缺哪个文件就失败并指出文件名**，不静默跳过 |
| `test/ffi/ffi_roundtrip_test.dart` | `ANISYNC_CORE_LIB` 存在时加载真库，跑 `core.version → core.ops → config.default → config.validate → config.save → config.get`，每步断言 `ok:true`；环境变量缺失 → `skip`（**不造假通过**） |
| `test/model/config_test.dart` | **三态往返**：`samples/05-双方案.yaml` 语义的 fixtures（`sync` 无 offset、`targets[].season=2`、`instances[].enabled` 缺失、`discover.japanOnly` 缺失）→ `fromJson → toJson`，断言这些键**不存在**，且**不等于** `false`/`0` |
| `test/model/*_test.dart` | 各 model 的 `fromJson`/`toJson` 往返；缺字段不炸；`null` 列表与 `[]` 列表**区别对待**（`null` 往返仍是 `null`） |
| `test/widget/overview_page_test.dart` | 概览页在 loading / empty / failed 三态下的渲染（骨架存在、空态有下一步按钮、错误有重试与复制） |
| `test/widget/subscriptions_page_test.dart` | 订阅列表在三态下的渲染 + 过滤空态文案 |

`flutter analyze` 必须 0 issue；`flutter test` 必须全绿。

---

## 10. 风险与对拍点（开工后逐条销账）

1. `app/test/fixtures/ops.json` 由 core-go 生成 → 出现前契约测试 skip；出现后**立刻对拍**，`journal.clear` 的编外情况当场拍板。
2. `app/test/fixtures/shapes/*.json` 同理；形状测试是「字段名一改就红」的唯一保障。
3. `flutter create` 会覆盖 `pubspec.yaml` / `lib/main.dart` → **t1 完成前不往 `app/` 写业务代码**（本文件在 `docs/` 下，不受影响）。
4. `config.get → save → get` 的逐字一致性：靠 6.2 的可空性表 + 三态省略键 + 键顺序。
5. `InstanceView.apiKey` 是遮蔽值，写回会毁密钥 → 编辑只走 `Config.instances`。
6. Android 上 `dart:io` 路径与 `path_provider` 的差异 → 工作区默认值按平台分支，写入前先 `Directory.create(recursive: true)`。

---

## 11. 与网页设置页的面板对等清单（验收底线）

网页设置页（`internal/webui/assets/index.html`）的面板清单是「功能对等」的底线。逐项对照，每一项都要在 App 里找得到入口与编辑能力：

| 网页面板 | App 入口 | 写哪里 | 状态 |
| --- | --- | --- | --- |
| `p-instances` ANI-RSS 实例 | 实例页 | `config.instances` | 计划 |
| `p-network` 网络与代理 | 设置页 → 网络 | `config.proxy` / `config.listen` | 计划 |
| `p-rules` 规则（主 RSS 挑选顺序） | 规则页 → 顶层「默认」→ 规则子块 | `config.rules.primary` | 计划 |
| `p-deny` 排除 | 规则页 → 顶层「默认」→ 排除子块 | `config.rules.deny` | 计划 |
| `p-profiles` 订阅方案（规则/排除/外置文件/站点顺序） | 规则页 → 命名方案 | `config.profiles[i]` | 计划 |
| `p-offsets` 季度与剧集偏移 | 番剧配置页（逐条 target；全局兜底在设置页 → 同步） | `config.targets[].season/offset/standbyOffset` / `config.sync.season/offset/standbyOffset` | 计划 |
| `p-sync` 同步与后台 | 设置页 → 同步 | `config.sync.intervalMinutes/preview/addMissing` | 计划 |
| `p-discover` 本季新番 | 本季新番页（来源与开关在设置页） | `config.discover.*` | 计划 |
| `p-run` 执行 | 概览页「立即同步」+ 运行日志页 | `sync.run` / `journal.tail` | 计划 |
| 添加番剧弹窗（搜 Bangumi → 选条目 → 加入） | 番剧配置页 / 本季新番页 → 「添加番剧」（见 4.6） | `config.targets` **或** `subscription.add` | 计划 |
| 外置规则文件的内容 | 规则页 → 外置规则文件子块（只读 + 查看内容） | — | **只读，是有意的**：仓库既有承诺「外置规则文件的内容仍然只能在文件里改」，不算缺口 |

补充：`p-offsets` 在 App 里被拆成两处——**番剧级**（番剧配置页，主路径）与**全局兜底**（设置页 → 同步，`sync.season/offset/standbyOffset`）。
拆开的理由：全局那三个值属于「统一覆盖」这种核选项，语义是「不碰 / 覆盖」，和逐条 target 的操作频率完全不同；放在一起会让人误以为「这里填 0 就是默认」。
两处都必须带 §4.5 那套三态文案。

---

## 12. 实测记录（交付时的真实证据，不是设计意图）

### 12.1 验收命令与结果

| 命令 | 结果 |
| --- | --- |
| `flutter analyze` | **No issues found!** exit 0 |
| `flutter test`（不设 `ANISYNC_CORE_LIB`） | **55 passed / 1 skipped**，exit 0（skip = 没有真库可加载） |
| `$env:ANISYNC_CORE_LIB=<dll>; flutter test` | **62 passed / 0 skipped**，exit 0（连跑 3 次一致） |
| `flutter test test/ffi/ffi_roundtrip_test.dart`（真库） | **7 passed**，exit 0 |

真库是自己一次性编出来的（不占 t4 的地盘）：
`CGO_ENABLED=1 CC=w64devkit\bin\gcc.exe go build -buildmode=c-shared -o app/build/native/windows-amd64/anisync_core.dll ./cmd/anisync-core` → 18.3 MB，产出 `.dll` + `.h`。

### 12.2 契约对拍（真跑过的）

- `Ops` 常量表 ↔ `app/test/fixtures/ops.json`：**37 个 op 双向一致**，异步集合正好 `sync.run` / `plan.apply` / `backup.restore` / `discover.autoAdd`。
- `shapes/*.json` 四份样例响应全部可反序列化；`ruleFiles` 已由新夹具**真检验**（`{path,kind,count}`，无 `owner`）。
- `config.get → toJson → fromJson → config.save → config.get`：真库回环通过；三态字段（`sync.*` / `targets.*` / `discover.japanOnly`）往返后仍是 `null`。
- `config.get` 的键集合与 Dart 侧 `toJson` 逐字一致（`fileRules` 例外：Go 有、Dart 刻意不写回）。

### 12.3 三处跨侧发现（已上报 captain / core-go）

1. **事件字符串的生命周期**（一次观察，3 次未复现）：第一次真机回环时，事件回调里
   `pointer.toDartString()` 抛 `Utf8Codec.decode` 失败。
   机理：`NativeCallable.listener` 的语义是**把调用投递到 isolate 事件循环**，
   回调体内的代码在「回调已返回」之后才执行；如果 Go 侧在那之后释放了事件字符串，
   Dart 读到的就是已释放内存（表现就是非 UTF-8 字节）。
   Dart 侧已加固：读失败降级成一条 `warn` 事件，不再让事件流把 App 带走。
   **根因在 Go 侧**：事件字符串要么在 Dart 读取之前保持存活，要么把所有权交给 Dart（由 Dart `AniSyncFree`）。
2. **未知 op 的错误码**：团队验收口径写的是 `bad_request`，真库回的是 `not_found`；
   `docs/ops-api.md` §3 对此**没有规定**。已钉住不变量（`ok:false` + 两个 code 之一 + 不崩），等文档拍板后改单值断言。
3. **`config.save` 会先跑 `Config.Validate`**：空工作区（一个实例都没有）保存会拿到
   `invalid_config: 还没有配置任何 ANI-RSS 实例`。这不违反 §7.2 的字面，但对 GUI 有实际影响：
   新建工作区后「只改代理、还没加实例」时保存会被拒。已在 UI 上如实呈现错误原文。

### 12.4 响应式（窄屏真跑过）

壳测试在 **1280px 与 600px** 两个宽度逐个点过 7 个导航页，断言「每次渲染零异常」。
这条测试当场抓出 4 处真实溢出（规则页标题行 / 页签行 / 规则表格 / 发现页工具栏），
修法是**宽表横向滚（`HScroll`）+ 工具栏折行（`Wrap`）**，不是把字号调小糊过去。

---

## 13. 契约变更落地（t14：Go 侧 t13 落地后的 Dart 对齐）

§12.3 那三个发现，契约已经把答案写回来了。Dart 侧按答案改完，现状如下：

### 13.1 事件字符串：所有权转移给调用方（§1.1）

**契约答案**：Go 用 `CString` 分配、**永不自行释放**；Dart 读成 Dart 字符串后自己调 `AniSyncFree`。

落地：
- 重新绑定 `AniSyncFree`，回调里 `toDartString()` → **立即释放**。
- **读失败不释放**（同一条理由的另一面）：指针合法性已不再被保证，对它调 `free` 不是清内存而是**堆破坏**；宁可泄漏一帧。
- **读失败后也不去读它**：不许 `asTypedList` / hex dump。上报的只能是「关于失败的事实」。
- 失败分支做三件事（**缺一不可，两者用途不同**）：
  1. **本地计数器** `eventReadFailures` —— 聚合视图，环形缓冲会被冲掉，它不会；
  2. 一条 **warn 事件** —— 立即就地可见；
  3. 一行 **`log.write`** —— 进引擎环形缓冲，**断线期间唯一不会丢**的记录。
  注意 `log.write` **刻意不推事件**（否则是「收到 log 事件 → 再 log.write」的无尽回环），要回看走 `log.recent`。
- 可观测出口：`eventReadCount` / `eventReleasedCount` 一对计数器，**真库上断言 `released == read` 且 `read > 0`** ——
  这是「读完就释放、没有泄漏」唯一能被观测到的证据（光有注释不算）。
- 客户端诊断块（设置页）显示计数器与 `dropped`；`dropped > 0` 是**背压不是错误**。

### 13.2 未知 op → `not_found` + `detail == op`（§3）

**契约答案**：`not_found`（不是 `bad_request`），因为「op 不认识」= **版本不匹配**，是正常状态、必须可识别。
判别方法：`code == not_found && detail == 刚发出的 op 名`——判别放在**调用点**，因为只有调用点知道自己发了哪个 op。

落地：
- `dataOf(envelope, op:)` 一处实现，**FFI 与 Node 两个后端共用**（远端旧节点才是版本偏斜最容易真实发生的地方）。
- `CoreError.versionSkew` 是 **Dart 侧派生标记，不是协议字段**；`toJson()` 不写它。
- 判别的两个档：
  - `versionSkew == true` → 「本机引擎比这个 App 旧，不认识 xxx 这个操作」，**不给重试按钮**（重试不会让它变好），指向「设置」检查后端；
  - `not_found && detail != op` → 资源不存在，**把 `detail` 里的资源名显示出来**；
  - `not_found && detail 缺失`（core-go 的 t23 欠债期间）→ 显示 `message`（给人看），**不从 `message` 里解析资源名做逻辑**。
  纪律：**机器判别只用 `detail`，给人看可以用 `message`**（后者会被翻译与改写）。

### 13.3 `config.save`：总是落盘 + 两种失败分开（§7.2）

**契约答案**：`{path, valid, errors, warnings}`。校验类问题**落盘** + `valid:false`；
唯一的硬错误是**引用的外置规则文件读不出来** → **不落盘**。

落地（三件真改动 + 一条线）：
1. `valid:false` **算保存成功**（文件真写下去了）—— `ConfigState.save()` 不再当失败；
2. errors **显著、非 modal** 列出（可展开，带字段名）；
3. **常驻状态条**，且**放在概览页 `AsyncView` 外面** —— 状态还在加载/加载失败时也必须看得见。
   「保存成功 ≠ 配置可用」，不能让用户点了「立即同步」才发现；
4. 硬错误走**红色**另一条：「没有保存」+ 指名文件 + 说明「放行会让 plan/sync 少一套规则地跑」。

另外：`config.validate` 跟着 `config.load` 跑一次（不发网络请求），
这样**没点过保存**也能显示「已保存但不能同步」——例如刚打开一个空工作区。

### 13.4 取消的两种表现（§1）

- 无自身错误字段的 op → `canceled` 信封（`CoreException`，静默处理）；
- 自带 `error?` 的 op（`instance.test` / `instance.subscriptions`）→ `ok:true` + `data.error = "context canceled"`。

`looksCanceled()` 把第二种也认成取消：**不认它，用户会看到一条红色的「连接失败」，而它其实是他自己取消的**。

### 13.5 顺带修掉的一处假契约

`NodeBackend.cancel` 以前会去 `POST core.ops` 并带上 callId —— 那没有任何意义（§2 的 HTTP 契约里**根本没有取消通道**）。
现在如实返回 `false` 并注明：进程内取消靠 `AniSyncCancel`（FFI 专属），异步 op 的取消走 `job.cancel`。
**编一个「看起来像取消」的请求发出去，比不支持更糟。**

