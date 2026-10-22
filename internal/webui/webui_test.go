package webui

import (
	"os"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// 设置页里的「自动发现来源」标签是写死在前端的，id 必须和 config 里的常量对得上。
// 只改一边会让用户在界面上勾选的来源保存成后端不认识的值，所以这里钉住。
func TestWebUIListsEveryDiscoverSource(t *testing.T) {
	raw, err := os.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("读取设置页失败: %v", err)
	}
	page := string(raw)
	for _, name := range config.DiscoverSourceNames {
		if !strings.Contains(page, "'"+name+"'") {
			t.Errorf("设置页里找不到发现来源 %q；config.DiscoverSourceNames 改了就要同步改 assets/index.html", name)
		}
	}
}

// 自动发现相关的输入控件必须有可访问名，否则键盘和读屏用户用不了。
func TestWebUIDiscoverInputsAreLabelled(t *testing.T) {
	raw, err := os.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("读取设置页失败: %v", err)
	}
	page := string(raw)
	for _, id := range []string{
		"discoverEnabled", "discoverEnable", "discoverBgmUser",
		"discoverDays", "discoverLimit", "discoverExclude",
	} {
		if !strings.Contains(page, `id="`+id+`"`) {
			t.Errorf("设置页里缺少控件 %s", id)
		}
	}
	// 每个控件后面都应该跟着 aria-label；粗略但够用的检查。
	for _, label := range []string{
		`id="discoverEnabled" aria-label="打开自动发现"`,
		`id="discoverEnable" aria-label="发现出来的新订阅直接启用"`,
		`id="discoverBgmUser" placeholder="留空 = 不用" aria-label="Bangumi 用户名"`,
		`id="discoverDays" min="1" max="3650" placeholder="180" aria-label="自动发现的时间窗口天数"`,
		`id="discoverLimit" min="1" max="500" placeholder="50" aria-label="自动发现单次新增上限"`,
		`id="discoverExclude" rows="3"`,
	} {
		if !strings.Contains(page, label) {
			t.Errorf("设置页里这段结构变了，检查可访问名是否还在: %s", label)
		}
	}
}
