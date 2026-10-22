# AniSync

多 ANI-RSS 实例的主副 RSS 自动打点工具。

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey.svg)](#安装)

## 简介

AniSync 读取 ANI-RSS 实例中的订阅列表，按配置的站点优先级获取字幕组与放流组信息，依据正则与优先级规则选出主 RSS 与副 RSS，并写回各实例。

适用于同时维护多个 ANI-RSS 实例、且对字幕组与字幕类型有固定偏好的场景。

## 特性

- **多实例**：一次同步，全部实例同时生效
- **规则驱动**：正则匹配 + 优先级，可排字幕组，也可排简日内嵌 / 繁日内嵌 / 简繁日内封等字幕类型
- **主副分工**：主 RSS 用字幕组规则、副 RSS 用放流组规则，由规则的 `scope` 决定
- **站点数据取自实例**：AniBT / AnimeGarden / Mikan 的字幕组列表由 ANI-RSS 接口给出，不重复实现站点解析
- **安全默认**：预览操作只读；新增订阅默认不启用，不触发下载
- **多形态**：CLI、Windows 通知区域托盘、macOS 菜单栏应用、本地 Web 设置页
- **单一二进制**：无运行时依赖；配置文件与缓存都留在程序同目录，删除目录即卸载干净

## 安装

从 [Releases](https://github.com/zzzwannasleep/anisync/releases) 下载对应平台的文件，解压到任意目录。

| 平台 | 文件 | 形态 |
| --- | --- | --- |
| Windows amd64 / arm64 | `AniSync-windows-*.exe` | 托盘 + CLI |
| macOS arm64 / amd64 | `AniSync-<版本>-macos-*.app.zip` | 菜单栏应用 |
| macOS arm64 / amd64 | `AniSync-darwin-*` | CLI |
| Linux amd64 / arm64 | `AniSync-linux-*` | CLI + Web 设置页 |

未签名的 `.app` 首次打开会被 Gatekeeper 拦截，可右键选择「打开」，或执行：

```bash
xattr -dr com.apple.quarantine /Applications/AniSync.app
```

从源码构建见 [docs/build.md](docs/build.md)。

## 快速开始

```bash
AniSync init            # 生成配置文件
AniSync check           # 校验配置并测试实例连通性
AniSync plan            # 预览将要发生的改动，不写入
AniSync sync -y         # 执行同步
AniSync tray            # 常驻托盘 + 后台定时爬取，同时提供 Web 设置页
```

Windows 下不带参数双击只打印用法；常驻请执行 `AniSync.exe tray`。

## 配置

配置文件 `config.yaml` 与程序位于同一目录，也可用 `--config` 指定。完整示例见 [config.example.yaml](config.example.yaml)。

```yaml
listen: 127.0.0.1:51837          # 设置页端口，五位数高位端口

sources:
  instance: ""                   # 站点数据取自哪个实例；留空 = 第一个启用的实例
  order: [anibt, animegarden, mikan, nyaa]
  cacheMinutes: 720              # 站点数据缓存时长；0 = 不缓存

rules:
  maxStandby: 3
  primary:
    - {name: 简日内嵌, match: '(?i)简日|CHS', priority: 10, scope: main}
    - {name: 桜都字幕组, match: '(?i)sakurato|桜都', priority: 40, scope: main}
    - {name: 放流组, match: '(?i)toonshub', priority: 10, scope: standby}
  deny:
    - {match: '合集|BDRip', why: 不要合集}

instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: '${ANISYNC_KEY_MAIN}'}

sync:
  intervalMinutes: 60            # 后台自动爬取间隔，单位分钟；0 = 不自动
  preview: false
  addMissing: false
```

字段说明：

- `apiKey` 支持 `${ENV_NAME}` 写法，密钥无需明文写入文件；Unix 下配置文件以 `0600` 权限保存
- 规则按声明顺序匹配，首条命中者生效；`scope` 取 `main`（默认）/ `standby` / `both`
- 规则匹配「字幕组名 + 最近的资源标题」，因此字幕类型与字幕组可写在同一个列表里
- 未命中任何规则的候选排在命中者之后，按发布热度、站点顺序、名称兜底
- 同一字幕组在不同站点的镜像按名称去重，仅保留最优者，因此副 RSS 中是不同字幕组而非同一组的多个镜像
- `enabled: false` 可临时停用某个实例

## 工作原理

1. 调用 `listAni` 读取各实例的订阅
2. 按 `sources.order` 依次获取候选 RSS；AniBT / AnimeGarden / Mikan 走实例接口，Nyaa 走关键字搜索
3. 应用规则过滤与排序：主 RSS 取首项，副 RSS 由 `scope: standby` 规则排序后取前若干项
4. 与实例现状比对，仅将差异写回

写回使用 ANI-RSS 的 `setAni` 接口。该接口为整对象覆盖语义，AniSync 始终回传完整对象，以免清空未修改的字段。新增订阅先经 `rssToAni` 由实例解析，再以 `enable: false` 写入。

## 站点适配

| 站点 | 数据来源 | 产出形式 |
| --- | --- | --- |
| AniBT | 实例的 `aniBTGroup` 接口 | `anibt.net/rss/anime.xml?bgmId=&groupSlug=` |
| AnimeGarden | 实例的 `animeGardenGroup` 接口 | `api.animes.garden/feed.xml?subject=&fansub=` |
| Mikan | 实例的 `mikanGroup` 接口（入参为番剧页地址） | `mikanani.me/RSS/Bangumi?bangumiId=&subgroupid=` |
| Nyaa | 关键字搜索 RSS，按标题中的 `[放流组]` 聚合 | `nyaa.si/?page=rss&q=&c=0_0&f=0` |

排查单个番剧未匹配的原因：

```bash
AniSync probe --site mikan --bgm 665390 --title "冰之城墙"
```

## 命令

| 命令 | 说明 |
| --- | --- |
| `AniSync init` | 生成默认配置文件 |
| `AniSync check` | 校验配置，测试实例连通性，输出订阅数量 |
| `AniSync probe` | 单站点探针，输出候选与错误 |
| `AniSync plan` | 生成计划，只读；`--json` 输出 JSON，`--limit N` 限制条数 |
| `AniSync sync` | 生成并执行；`--preview` 仅预览，`-y` 跳过确认，`--add-missing` 包含新增订阅 |
| `AniSync serve` | 启动本地 Web 设置页 |
| `AniSync tray` | 常驻托盘；`sync.intervalMinutes` 大于 0 时启用后台自动同步 |

## 文档

- [从源码构建](docs/build.md)
- [架构](docs/architecture.md)
- [开发](docs/development.md)

## License

[AGPL-3.0](LICENSE)
