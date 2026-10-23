# AniSync

多 ANI-RSS 实例的主副 RSS 自动打点工具。

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey.svg)](#安装)

## 简介

AniSync 是 ANI-RSS 的生态位补充，不是它的替代品：**让你不必打开 ANI-RSS 的 WebUI 去加订阅**。

它做两件事：

1. **自动发现该订阅什么** —— 读 Yuc 的当季番剧表，看这一季有什么日本番剧在放，把实例里还没有的补进去
2. **自动挑主副 RSS** —— 按配置的站点优先级获取字幕组与放流组信息，依据正则与优先级规则选出主 RSS 与副 RSS，写回各实例

适用于同时维护多个 ANI-RSS 实例、对字幕组与字幕类型有固定偏好、又不想每季手动加几十个订阅的场景。

## 特性

**该订什么**

- **本季新番一键入库**：不用打开 ASS 加订阅。设置页列出当季在放送的番剧（带封面、原名、在看人数），勾选后一键加进 targets；也可以打开「同步时自动添加」，让后台自己扫、自己补
- **列表干净**：番剧清单来自 [Yuc](https://yuc.wiki/)（人工整理的日本番剧表），不混国漫、美漫、OVA、网络短片；Bangumi 只负责补 `bgmId` 与封面

**选哪个组**

- **规则驱动**：正则匹配 + 优先级，可排字幕组，也可排简日内嵌 / 繁日内嵌 / 简繁日内封等字幕类型
- **主副分工**：主 RSS 用字幕组规则、副 RSS 用放流组规则，由规则的 `scope` 决定
- **副 RSS 点名制**：只有被 `scope: standby` 或 `scope: both` 规则点过名的组才进副 RSS，没点名的一律不要 —— 不点名就按发布条数兜底的话，会被搬运组和实例侧的伪组名刷满
- **组名单外置**：字幕组排名、放流组排名、黑名单各一个 YAML，主配置只留引用；换口味只换文件，主配置不用动
- **组名写一个就够**：规则的 `in: group` 只看组名，而组名这一侧同时包含「选组界面上的显示名」与「种子标题里印出来的名字」。两者有时并不一样（`猎户发布组` / `[猎户压制部]`），但写哪一个都能命中 —— 别名跟着候选现算，不用人去核对
- **两家站点互补**：AniBT 走实例接口（字幕组维度、API 齐全、带热度权重），Nyaa 由 AniSync 自己按关键字搜索（资源覆盖最广）。默认只跑这两家

**写回实例**

- **多实例**：一次同步，全部实例同时生效
- **按番剧规定季度与偏移**：和 ASS 一样，季度按番剧给一个值，主 RSS 与副 RSS 的集偏移分开给；实例里已有的订阅同样生效，不必逐个订阅改
- **安全默认**：预览操作只读；自动新增的订阅默认不启用，不触发下载
- **动手前能备份**：`AniSync backup` 导出订阅存档，`AniSync restore` 还原；`--prune` 连新增的一起删

**怎么用**

- **开箱即用**：`samples/` 里有三份完整配置可直接抄 —— 只追放流 / 只要字幕组 / 字幕组加放流
- **设置页能干活**：搜 Bangumi 选番剧（带封面与简介）直接加订阅，不用手抄 bgm 链接；深色 / 浅色双模式；每个区块各自带保存按钮，改哪块存哪块
- **多形态**：命令行、本地 Web 设置页、Windows 通知区域托盘、macOS 菜单栏应用 —— 同一个程序，按需要挑一种跑法
- **一个文件夹就是全部**：单一二进制、无运行时依赖；配置与缓存都写在程序同目录，删掉文件夹即卸载干净

## 安装

从 [Releases](https://github.com/zzzwannasleep/anisync/releases) 下载对应平台的 **zip**，解压得到一个
`AniSync/` 文件夹：

```
AniSync/
  AniSync.exe             Windows 的程序本体
  AniSync                 Linux 的程序本体
  AniSync.app             macOS 是菜单栏应用（程序本体在它里面）
  config.example.yaml     配置示例
  README.md               样例说明：怎么改、怎么用
  01-只追放流.yaml          三份现成配置，挑一份复制成 config.yaml 就能跑
  02-只要字幕组.yaml
  03-字幕组加放流.yaml
  subs.yaml               组名单：字幕组 / 压制组排名
  raws.yaml               组名单：放流组排名
  deny.yaml               黑名单
```

**程序只在这个文件夹里写文件** —— `config.yaml`、`data/cache.json`、`data/backup/`。
不碰 `%APPDATA%`、`~/.config`、注册表，也不碰系统临时目录。删掉文件夹就等于卸载干净。

| 平台 | 下载 | 里面是什么 |
| --- | --- | --- |
| Windows amd64 / arm64 | `AniSync-<版本>-windows-<架构>.zip` | `AniSync.exe` |
| macOS arm64 / amd64 | `AniSync-<版本>-macos-<架构>.zip` | `AniSync.app`（菜单栏应用） |
| Linux amd64 / arm64 | `AniSync-<版本>-linux-<架构>.zip` | `AniSync` |

**发布物一律是 zip，没有裸二进制** —— 这样「解压出来的那个文件夹」就是程序全部的地盘。

### 三种跑法

同一个程序，三种用法。**三种每个平台都能跑**，区别只在有没有那个图标。

| 跑法 | 怎么起 | 是什么 |
| --- | --- | --- |
| **CLI** | `AniSync <命令>` | `init` / `check` / `plan` / `sync` / `probe` 这些一次性命令。手跑、写脚本、丢给 cron 都用它 |
| **Web 设置页** | `AniSync serve` | 只在 `127.0.0.1:51837` 起一个本地网页：改配置、搜番剧加订阅、看计划。**不跑定时爬取** |
| **常驻 + 定时** | `AniSync tray` | 常驻后台，按 `sync.intervalMinutes` 定时爬取，同时把 Web 设置页一起起着 |

`tray` 在 Windows 和 macOS 上会多出一个图标（右键菜单：打开设置页 / 立即同步 / 状态 / 退出），
macOS 上那个图标就是 `AniSync.app`（声明了 `LSUIElement`，不在 Dock 里占位）。
**Linux 上 `tray` 照常常驻、照常定时、照常提供网页，只是没有图标** ——
它启动时会打印一行提示，`Ctrl+C` 退出：

```
AniSync 托盘：当前平台 linux 使用本地 Web 设置页。
  设置页: http://127.0.0.1:51837
  配置:   /path/to/AniSync/config.yaml
按 Ctrl+C 退出。
```

几点：

- **要定时爬取就用 `tray`，别用 `serve`** —— `serve` 只起网页，不跑定时。
- **macOS 的 `.app` 里就是同一个二进制**，所以也能当 CLI 用：
  `AniSync.app/Contents/MacOS/AniSync plan`
- **三种跑法读写同一份 `config.yaml`**，随时能换。
- **别把 `.app` 单独拖进 `/Applications`**：配置写在 `.app` 旁边，也就是解压出来的那个文件夹里。
  整个 `AniSync/` 文件夹一起挪到你想放的地方就行。

未签名的 `.app` 首次打开会被 Gatekeeper 拦截，可右键选择「打开」，或执行：

```bash
xattr -dr com.apple.quarantine /path/to/AniSync/AniSync.app
```

从源码构建见 [docs/build.md](docs/build.md)。

## 快速开始

```bash
AniSync init            # 生成配置文件（或直接从 samples/ 抄一份）
AniSync check           # 校验配置并测试实例连通性
AniSync discover        # 看现在有什么番剧在放（只读，没配实例也能跑）
AniSync backup          # 把当前订阅导出成存档（动手前先留一份）
AniSync plan            # 预览将要发生的改动，不写入
AniSync plan --discover # 预览自动发现会新增哪些订阅，不写入
AniSync sync -y         # 执行同步
AniSync restore <存档>   # 用存档把订阅改回去；加 --prune 连新增的一起删掉
AniSync tray            # 常驻托盘 + 后台定时爬取，同时提供 Web 设置页
```

Windows 下不带参数双击只打印用法；常驻请执行 `AniSync.exe tray`。

## 样例配置

`samples/` 里有三份完整配置 —— 追新番只有三种情况，整份复制成 `config.yaml`
改掉 `instances` 就能用：

| 样例 | 主 RSS | 副 RSS | 适合谁 |
| --- | --- | --- | --- |
| [01-只追放流](samples/01-只追放流.yaml) | 放流组 | 另一个放流组 | 不要中文字幕，只要第一时间拿到片子 |
| [02-只要字幕组](samples/02-只要字幕组.yaml) | 字幕组 | 不配 | 只认一家字幕，缺集宁可等 |
| [03-字幕组加放流](samples/03-字幕组加放流.yaml) | 字幕组 / 压制组 | 放流组 | 想要中文字幕，又不愿意为了等字幕组漏集 |

组名单与黑名单是**单独的文件**，主配置只留引用（复制时一起拿，放同一个目录）：

| 文件 | 是什么 |
| --- | --- |
| [samples/subs.yaml](samples/subs.yaml) | 字幕组 / 压制组排名，`scope: main` |
| [samples/raws.yaml](samples/raws.yaml) | 放流组排名，`scope: both`，priority 从 300 起 |
| [samples/deny.yaml](samples/deny.yaml) | 黑名单：明确不要的组 |

```yaml
rules:
  maxStandby: 1                            # 保留几条副 RSS；默认 1
  primaryFiles: [subs.yaml, raws.yaml]     # 组名单
  denyFiles: [deny.yaml]                   # 黑名单
```

名单里每条规则都写了 `in: group`：只看组名，不看资源标题。写 ANI-RSS 选组界面上显示的名字就行 ——
种子标题里印出来的名字会自动算成这个组的别名（`猎户发布组` / `[猎户压制部]`），写哪个都能命中。

[samples/README.md](samples/README.md) 讲了怎么挑、怎么改，以及怎么把样例丢给 AI 帮你改。

## 配置

配置文件 `config.yaml` 与程序位于同一目录，也可用 `--config` 指定。完整示例见 [config.example.yaml](config.example.yaml)。

```yaml
listen: 127.0.0.1:51837          # 设置页端口，五位数高位端口

sources:
  instance: ""                   # 站点数据取自哪个实例；留空 = 第一个启用的实例
  order: [anibt, nyaa]           # 两家互补；animegarden / mikan 也能写，但和 anibt 同类
  cacheMinutes: 720              # 站点数据缓存时长；0 = 不缓存

rules:
  maxStandby: 1                  # 保留几条副 RSS；默认 1
  primary:
    - {name: 简日内嵌, match: '(?i)简日|CHS', priority: 10, scope: main, in: title}
    # 排字幕组用 in: group：只看组名。写选组界面上显示的名字就行，
    # 种子标题里的名字（[猎户压制部]）会自动算成别名，写哪个都能命中。
    - {name: 猎户发布组, match: '(?i)猎户发布组', priority: 40, scope: main, in: group}
  # 组名单与黑名单放外置文件，主配置只留引用 —— 换口味只改文件，这里一个字不用动
  primaryFiles: [subs.yaml, raws.yaml]   # 字幕组走 main，放流组走 both
  denyFiles: [deny.yaml]         # 黑名单：命中即剔除
  deny:
    - {match: '合集|BDRip', why: 不要合集}   # 个人口味留在主配置里

instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: '${ANISYNC_KEY_MAIN}'}

sync:
  intervalMinutes: 60            # 后台自动爬取间隔，单位分钟；0 = 不自动
  preview: false
  addMissing: false
  # 全局兜底，一般留空：正常用法是按番剧写在下面的 targets 里
  # season: 1
  # offset: 0
  # standbyOffset: 0

# 本季新番：不用打开 ASS 的 WebUI 加订阅
discover:
  enabled: true                  # 命令行 --discover 等价
  sources: [yuc]                 # Yuc 只给「这一季有哪些番」，bgmId 与封面由 Bangumi 补
  limit: 50                      # 每个实例单次最多新增多少条
  enable: false                  # 新订阅是否直接启用；false = 先只建不下载
  # exclude: ['合集', '(?i)国漫|国创']   # 标题黑名单正则
  # 想换成 animegarden（只列真的有字幕组在做的）时再加 days: 180

# 按番剧规定季度与剧集偏移（和 ASS 一样）：季度按番剧给一个值，
# 主 RSS 与副 RSS 的集偏移分开给。命中该番剧的订阅都按这里的值写入，
# 实例里已有的订阅同样生效，不只是新增的。
targets:
  - {title: 冰之城墙, bgmId: '665390', season: 2, offset: -12, standbyOffset: -12}
```

字段说明：

- `apiKey` 支持 `${ENV_NAME}` 写法，密钥无需明文写入文件；Unix 下配置文件以 `0600` 权限保存
- 规则按声明顺序匹配，首条命中者生效；`scope` 取 `main`（默认）/ `standby` / `both`
- `in` 决定规则匹配什么：`group` 只看组名，`title` 只看资源标题，`any`（默认）两者一起看。排字幕组用 `group`，排字幕类型用 `title`
- 未命中任何规则的候选排在命中者之后，按发布热度、站点顺序、名称兜底
- 同一字幕组在不同站点的镜像按名称去重，仅保留最优者，因此副 RSS 中是不同字幕组而非同一组的多个镜像
- `targets` 里按番剧规定 `season` / `offset` / `standbyOffset`：`season` 按番剧给一个值（该番剧的所有订阅共用），`offset` 管主 RSS、`standbyOffset` 管副 RSS。匹配先按 `bgmId` 精确匹配，再按 `title` 对订阅标题做包含匹配（订阅标题形如「冰之城墙 (2026) [tmdbid=283428]」，写「冰之城墙」即可）。同一份 `targets` 兼作新增列表：加 `--add-missing` 时，实例里没有的番剧按规则新建，默认 `enable: false`
- `season` / `offset` / `standbyOffset` 留空时 AniSync 不碰订阅里已有的值。ANI-RSS 会按每个 RSS 的实际集数自动推断偏移，因此默认留空；只有确实要统一指定时才写全局值
- `enabled: false` 可临时停用某个实例

### 组名怎么写

**写 ANI-RSS 选组界面上显示的那个名字就行**，配上 `in: group`：

```yaml
- {name: 猎户发布组, match: '(?i)猎户发布组', priority: 40, scope: main, in: group}
```

`in: group` 只看组名，而组名这一侧有两个来源：

- **候选的显示名** —— `anibt` / `animegarden` / `mikan` 走实例接口，实例给什么名字就是什么名字
- **种子标题里印出来的名字** —— 从资源标题的方括号里现算（`internal/source/source.go` 的
  `GroupAliasesFromTitles`，与 ANI-RSS 的 `ItemsUtil.getSubgroup()` 同源）

所以 `猎户发布组`（显示名）和 `猎户压制部`（`[猎户压制部] 朱色假面 …`）写哪个都能命中，
不用两个都写，也不用去核对 —— 别名是跟着候选一起算出来的。

`绿茶字幕组` / `綠茶字幕組` 这种**简繁两种字形**是例外：字不一样，别名对不上，得写全。

想看某个番剧的候选都有哪些名字：

```bash
AniSync probe -site anibt -bgm 517106
```

**短名字锚在开头。** `ANi`、`Judas`、`ASW` 这类名字很短，写 `(?i)ani` 有可能蹭到别的组名，
写成 `(?i)^ani\b` 就不会。

以前这里得写 `^ani\b|\[ani\]`，还要解释「为什么不能写 `\bani\b`」—— 因为规则当时连资源标题
一起匹配，作品名「Ani ni Tsukeru Kusuri wa Nai」里的 `Ani` 会把别的组认成 ANi。
`in: group` 之后标题根本不参与匹配，这个坑就不存在了。

## 工作原理

1. **本季新番**（可选）：读 Yuc 的当季番剧表，得到这一季要播的日本番剧（Bangumi 补 `bgmId`）；
   实例里还没有的，被当成自动生成的 `targets` 进入下一步
2. 调用 `listAni` 读取各实例的订阅
3. 按 `sources.order` 依次获取候选 RSS；AniBT 走实例接口，Nyaa 走关键字搜索
4. 应用规则过滤与排序：主 RSS 取首项，副 RSS 由 `scope: standby` 规则排序后取前若干项
5. 与实例现状比对，仅将差异写回；新增的订阅先经 `rssToAni` 由实例解析，再按配置决定是否启用

写回使用 ANI-RSS 的 `setAni` 接口。该接口为整对象覆盖语义，AniSync 始终回传完整对象，以免清空未修改的字段。

## 站点适配

默认只跑前两行。后两行仍可用（老配置照常生效），只是不再进默认顺序 —— 它们和 AniBT 走同一类实例接口，换不回额外覆盖面。

| 站点 | 数据来源 | 产出形式 |
| --- | --- | --- |
| AniBT | 实例的 `aniBTGroup` 接口 | `anibt.net/rss/anime.xml?bgmId=&groupSlug=` |
| Nyaa | 关键字搜索 RSS，按标题中的 `[放流组]` 聚合 | `nyaa.si/?page=rss&q=&c=0_0&f=0` |
| AnimeGarden | 实例的 `animeGardenGroup` 接口 | `api.animes.garden/feed.xml?subject=&fansub=` |
| Mikan | 实例的 `mikanGroup` 接口（入参为番剧页地址） | `mikanani.me/RSS/Bangumi?bangumiId=&subgroupid=` |

排查单个番剧未匹配的原因：

```bash
AniSync probe --site mikan --bgm 665390 --title "冰之城墙"
```

## 命令

| 命令 | 说明 |
| --- | --- |
| `AniSync init` | 生成默认配置文件 |
| `AniSync check` | 校验配置，测试实例连通性，输出订阅数量 |
| `AniSync discover` | 只做自动发现，列出现在有什么番剧在放；只读，不碰实例 |
| `AniSync backup` | 把每个实例当前的订阅导出成 `data/backup/*.json` |
| `AniSync restore` | 用存档把订阅改回去；默认只还原，`--prune` 连存档里没有的订阅一起删 |
| `AniSync probe` | 单站点探针，输出候选与错误 |
| `AniSync plan` | 生成计划，只读；`--json` 输出 JSON，`--limit N` 限制条数，`--discover` 包含自动发现 |
| `AniSync sync` | 生成并执行；`--preview` 仅预览，`-y` 跳过确认，`--add-missing` / `--discover` 包含新增订阅 |
| `AniSync serve` | 启动本地 Web 设置页 |
| `AniSync tray` | 常驻托盘；`sync.intervalMinutes` 大于 0 时启用后台自动同步 |

## 文档

- [从源码构建](docs/build.md)
- [架构](docs/architecture.md)
- [开发](docs/development.md)

## License

[AGPL-3.0](LICENSE)
