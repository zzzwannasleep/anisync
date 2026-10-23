//go:build windows

package tray

import (
	_ "embed"
	"os/exec"

	"github.com/getlantern/systray"
	"golang.org/x/sys/windows/registry"
)

// 托盘图标用品牌资产，不再在代码里画像素。
//
//go:embed tray-light.ico
var iconLight []byte

//go:embed tray-dark.ico
var iconDark []byte

// personalizeKey 是 Windows 存「任务栏/应用用浅色还是深色」的地方。
const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

// setIcon 按任务栏明暗挑一份图标。
func setIcon() { systray.SetIcon(iconForTaskbar()) }

// iconForTaskbar 返回当前任务栏配色对应的图标。
func iconForTaskbar() []byte {
	if lightTaskbar() {
		return iconLight
	}
	return iconDark
}

// lightTaskbar 读 SystemUsesLightTheme：1 = 浅色任务栏，0 = 深色任务栏。
//
// 读不到（键不存在、被策略挡掉、值类型不对）一律按浅色处理：
// 托盘图标颜色不对是小事，因为读个注册表把启动卡住或者 panic 才是大事。
func lightTaskbar() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, personalizeKey, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer key.Close()

	value, _, err := key.GetIntegerValue("SystemUsesLightTheme")
	if err != nil {
		return true
	}
	return value == 1
}

// openBrowser 用系统默认浏览器打开链接。
func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
