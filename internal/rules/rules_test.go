package rules

import (
	"testing"

	"github.com/zzzwannasleep/anisync/internal/source"
)

func mustEngine(t *testing.T, primary []Rule, deny []DenyRule, maxStandby int) *Engine {
	t.Helper()
	engine, err := New(primary, deny, maxStandby)
	if err != nil {
		t.Fatalf("编译规则失败: %v", err)
	}
	return engine
}

func labels(candidates []source.Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Label+"@"+candidate.Site)
	}
	return out
}

func TestRankPriorityBeatsSiteOrder(t *testing.T) {
	engine := mustEngine(t, []Rule{
		{Name: "桜都", Match: `(?i)sakurato|桜都`, Priority: 10},
		{Name: "LoliHouse", Match: `(?i)lolihouse`, Priority: 20},
	}, nil, 3)

	// 站点顺序上 anibt 更靠前，但 sakurato 在 mikan 上、且规则优先级更高
	candidates := []source.Candidate{
		{Site: "anibt", Label: "LoliHouse", SiteOrder: 0},
		{Site: "mikan", Label: "桜都字幕组", SiteOrder: 2},
	}
	ranked := engine.Rank(candidates)
	if got := labels(ranked); len(got) != 2 || got[0] != "桜都字幕组@mikan" {
		t.Fatalf("期望桜都排第一，实际 %v", got)
	}
}

func TestRankDeduplicatesSameGroupAcrossSites(t *testing.T) {
	engine := mustEngine(t, nil, nil, 3)
	candidates := []source.Candidate{
		{Site: "anibt", Label: "桜都字幕组", SiteOrder: 0, Weight: 5, URL: "a"},
		{Site: "animegarden", Label: "桜都字幕组", SiteOrder: 1, Weight: 99, URL: "b"},
		{Site: "mikan", Label: "LoliHouse", SiteOrder: 2, URL: "c"},
	}
	ranked := engine.Rank(candidates)
	if len(ranked) != 2 {
		t.Fatalf("同名字幕组应只保留一条，实际 %d 条: %v", len(ranked), labels(ranked))
	}
	// 同优先级下按 weight 降序，所以保留 animegarden 那条
	if ranked[0].URL != "b" {
		t.Fatalf("应保留 weight 更高的镜像，实际 %v", labels(ranked))
	}
}

func TestRankWeightBreaksTieWithoutRules(t *testing.T) {
	engine := mustEngine(t, nil, nil, 3)
	candidates := []source.Candidate{
		{Site: "anibt", Label: "杂鱼组", SiteOrder: 0, Weight: 1},
		{Site: "anibt", Label: "主力组", SiteOrder: 0, Weight: 50},
	}
	ranked := engine.Rank(candidates)
	if ranked[0].Label != "主力组" {
		t.Fatalf("无规则命中时应按发布量排序，实际 %v", labels(ranked))
	}
}

func TestDenyRuleDropsCandidate(t *testing.T) {
	engine := mustEngine(t, nil, []DenyRule{{Match: `合集|BDRip`, Why: "不要合集"}}, 3)
	candidates := []source.Candidate{
		{Site: "anibt", Label: "某某字幕组", URL: "https://x/?q=BDRip"},
		{Site: "anibt", Label: "正常组", URL: "https://x/?q=normal"},
	}
	ranked := engine.Rank(candidates)
	if len(ranked) != 1 || ranked[0].Label != "正常组" {
		t.Fatalf("合集候选应被剔除，实际 %v", labels(ranked))
	}
}

func TestSiteScopedRuleOnlyMatchesThatSite(t *testing.T) {
	engine := mustEngine(t, []Rule{{Name: "只在 nyaa 生效", Match: "ToonsHub", Priority: 5, Site: "nyaa"}}, nil, 3)

	anibtCandidate := source.Candidate{Site: "anibt", Label: "ToonsHub", SiteOrder: 0}
	if name, priority, ok := engine.Match(anibtCandidate); ok {
		t.Fatalf("站点限定规则不应命中 anibt 候选，实际 name=%q priority=%d", name, priority)
	} else if priority != UnmatchedPriority {
		t.Fatalf("未命中时应为兜底优先级，实际 %d", priority)
	}

	nyaaCandidate := source.Candidate{Site: "nyaa", Label: "ToonsHub", SiteOrder: 3}
	name, priority, ok := engine.Match(nyaaCandidate)
	if !ok || name != "只在 nyaa 生效" || priority != 5 {
		t.Fatalf("nyaa 候选应命中站点限定规则，实际 ok=%v name=%q priority=%d", ok, name, priority)
	}
}

func TestSiteScopedRulePromotesItsSiteInDedupe(t *testing.T) {
	// 同名候选跨站点去重时，被站点限定规则提权的那条应该胜出
	engine := mustEngine(t, []Rule{{Name: "nyaa 优先", Match: "ToonsHub", Priority: 5, Site: "nyaa"}}, nil, 3)
	ranked := engine.Rank([]source.Candidate{
		{Site: "anibt", Label: "ToonsHub", SiteOrder: 0, URL: "anibt-url"},
		{Site: "nyaa", Label: "ToonsHub", SiteOrder: 3, URL: "nyaa-url"},
	})
	if len(ranked) != 1 || ranked[0].URL != "nyaa-url" {
		t.Fatalf("应保留被规则提权的 nyaa 候选，实际 %v", labels(ranked))
	}
}

func TestInvalidRegexIsRejected(t *testing.T) {
	if _, err := New([]Rule{{Name: "坏规则", Match: "([", Priority: 1}}, nil, 3); err == nil {
		t.Fatal("非法正则应当报错")
	}
}

// 字幕类型（简日/繁日/内嵌/内封）只写在资源标题里，规则必须能匹配到标题。
func TestRuleMatchesSubtitleTypeInTitles(t *testing.T) {
	engine := mustEngine(t, []Rule{
		{Name: "简日内嵌", Match: `简日|CHS`, Priority: 10},
		{Name: "繁日内嵌", Match: `繁日|CHT`, Priority: 20},
	}, nil, 3)

	chs := source.Candidate{Site: "anibt", Label: "桜都字幕组", Titles: []string{"[桜都字幕组] 冰之城墙 [15][1080P][简日内嵌]"}}
	if name, priority, ok := engine.Match(chs); !ok || name != "简日内嵌" || priority != 10 {
		t.Fatalf("简日标题应命中简日规则，实际 ok=%v name=%q priority=%d", ok, name, priority)
	}

	cht := source.Candidate{Site: "anibt", Label: "桜都字幕组", Titles: []string{"[桜都字幕组] 冰之城墙 [15][1080P][繁日内嵌]"}}
	if name, _, ok := engine.Match(cht); !ok || name != "繁日内嵌" {
		t.Fatalf("繁日标题应命中繁日规则，实际 ok=%v name=%q", ok, name)
	}
}

// 主 RSS 用字幕组规则、副 RSS 用放流组规则：靠 scope 把两套规则分开。
func TestStandbyScopeSelectsRawReleaseGroup(t *testing.T) {
	engine := mustEngine(t, []Rule{
		{Name: "主力字幕组", Match: `桜都`, Priority: 10, Scope: ScopeMain},
		{Name: "放流组", Match: `ToonsHub`, Priority: 10, Scope: ScopeStandby},
	}, nil, 3)

	// 字幕组候选在主 RSS 排第一，但它不参与副 RSS 规则，所以副 RSS 里排不过放流组
	candidates := []source.Candidate{
		{Site: "mikan", Label: "桜都字幕组", SiteOrder: 2, Weight: 9, URL: "sub"},
		{Site: "nyaa", Label: "ToonsHub", SiteOrder: 3, Weight: 1, URL: "raw"},
	}
	ranked := engine.Rank(candidates)
	if ranked[0].URL != "sub" {
		t.Fatalf("主 RSS 应是字幕组，实际 %v", labels(ranked))
	}
	standby := engine.Standby(ranked[1:])
	if len(standby) != 1 || standby[0].URL != "raw" {
		t.Fatalf("副 RSS 应是放流组，实际 %v", labels(standby))
	}
}

// 副 RSS 只放被 standby 规则点过名的组。
//
// 这里以前是「没命中就按发布条数兜底」，真实数据里那样会被搬运组刷满：
// Kirara Fantasia（2833 条样本）、生肉/不明字幕、20190912 这类实例侧伪组名，
// 全靠热度挤进副 RSS，副 RSS 也就没意义了。所以现在没点名的一律不要。
func TestStandbyOnlyKeepsNamedGroups(t *testing.T) {
	engine := mustEngine(t, []Rule{
		{Name: "字幕组", Match: `桜都`, Priority: 10},
		{Name: "LoliHouse", Match: `LoliHouse`, Priority: 10, Scope: ScopeStandby},
		{Name: "ToonsHub", Match: `ToonsHub`, Priority: 20, Scope: ScopeStandby},
	}, nil, 5)

	ranked := engine.Rank([]source.Candidate{
		{Site: "mikan", Label: "桜都字幕组", SiteOrder: 0, Weight: 9, URL: "sub"},
		{Site: "anibt", Label: "热门搬运组", SiteOrder: 1, Weight: 9999, URL: "junk"},
		{Site: "mikan", Label: "生肉/不明字幕", SiteOrder: 4, Weight: 8888, URL: "unknown"},
		{Site: "nyaa", Label: "ToonsHub", SiteOrder: 2, Weight: 1, URL: "toons"},
		{Site: "anibt", Label: "LoliHouse", SiteOrder: 3, Weight: 1, URL: "loli"},
	})
	standby := engine.Standby(ranked[1:])

	if len(standby) != 2 {
		t.Fatalf("副 RSS 只应有被点名的两个组，实际 %v", labels(standby))
	}
	// 顺序由规则的 priority 决定：LoliHouse(10) 排在 ToonsHub(20) 前面
	if standby[0].URL != "loli" || standby[1].URL != "toons" {
		t.Fatalf("副 RSS 应按规则优先级排序，实际 %v", labels(standby))
	}
}

// 没配任何 standby 规则时副 RSS 就是空的：没点名就是不要，不拿热度凑数。
func TestStandbyEmptyWithoutStandbyRules(t *testing.T) {
	engine := mustEngine(t, []Rule{{Name: "字幕组", Match: `桜都`, Priority: 10}}, nil, 2)
	ranked := engine.Rank([]source.Candidate{
		{Site: "mikan", Label: "桜都字幕组", SiteOrder: 0, Weight: 9, URL: "a"},
		{Site: "nyaa", Label: "热门组", SiteOrder: 2, Weight: 50, URL: "c"},
	})
	if standby := engine.Standby(ranked[1:]); len(standby) != 0 {
		t.Fatalf("没有 standby 规则时副 RSS 应为空，实际 %v", labels(standby))
	}
}

// maxStandby 决定最多留几条。多数人只要一条备胎；「所有版本都收」的人把它调大，
// 并在规则里把想要的组都写成 scope: standby。
func TestStandbyRespectsMaxStandby(t *testing.T) {
	primary := []Rule{
		{Name: "LoliHouse", Match: `LoliHouse`, Priority: 10, Scope: ScopeStandby},
		{Name: "Nix-Raws", Match: `Nix-Raws`, Priority: 20, Scope: ScopeStandby},
		{Name: "ToonsHub", Match: `ToonsHub`, Priority: 30, Scope: ScopeStandby},
	}
	candidates := []source.Candidate{
		{Site: "anibt", Label: "LoliHouse", SiteOrder: 0, URL: "loli"},
		{Site: "anibt", Label: "Nix-Raws", SiteOrder: 1, URL: "nix"},
		{Site: "nyaa", Label: "ToonsHub", SiteOrder: 2, URL: "toons"},
	}

	one := mustEngine(t, primary, nil, 1)
	if standby := one.Standby(one.Rank(candidates)); len(standby) != 1 || standby[0].URL != "loli" {
		t.Fatalf("maxStandby=1 时应只留优先级最高的 LoliHouse，实际 %v", labels(standby))
	}

	all := mustEngine(t, primary, nil, 3)
	if standby := all.Standby(all.Rank(candidates)); len(standby) != 3 {
		t.Fatalf("maxStandby=3 时应三条全留，实际 %v", labels(standby))
	}
}

func TestScopeBothParticipatesInBoth(t *testing.T) {
	engine := mustEngine(t, []Rule{{Name: "两边都算", Match: `通用组`, Priority: 1, Scope: ScopeBoth}}, nil, 3)
	candidate := source.Candidate{Site: "anibt", Label: "通用组"}
	if _, _, ok := engine.Match(candidate); !ok {
		t.Fatal("scope=both 应参与主 RSS 排序")
	}
	if _, _, ok := engine.StandbyMatch(candidate); !ok {
		t.Fatal("scope=both 应参与副 RSS 排序")
	}
}
