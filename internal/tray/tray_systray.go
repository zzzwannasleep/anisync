//go:build windows || (darwin && cgo)

package tray

import (
	"sync"

	"github.com/getlantern/systray"
)

// Run 启动托盘并阻塞，直到用户点「退出」。
//
// Windows 为纯 Go 实现；macOS 走 cgo（Objective-C），因此 darwin 需要 CGO_ENABLED=1，
// 且必须在 macOS 上构建。darwin 未开 cgo 时会落到 tray_stub.go。
func Run(opts Options) int {
	systray.Run(func() { onReady(opts) }, func() {})
	return 0
}

// Quit 关闭托盘。
func Quit() { systray.Quit() }

func onReady(opts Options) {
	setIcon()
	systray.SetTitle(opts.Title)
	systray.SetTooltip("AniSync · 多 ANI-RSS 实例主副 RSS 自动打点")

	settings := systray.AddMenuItem("打开设置页", opts.Settings)
	syncItem := systray.AddMenuItem("立即同步", "按规则重新打点主副 RSS")
	status := systray.AddMenuItem("状态：就绪", "最近一次同步结果")
	status.Disable()
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出", "退出 AniSync")

	var busy sync.Mutex
	for {
		select {
		case <-settings.ClickedCh:
			openBrowser(opts.Settings)
		case <-syncItem.ClickedCh:
			// 同步是重活，重复点击直接忽略，避免并发写同一个实例
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

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
