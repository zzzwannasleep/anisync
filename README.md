# assm

多 ANI-RSS 实例的主副 RSS 自动打点工具。

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8.svg)](https://go.dev)
[![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-lightgrey.svg)](#安装)

## 简介

assm 读取 ANI-RSS 实例中的订阅列表，按设定的站点优先级爬取字幕组与放流组信息，依据正则与优先级规则选出主 RSS 与备用 RSS，并写回各实例。

适用于同时维护多个 ANI-RSS 实例、且对字幕组有固定偏好的场景。

## 特性

- **多实例**：一次同步，全部实例同时生效
- **规则驱动**：正则匹配 + 优先级排序，可限定规则仅在指定站点生效
- **站点优先级**：按配置顺序爬取，默认 AniBT → AnimeGarden → Mikan → Nyaa
- **安全默认**：预览操作只读；新增订阅默认不启用，不触发下载
- **多形态**：CLI、Windows 通知区域托盘、macOS 菜单栏应用、本地 Web 设置页
- **单一二进制**：7–8 MB，无运行时依赖，可交叉编译 Windows / macOS / Linux

## 安装

### 预编译版本

从 [Releases](https://github.com/zzzwannasleep/assm/releases) 下载。

| 平台 | 文件 | 形态 |
| --- | --- | --- |
| Windows amd64 / arm64 | `assm-windows-*.exe` | 托盘 + CLI |
| macOS arm64 / amd64 | `assm-<版本>-macos-*.app.zip` | 菜单栏应用 |
| macOS arm64 / amd64 | `assm-darwin-*` | CLI |
| Linux amd64 / arm64 | `assm-linux-*` | CLI + Web 设置页 |

未签名的 `.app` 首次打开会被 Gatekeeper 拦截，可右键选择「打开」，或执行：

```bash
xattr -dr com.apple.quarantine /Applications/assm.app
```

### 从源码构建

要求 Go 1.24 或更高版本。

```bash
go build -o assm ./cmd/assm
```

macOS 菜单栏图标由 Objective-C 实现，依赖 cgo，须在 macOS 上构建：

```bash
CGO_ENABLED=1 go build -o assm ./cmd/assm
bash scripts/package-macos.sh ./assm 0.1.0 arm64    # 打包为 .app
```

批量交叉编译见 `scripts/build-all.sh` 与 `scripts/build-all.ps1`。

## 快速开始

```bash
assm init          # 生成配置文件
assm check         # 校验配置并测试实例连通性
assm plan          # 预览将要发生的改动，不写入
assm sync -y       # 执行同步
assm tray          # 常驻托盘（同时提供 Web 设置页）
assm serve         # 仅启动本地设置页，默认 http://127.0.0.1:7788
```

## 配置

配置文件位于 `%APPDATA%\assm\config.yaml`（Windows）或 `~/.config/assm/config.yaml`（macOS / Linux），也可通过 `--config` 指定。完整示例见 [`config.example.yaml`](config.example.yaml)。

```yaml
sources:
  order: [anibt, animegarden, mikan, nyaa]    # 站点优先级，自高至低

rules:
  primary:                                     # 正则 + 优先级，数值越小越优先
    - {name: 桜都字幕组, match: '(?i)sakurato|桜都', priority: 10}
    - {name: LoliHouse, match: '(?i)lolihouse', priority: 20}
    - {name: Nyaa 上的 ToonsHub, match: '(?i)toonshub', priority: 50, site: nyaa}
  deny:                                        # 命中即丢弃
    - {match: '合集|BDRip', why: 不要合集}
  maxStandby: 3                                # 每个订阅保留的备用 RSS 数量

instances:
  - {name: 主库,   baseUrl: 'https://ass.example.com/',    apiKey: '${ASSM_KEY_MAIN}'}
  - {name: 备用库, baseUrl: 'http://192.168.1.10:7789/',   apiKey: '${ASSM_KEY_BACKUP}'}
```

字段说明：

- `apiKey` 支持 `${ENV_NAME}` 形式，密钥无需明文写入文件；配置文件在 Unix 下以 `0600` 权限保存
- 规则按声明顺序匹配，首条命中者生效；未命中任何规则的候选排在末尾，并按站点热度（发布量、资源数）兜底排序
- 同一字幕组在不同站点的镜像会按名称去重，仅保留最优者，因此备用 RSS 中是不同字幕组而非同一组的多个镜像
- `enabled: false` 可临时停用某个实例

## 工作原理

1. 调用 `listAni` 读取各实例的订阅
2. 按 `sources.order` 依次爬取候选 RSS
3. 应用规则过滤与排序，取首项为主 RSS，其后若干项为备用 RSS
4. 与实例现状比对，仅将差异写回

写回使用 ANI-RSS 的 `setAni` 接口。该接口为整对象覆盖语义，assm 始终回传完整对象，以免清空未修改的字段。

## 站点适配

| 站点 | 数据来源 | 产出形式 |
| --- | --- | --- |
| AniBT | `GET /api/anime/groups?bgmId=` | `anibt.net/rss/anime.xml?bgmId=&groupSlug=` |
| AnimeGarden | `GET /api.animes.garden/resources?subject=`，按 fansub 聚合 | `api.animes.garden/feed.xml?subject=&fansub=` |
| Mikan | 标题搜索，再核对详情页的 bgm 链接 | `mikanani.me/RSS/Bangumi?bangumiId=&subgroupid=` |
| Nyaa | `?page=rss&q=` 搜索，按标题中的 `[放流组]` 聚合 | 限定搜索词的 Nyaa RSS（`type=other`） |

排查单个番剧未匹配的原因：

```bash
assm probe --site mikan --bgm 622288 --title "FX战士久留美 (2026) [tmdbid=311842]"
```

## 命令

| 命令 | 说明 |
| --- | --- |
| `assm init` | 生成默认配置文件 |
| `assm check` | 校验配置，测试实例连通性，输出订阅数量 |
| `assm probe` | 单站点探针，输出候选与错误 |
| `assm plan` | 生成计划，只读；`--json` 输出 JSON，`--limit N` 限制条数 |
| `assm sync` | 生成并执行；`--dry-run` 仅预览，`-y` 跳过确认，`--add-missing` 包含新增订阅 |
| `assm serve` | 启动本地 Web 设置页 |
| `assm tray` | 常驻托盘；`sync.intervalMinutes` 大于 0 时启用定时同步 |

## 架构

```
cmd/assm             命令行入口
internal/config      配置读写（YAML、环境变量展开、文件权限）
internal/assapi      ANI-RSS API 客户端
internal/source      站点适配层（anibt / animegarden / mikan / nyaa）
internal/rules       规则打分与去重
internal/planner     计划生成与执行
internal/webui       本地设置页（内嵌资源，无前端构建）
internal/tray        托盘（Windows 纯 Go；macOS 依赖 cgo）
scripts/             构建与打包脚本
```

## 开发

```bash
go vet ./...
go test ./...
go build ./...
```

CI 覆盖 Windows / macOS / Linux 六个目标，macOS 目标会校验二进制已链接 Cocoa，以确认菜单栏代码确实编入。

## 已知限制

- ANI-RSS 官方的 mikanId 与 bgmId 映射依赖付费缓存，assm 改用标题搜索加详情页反查；搜索无结果时跳过该站点的候选，不影响其他站点
- Nyaa 无字幕组维度，仅能按 `[放流组]` 前缀聚合，产出为搜索型 RSS
- macOS 菜单栏版本须在 macOS 上构建，本仓库的 Windows 本地构建脚本产出的 darwin 文件不含菜单栏图标

## 许可证

[GNU Affero General Public License v3.0](LICENSE)。

允许自由使用、修改与分发。分发修改版本须同样以 AGPL-3.0 授权；若通过网络提供服务，须向使用者提供完整源码。本软件不提供任何担保。
