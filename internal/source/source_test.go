package source

import (
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/assapi"
)

func TestCleanKeywordStripsAniRssSuffixes(t *testing.T) {
	cases := map[string]string{
		"铁旅！邂逅铁道少女 (2026) [tmdbid=302020]": "铁旅！邂逅铁道少女",
		"FX战士久留美 (2026) [tmdbid=311842]":   "FX战士久留美",
		"冰之城墙":       "冰之城墙",
		"葬送的芙莉莲【合集】": "葬送的芙莉莲",
	}
	for input, want := range cases {
		if got := CleanKeyword(input); got != want {
			t.Errorf("CleanKeyword(%q) = %q, 期望 %q", input, got, want)
		}
	}
}

// Nyaa 搜索页「RSS」按钮给出的链接就是这个形式，代码必须一字不差地对齐。
// 真实样例（用户实测）：搜 [ToonsHub] White Cat Legend S03 得到
// https://nyaa.si/?page=rss&q=%5BToonsHub%5D+White+Cat+Legend+S03&c=0_0&f=0
func TestNyaaSearchURLMatchesSiteRSSButton(t *testing.T) {
	n := &Nyaa{}
	want := "https://nyaa.si/?page=rss&q=%5BToonsHub%5D+White+Cat+Legend+S03&c=0_0&f=0"
	if got := n.searchURL("[ToonsHub] White Cat Legend S03", ""); got != want {
		t.Fatalf("nyaa 搜索地址应与站点 RSS 按钮一致\n实际 %s\n期望 %s", got, want)
	}
}

func TestNyaaSearchURLAppendsGroup(t *testing.T) {
	n := &Nyaa{}
	got := n.searchURL("氷の城壁", "LoliHouse")
	if !contains(got, "c=0_0") {
		t.Fatalf("nyaa 必须用全分类 0_0（没有字幕组维度，按分类过滤会漏结果），实际 %s", got)
	}
	if !contains(got, "LoliHouse") || !contains(got, "%E6%B0%B7") {
		t.Fatalf("nyaa 搜索词编码异常: %s", got)
	}
}

func TestMikanIDFromURL(t *testing.T) {
	cases := map[string]string{
		"https://mikanani.me/RSS/Bangumi?bangumiId=3887&subgroupid=203": "3887",
		"https://mikanani.me/RSS/Bangumi?subgroupid=203":                "",
		"https://anibt.net/rss/anime.xml?bgmId=665390":                  "",
	}
	for input, want := range cases {
		if got := MikanIDFromURL(input); got != want {
			t.Errorf("MikanIDFromURL(%q) = %q, 期望 %q", input, got, want)
		}
	}
}

// mikanGroup 接口吃的是番剧页地址，不是 RSS 地址。
func TestMikanPageFromID(t *testing.T) {
	if got := mikanPageFromID("3887"); got != "https://mikanani.me/Home/Bangumi/3887" {
		t.Fatalf("mikan 番剧页地址拼错: %s", got)
	}
	if got := mikanPageFromID("  "); got != "" {
		t.Fatalf("空 id 应返回空串，实际 %q", got)
	}
}

func TestSiteTypeMatchesAniRssValues(t *testing.T) {
	// 取值来自 ani-rss AniUtil.getAni 的 switch：mikan / ani-bt / anime-garden / 其它
	cases := map[string]string{
		"mikan":       "mikan",
		"anibt":       "ani-bt",
		"animegarden": "anime-garden",
		"nyaa":        "other",
	}
	for site, want := range cases {
		if got := SiteType(site); got != want {
			t.Errorf("SiteType(%q) = %q, 期望 %q", site, got, want)
		}
	}
}

// 实例返回的字幕组要带上最近标题，字幕类型规则才有东西可匹配。
func TestToCandidatesCarriesTitlesAndWeight(t *testing.T) {
	groups := []assapi.Group{{
		Label:  "桜都字幕组",
		RSS:    "https://mikanani.me/RSS/Bangumi?bangumiId=3887&subgroupid=203",
		BgmURL: "https://bgm.tv/subject/551918",
		Items: []assapi.GroupItem{
			{Title: "[桜都字幕组] 飙马野郎 [05][1080P][简繁内封]"},
			{Title: "[桜都字幕组] 飙马野郎 [04][1080P][简繁内封]"},
		},
	}}
	out := toCandidates("mikan", groups)
	if len(out) != 1 {
		t.Fatalf("应有 1 个候选，实际 %d", len(out))
	}
	got := out[0]
	if got.Label != "桜都字幕组" || got.Subgroup != "桜都字幕组" {
		t.Fatalf("字幕组名解析错误: %+v", got)
	}
	if got.Type != "mikan" {
		t.Fatalf("mikan 候选的 type 应为 mikan，实际 %q", got.Type)
	}
	if got.Weight != 2 || len(got.Titles) != 2 {
		t.Fatalf("热度/标题样本数不对: weight=%d titles=%d", got.Weight, len(got.Titles))
	}
}

func TestToCandidatesSkipsGroupsWithoutRSS(t *testing.T) {
	out := toCandidates("anibt", []assapi.Group{{Name: "没有 RSS 的组"}, {Name: "正常组", RSS: "https://x/rss"}})
	if len(out) != 1 || out[0].Label != "正常组" {
		t.Fatalf("缺少 RSS 的组应被跳过，实际 %+v", out)
	}
}

// 缓存只存标题样本，热度必须靠 Total 保住，否则排序会退化。
func TestTrimGroupsKeepsWeight(t *testing.T) {
	items := make([]assapi.GroupItem, 0, 40)
	for i := 0; i < 40; i++ {
		items = append(items, assapi.GroupItem{Title: "t"})
	}
	trimmed := trimGroups([]assapi.Group{{Name: "大组", RSS: "https://x/rss", Items: items}})
	if len(trimmed[0].Items) != maxSampleTitles {
		t.Fatalf("标题样本应截到 %d 条，实际 %d", maxSampleTitles, len(trimmed[0].Items))
	}
	if trimmed[0].Total != 40 {
		t.Fatalf("Total 应保留原始条数 40，实际 %d", trimmed[0].Total)
	}
	// 缓存命中后再转候选，热度不能变成样本数
	out := toCandidates("mikan", trimmed)
	if out[0].Weight != 40 {
		t.Fatalf("缓存命中后热度应仍为 40，实际 %d", out[0].Weight)
	}
}

// 组名别名：只取标题**开头**方括号里的名字。
//
// 目的不是「猜这个组叫什么」，而是「规则里写哪一个名字都能命中」，
// 于是显示名（猎户发布组）和种子名（猎户压制部）不用都写。
// 只取开头那一个，是因为后面的方括号是集数 / 分辨率 / 字幕类型，不是组名。
func TestGroupAliasesFromTitles(t *testing.T) {
	got := GroupAliasesFromTitles([]string{
		"[猎户压制部] 朱色假面 [02][1080P][简繁日内封]",
		"【喵萌奶茶屋】★10月新番★[被放逐的作弊赋予魔法师][01][1080P]",
		"[猎户压制部] 朱色假面 [01][1080P][简繁日内封]",
		"没有方括号的标题 01",
	})
	want := []string{"猎户压制部", "喵萌奶茶屋"}
	if len(got) != len(want) {
		t.Fatalf("别名应只有标题开头的方括号，期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("别名不匹配，期望 %v，实际 %v", want, got)
		}
	}
}

// 没有方括号的标题不该凭空造出别名 —— 作品名里的 Ani 不是组名。
func TestGroupAliasesIgnoresBracketlessTitles(t *testing.T) {
	if got := GroupAliasesFromTitles([]string{"Ani ni Tsukeru Kusuri wa Nai 01"}); len(got) != 0 {
		t.Fatalf("没有方括号就没有别名，实际 %v", got)
	}
	if got := GroupAliasesFromTitles(nil); got != nil {
		t.Fatalf("没有标题时不应有别名，实际 %v", got)
	}
}

// 候选要带上别名，规则才能用一个名字同时命中显示名和种子名两种情况。
func TestToCandidatesCarriesGroupAliases(t *testing.T) {
	out := toCandidates("animegarden", []assapi.Group{{
		Name:  "猎户发布组",
		RSS:   "https://api.animes.garden/feed.xml?subject=517106&fansub=猎户发布组",
		Items: []assapi.GroupItem{{Title: "[猎户压制部] 朱色假面 [02][1080P][简繁日内封]"}},
	}})
	if len(out) != 1 {
		t.Fatalf("应有 1 个候选，实际 %d", len(out))
	}
	if out[0].Label != "猎户发布组" {
		t.Fatalf("Label 应是显示名，实际 %q", out[0].Label)
	}
	found := false
	for _, alias := range out[0].GroupAliases {
		if alias == "猎户压制部" {
			found = true
		}
	}
	if !found {
		t.Fatalf("别名里应有种子名 猎户压制部，实际 %v", out[0].GroupAliases)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
