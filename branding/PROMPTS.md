# AniSync 图标：设计说明 + 生成提示词

> 目录：`anisync/branding/`。图标本体（SVG / PNG / ICO）已经在这里，可直接用；
> 下面的提示词是拿去别的生成器重画/衍生时用的。所有提示词都可直接复制。

---

## 1. 这个图标在讲什么

| 元素 | 含义 |
| --- | --- |
| 圆角方块（squircle） | 应用本体，托盘 / 菜单栏 / favicon 都复用同一个轮廓 |
| 断口圆环 + 箭头 | 循环同步：多实例之间来回写回订阅 |
| 上方长条（亮） | **主 RSS**（primary） |
| 下方短条（暗） | **副 RSS**（standby），也是「双语字幕两行」的字形隐喻 |
| 祖母绿 | 沿用现有 WebUI 主色 `--accent: #31c48d`，不要换成蓝色/紫色 |

一句话：**一圈同步箭头里，躺着两条字幕条** —— 字幕组 + 订阅 + 循环写回。
所有元素都保持同一笔画粗细、纯几何、无文字，因此 16px 下仍然成立。

## 2. 颜色令牌

| 用途 | 值 |
| --- | --- |
| 品牌主色（与 WebUI 一致） | `#31C48D`，亮版 `#4DDCA6` |
| 浅色图标底：渐变 | `#46E3AB` → `#1FBE85` → `#0B8A5C`（左上→右下） |
| 浅色图标图形 | `#FFFFFF`，副条 62% 不透明 |
| 深色图标底：渐变 | `#175C45` → `#0B3527` → `#04180F` |
| 深色图标图形 | 主条 `#5CE8B0`（带微光），副条 `#4DDCA6` 约 50%，环 `#3FC79A` |
| 浅底上的单色标记 | `#0B1F17` |
| 深底上的单色标记 | `#E9ECF1` |

---

## 3. 生成提示词

### 3.1 浅色版（浅色 / 白色界面用）

**English**

```
App icon, flat vector, 1:1 square. A squircle (superellipse) tile filled with a smooth
emerald-green gradient from #46E3AB at the top-left through #1FBE85 to #0B8A5C at the
bottom-right, with a thin white inner rim at 30% opacity and a very soft white gloss
across the top edge. Centered inside: an open circular sync arrow — a thick broken ring
(stroke about 5% of the canvas width, round caps) that opens at the top, ending in one
clean arrowhead at the upper-left pointing clockwise — wrapping two horizontal rounded
caption bars: a long bright white bar on top, and a shorter white bar at about 60%
opacity below it, both centered like a pair of subtitle lines. Flat geometric icon design,
uniform stroke weight, crisp vector edges, no gradient inside the glyph, no text, no
letters, no drop shadow, transparent background, even margins, legible at 16px.
```

**中文**

```
应用图标，扁平矢量，1:1 正方形。一个超椭圆（squircle）方块，填充祖母绿渐变：
左上 #46E3AB → 中间 #1FBE85 → 右下 #0B8A5C；内侧一圈 30% 不透明的白色细描边，
顶部有一层很淡的白色高光。正中放：一个断口的圆形同步箭头——粗圆环（线宽约为画布
宽度的 5%，圆头端点）在正上方开口，开口左端是一个干净的箭头，指向顺时针方向——
圆环里包着两条水平圆角字幕条：上面一条长的亮白色，下面一条更短、约 60% 不透明，
两条都居中，像一对字幕行。扁平几何风格，笔画粗细统一，矢量锐边，图形内部不用渐变，
不要文字、不要字母、不要投影，背景透明，四周留白均匀，16px 下仍能看清。
```

### 3.2 深色版（深色界面 / 深色背景用）

**English**

```
App icon, flat vector, 1:1 square, designed for dark UI. A squircle (superellipse) tile in
deep emerald, gradient from #175C45 at the top through #0B3527 to #04180F at the bottom,
with a thin mint rim light (#4DDCA6 at about 40%) and a soft mint outer glow so the tile
separates from a near-black background (#0A0C0F). Centered inside: an open circular sync
arrow — a thick broken ring in mint (#3FC79A, stroke about 5% of the canvas width, round
caps) opening at the top, ending in one clean arrowhead at the upper-left pointing
clockwise — wrapping two horizontal rounded caption bars: the top bar a bright glowing
mint (#5CE8B0, faint bloom), the bottom bar shorter and dimmer (#4DDCA6 at about 50%).
Flat geometric icon design, uniform stroke weight, no text, no letters, no photographic
texture, no 3D bevel, centered composition, even margins, legible at 16px.
```

**中文**

```
应用图标，扁平矢量，1:1 正方形，为深色界面设计。一个超椭圆（squircle）方块，深祖母绿
渐变：顶部 #175C45 → 中间 #0B3527 → 底部 #04180F；外沿一圈薄薄的薄荷色轮廓光
（#4DDCA6，约 40% 不透明），外围有一层柔和薄荷辉光，让它在近黑背景（#0A0C0F）上不糊成
一团。正中放：断口的圆形同步箭头——薄荷色粗圆环（#3FC79A，线宽约为画布宽度的 5%，
圆头端点）在正上方开口，开口左端是一个干净的箭头，指向顺时针——圆环里包着两条水平
圆角字幕条：上面一条亮的发光薄荷色（#5CE8B0，带一点辉光），下面一条更短更暗
（#4DDCA6 约 50%）。扁平几何风格，笔画粗细统一，不要文字、不要字母、不要照片质感、
不要 3D 倒角，构图居中，四周留白均匀，16px 下仍能看清。
```

### 3.3 单色标记（托盘 / favicon / 菜单栏）

**English**

```
Monochrome app mark, flat vector, 1:1 square, transparent background, single flat color
(#0B1F17 for light backgrounds, #E9ECF1 for dark backgrounds). A thick broken ring with
round caps opening at the top, ending in one clean arrowhead at the upper-left pointing
clockwise, wrapping two horizontal rounded caption bars — a long bar on top and a shorter
bar below at about 50% opacity, both centered. Uniform stroke weight, geometric, no text,
no letters, no gradient, no shadow, high contrast, reads clearly at 16px.
```

**中文**

```
单色应用标记，扁平矢量，1:1 正方形，背景透明，单一纯色（浅底用 #0B1F17，深底用
#E9ECF1）。粗圆环在正上方断口、圆头端点，开口左端是一个干净的箭头、指向顺时针；
环内包两条水平圆角字幕条：上面一条长，下面一条更短、约 50% 不透明，都居中。
笔画粗细统一，纯几何，不要文字字母、不要渐变、不要阴影，对比度高，16px 下清楚。
```

### 3.4 一句话版（提示词框很小的时候用）

- 浅色：`Flat vector app icon, 1:1: emerald squircle tile, a thick open circular sync arrow (clockwise, arrowhead upper-left) wrapping two centered rounded caption bars — long bright white on top, short dim white below. No text, no shadow, crisp edges, legible at 16px.`
- 深色：`Flat vector app icon for dark UI, 1:1: deep emerald squircle tile with a mint rim light and soft mint glow, a thick mint open circular sync arrow (clockwise, arrowhead upper-left) wrapping two centered rounded caption bars — bright glowing mint on top, short dim mint below. No text, no 3D, legible at 16px.`

### 3.5 负面提示词（Negative prompt）

```
text, letters, words, typography, watermark, signature, logo of another brand, 3D render,
glossy plastic, bevel, emboss, photographic, mockup, device frame, drop shadow, multiple
icons, icon grid, thin hairline strokes, uneven stroke weight, blue, purple, orange,
rainbow gradient, mascot, character, face, smiley, cluttered background, perspective,
isometric, busy detail, low contrast, cropped edges
```

中文负面词（有的工具只吃中文）：`文字、字母、水印、签名、3D、塑料高光、倒角、照片、样机、设备外壳、投影、多个图标、图标网格、发丝细线、笔画粗细不一、蓝色、紫色、橙色、彩虹渐变、吉祥物、人物、脸、笑脸、杂乱背景、透视、等距、细节过多、对比度低、边缘被裁`

---

## 4. 各生成器的用法

| 工具 | 怎么用 |
| --- | --- |
| **Nano Banana / Gemini 图像（图生图）** | 最推荐。把 `png/anisync-icon-light-1024.png`（或深色版）当参考图传进去，再配一句：`保持这个构图和元素不变，重画成更精致的扁平矢量应用图标，配色按下面给的色值` —— 构图不会跑偏。 |
| **Midjourney** | 用 3.1/3.2 的英文提示词，末尾加 `--ar 1:1 --style raw --no text,letters,watermark`；版本号按你账号当前可用的填。想稳一点就先垫图（`--iw 1.5` 配参考图 URL）。 |
| **GPT-image / DALL·E** | 用英文长提示词，明确写 `flat vector, no text, transparent background`；它对「不要文字」这句比较敏感，别省。 |
| **Ideogram / Recraft** | Recraft 选 `Vector art` 风格并直接导出 SVG，适合拿到能进仓库的矢量成品；Ideogram 出图更"设计感"但线宽容易不匀。 |
| **Stable Diffusion / ComfyUI** | 用 3.1/3.2 + 负面词，CFG 7–9，steps 30+；出图后仍需人工把笔画粗细对齐。 |

> 不管用哪个：**AI 出的是参考稿，不是最终资产**。它的线宽、圆角、间距一定会有 3–8% 的抖动，
> 直接当图标用会在小尺寸下显脏。最终资产请用 `anisync-icon-*.svg` 改色/微调，
> 或照 AI 稿在 Figma / Illustrator 里重描一遍再导出。

---

## 5. 验收清单（生成完照着打勾）

- [ ] **剪影测试**：整体填成一个纯色块，还能认出「环 + 两条字幕条」
- [ ] **16px 测试**：缩到 16px 不糊成一坨（托盘的看 `anisync-tray-*.ico` 那条路线）
- [ ] **灰度测试**：转成黑白后主条仍比副条重，层次没丢
- [ ] **单色测试**：只用一种颜色也能成立（说明没靠渐变撑结构）
- [ ] **两色上限**：底色 + 图形色，最多再加一个副条透明度，不要再加第三个色相
- [ ] **笔画一致**：圆环与字幕条粗细目视一致（抖动超过约 8% 就是不合格）
- [ ] **无文字**：任何字母、单词、水印都不允许
- [ ] **安全区**：图形落在中心 80% 的圆内，四角留白均匀
- [ ] **双底测试**：同一个图标分别贴在 `#FFFFFF` 和 `#0A0C0F` 上都清楚

## 6. 导出规格

| 用途 | 规格 |
| --- | --- |
| 母版 | 1024×1024 PNG（透明底）+ SVG 源文件 |
| Windows 图标 | `.ico`，含 16/20/24/32/40/48/64/128/256 |
| macOS | `.icns`：16/32/64/128/256/512 各 @1x@2x，用 `iconutil -c icns` 打包 |
| Web / favicon | 32、180（apple-touch）、192、512；maskable 版本把图形收进中心 80% |
| 托盘 / 菜单栏 | 用 `anisync-mark-mini-*.svg`（实心 chip）或 `anisync-tray-*.ico` |

## 7. 目录里有什么

| 文件 | 说明 |
| --- | --- |
| `anisync-icon-light.svg` / `anisync-icon-dark.svg` | 应用图标（矢量源，浅色 / 深色） |
| `anisync-mark-light.svg` / `anisync-mark-dark.svg` | 单色标记（透明底，浅底 / 深底用） |
| `anisync-mark-mini-light.svg` / `-dark.svg` | 迷你标记：实心 chip 挖空字幕条，16px 用 |
| `anisync.ico` / `anisync-dark.ico` | 应用图标 ICO |
| `anisync-tray-light.ico` / `anisync-tray-dark.ico` | 托盘 ICO（浅色任务栏 / 深色任务栏） |
| `png/` | 各尺寸 PNG：1024 / 512 / 256 / 128 / 64 / 48 / 32 / 24 / 20 / 16 |
| `preview-sheet.png` | 总览图：浅深两套 + 小尺寸阶梯 |
| `build.py` | 生成脚本，改参数重跑即可 |

改配色 / 换对齐方式，直接改 `build.py` 顶部的常量再跑：

```powershell
python build.py            # 重建 SVG + PNG + ICO + 预览图
python build.py --svg-only # 只出 SVG
```

常用旋钮：`BAR_ALIGN`（`"center"` 居中 / `"left"` 左对齐）、`RING_R` / `RING_W`（环的大小与粗细）、
`BAR_H` / `BAR_PRIMARY_W` / `BAR_STANDBY_W`（两条字幕条）、`GAP_START` / `GAP_SWEEP`（开口位置与大小）。
