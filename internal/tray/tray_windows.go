//go:build windows

package tray

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os/exec"
	"sync"

	"github.com/getlantern/systray"
)

// Run 启动托盘并阻塞，直到用户点「退出」。
func Run(opts Options) int {
	systray.Run(func() { onReady(opts) }, func() {})
	return 0
}

// Quit 关闭托盘。
func Quit() { systray.Quit() }

func onReady(opts Options) {
	systray.SetIcon(iconBytes())
	systray.SetTitle(opts.Title)
	systray.SetTooltip("assm · 多 ANI-RSS 实例主副 RSS 打点")

	settings := systray.AddMenuItem("打开设置页", opts.Settings)
	syncItem := systray.AddMenuItem("立即同步", "按规则重新打点主副 RSS")
	status := systray.AddMenuItem("状态：就绪", "最近一次同步结果")
	status.Disable()
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出", "退出 assm")

	var busy sync.Mutex
	for {
		select {
		case <-settings.ClickedCh:
			openBrowser(opts.Settings)
		case <-syncItem.ClickedCh:
			if !busy.TryLock() {
				continue
			}
			status.SetTitle("状态：同步中…")
			go func() {
				defer busy.Unlock()
				message, err := opts.Sync()
				if err != nil {
					status.SetTitle("状态：失败 " + truncate(err.Error(), 40))
					return
				}
				status.SetTitle("状态：" + truncate(message, 48))
			}()
		case <-quit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// iconBytes 在内存里生成一个 16x16 的 ICO，避免带二进制资源进仓库。
func iconBytes() []byte {
	const size = 16
	const headerSize = 40
	xor := size * size * 4
	and := size * size / 8
	image := make([]byte, headerSize+xor+and)

	// BITMAPINFOHEADER：高度写两倍（XOR + AND 掩码）
	binary.LittleEndian.PutUint32(image[0:], headerSize)
	binary.LittleEndian.PutUint32(image[4:], size)
	binary.LittleEndian.PutUint32(image[8:], size*2)
	binary.LittleEndian.PutUint16(image[12:], 1)
	binary.LittleEndian.PutUint16(image[14:], 32)

	// 画一个圆角绿块 + 中间深色横杠，远看像「订阅列表」
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			offset := headerSize + (y*size+x)*4
			inside := x >= 2 && x < size-2 && y >= 2 && y < size-2
			bar := inside && (y == 6 || y == 9 || y == 12) && x >= 5 && x < size-4
			switch {
			case bar:
				image[offset+0], image[offset+1], image[offset+2], image[offset+3] = 20, 30, 25, 255
			case inside:
				image[offset+0], image[offset+1], image[offset+2], image[offset+3] = 118, 167, 59, 255
			default:
				image[offset+0], image[offset+1], image[offset+2], image[offset+3] = 0, 0, 0, 0
			}
		}
	}

	var out bytes.Buffer
	// ICONDIR
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	// ICONDIRENTRY
	out.WriteByte(size)
	out.WriteByte(size)
	out.WriteByte(0)
	out.WriteByte(0)
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(32))
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(image)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(22))
	out.Write(image)
	return out.Bytes()
}

var _ = fmt.Sprintf
