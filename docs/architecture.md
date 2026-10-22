# 架构

## 目录职责

```
cmd/anisync          命令行入口：init / check / discover / probe / plan / sync / serve / tray
internal/workdir     运行期文件位置（程序同目录）
internal/config      配置读写：YAML、环境变量展开、校验与默认值
internal/cache       站点数据磁盘缓存（TTL + 原子落盘）
internal/assapi      ANI-RSS API 客户端
internal/discover    自动发现：扫 AnimeGarden / Bangumi，产出待新增的番剧
internal/source      站点适配层（anibt / animegarden / mikan / nyaa）
internal/rules       规则编译、打分、去重
internal/planner     计划生成与执行
internal/webui       本地设置页（内嵌资源，无前端构建步骤）
internal/tray        托盘：Windows 通知区域、macOS 菜单栏
samples/             开箱即用的配置样例与组清单
scripts/             构建与打包脚本
docs/                文档
```

依赖方向单向：`cmd → planner / webui / tray → discover / rules / source / cache / config → assapi / httpx`。

## 数据流

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

**自动发现复用新增链路，不另造一套写入逻辑。** `discover` 只负责「现在有什么番剧在放」，
产出的 `config.Target` 和用户手写的 `targets` 走同一条路：同样的站点爬取、同样的规则排序、
同样的 `rssToAni` → `addAni`。这样自动发现不需要理解 ANI-RSS 的字段规则，也不会绕过任何校验。
去重按 `bgmId` 精确比对（只比标题会因为中/日文名不同而漏），手写的 `targets` 优先于发现结果，
避免用户为某部番剧规定的季度与偏移被自动生成的空值顶掉。

**setAni 是整对象覆盖语义。** ANI-RSS 服务端用 `BeanUtil.copyProperties` 拷贝，null 也会覆盖过去。因此 `applyDesired` 从不构造局部对象，而是把 `listAni` 拿到的原始 map 改完再整份回传，避免把未修改的字段洗掉。

**备用 RSS 列表整体重建，但偏移必须继承。** ANI-RSS 会按每个 RSS 的实际集数自动推断 `StandbyRss.offset`（`AniUtil.autoOffset`）。AniSync 每次都重建 `standbyRssList`，所以先按 URL 把订阅里已有的 `offset` 继承回来，只有配置显式写了 `standbyOffset` 才覆盖。`diff` 比较备用列表时把 `url#offset` 一起比，否则「只改了偏移」会被误判成无需变更。季度与主 RSS 偏移同理：配置留空（指针为 nil）就不写这两个字段，绝不把 ANI-RSS 的推断结果洗掉。

**季度与偏移按番剧给，偏移按 RSS 分。** 与 ANI-RSS 的 `Ani.season` / `Ani.offset` / `StandbyRss.offset` 对应：`targets` 的一条配置就是「一个番剧」，`season` 给一个值（该番剧的所有订阅共用），`offset` 管主 RSS、`standbyOffset` 管副 RSS。`matchTarget` 先按 `bgmId` 精确匹配（订阅的 `bgmUrl` 或 RSS 参数里的 `bgmId`），再按标题包含匹配 —— 订阅标题带年份与 tmdbid（如「冰之城墙 (2026) [tmdbid=283428]」），只能包含匹配，标题同时看 `title` / `jpTitle` / `mikanTitle`。`offsetsOf` 的优先级是「命中的 target > 全局 `sync` 值 > 不碰」，所以番剧级配置对**已有订阅**同样生效，不只是新增。命中的番剧名会记在 `Action.Target` 里，计划和 Web 设置页都据此说明偏移是谁规定的。

**`${ENV}` 只在发请求时展开。** 展开发生在 `Instance.Resolve()` 与 `Config.ResolvedProxy()` 里，配置结构里始终保留占位符原文，`Validate` 也按展开后的值判断。Web 设置页是「读配置 → 改 → 存回」的闭环：如果在 `Load` 时就把密钥展开进内存，用户点一次保存就会把明文写进 `config.yaml`。同理由 `ResolvedProxy()` 兜底 `ANISYNC_PROXY`，避免环境变量被固化进配置文件。

**站点数据来自实例。** AniBT / AnimeGarden / Mikan 不自己爬网页，而是调用实例的 `aniBTGroup` / `animeGardenGroup` / `mikanGroup` 接口。实例怎么认这些站点，AniSync 就怎么认，拿到的是实例自己拼好的 RSS 地址，站点改版不会导致各写一份解析。`sources.instance` 指定借用哪个实例，留空取第一个启用的实例。

**Mikan 需要番剧页地址而非 RSS 地址。** `mikanGroup` 的入参是 `https://mikanani.me/Home/Bangumi/<id>`。优先从订阅现有 RSS 的 `bangumiId` 推导；推不出来时用实例的 `mikan` 搜索接口按标题反查，并用 `bgmId` 精确匹配结果，而不是按标题猜。

**Nyaa 例外。** ANI-RSS 本身不集成 Nyaa，Nyaa 也没有字幕组维度。AniSync 按关键字搜一次 RSS，从标题里提取 `[放流组]` 聚合，为每个组生成一个限定搜索词的 RSS（`?page=rss&q=…&c=0_0&f=0`，与站点搜索页的 RSS 按钮一致）。这种搜索型 RSS 的 `type` 为 `other`，ANI-RSS 可以直接消费。

**规则匹配标题。** 字幕类型（简日 / 繁日 / 内嵌 / 内封）只写在资源标题里，所以规则的匹配对象是「字幕组名 + 最近的资源标题」。`scope` 把规则分给主 RSS、副 RSS 或两者，因此「主 RSS 是字幕组、副 RSS 是放流组」只是一组 `scope: standby` 规则。

**缓存只存需要的部分。** 缓存键为 `站点:番剧标识`，只保留每个字幕组最近 12 条标题样本，热度单独用 `Total` 字段承载。缓存写入是「临时文件 + rename」的原子替换，读不出来就退化成无缓存，不影响同步结果。

**macOS 托盘需要 cgo。** 托盘实现按构建约束分流：`windows || (darwin && cgo)` 走 systray，其余走 stub（功能由 CLI 与 Web 设置页承担）。darwin 未开 cgo 时不会编译失败，只是没有菜单栏图标。
