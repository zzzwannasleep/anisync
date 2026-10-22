# 架构

## 目录职责

```
cmd/anisync          命令行入口：init / check / probe / plan / sync / serve / tray
internal/workdir     运行期文件位置（程序同目录）
internal/config      配置读写：YAML、环境变量展开、校验与默认值
internal/cache       站点数据磁盘缓存（TTL + 原子落盘）
internal/assapi      ANI-RSS API 客户端
internal/source      站点适配层（anibt / animegarden / mikan / nyaa）
internal/rules       规则编译、打分、去重
internal/planner     计划生成与执行
internal/webui       本地设置页（内嵌资源，无前端构建步骤）
internal/tray        托盘：Windows 通知区域、macOS 菜单栏
scripts/             构建与打包脚本
docs/                文档
```

依赖方向单向：`cmd → planner / webui / tray → rules / source / cache / config → assapi / httpx`。

## 数据流

```
listAni（每个实例）
      │  订阅列表
      ▼
queryOf ──► source.Registry ──► 候选 RSS（含字幕组名与最近的资源标题）
      │                              │
      │                              ▼
      │                        rules.Rank（主 RSS 排序 + 去重）
      │                              │
      │                              ├─► 首项 = 主 RSS
      │                              └─► rules.Standby（副 RSS 排序）→ 副 RSS 列表
      ▼
diff（与实例现状比对）──► plan（update / add / noop）
      │
      ▼
setAni（update） / rssToAni + addAni（add）
```

## 关键设计

**setAni 是整对象覆盖语义。** ANI-RSS 服务端用 `BeanUtil.copyProperties` 拷贝，null 也会覆盖过去。因此 `applyDesired` 从不构造局部对象，而是把 `listAni` 拿到的原始 map 改完再整份回传，避免把未修改的字段洗掉。

**站点数据来自实例。** AniBT / AnimeGarden / Mikan 不自己爬网页，而是调用实例的 `aniBTGroup` / `animeGardenGroup` / `mikanGroup` 接口。实例怎么认这些站点，AniSync 就怎么认，拿到的是实例自己拼好的 RSS 地址，站点改版不会导致各写一份解析。`sources.instance` 指定借用哪个实例，留空取第一个启用的实例。

**Mikan 需要番剧页地址而非 RSS 地址。** `mikanGroup` 的入参是 `https://mikanani.me/Home/Bangumi/<id>`。优先从订阅现有 RSS 的 `bangumiId` 推导；推不出来时用实例的 `mikan` 搜索接口按标题反查，并用 `bgmId` 精确匹配结果，而不是按标题猜。

**Nyaa 例外。** ANI-RSS 本身不集成 Nyaa，Nyaa 也没有字幕组维度。AniSync 按关键字搜一次 RSS，从标题里提取 `[放流组]` 聚合，为每个组生成一个限定搜索词的 RSS（`?page=rss&q=…&c=0_0&f=0`，与站点搜索页的 RSS 按钮一致）。这种搜索型 RSS 的 `type` 为 `other`，ANI-RSS 可以直接消费。

**规则匹配标题。** 字幕类型（简日 / 繁日 / 内嵌 / 内封）只写在资源标题里，所以规则的匹配对象是「字幕组名 + 最近的资源标题」。`scope` 把规则分给主 RSS、副 RSS 或两者，因此「主 RSS 是字幕组、副 RSS 是放流组」只是一组 `scope: standby` 规则。

**缓存只存需要的部分。** 缓存键为 `站点:番剧标识`，只保留每个字幕组最近 12 条标题样本，热度单独用 `Total` 字段承载。缓存写入是「临时文件 + rename」的原子替换，读不出来就退化成无缓存，不影响同步结果。

**macOS 托盘需要 cgo。** 托盘实现按构建约束分流：`windows || (darwin && cgo)` 走 systray，其余走 stub（功能由 CLI 与 Web 设置页承担）。darwin 未开 cgo 时不会编译失败，只是没有菜单栏图标。
