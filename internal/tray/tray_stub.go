//go:build !windows && !(darwin && cgo)

package tray

import (
	"fmt"
	"os"
	"runtime"
)

// Run 在没有原生托盘的平台上打印提示并常驻，功能由 CLI 与本地 Web 设置页承担。
//
// 两种情况会走到这里：
//   - Linux：systray 需要 gtk/appindicator 与 cgo，交给桌面环境自行选择用不用；
//   - macOS 未开 cgo：Objective-C 实现无法编译，用 Web 设置页代替菜单栏图标。
func Run(opts Options) int {
	fmt.Printf("assm 托盘：当前平台 %s 使用本地 Web 设置页。\n", runtime.GOOS)
	if runtime.GOOS == "darwin" {
		fmt.Println("提示：macOS 原生菜单栏图标需要以 CGO_ENABLED=1 在 macOS 上构建。")
	}
	fmt.Printf("  设置页: %s\n", opts.Settings)
	fmt.Printf("  配置:   %s\n", opts.ConfigPath)
	fmt.Println("按 Ctrl+C 退出。")
	<-make(chan struct{})
	return 0
}

// Quit 直接退出进程。
func Quit() { os.Exit(0) }
