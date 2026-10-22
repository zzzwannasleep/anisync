# assm · 多 ANI-RSS 实例主副 RSS 自动打点

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey.svg)](#安装)

**你不用再手动给每个 ASS 实例挑主副 RSS。** 规则写一次，assm 按站点优先级爬一遍，自动把主 RSS 和备用 RSS 打点进所有实例。

```
                    ┌──────────────┐
   anibt  ─┐        │  ANI-RSS 主库 │  ← 主 RSS: Ani.url
 animegarden ─┼─ 规则 ─→ │              │    副 RSS: Ani.standbyRssList[]
   mikan  ─┤  正则+优先级 └──────────────┘
   nyaa   ─┘        │  ANI-RSS 备用库 │  ← 同一套规则，一起打点
                    └──────────────┘
```

- 单个静态二进制，**7~8 MB**，无运行时依赖（不用 Node / Python / JVM）
- 一套代码交叉编译 **Windows / macOS / Linux**（amd64 + arm64）
- 同一个二进制既是 **CLI**（Linux 服务器友好），也是 **Windows 原生托盘程序**（纯 Go，不需要 cgo），还自带一个**本地 Web 设置页**
- 不 mock、不猜接口：站点适配全部对照 [ani-rss](https://github.com/wushuo894/ani-rss) 源码与线上真实响应实现

---

## 为什么是 Go，为什么不是浏览器插件

| 方案 | 结论 |
| --- | --- |
| 浏览器插件 | **删掉**。管不了多实例、爬不动站点、挂不了后台，多实例要开多个浏览器配置 |
| Electron / Node | **删掉**。100MB 起步的运行时，跟"占用极小"直接冲突 |
| Python / Rust | 你明确排除 |
| **Go** | 单文件 7~8MB、零依赖、6 个平台交叉编译、Windows 托盘纯 Go、同一份代码同时是 CLI 和 GUI 后端 |

## 它解决什么

| 痛点 | assm 的做法 |
| --- | --- |
| 管多个 ASS 实例，每次都要挨个登录改 | 配置里加多个实例，一次同步全部打点 |
| 每次订阅都要手动找主 RSS + 副 RSS | 按「正则 + 优先级」自动选主 RSS，其余按优先级填备用 RSS |
| 想按站点偏好爬（先 anibt、再 nyaa、再 mikan） | `sources.order` 定义站点优先级，命中规则时优先用高优先级站点 |
| 想在后台常驻，还要能一键退出 | `assm tray`：Windows 通知区域图标，右键「立即同步 / 打开设置页 / 退出」 |
| Linux 服务器没有图形界面 | 同一个二进制跑 `assm sync`（可配 cron / systemd timer） |

## 安装

```bash
# 需要 Go 1.24+
go build -o assm ./cmd/assm

# 交叉编译全部平台
pwsh ./scripts/build-all.ps1      # Windows
bash ./scripts/build-all.sh       # macOS / Linux
```

产物：

| 平台 | 文件 | 托盘 |
| --- | --- | --- |
| Windows amd64 / arm64 | `assm-windows-*.exe` | ✅ 原生托盘（纯 Go，免 cgo） |
| macOS arm64 / amd64 | `assm-darwin-*` | ➖ 目前用 Web 设置页；原生菜单栏图标需 cgo + Mac 构建，见路线图 |
| Linux amd64 / arm64 | `assm-linux-*` | ➖ CLI + Web 设置页 |

## 快速开始

```bash
assm init                     # 生成 ~/.config/assm/config.yaml
# 编辑配置：填实例、apiKey、规则、站点顺序
assm check                    # 校验配置 + 测试每个实例连通性
assm plan --limit 5           # 只读预览：看看会改成什么样
assm sync --dry-run           # 同上，明确不写入
assm sync -y                  # 真正写入
assm serve                    # 打开 http://127.0.0.1:7788 用图形界面配置
assm tray                     # 常驻托盘（同时提供设置页）
```

## 配置

见 [`config.example.yaml`](config.example.yaml)。核心四块：

```yaml
sources:
  order: [anibt, animegarden, mikan, nyaa]   # 站点优先级，从上到下
rules:
  primary:                                    # 正则 + 优先级，数字越小越优先
    - {name: 桜都字幕组, match: '(?i)sakurato|桜都', priority: 10}
    - {name: LoliHouse, match: '(?i)lolihouse', priority: 20}
    - {name: Nyaa 上的 ToonsHub, match: '(?i)toonshub', priority: 50, site: nyaa}
  deny:
    - {match: '合集|BDRip', why: 不要合集}
  maxStandby: 3                               # 每个订阅保留几条备用 RSS
instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: '${ASSM_KEY_MAIN}'}
  - {name: 备用库, baseUrl: 'http://192.168.1.10:7789/', apiKey: '${ASSM_KEY_BACKUP}'}
```

- `apiKey` 支持 `${ENV_NAME}`，密钥不必明文落盘；配置文件在 Unix 下按 `0600` 写入
- 规则命中第一条即生效；**没命中任何规则**的候选排最后，并按站点热度（发布量/资源数）兜底排序
- **同一个字幕组跨站点的镜像会去重**，只保留最优的那条，所以备用 RSS 里是不同字幕组而不是同一组的多个镜像
- `site: nyaa` 可把规则限定在某个站点

## 站点适配（真实接口）

| 站点 | 用什么 | 产出 |
| --- | --- | --- |
| AniBT | `GET /api/anime/groups?bgmId=` | `https://anibt.net/rss/anime.xml?bgmId=&groupSlug=` |
| AnimeGarden | `GET /api.animes.garden/resources?subject=` 按 fansub 聚合 | `https://api.animes.garden/feed.xml?subject=&fansub=` |
| Mikan | 标题搜索 + 详情页反查 bgm 链接（并发核对） | `https://mikanani.me/RSS/Bangumi?bangumiId=&subgroupid=` |
| Nyaa | `?page=rss&q=` 搜索，按 `[放流组]` 聚合 | 限定搜索词的 nyaa RSS（`type=other`） |

排障用 `assm probe`：

```bash
assm probe --site mikan --bgm 622288 --title "FX战士久留美 (2026) [tmdbid=311842]"
```

## 安全默认值

- `assm plan` / `assm sync --dry-run` **只读**，不写任何东西
- 真正写入需要 `-y`，否则只打印计划
- `targets` 自动新增的订阅默认 **`enable: false`**（ANI-RSS 源码里 `addAni` 对 `enable=false` 只统计集数、不触发下载）
- 只改动主 RSS / 字幕组 / 备用 RSS 三个字段：由于 ANI-RSS 的 `setAni` 是**整对象覆盖**语义（hutool `copyProperties` 连 null 一起拷），assm 从不构造局部对象，而是把 `listAni` 拿到的原始对象改完再整份回传，避免把其它字段洗掉
- 配置文件里的 `apiKey` 支持环境变量展开，仓库里不带任何密钥

## 命令一览

| 命令 | 作用 |
| --- | --- |
| `assm init` | 生成默认配置 |
| `assm check` | 校验配置 + 测试实例连通性 + 拉取订阅数 |
| `assm probe --site X --bgm Y` | 单站点探针，排查「为什么没匹配到」 |
| `assm plan [--json] [--limit N]` | 只生成计划 |
| `assm sync [--dry-run] [-y] [--add-missing]` | 生成并执行 |
| `assm serve [--listen addr]` | 本地 Web 设置页 |
| `assm tray` | 托盘常驻（Windows），可配 `sync.intervalMinutes` 定时同步 |

## 架构

```
cmd/assm            CLI 入口（init/check/probe/plan/sync/serve/tray）
internal/config     配置读写（YAML、${ENV} 展开、0600 权限）
internal/assapi     ANI-RSS API 客户端（about/listAni/setAni/addAni/deleteAni/rssToAni/*Group）
internal/source     站点适配层（anibt / animegarden / mikan / nyaa）
internal/rules      正则 + 优先级打分与去重
internal/planner    生成计划（并发爬取 + 缓存 + diff）与执行
internal/webui      本地设置页（内嵌 HTML，无前端构建）
internal/tray       托盘（Windows 纯 Go；其它平台 stub）
```

## 开发

```bash
go vet ./...
go test ./...
go build ./...
```

## 路线图

- [ ] macOS 原生菜单栏图标（cgo + CI darwin job）
- [ ] 每个实例独立规则集
- [ ] 从实例反向导入订阅作为 `targets`
- [ ] 同步结果落盘（历史记录 + 变更审计）

## 已知限制

- ANI-RSS 的 mikanId↔bgmId 映射官方走付费缓存，assm 改用「标题搜索 + 详情页反查」，偶发搜索不到时会跳过该站点的候选（不影响其它站点）
- Nyaa 没有字幕组维度，只能按 `[放流组]` 前缀聚合，产出的 RSS 是搜索型 RSS

## 许可证

本项目采用 **GNU Affero General Public License v3.0 (AGPL-3.0)**，全文见 [LICENSE](LICENSE)。

简单说：

- ✅ 可以自由使用、修改、分发
- ⚠️ 分发修改版必须同样以 AGPL-3.0 开源
- ⚠️ **通过网络提供服务**（比如部署成公开服务给别人用）也必须向用户提供完整源码
- ❌ 不提供任何担保

Copyright (C) 2026 zzzwannasleep
