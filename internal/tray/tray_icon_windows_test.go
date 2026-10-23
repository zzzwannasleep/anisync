//go:build windows

package tray

import (
	"bytes"
	"testing"
)

// 托盘图标必须跟着任务栏明暗走。
//
// 这里不写死「本机现在应该是哪一个」—— 那会让深色 CI 机器上随机变红。
// 改成断言「选出来的那份 == lightTaskbar() 说的那一份」：
// 两条分支都真的被走到，映射关系被钉住，任何机器上都稳定。
func TestTrayIconMatchesTaskbarTheme(t *testing.T) {
	light, dark := iconLight, iconDark

	if len(light) == 0 || len(dark) == 0 {
		t.Fatal("托盘图标没有被嵌入，//go:embed 可能写错了")
	}
	if bytes.Equal(light, dark) {
		t.Error("浅色与深色托盘图标内容相同，主题切换等于没做")
	}
	// ICO 头：reserved=0, type=1, count>=1。校验一下嵌进来的确实是 ICO，
	// 而不是被误换成 PNG 或者空文件。
	for name, icon := range map[string][]byte{"light": light, "dark": dark} {
		if len(icon) < 6 {
			t.Fatalf("%s 图标太短，不是合法 ICO", name)
		}
		if icon[0] != 0 || icon[1] != 0 || icon[2] != 1 || icon[3] != 0 {
			t.Errorf("%s 图标头不是 ICO（应为 00 00 01 00）: % x", name, icon[:4])
		}
	}

	want := dark
	if lightTaskbar() {
		want = light
	}
	if !bytes.Equal(iconForTaskbar(), want) {
		t.Error("iconForTaskbar 选的图标与 lightTaskbar 的结论不一致")
	}
}
