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
- **主副分工**：主 RSS 按版本档挑（`subs.yaml`）、副 RSS 用放流组规则（`raws.yaml`），由规则的 `scope` 决定
- **副 RSS 点名制**：只有被 `scope: standby` 或 `scope: both` 规则点过名的组才进副 RSS，没点名的一律不要 —— 不点名就按发布条数兜底的话，会被搬运组和实例侧的伪组名刷满
- **规则外置**：字幕版本分档、放流组排名、黑名单各一个 YAML，主配置只留引用；换口味只换文件，主配置不用动
- **组名写一个就够**：规则的 `in: group` 只看组名，而组名这一侧同时包含「选组界面上的显示名」与「种子标题里印出来的名字」。两者有时并不一样（`猎户发布组` / `[猎户压制部]`），但写哪一个都能命中 —— 别名跟着候选现算，不用人去核对
- **两家站点互补**：AniBT 走实例接口（字幕组维度、API 齐全、带热度权重），Nyaa 由 AniSync 自己按关键字搜索（资源覆盖最广）。默认只跑这两家

**写回实例**

- **多实例**：一次同步，全部实例同时生效
- **按番剧规定季度与偏移**：和 ASS 一样，季度按番剧给一个值，主 RSS 与副 RSS 的集偏移分开给；实例里已有的订阅同样生效，不必逐个订阅改
- **安全默认**：预览操作只读；自动新增的订阅默认不启用，不触发下载
- **动手前能备份**：`AniSync backup` 导出订阅存档，`AniSync restore` 还原；`--prune` 连新增的一起删

**怎么用**

- **开箱即用**：`samples/` 里有五份完整配置可直接抄 —— 只追放流 / 只要字幕组 / 字幕组加放流 / 全收 / 一份配两套方案
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
  01-只追放流.yaml          五份现成配置，挑一份复制成 config.yaml 就能跑
  02-只要字幕组.yaml
  03-字幕组加放流.yaml
  04-全收.yaml
  05-双方案.yaml           同一份配置里两套方案，两个实例各挂一套
  subs.yaml               字幕版本分档：简繁日内封 / 简日内嵌 / 繁日内嵌
  raws.yaml               放流组排名（按「这个组这部番剧有没有中文」分档）
  all.yaml                「谁都行」：全收样例用，也是任何偏好名单的兜底
  deny.yaml               黑名单
```

**程序只在这个文件夹里写文件** —— `config.yaml`、`data/cache.json`、`data/backup/`、
`data/runs.jsonl`（最近若干轮同步结果）、`data/sync.lock`（单飞租约）。
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
- **同一份配置只允许一个进程在写**：常驻进程占着 `data/sync.lock` 时，再起一个 `tray`
  会直接报错退出，`sync -y` 也会拒绝写入。确认旧的已经死了就加 `--force`，细节见[无人值守](#无人值守)。
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
AniSync check           # 校验配置并测试实例连通性（顺带打出每个实例挂的方案）
AniSync status          # 每个实例挂哪套方案 / 租约归谁 / 最近几轮跑成什么样（不发网络请求）
AniSync discover        # 看现在有什么番剧在放（只读，没配实例也能跑）
AniSync backup          # 把当前订阅导出成存档（动手前先留一份）
AniSync plan            # 预览将要发生的改动，不写入
AniSync plan --discover # 预览自动发现会新增哪些订阅，不写入
AniSync sync -y         # 执行同步（写入前抢单飞租约，抢不到就报错）
AniSync restore <存档>   # 用存档把订阅改回去；加 --prune 连新增的一起删掉
AniSync tray            # 常驻托盘 + 后台定时爬取，同时提供 Web 设置页
```

Windows 下不带参数双击只打印用法；常驻请执行 `AniSync.exe tray`。

## 样例配置

`samples/` 里有五份完整配置 —— 追新番只有四种倾向，整份复制成 `config.yaml`
改掉 `instances` 就能用；第五份不是第五种倾向，而是把其中两种并到一份配置里：

| 样例 | 主 RSS | 副 RSS | 适合谁 |
| --- | --- | --- | --- |
| [01-只追放流](samples/01-只追放流.yaml) | 放流组 | 另一个放流组 | 不要中文字幕，只要第一时间拿到片子 |
| [02-只要字幕组](samples/02-只要字幕组.yaml) | 按版本挑中文字幕 | 不配 | 只认中文软字幕，缺集宁可等 |
| [03-字幕组加放流](samples/03-字幕组加放流.yaml) | 字幕组（按版本） | 放流组 | 想要中文字幕，又不愿意为了等字幕组漏集 |
| [04-全收](samples/04-全收.yaml) | 谁都行 | 其余全部 | 不挑，除了黑名单全都要 |
| [05-双方案](samples/05-双方案.yaml) | 每个实例一套 | 每个实例一套 | 追番库和收藏库各跑各的口味，一份配置管两个 |

规则文件与黑名单是**单独的文件**，主配置只留引用（复制时一起拿，放同一个目录）：

| 文件 | 是什么 |
| --- | --- |
| [samples/subs.yaml](samples/subs.yaml) | 字幕**版本**分档（简繁日内封 100 > 简日内嵌 200 > 繁日内嵌 300），`scope: main`、`in: title` |
| [samples/raws.yaml](samples/raws.yaml) | 放流**组**排名（有中文的 ToonsHub / Nix-Raws 优先，400 / 450 / 500 三档），`scope: both` |
| [samples/all.yaml](samples/all.yaml) | 「谁都行」（`match: '.+'`，`scope: both`）—— 04 号全收用它，也是任何偏好名单的兜底 |
| [samples/deny.yaml](samples/deny.yaml) | 黑名单：明确不要的组 |

五份样例各自要哪些文件：01 要 `raws.yaml` + `deny.yaml`；02 要 `subs.yaml` + `deny.yaml`；
03 要 `raws.yaml` + `subs.yaml` + `deny.yaml`（顺序就是 raws 在前）；04 要 `all.yaml` + `deny.yaml`；
05 要 `raws.yaml` + `subs.yaml` + `all.yaml` + `deny.yaml`（两套方案合起来用到的全拿）。

```yaml
rules:
  maxStandby: 1                            # 保留几条副 RSS；默认 1
  primaryFiles: [raws.yaml, subs.yaml]     # 规则文件：顺序就是匹配顺序
  denyFiles: [deny.yaml]                   # 黑名单
```

`primaryFiles` 的顺序就是匹配顺序：规则按声明顺序匹配，**第一条命中的生效**，`priority` 数字只决定同一档内的先后。
03 号样例把 `raws.yaml` 写在前面，点过名的放流组先落进 400 / 450 / 500 档，不会再往下掉进 `subs.yaml` 的版本档 ——
否则 Nix-Raws 标题里的 `[简繁内封]` 会和 LoliHouse 同档，再靠发布条数挤掉字幕组（实测发生过 6 部）。

`subs.yaml` 按**版本**排（`in: title`，读标题里的 `[简繁内封]` / `[简日内嵌]` 这类标签）；
`raws.yaml` 按**组**排。按组排时写 ANI-RSS 选组界面上显示的名字就行 ——
种子标题里印出来的名字会自动算成这个组的别名（`猎户发布组` / `[猎户压制部]`），写哪个都能命中。

[samples/README.md](samples/README.md) 讲了怎么挑、怎么改，以及怎么把样例丢给 AI 帮你改。

## 配置

配置文件 `config.yaml` 与程序位于同一目录，也可用 `--config` 指定。完整示例见 [config.example.yaml](config.example.yaml)。

```yaml
listen: 127.0.0.1:51837          # 设置页端口，五位数高位端口

sources:
  instance: ""                   # 站点数据取自哪个实例；留空 = 第一个启用的实例
  order: [anibt, nyaa]           # 默认两家；animegarden 是另一批组，要字幕组就加进来
  cacheMinutes: 720              # 站点数据缓存时长；0 = 不缓存

rules:
  maxStandby: 1                  # 保留几条副 RSS；默认 1
  primary:
    # 按版本分档：简繁日内封 > 简日内嵌 > 繁日内嵌。规则按声明顺序匹配，
    # 第一条命中的生效 —— 简繁必须排在繁日前，否则 [简繁日内封] 会先落进繁日档。
    - {name: 简繁日内封, match: '(?i)简繁|簡繁|CH[ST]&CH[ST]', priority: 100, scope: main, in: title}
    - {name: 简日内嵌,   match: '(?i)简日|簡日|简体|簡體|简中|簡中|\bCHS\b|\bGB\b|GB_CN', priority: 200, scope: main, in: title}
    - {name: 繁日内嵌,   match: '(?i)繁日|繁体|繁體|繁中|\bCHT\b|BIG5', priority: 300, scope: main, in: title}
    # 按组排用 in: group：只看组名。写选组界面上显示的名字就行，
    # 种子标题里的名字（[猎户压制部]）会自动算成别名，写哪个都能命中。
    # 40 比版本档都小，所以这条会盖过上面的版本分档 —— 想给某个组开小灶就这么写。
    - {name: 猎户发布组, match: '(?i)猎户发布组', priority: 40, scope: main, in: group}
  # 规则文件与黑名单放外置文件，主配置只留引用 —— 换口味只改文件，这里一个字不用动
  primaryFiles: [raws.yaml, subs.yaml]   # 顺序即匹配顺序：先放流组定档，再按版本分档
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
- `maxStandby` 决定每条订阅保留几条副 RSS（默认 1）。ANI-RSS 抓每个副 RSS 之间要 `sleep 1` 秒（`ItemsUtil.java:54`），所以 N 条副 RSS = 每条订阅每轮多花 N 秒
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

## 不同实例跑不同方案

一套顶层 `rules` 只能服务一种使用倾向，卡在两条上：

- **`maxStandby` 只有一档。** 追番只要一个备胎，收藏想收满 12 条。调到 12，追番那边每条订阅每轮要多花 12 秒（ANI-RSS 抓每个副 RSS 之间 `sleep 1` 秒，`ItemsUtil.java:54`）；调到 1，收藏库就只能收一个备胎。
- **`sources.order` 也只有一档。** 追番不需要 Nyaa（那边几乎全是放流组），收藏恰恰要它去补 Erai-raws / SubsPlease / Judas。

写成一套，等于让其中一个实例将就。`profiles` 就是把这份将就去掉：实例是「谁在用」，方案是「怎么用」，一个实例挂一套。

```yaml
profiles:
  - name: 追番
    rules:
      maxStandby: 1
      primaryFiles: [raws.yaml, subs.yaml]   # 顺序即匹配顺序，raws 在前
      denyFiles: [deny.yaml]
      deny:
        - {match: '(?i)bdrip', why: 追番不等 BDRip}
    sources:
      order: [anibt, animegarden]

  - name: 收藏
    rules:
      maxStandby: 12
      primaryFiles: [raws.yaml, subs.yaml, all.yaml]   # 兜底必须排最后
      denyFiles: [deny.yaml]
    sources:
      order: [anibt, animegarden, mikan, nyaa]

instances:
  - {name: 追番库, baseUrl: 'https://ass-a.example.com/', apiKey: '${ANISYNC_KEY_A}', profile: 追番}
  - {name: 收藏库, baseUrl: 'https://ass-b.example.com/', apiKey: '${ANISYNC_KEY_B}', profile: 收藏}
```

完整可运行的一份见 [samples/05-双方案.yaml](samples/05-双方案.yaml)。

生效规则八条 —— 写错不会报错的地方都在这里：

1. 实例**不写** `profile` → 用顶层 `rules` + `sources.order`，与分方案之前逐字节一致，老配置一个字段都不用改
2. 实例**写了** `profile` → 用那套方案的 `rules`（内联 `primary` / `deny` 在前，`primaryFiles` / `denyFiles` 按声明顺序追加）与它自己的 `sources.order`；顶层那套此时完全不参与
3. 方案**没写** `rules.maxStandby`（或写 0）→ 继承顶层 `rules.maxStandby`
4. 方案**没写** `sources` → 用顶层 `sources.order`
5. 规则文件路径同样按主配置所在目录解析；两套方案引用同一份文件是正常用法（各自读一次、各自生效），也可以各引各的
6. `targets` 里某条写了 `profile` → **命中它的订阅**按那套方案挑，实例挂的那套不参与（季度与偏移仍按 target 走，见下「按番剧覆盖」）
7. `check` 会当场拦下：方案名为空或重复；实例挂了不存在的方案、`targets` 挂了不存在的方案（报错里都列出可选方案名）；`sources.order`（顶层或方案内）出现不认识的站点名 —— 合法值只有 `anibt` / `animegarden` / `mikan` / `nyaa`
8. 设置页的「订阅方案」面板能加方案、改规则、勾站点顺序、填外置文件清单；保存时整份配置回传，所以 `profiles` 原样存回去，不会被洗掉。**但外置规则文件的内容仍只能在文件里改** —— 面板只显示「这套方案引用了哪几个文件、各几条」。

**方案是一套完整的规则表，不是「在顶层之上加几条」。** 上例的「收藏」没写 `deny`，它就真的没有黑名单条目：哪怕顶层 `rules.deny` 里写着不要 `BDRip`，收藏照样收 BDRip（那正是它要的）。这条最容易被当成「合并」，实际是「覆盖」。

怎么确认它生效：

```bash
AniSync check    # 每个实例一行：方案 ｜ 站点顺序 ｜ 白名单/黑名单条数 ｜ 副 RSS 上限
AniSync status   # 同一张表 + 租约 + 最近几轮；不发任何网络请求
AniSync plan     # 计划顶部的「实例与方案」表：哪个实例用哪套、站点顺序、规则条数
```

`check` 里那两行长这样（数字随你自己的规则文件变）：

```
   · 追番库         方案 追番 ｜ anibt>animegarden ｜ 白名单 7 / 黑名单 7 ｜ 副 RSS ≤ 1
   · 收藏库         方案 收藏 ｜ anibt>animegarden>mikan>nyaa ｜ 白名单 8 / 黑名单 6 ｜ 副 RSS ≤ 12
```

显示「默认（顶层 rules）」说明那个实例没写 `profile`。要是写了一个不存在的方案名，`check` 会直接报错并列出可选方案名，不会悄悄退回顶层规则。

### 按番剧覆盖

实例级的方案解决「这台机器怎么用」，但还有更细的一档：整库照常追番，就那几部本命要全收。为 3 部番另开一个实例是过度设计 —— 规则本来就是按番剧挑的，所以 `targets` 里也能写 `profile:`：

```yaml
targets:
  - {title: 冰之城墙, bgmId: '665390', profile: 收藏}   # 本命：按收藏档全收
  - {title: 药屋少女的呢喃, bgmId: '400602'}            # 不写 profile = 照实例那套
```

命中的订阅按覆盖的那套挑（规则、站点顺序、副 RSS 上限全换），**实例上挂的那套不参与**；季度与偏移仍按这条 target 自己的 `season` / `offset` / `standbyOffset` 走，两件事互不影响。写了一个不存在的方案名，`check` 会当场报错并列出可选方案名。

可见性上只做一处提示：`AniSync plan` 里**只有和实例方案不一样**的行才追加 `（按番剧覆盖方案：收藏）`，其余行保持原样不刷屏。`plan --json` 里每条动作都带 `profile` 字段，记的就是这条订阅实际生效的方案名 —— 想批量核对「哪几条被覆盖过」看它。

## 无人值守

`AniSync tray` 是常驻的那一档：托盘图标 + Web 设置页 + 后台定时同步。要它真的能无人值守，下面五件事得成立 —— 它们解决的不是「跑得动」，而是「跑错了你还看不看得见」。

- **每轮重新读配置。** 每轮都重新加载 `config.yaml` 与外置规则文件，设置页里改完规则不用重启进程。
- **启动后先等一个间隔。** 双击启动就立刻重写一百多条订阅是个会让人措手不及的惊喜，所以默认不抢跑；`AniSync tray --sync-now` 表示「启动就立刻跑一轮」。
- **间隔是每轮重取的。** `sync.intervalMinutes` 改成 `0` 就停；停着的时候改回非 0 会**立刻**跑一轮，而不是先等一个完整间隔。
- **睡眠切成小段。** 等待被切成不超过 30 秒的片段，每段结束时重算「到点没有」。所以上面两条的改动半分钟内就生效，不用等到下一次醒来。
- **一轮崩了不带走进程。** 单轮同步的 `panic` 会被兜住并记进运行日志，进程继续跑下一轮；失败（代理抖动、实例 502）按 1 / 2 / 5 / 10 / 30 分钟退避重试。
- **同一份配置只允许一个进程在写。** 因为 ANI-RSS 的 `setAni` 是**全量覆盖**：两个进程各读一份 `listAni`、各算一份目标状态、各自整份写回，后写的盖掉先写的 —— 不报错，只是主 RSS 在两个组之间来回跳。

### 单飞租约

`data/sync.lock` 是一份心跳租约，TTL 10 分钟、每 2 分钟续一次：

- `AniSync tray` 启动时抢不到 → 直接报错退出，不接管
- `AniSync serve` 同样要抢（设置页里也能点同步），所以 `serve` 和 `tray` 不能同时开 —— 其实它们本来也要抢同一个监听端口
- `AniSync sync -y` 在**算计划之前**就要抢（读-改-写整段互斥），抢不到就报错；只读的 `plan` / `--preview` 不需要
- 三处都支持 `--force`：确认上一个进程已经被你杀掉、但租约还没到过期时间时用它接管
- 谁已经被接管了，自己也会知道：续约会校验归属，发现不是自己就打印一行错误、停止后台写入，并记进运行日志

判断靠心跳而不是进程存活，所以被强杀之后最多等一个 TTL 就能被别人接手，急的时候用 `--force`。

### 怎么确认它还活着

托盘模式在 Windows 上双击运行没有控制台，所以「它昨天到底跑没跑」不能靠 stdout 回答：

```bash
AniSync status
AniSync status --json --limit 20    # 给脚本或监控用
```

输出三块，正好对应上面那几件事：

- **实例与方案** —— 每个实例挂的方案、站点顺序、规则条数、用到的外置规则文件
- **租约** —— 谁在持有（pid + 用途）、最后一次心跳在多久之前。显示「空闲」说明现在没有常驻进程在写；显示「已经超过 TTL」说明上一个进程可能死在半路，下一个 `tray` 会直接接管
- **最近 N 轮** —— 时间、成功/失败、更新/新增/跳过条数、逐条失败原因、被兜住的崩溃、下次运行时间

原始数据在 `data/runs.jsonl`（一行一轮，保留最近 200 轮），可以直接打开看，`status` 读的就是它。**一条记录都没有**通常意味着常驻进程根本没起来 —— 这是最容易看错的一处：在托盘模式里，「一直失败」和「根本没在跑」的体感是一样的。

## 工作原理

1. **本季新番**（可选）：读 Yuc 的当季番剧表，得到这一季要播的日本番剧（Bangumi 补 `bgmId`）；
   实例里还没有的，被当成自动生成的 `targets` 进入下一步
2. 调用 `listAni` 读取各实例的订阅
3. 按**该实例所挂方案**的 `sources.order` 依次获取候选 RSS；AniBT 走实例接口，Nyaa 走关键字搜索
4. 用**该实例所挂方案**的规则过滤与排序：主 RSS 取首项，副 RSS 由 `scope: standby` / `both` 规则排序后取前若干项。
   某部番在 `targets` 里写了 `profile` 时，这一条改用它覆盖的那套方案（规则、站点顺序、副 RSS 上限一起换）
5. 与实例现状比对，仅将差异写回；新增的订阅先经 `rssToAni` 由实例解析，再按配置决定是否启用

写回使用 ANI-RSS 的 `setAni` 接口。该接口为整对象覆盖语义，AniSync 始终回传完整对象，以免清空未修改的字段。

## 站点适配

默认只跑前两行。后两行仍可用（老配置照常生效），只是不再进默认顺序 —— 不过 AnimeGarden 不是 AniBT 的备份，而是**另一批组**：实测 121 部番剧里，只在它那边出现的「番剧 × 字幕组」组合有 111 个（喵萌奶茶屋 16、猎户发布组 9 …），只在 AniBT 出现的多是放流组（ToonsHub 60 部）。要中文字幕就把 AnimeGarden 写进 `sources.order`（02 / 03 / 04 / 05 号样例都写了）。

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
| `AniSync check` | 校验配置，测试实例连通性，输出订阅数量；顺带打出每个实例挂的方案 |
| `AniSync status` | 每个实例挂哪套方案、租约归谁、最近几轮跑成什么样；`--json` / `--limit N`，不发任何网络请求 |
| `AniSync discover` | 只做自动发现，列出现在有什么番剧在放；只读，不碰实例 |
| `AniSync backup` | 把每个实例当前的订阅导出成 `data/backup/*.json` |
| `AniSync restore` | 用存档把订阅改回去；默认只还原，`--prune` 连存档里没有的订阅一起删 |
| `AniSync probe` | 单站点探针，输出候选与错误 |
| `AniSync plan` | 生成计划，只读；`--json` 输出 JSON，`--limit N` 限制条数，`--discover` 包含自动发现 |
| `AniSync sync` | 生成并执行；`--preview` 仅预览，`-y` 跳过确认，`--add-missing` / `--discover` 包含新增订阅，`--force` 忽略已有租约 |
| `AniSync serve` | 启动本地 Web 设置页 |
| `AniSync tray` | 常驻托盘；`sync.intervalMinutes` 大于 0 时启用后台自动同步，`--sync-now` 启动即跑一轮，`--force` 接管已有租约 |

## 文档

- [从源码构建](docs/build.md)
- [架构](docs/architecture.md)
- [开发](docs/development.md)

## License

[AGPL-3.0](LICENSE)
