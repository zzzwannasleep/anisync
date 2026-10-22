//go:build darwin && cgo

package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os/exec"

	"github.com/getlantern/systray"
)

// setIcon 使用「模板图」（template image）。
// macOS 菜单栏会按系统色渲染黑白模板图，深色/浅色模式都自动适配，
// 因此这里只画形状，颜色交给系统。
func setIcon() { systray.SetTemplateIcon(templateIcon(), templateIcon()) }

// openBrowser 用系统默认浏览器打开链接。
func openBrowser(url string) {
	_ = exec.Command("open", url).Start()
}

// templateIcon 生成 32x32 的模板图：不透明黑色形状 + 透明背景，中间挖三条横杠。
//
// systray 在 darwin 上会把 NSImage 的逻辑尺寸固定为 16x16，
// 用 32x32 位图可以让 Retina 屏拿到 2x 的清晰度。
func templateIcon() []byte {
	const size = 32
	const margin = 3
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	shape := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	blank := color.RGBA{}

	roundedRect := func(x, y int) bool {
		if x < margin || x >= size-margin || y < margin+1 || y >= size-margin-1 {
			return false
		}
		// 四角切掉，做出圆角观感
		corners := [][2]int{
			{margin, margin + 1}, {margin + 1, margin + 1},
			{size - margin - 1, margin + 1}, {size - margin - 2, margin + 1},
			{margin, size - margin - 2}, {margin + 1, size - margin - 2},
			{size - margin - 1, size - margin - 2}, {size - margin - 2, size - margin - 2},
		}
		for _, corner := range corners {
			if x == corner[0] && y == corner[1] {
				return false
			}
		}
		return true
	}
	bar := func(x, y int) bool {
		return (y == 11 || y == 16 || y == 21) && x >= 9 && x < size-9
	}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			switch {
			case bar(x, y):
				img.Set(x, y, blank)
			case roundedRect(x, y):
				img.Set(x, y, shape)
			default:
				img.Set(x, y, blank)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
