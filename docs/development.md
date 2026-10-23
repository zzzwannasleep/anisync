# 开发

## 常用命令

```bash
go vet ./...
go test ./...
go build ./...
```

## 测试

单元测试集中在四处：

- `internal/rules`：规则编译、主/副 RSS 排序、去重、`scope` 语义
- `internal/source`：关键词清洗、Nyaa 搜索地址拼接、mikan 番剧页推导、站点到 `Ani.type` 的映射、缓存裁剪
- `internal/planner`：备用 RSS 的偏移继承、偏移变化的差异识别、季度/偏移的取值优先级、番剧级配置的匹配（bgmId / 标题包含 / 限定实例）
- `internal/config`：番剧级配置的校验与规整

涉及网络的适配器不做单元测试，改用 `probe` 打真实站点验证：

```bash
AniSync probe --site anibt  --bgm 665390
AniSync probe --site mikan  --mikan 3887
AniSync probe --site nyaa   --jp "White Cat Legend S03"
```

`probe` 输出每个候选的字幕组名、热度与标题样本数，用来判断「为什么这个番剧没匹配到」。
每个候选还会多打一行 `组名可写`，列的是**这个候选在 `in: group` 规则里能用的全部名字**
（显示名 + 标题开头方括号里的别名）—— 规则里写这里面任何一个都能命中。

## 设置页（WebUI）

`internal/webui/assets/index.html` 是单文件内嵌资源，没有前端构建步骤：改完 `go build` 即生效。样式与脚本都写在这一个文件里，改完跑一遍静态检查，避免把「AI 味」的样式写回去：

```bash
npx ui-craft-detect internal/webui/assets/index.html
```

判定口径是 0 error / 0 warning：占位符不能当 label、`:hover` 必须配 `:focus-visible`、表格要有横向溢出与 sticky 表头、可见文案里的破折号不超过两条、字号层级不超过 6 档。

### 深色 / 浅色双模式

- **深色是 `:root` 的默认值，浅色是 `:root[data-theme="light"]` 的覆盖块**。只覆盖颜色类变量，尺寸 / 圆角 / 动效 / 字体两套主题共用，所以加新样式时不用考虑主题。
- `data-theme` 由 `head` 里一段**内联脚本**在首次绘制前设好。这段不能挪到主脚本里等 DOM 就绪——那样跟随系统的深色用户会先闪一帧亮色。
- 档位存在 `localStorage` 的 `anisync-theme`：`auto`（默认，跟随系统）/ `light` / `dark`。`auto` 每次现读 `prefers-color-scheme`，并监听它的变化。
- **accent 拆成三个变量**，别合并：`--accent` 是填充（主按钮底、指示条、选中边框），`--accent-hover` 是填充的悬停态，`--accent-bright` 是当文字/图标用。深色下填充和文字都是亮绿；浅色下填充要亮（配 `--accent-ink` 深色字）、文字要暗（白底上才有对比度）。合成一个变量必然有一边看不清。
- **不要在规则里写死颜色**：新增颜色一律先加变量，深浅两套都给值。写死的颜色浅色模式下改不动，会留下一块深色斑。
- `TestWebUIHasThemeSupport` 盯着上面这些：浅色色板在不在、三档按钮全不全、accent 三个变量在不在、主题脚本是不是在 `<body>` 之前。

### 圆角只有一个

`--r` 是**全站唯一的圆角**，面板 / 输入 / 按钮 / 标签 / 开关轨道 / 弹窗一律用它。以前是 6/8/12/999 四档混着来，同一屏里又圆又方，所以刻意不留第二档 —— 加新组件时不要再引入 `--r-sm` / `--r-full` 之类。唯一还保留圆形的是真正的圆（状态点、开关旋钮、转圈）和 2px 的导航指示条：给一条 2px 的线设圆角没有意义。

### 侧栏收起

收起状态由 `<html data-rail="1">` 驱动，`--side-w` 换成 `--side-w-rail` 就是全部改动；标签用 `opacity` 淡出而不是 `display:none`，否则宽度动画期间文字会先跳一下。这个属性同样由 `head` 的内联脚本在首帧前设好，存 `localStorage` 的 `anisync-rail`。窄屏（≤900px）下侧栏改成顶部横条，收起开关隐藏 —— 那一档没有可收的宽度。

**收起态的对齐有三个必须显式覆盖的属性，少一个图标就会排成两列**（踩过一次）：

```css
:root[data-rail="1"] .nav-item .lbl,
:root[data-rail="1"] .nav-item .n{flex:0 0 0px;width:0;margin:0;overflow:hidden}
:root[data-rail="1"] .nav-item{justify-content:center;gap:0;padding:9px 0}
```

- `flex:0 0 0px` —— `.lbl` / `.n` 是 flex 子项，宽度由 `flex-basis` 决定。只写 `width:0` 而 basis 仍是 `auto` 时会被内容撑开。
- `margin:0` —— `.n` 带 `margin-left:auto`。留着它，**有角标的项**被 auto 外边距顶到左边、**没角标的项**被 `justify-content:center` 推到中间，同一个列表里跑出两套对齐规则，图标正好分成两列。
- `gap:0` —— gap 夹在图标和 0 宽标签之间仍然占 10px，有没有角标差一个 gap，图标又会错开半个间距。

`TestWebUIRailKeepsIconsAligned` 钉住这两条规则。改完用 `getBoundingClientRect()` 量一遍所有 `.nav-item svg` 的 `x`，必须全部相同 —— 肉眼看「差不多齐」不算数。

### 面板标题不编号

面板标题上没有 `01` / `02` 之类的序号（曾经有过 `<span class="idx">`）。序号不携带任何信息，八块面板的先后也不代表任何流程，去掉。`TestWebUIPanelsHaveNoOrdinalBadge` 钉住。

### 本季新番

「本季新番」面板的列表整份来自 `/api/season`，前端只做渲染、勾选、写进 `cfg.targets` 三件事，不自己拼番剧列表。

**列表来源默认是 `yuc`（Yuc 当季番剧表）**，Bangumi 只负责补 `bgmId`。理由见 `internal/discover/yuc.go` 的包注释，一句话：放送表把国漫美漫 OVA 混在一起且不带地区与平台，筛干净要逐条查详情；Yuc 本身就是人工整理过的日本番剧表。

改动这块时注意三件事：

1. **匹配用日文原名（`titles.original.text`），不是中文名。** 中文译名是各家自己的写法，实测命中 46% 对日文原名 88%。
2. **`foldTitle` 只管写法差异，不管语言差异。** 全角折半角、罗马数字折 ASCII、去标点、季度写法统一。`Ⅱ` → `II` 这一步不能省 —— Yuc 用 U+2161、Bangumi 用两个 ASCII 的 `I`，不折就落不到同一个键上，会退化到检索并认成第一季。别用 `strings.ToLower` 之外的方式去猜语言。
3. **检索兜底要在全部结果里挑最像的，不能取第一条沾边的。** `bestTitleRank` 把「候选更具体」排在「候选更笼统」前面，否则续作会被认成第一季。

`bangumi-calendar` / `animegarden` / `bangumi-collections` 仍在 `config.DiscoverSourceNames` 里可用，只是不进默认值。`japanOnly` 只对 `bangumi-calendar` 生效。

面板上的「已添加」判定必须读**实例的真实订阅**（`listAni` 的 `bgmUrl`），不能只看配置里的 `targets` —— `targets` 是手写的番剧级配置，用户在 ANI-RSS 里手动加的订阅根本不在里面。实测踩过：实例里订了 121 条、本季重合 71 部，界面却显示一部都没加。实例读不到时报 `instanceError`，不要静默退化成「全部未添加」。

「添加勾选」直接落盘，不要求用户再点一次保存 —— 这一步就是「一键加番」本身。落盘只改配置文件，写进实例仍然要用户到「执行」跑同步，那一步才是不可逆的。勾选框不是配置项，所以启动时那个全局 `change` 监听要排掉 `.season-grid` 里的元素，否则勾一张卡就把「保存」点亮、点下去却什么都没保存。

### 保存按钮跟着区块走

每个会写配置文件的区块自带一个 `.js-save`（`data-panel` 指向自己的 section id），顶栏不再有全局保存入口。`dirtyPanels` 记的是**哪些区块**有改动，所以只有真正改过的那块按钮会亮起来；点任意一个都是整份保存（后端 `/api/config` 本来就是整份 PUT），存完统一清空。新增区块时别忘了补按钮，`data-panel` 要和 section 的 `id` 对上。

### 选番弹窗与封面中转

「添加番剧」走 `<dialog id="bgmPicker">`，数据来自 `/api/bgm/search`。用原生 `<dialog>` 是为了白拿焦点陷阱、Esc 关闭和背景 inert —— 这三样自己用 div 重写很容易漏掉一个，而漏掉的那个就是无障碍缺口。

**封面必须走 `/api/bgm/cover` 中转，不能直接 `<img src="http://lain.bgm.tv/...">`。** 两个原因：图床在国内直连不通（要过代理），而且它返回的是 `http://` 再 301 到 `https://`。中转端点对 host 做**精确白名单**，不是后缀匹配 —— 后缀匹配会让 `evil-lain.bgm.tv.attacker.com` 通过。改这里时把 `TestBgmCoverRejectsForeignHost` 一起看。

### 图标从 branding/ 来

品牌资产在 `branding/`（设计源 + `build.py` + `PROMPTS.md`），运行时要用的那几份**拷进代码树**再由 `go:embed` 打进二进制 —— Go 的 embed 不能跨包目录取文件。

| 用途 | 来源 | 落在 |
| --- | --- | --- |
| 页签图标 | `anisync-icon-{light,dark}.svg` | `internal/webui/assets/`，由 `/favicon.svg` 提供，随主题切 `?theme=dark` |
| 页面内品牌标记 | `anisync-mark-light.svg` | 直接内联成 `#i-brand` 符号，写死的深色换成 `currentColor`，跟着 accent 走 |
| 托盘 | `anisync-tray-{light,dark}.ico` | `internal/tray/`，按 Windows 任务栏明暗选一个 |

换图标就重跑 `branding/build.py`，再把上面几份拷过去；不要手改 SVG 里的颜色。

## 改动站点适配的注意事项

默认只跑 AniBT + Nyaa。AniBT 的接口契约以 ANI-RSS 源码为准（`controller/AniBTController.java` 与对应 Service），不要凭页面结构猜。

`animegarden` / `mikan` 仍保留在 `NewRegistry` 和 `SiteType` 里，但**不在 `config.DefaultSourceOrder`**。
删它们之前先想清楚：老配置里写着这两个站点的用户会直接失效。接口契约同样以 ANI-RSS 源码为准
（`controller/{AnimeGarden,Mikan}Controller.java`），字段名三家不同（mikan 用 `label`，另两家用 `name`），
统一在 `assapi.Group` 收口。

Nyaa 的搜索地址必须与站点搜索页「RSS」按钮给出的形式一致，改动后用真实站点复核。

## 新增一个站点

1. 在 `internal/source` 实现 `Source` 接口；
2. 在 `NewRegistry` 注册，并在 `SiteType` 里补上对应的 `Ani.type`；
3. 在 `internal/webui` 的 `/api/sources` 里补上说明，供设置页展示；
4. 补上能离线跑的单元测试（地址拼接、字段映射），网络部分交给 `probe`。

## 提交前

```bash
go vet ./... && go test ./... && go build ./...
```

CI 覆盖 Windows / macOS / Linux 六个目标，并校验 macOS 二进制已链接 Cocoa。推送 `v*` 标签会触发发布；推送到 `main` 会自动递增补丁号并发版。
