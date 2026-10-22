//go:build !windows

package tray

import (
	"fmt"
	"os"
	"runtime"
)

// Run 在非 Windows 平台打印提示并常驻（功能由 CLI 与 Web 设置页承担）。
// macOS 的原生菜单栏图标需要 cgo + Xcode 工具链，交由 CI 的 darwin job 构建。
func Run(opts Options) int {
	fmt.Printf("assm 托盘：当前平台 %s 使用本地 Web 设置页。\n", runtime.GOOS)
	fmt.Printf("  设置页: %s\n", opts.Settings)
	fmt.Printf("  配置:   %s\n", opts.ConfigPath)
	fmt.Println("按 Ctrl+C 退出。")
	<-make(chan struct{})
	return 0
}

// Quit 直接退出进程。
func Quit() { os.Exit(0) }
