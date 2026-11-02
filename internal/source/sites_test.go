package source

import (
	"sort"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// TestKnownSitesMatchConfig 守住两份站点清单不分叉。
//
// 站点标识要在两个地方各写一次：config 用它在 Validate 里当场指出写错的名字，
// source 用它在 NewRegistry 里建适配器。两份清单漂了的后果是不对称的：
//   - source 里少一个 -> 配置里写对了也被当成不存在，Regsitry 静默丢掉，表现为「这个站点不返回候选」；
//   - config 里少一个 -> 用户写了正确的站点名却被判成非法，配置直接过不了校验。
//
// 两种都很难从现象反推原因，所以用一条测试把两份清单钉在一起。
func TestKnownSitesMatchConfig(t *testing.T) {
	fromSource := KnownSites()
	fromConfig := config.KnownSiteNames

	if len(fromSource) != len(fromConfig) {
		t.Fatalf("站点数量不一致：source %d 个 %v，config %d 个 %v",
			len(fromSource), fromSource, len(fromConfig), fromConfig)
	}
	sortedSource := append([]string(nil), fromSource...)
	sortedConfig := append([]string(nil), fromConfig...)
	sort.Strings(sortedSource)
	sort.Strings(sortedConfig)
	for i := range sortedSource {
		if sortedSource[i] != sortedConfig[i] {
			t.Fatalf("第 %d 个站点不一致：source=%q config=%q（完整：%v vs %v）",
				i, sortedSource[i], sortedConfig[i], sortedSource, sortedConfig)
		}
	}
}

// TestRegistryAcceptsEveryKnownSite 证明清单里每个站点都能真的建出适配器。
//
// 只比对字符串不够：如果 NewRegistry 的映射表里漏了一个名字，
// 清单两边一致但那个站点照样会被静默忽略。
func TestRegistryAcceptsEveryKnownSite(t *testing.T) {
	registry := NewRegistry(nil, nil, 0, KnownSites(), nil)
	got := registry.Order()
	if len(got) != len(KnownSites()) {
		t.Fatalf("注册表只认下 %d 个站点 %v，清单里有 %d 个 %v",
			len(got), got, len(KnownSites()), KnownSites())
	}
	for i, want := range KnownSites() {
		if got[i] != want {
			t.Fatalf("第 %d 个站点：注册表 %q，清单 %q", i, got[i], want)
		}
	}
}

// TestRegistrySilentlyDropsUnknownSite 记录一个已知的安静行为。
//
// NewRegistry 对认不出的站点名是**丢弃**而不是报错。这个行为本身保留
// （注册表不该替配置做校验），但必须有人替它报错 —— 那就是 config.Validate。
// 这条测试把这个前提钉住：哪天 NewRegistry 改成报错了，这个测试会失败，
// 提醒去 Review 那些「以为它不会报错」的调用点。
func TestRegistrySilentlyDropsUnknownSite(t *testing.T) {
	registry := NewRegistry(nil, nil, 0, []string{"anibt", "nyaa2", "nyaa"}, nil)
	got := registry.Order()
	if len(got) != 2 || got[0] != "anibt" || got[1] != "nyaa" {
		t.Fatalf("认不出的站点应该被丢掉、其余保持顺序，实际 %v", got)
	}
}
