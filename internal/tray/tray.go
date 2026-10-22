// Package tray 提供常驻托盘。Windows 用纯 Go 实现（无需 cgo）；
// 其它平台走 stub，功能由 CLI 与本地 Web 设置页承担。
package tray

// Options 是托盘菜单需要的信息与回调。
type Options struct {
	Title      string
	ConfigPath string
	Settings   string
	Sync       func() (string, error)
}
