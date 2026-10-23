# -*- coding: utf-8 -*-
"""AniSync 图标构建脚本。

流程：几何定义 -> 生成 4 个 SVG（浅色/深色 应用图标 + 浅色/深色 单色标记）
      -> Edge 无头渲染 1024 PNG -> Pillow 派生小尺寸 / ICO / 预览图。

用法：python build.py            # 全部重建
      python build.py --svg-only # 只出 SVG，不渲染
"""
from __future__ import annotations

import math
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parent
PNG_DIR = ROOT / "png"
EDGE = Path(r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe")

S = 1024
CX = CY = S / 2

# ---------------------------------------------------------------- 几何参数
# 应用图标：方形底板（squircle）+ 环形同步箭头 + 两条字幕条（主 RSS / 副 RSS）
RING_R, RING_W = 310, 54
BAR_H = 110
BAR_GAP = 0.72 * BAR_H
BAR_ALIGN = "center"          # center = 像字幕一样居中；left = 左对齐
BAR_PRIMARY_W = 400
BAR_STANDBY_W = 250
GAP_START, GAP_SWEEP = 318.0, 264.0  # 圆环开口在正上方，顺时针 264°

# 单色标记：去掉底板，线更粗，给 16~32px 用
MARK_RING_R, MARK_RING_W = 310, 64
MARK_BAR_H = 120
MARK_BAR_GAP = 0.72 * MARK_BAR_H
MARK_PRIMARY_W = 380
MARK_STANDBY_W = 240

# 迷你标记：实心 chip 挖空两条字幕条，给 16~24px 的托盘 / favicon 用
MINI_CHIP = 940
MINI_BAR_H = 112


# ---------------------------------------------------------------- 路径工具
def polar(r: float, deg: float) -> tuple[float, float]:
    t = math.radians(deg)
    return (CX + r * math.cos(t), CY + r * math.sin(t))


def squircle_path(size: float = S, n: float = 4.2, seg: int = 8) -> str:
    """超椭圆（squircle）路径，Apple 风格的连续圆角。"""
    a = size / 2
    pts = []
    total = seg * 4
    for i in range(total):
        t = 2 * math.pi * i / total
        ct, st = math.cos(t), math.sin(t)
        x = a * math.copysign(abs(ct) ** (2 / n), ct)
        y = a * math.copysign(abs(st) ** (2 / n), st)
        pts.append((CX + x, CY + y))
    return catmull_to_bezier(pts)


def catmull_to_bezier(pts: list[tuple[float, float]]) -> str:
    n = len(pts)
    out = [f"M{pts[0][0]:.2f} {pts[0][1]:.2f}"]
    for i in range(n):
        p0, p1 = pts[(i - 1) % n], pts[i]
        p2, p3 = pts[(i + 1) % n], pts[(i + 2) % n]
        c1 = (p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6)
        c2 = (p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6)
        out.append(
            f"C{c1[0]:.2f} {c1[1]:.2f} {c2[0]:.2f} {c2[1]:.2f} {p2[0]:.2f} {p2[1]:.2f}"
        )
    out.append("Z")
    return "".join(out)


def ring_path(r: float, start: float, sweep: float) -> str:
    end = start + sweep
    x0, y0 = polar(r, start)
    x1, y1 = polar(r, end)
    large = 1 if sweep % 360 > 180 else 0
    return f"M{x0:.2f} {y0:.2f}A{r} {r} 0 {large} 1 {x1:.2f} {y1:.2f}"


def round_rect_path(x: float, y: float, w: float, h: float, r: float) -> str:
    """圆角矩形路径，用于 evenodd 挖空。"""
    return (
        f"M{x + r:.2f} {y:.2f}H{x + w - r:.2f}A{r} {r} 0 0 1 {x + w:.2f} {y + r:.2f}"
        f"V{y + h - r:.2f}A{r} {r} 0 0 1 {x + w - r:.2f} {y + h:.2f}"
        f"H{x + r:.2f}A{r} {r} 0 0 1 {x:.2f} {y + h - r:.2f}"
        f"V{y + r:.2f}A{r} {r} 0 0 1 {x + r:.2f} {y:.2f}Z"
    )


def arrow_head(r: float, deg: float, length: float = 74, back: float = 22, half: float = 58,
               notch: float = 0.34) -> str:
    """圆环末端的箭头：带凹底的箭头，指向顺时针方向。"""
    t = math.radians(deg)
    tx, ty = -math.sin(t), math.cos(t)          # 顺时针切向
    ex, ey = polar(r, deg)
    tip = (ex + tx * length, ey + ty * length)
    bx, by = ex - tx * back, ey - ty * back
    nx, ny = -ty, tx                            # 法向
    p1 = (bx + nx * half, by + ny * half)
    p2 = (bx - nx * half, by - ny * half)
    mid = (bx + tx * length * notch, by + ty * length * notch)
    return (
        f"M{tip[0]:.2f} {tip[1]:.2f}L{p1[0]:.2f} {p1[1]:.2f}"
        f"L{mid[0]:.2f} {mid[1]:.2f}L{p2[0]:.2f} {p2[1]:.2f}Z"
    )


def bars(bar_h: float, gap: float, primary_w: float, standby_w: float,
         fill_a: str, fill_b: str, align: str | None = None,
         cx: float = CX, left_x: float | None = None) -> str:
    """两条字幕条：上长下短，构成一对双语字幕行（主 RSS / 副 RSS）。"""
    r = bar_h / 2
    y_top = CY - (bar_h + gap) / 2 - bar_h / 2
    y_bot = CY + (bar_h + gap) / 2 - bar_h / 2
    align = align or BAR_ALIGN
    if align == "center":
        xa, xb = cx - primary_w / 2, cx - standby_w / 2
    else:
        base = left_x if left_x is not None else cx - primary_w / 2
        xa = xb = base
    return (
        f'<rect x="{xa:.2f}" y="{y_top:.2f}" width="{primary_w}" height="{bar_h}" rx="{r}" fill="{fill_a}"/>'
        f'<rect x="{xb:.2f}" y="{y_bot:.2f}" width="{standby_w}" height="{bar_h}" rx="{r}" fill="{fill_b}"/>'
    )


def glyph(ring_r: float, ring_w: float, bar_h: float, gap: float,
          primary_w: float, standby_w: float,
          ring_fill: str, primary_fill: str, standby_fill: str,
          arrow_fill: str | None = None, glow: str = "",
          arrow: tuple[float, float, float] | None = None,
          align: str | None = None, single_bar: bool = False) -> str:
    arrow_fill = arrow_fill or ring_fill
    a_len, a_back, a_half = arrow or (88, 30, 66)
    if single_bar:
        r = bar_h / 2
        body = (
            f'<rect x="{CX - primary_w / 2:.2f}" y="{CY - r:.2f}" width="{primary_w}" '
            f'height="{bar_h}" rx="{r}" fill="{primary_fill}"/>'
        )
    else:
        body = bars(bar_h, gap, primary_w, standby_w, primary_fill, standby_fill, align)
    return (
        f'<g{glow}>'
        f'<path d="{ring_path(ring_r, GAP_START, GAP_SWEEP)}" fill="none" stroke="{ring_fill}" '
        f'stroke-width="{ring_w}" stroke-linecap="round"/>'
        f'<path d="{arrow_head(ring_r, GAP_START + GAP_SWEEP, a_len, a_back, a_half)}" fill="{arrow_fill}"/>'
        f'{body}'
        f'</g>'
    )


# ---------------------------------------------------------------- SVG 组装
def tile_svg(dark: bool) -> str:
    sq = squircle_path()
    sq_in = squircle_path(S - 18)
    if dark:
        tile = (
            '<linearGradient id="tile" x1="0.1" y1="0" x2="0.85" y2="1">'
            '<stop offset="0" stop-color="#175C45"/>'
            '<stop offset="0.52" stop-color="#0B3527"/>'
            '<stop offset="1" stop-color="#04180F"/>'
            "</linearGradient>"
        )
        gloss = (
            '<radialGradient id="gloss" cx="0.5" cy="0.0" r="0.85">'
            '<stop offset="0" stop-color="#7CF0C0" stop-opacity="0.20"/>'
            '<stop offset="0.55" stop-color="#7CF0C0" stop-opacity="0.04"/>'
            '<stop offset="1" stop-color="#7CF0C0" stop-opacity="0"/>'
            "</radialGradient>"
        )
        halo = f'<path d="{squircle_path(S - 10)}" fill="#31C48D" opacity="0.18" filter="url(#halo)"/>'
        rim = f'<path d="{sq_in}" fill="none" stroke="#4DDCA6" stroke-opacity="0.42" stroke-width="9"/>'
        glyph_svg = glyph(
            RING_R, RING_W, BAR_H, BAR_GAP, BAR_PRIMARY_W, BAR_STANDBY_W,
            ring_fill="#3FC79A", primary_fill="#5CE8B0", standby_fill="#4DDCA6",
        ).replace('stroke="#3FC79A"', 'stroke="#3FC79A" stroke-opacity="0.9"', 1)
        extra_defs = (
            '<filter id="halo" x="-15%" y="-15%" width="130%" height="130%">'
            '<feGaussianBlur stdDeviation="22"/></filter>'
            '<filter id="soft" x="-40%" y="-40%" width="180%" height="180%">'
            '<feDropShadow dx="0" dy="0" stdDeviation="13" flood-color="#4DDCA6" flood-opacity="0.55"/>'
            "</filter>"
        )
        standby_opacity = 0.52
    else:
        tile = (
            '<linearGradient id="tile" x1="0.1" y1="0" x2="0.85" y2="1">'
            '<stop offset="0" stop-color="#46E3AB"/>'
            '<stop offset="0.5" stop-color="#1FBE85"/>'
            '<stop offset="1" stop-color="#0B8A5C"/>'
            "</linearGradient>"
        )
        gloss = (
            '<radialGradient id="gloss" cx="0.5" cy="0.0" r="0.85">'
            '<stop offset="0" stop-color="#ffffff" stop-opacity="0.34"/>'
            '<stop offset="0.55" stop-color="#ffffff" stop-opacity="0.06"/>'
            '<stop offset="1" stop-color="#ffffff" stop-opacity="0"/>'
            "</radialGradient>"
        )
        halo = ""
        rim = f'<path d="{sq_in}" fill="none" stroke="#ffffff" stroke-opacity="0.34" stroke-width="9"/>'
        glyph_svg = glyph(
            RING_R, RING_W, BAR_H, BAR_GAP, BAR_PRIMARY_W, BAR_STANDBY_W,
            ring_fill="#FFFFFF", primary_fill="#FFFFFF", standby_fill="#FFFFFF",
        ).replace('stroke="#FFFFFF"', 'stroke="#FFFFFF" stroke-opacity="0.92"', 1)
        extra_defs = ""
        standby_opacity = 0.62

    # 副条单独降透明度（保持语义：主 RSS 实心，副 RSS 次之）
    glyph_svg = _dim_standby(glyph_svg, standby_opacity)
    if dark:
        # 只给主条加光晕，圆环保持锐利
        glyph_svg = _glow_first_bar(glyph_svg, "soft")

    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{S}" height="{S}" viewBox="0 0 {S} {S}">
  <defs>
    {tile}
    {gloss}
    {extra_defs}
  </defs>
  {halo}
  <path d="{sq}" fill="url(#tile)"/>
  <path d="{sq}" fill="url(#gloss)"/>
  {rim}
  {glyph_svg}
</svg>
'''


def _glow_first_bar(svg: str, filter_id: str) -> str:
    """把第一条字幕条包进 <g filter>，只让主条发光。"""
    start = svg.find("<rect")
    end = svg.find("/>", start) + 2
    return svg[:start] + f'<g filter="url(#{filter_id})">' + svg[start:end] + "</g>" + svg[end:]


def _dim_standby(svg: str, opacity: float) -> str:
    """给第二条字幕条加 opacity（它是 glyph 里第二个 rect）。"""
    first = svg.find("<rect")
    second = svg.find("<rect", first + 1)
    end = svg.find("/>", second) + 2
    seg = svg[second:end]
    return svg[:second] + seg.replace("<rect", f'<rect opacity="{opacity}"', 1) + svg[end:]


def mark_svg(dark: bool) -> str:
    ink = "#E9ECF1" if dark else "#0B1F17"
    g = glyph(
        MARK_RING_R, MARK_RING_W, MARK_BAR_H, MARK_BAR_GAP,
        MARK_PRIMARY_W, MARK_STANDBY_W,
        ring_fill=ink, primary_fill=ink, standby_fill=ink,
        arrow=(84, 24, 70),
    )
    g = _dim_standby(g, 0.5)
    # 圆环再压一点，避免小尺寸下糊成一坨
    g = g.replace(f'stroke="{ink}"', f'stroke="{ink}" stroke-opacity="0.9"', 1)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{S}" height="{S}" viewBox="0 0 {S} {S}">
  {g}
</svg>
'''


def mini_svg(dark: bool) -> str:
    """迷你标记：实心 chip 挖空两条字幕条，16px 下最清楚（也是项目原本的托盘形态）。"""
    ink = "#E9ECF1" if dark else "#0B1F17"
    chip = squircle_path(MINI_CHIP)
    bar_h = MINI_BAR_H
    y1 = CY - 76 - bar_h / 2
    y2 = CY + 76 - bar_h / 2
    hole1 = round_rect_path(CX - 210, y1, 420, bar_h, bar_h / 2)
    hole2 = round_rect_path(CX - 140, y2, 280, bar_h, bar_h / 2)
    return f'''<svg xmlns="http://www.w3.org/2000/svg" width="{S}" height="{S}" viewBox="0 0 {S} {S}">
  <path d="{chip}{hole1}{hole2}" fill="{ink}" fill-rule="evenodd"/>
</svg>
'''


# ---------------------------------------------------------------- 渲染
def render(svg_path: Path, out_path: Path, size: int = S) -> bool:
    if not EDGE.exists():
        print(f"  ! 未找到 Edge，跳过渲染：{out_path.name}")
        return False
    with tempfile.TemporaryDirectory() as td:
        td = Path(td)
        html = td / "page.html"
        html.write_text(
            "<!doctype html><meta charset='utf-8'><style>"
            f"html,body{{margin:0;padding:0;width:{size}px;height:{size}px;"
            "background:transparent;overflow:hidden}svg{display:block;width:100%;height:100%}"
            f"</style>{svg_path.read_text(encoding='utf-8')}",
            encoding="utf-8",
        )
        cmd = [
            str(EDGE), "--headless=new", "--disable-gpu", "--hide-scrollbars",
            "--no-first-run", "--no-default-browser-check",
            "--force-device-scale-factor=1", "--default-background-color=00000000",
            "--virtual-time-budget=3000", f"--window-size={size},{size}",
            f"--user-data-dir={td / 'profile'}", f"--screenshot={out_path}", html.as_uri(),
        ]
        r = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
        if not out_path.exists():
            print(f"  ! 渲染失败 {out_path.name}: {r.stderr.strip()[:300]}")
            return False
    return True


def main() -> None:
    PNG_DIR.mkdir(parents=True, exist_ok=True)
    assets = {
        "anisync-icon-light.svg": tile_svg(False),
        "anisync-icon-dark.svg": tile_svg(True),
        "anisync-mark-light.svg": mark_svg(False),
        "anisync-mark-dark.svg": mark_svg(True),
        "anisync-mark-mini-light.svg": mini_svg(False),
        "anisync-mark-mini-dark.svg": mini_svg(True),
    }
    for name, text in assets.items():
        (ROOT / name).write_text(text, encoding="utf-8")
        print(f"svg  {name}")

    if "--svg-only" in sys.argv:
        return

    masters: dict[str, Path] = {}
    for name in assets:
        src = ROOT / name
        out = PNG_DIR / (name.replace(".svg", "-1024.png"))
        if render(src, out):
            masters[name] = out
            print(f"png  {out.name}")

    sizes = [512, 256, 128, 64, 48, 32, 24, 20, 16]
    for name, master in masters.items():
        im = Image.open(master).convert("RGBA")
        for s in sizes:
            im.resize((s, s), Image.LANCZOS).save(PNG_DIR / name.replace(".svg", f"-{s}.png"))
    print(f"png  派生尺寸：{', '.join(map(str, sizes))}")

    ico_sizes = [(16, 16), (20, 20), (24, 24), (32, 32), (40, 40), (48, 48),
                 (64, 64), (128, 128), (256, 256)]
    tray_sizes = [(16, 16), (20, 20), (24, 24), (32, 32), (48, 48), (64, 64)]
    for variant, ico, use_tray in (
        ("light", "anisync.ico", False),
        ("dark", "anisync-dark.ico", False),
        ("light", "anisync-tray-light.ico", True),
        ("dark", "anisync-tray-dark.ico", True),
    ):
        stem = "anisync-mark-mini" if use_tray else "anisync-icon"
        src = PNG_DIR / f"{stem}-{variant}-1024.png"
        if src.exists():
            Image.open(src).convert("RGBA").save(ROOT / ico, sizes=tray_sizes if use_tray else ico_sizes)
            print(f"ico  {ico}")

    build_sheet()


def build_sheet() -> None:
    if not (PNG_DIR / "anisync-icon-light-1024.png").exists():
        return
    W, H = 1600, 1040
    sheet = Image.new("RGBA", (W, H), "#FFFFFF")
    d = ImageDraw.Draw(sheet)
    try:
        f = ImageFont.truetype(r"C:\Windows\Fonts\segoeui.ttf", 24)
        fs = ImageFont.truetype(r"C:\Windows\Fonts\segoeui.ttf", 17)
    except OSError:
        f = fs = ImageFont.load_default()

    rows = [(0, "#FFFFFF", "#6D7684", "light"), (H // 2, "#0A0C0F", "#9AA4B2", "dark")]
    for top, bg, fg, variant in rows:
        d.rectangle([0, top, W, top + H // 2], fill=bg)
        tile = Image.open(PNG_DIR / f"anisync-icon-{variant}-1024.png").convert("RGBA").resize((290, 290), Image.LANCZOS)
        mark = Image.open(PNG_DIR / f"anisync-mark-{variant}-1024.png").convert("RGBA").resize((180, 180), Image.LANCZOS)
        sheet.alpha_composite(tile, (70, top + 74))
        sheet.alpha_composite(mark, (425, top + 130))
        d.text((70, top + 392), f"app icon · {variant}", font=f, fill=fg)
        d.text((425, top + 336), f"mono mark · {variant}", font=f, fill=fg)
        d.text((425, top + 372), "tray / favicon", font=fs, fill=fg)

        # 迷你标记：托盘尺寸下的实际观感
        x = 660
        for s in (32, 24, 20, 16):
            im = Image.open(PNG_DIR / f"anisync-mark-mini-{variant}-{s}.png").convert("RGBA")
            sheet.alpha_composite(im, (x, top + 226 - s // 2))
            d.text((x, top + 292), f"{s}", font=fs, fill=fg)
            x += s + 34
        d.text((660, top + 336), "mini mark · tray", font=f, fill=fg)
        d.text((660, top + 372), "16–32px, filled chip", font=fs, fill=fg)

        # 应用图标的清晰度阶梯
        x = 1000
        for s in (128, 64, 32, 16):
            im = Image.open(PNG_DIR / f"anisync-icon-{variant}-{s}.png").convert("RGBA")
            sheet.alpha_composite(im, (x, top + 226 - s // 2))
            d.text((x, top + 292), f"{s}px", font=fs, fill=fg)
            x += s + 52
        d.text((1000, top + 336), "app icon ladder", font=f, fill=fg)

    sheet.convert("RGB").save(ROOT / "preview-sheet.png")
    print("png  preview-sheet.png")


if __name__ == "__main__":
    main()
