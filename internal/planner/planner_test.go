package planner

import (
	"testing"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/source"
)

func intPtr(v int) *int { return &v }

func offsetOf(t *testing.T, item any) int {
	t.Helper()
	m, ok := item.(map[string]any)
	if !ok {
		t.Fatalf("备用 RSS 项不是 map: %#v", item)
	}
	offset, _ := intField(m, "offset")
	return offset
}

// ANI-RSS 会按每个备用 RSS 的实际集数自动推断 offset（AniUtil.autoOffset）。
// 备用 RSS 列表每次同步都要整体重建，offset 必须按 URL 继承回来，否则一次同步就把它们全洗成 0。
func TestStandbyListInheritsExistingOffset(t *testing.T) {
	existing := assapi.Ani{
		"standbyRssList": []any{
			map[string]any{"label": "LoliHouse", "url": "https://a", "offset": float64(-12)},
			map[string]any{"label": "已下线", "url": "https://gone", "offset": float64(3)},
		},
	}
	list := standbyList([]source.Candidate{
		{Label: "LoliHouse", URL: "https://a"},
		{Label: "新组", URL: "https://new"},
	}, existing, nil)

	if got := offsetOf(t, list[0]); got != -12 {
		t.Fatalf("已有 RSS 的偏移应被继承为 -12，实际 %d", got)
	}
	if got := offsetOf(t, list[1]); got != 0 {
		t.Fatalf("新出现的 RSS 没有原值，偏移应为 0，实际 %d", got)
	}
}

func TestStandbyListOverrideWinsOverInherited(t *testing.T) {
	existing := assapi.Ani{
		"standbyRssList": []any{
			map[string]any{"label": "LoliHouse", "url": "https://a", "offset": float64(-12)},
		},
	}
	list := standbyList([]source.Candidate{{Label: "LoliHouse", URL: "https://a"}}, existing, intPtr(0))
	if got := offsetOf(t, list[0]); got != 0 {
		t.Fatalf("配置显式写了 standbyOffset 就应覆盖继承值，实际 %d", got)
	}
}

// 回归护栏：URL 与偏移都没变时必须判为无需变更，不能每次都写回一遍。
func TestDiffNoopWhenOffsetsInherited(t *testing.T) {
	ani := assapi.Ani{
		"url":      "https://primary",
		"subgroup": "桜都字幕组",
		"offset":   float64(-12),
		"season":   float64(1),
		"standbyRssList": []any{
			map[string]any{"label": "LoliHouse", "url": "https://a", "offset": float64(-12)},
		},
	}
	primary := source.Candidate{URL: "https://primary", Subgroup: "桜都字幕组"}
	desired := standbyList([]source.Candidate{{Label: "LoliHouse", URL: "https://a"}}, ani, nil)

	if changes := diff(ani, primary, desired, offsets{}); len(changes) != 0 {
		t.Fatalf("没写季度/偏移配置时应完全不碰，实际产生变更 %+v", changes)
	}
}

// 只改了偏移也必须被识别出来，否则 setAni 永远不会被调用。
func TestDiffDetectsStandbyOffsetChange(t *testing.T) {
	ani := assapi.Ani{
		"url":      "https://primary",
		"subgroup": "桜都字幕组",
		"standbyRssList": []any{
			map[string]any{"label": "LoliHouse", "url": "https://a", "offset": float64(-12)},
		},
	}
	primary := source.Candidate{URL: "https://primary", Subgroup: "桜都字幕组"}
	desired := standbyList([]source.Candidate{{Label: "LoliHouse", URL: "https://a"}}, ani, intPtr(-5))

	changes := diff(ani, primary, desired, offsets{})
	if len(changes) != 1 || changes[0].Field != "standbyRssList" {
		t.Fatalf("备用 RSS 偏移变化应产生一条 standbyRssList 变更，实际 %+v", changes)
	}
}

func TestDiffDetectsMainOffsetAndSeason(t *testing.T) {
	ani := assapi.Ani{"url": "https://primary", "offset": float64(0), "season": float64(1)}
	primary := source.Candidate{URL: "https://primary"}

	changes := diff(ani, primary, nil, offsets{main: intPtr(-12), season: intPtr(2)})
	if len(changes) != 2 {
		t.Fatalf("应产生 offset 与 season 两条变更，实际 %+v", changes)
	}
	if changes[0].Field != "season" || changes[1].Field != "offset" {
		t.Fatalf("变更字段名不对: %+v", changes)
	}
}

// 新增订阅：target 上的值优先，其次回落到全局配置。
func TestOffsetsOfPrecedence(t *testing.T) {
	cfg := &config.Config{
		Sync: config.Sync{Season: intPtr(1), Offset: intPtr(0), StandbyOffset: intPtr(0)},
	}
	global := offsetsOf(cfg, nil)
	if global.season == nil || *global.season != 1 || global.main == nil || *global.main != 0 {
		t.Fatalf("没有 target 时应取全局配置，实际 %+v", global)
	}

	target := config.Target{Season: intPtr(2), Offset: intPtr(-12)}
	merged := offsetsOf(cfg, &target)
	if merged.season == nil || *merged.season != 2 {
		t.Fatalf("target 的 season 应覆盖全局，实际 %+v", merged.season)
	}
	if merged.main == nil || *merged.main != -12 {
		t.Fatalf("target 的 offset 应覆盖全局，实际 %+v", merged.main)
	}
	if merged.standby == nil || *merged.standby != 0 {
		t.Fatalf("target 没写的字段应回落到全局，实际 %+v", merged.standby)
	}
}

func TestApplyOffsetsSkipsUnsetFields(t *testing.T) {
	body := map[string]any{"url": "https://x", "offset": 7, "season": 3}
	applyOffsets(body, offsets{})
	if body["offset"] != 7 || body["season"] != 3 {
		t.Fatalf("没配置时不应改动已有值，实际 %+v", body)
	}

	applyOffsets(body, offsets{main: intPtr(-1), season: intPtr(4)})
	if body["offset"] != -1 || body["season"] != 4 {
		t.Fatalf("配置了就该覆盖，实际 %+v", body)
	}
}

// 展示用文本要能看出偏移，不然计划里两条 RSS 长得一模一样。
func TestStandbyTextShowsOffset(t *testing.T) {
	list := []any{
		map[string]any{"label": "LoliHouse", "url": "https://a", "offset": -12},
		map[string]any{"label": "ToonsHub", "url": "https://b", "offset": 0},
	}
	if got := standbyText(list); got != "LoliHouse(偏移-12) , ToonsHub" {
		t.Fatalf("备用 RSS 文本不对: %q", got)
	}
}

// 番剧级配置优先按 bgmId 精确匹配，这是最稳的一条路。
func TestMatchTargetByBgmID(t *testing.T) {
	cfg := &config.Config{Targets: []config.Target{
		{Title: "别的番", BgmID: "111111", Season: intPtr(1)},
		{Title: "冰之城墙", BgmID: "665390", Season: intPtr(2)},
	}}
	ani := assapi.Ani{
		"title":  "冰之城墙 (2026) [tmdbid=283428]",
		"bgmUrl": "https://bgm.tv/subject/665390",
	}

	target := matchTarget(cfg, "主库", ani)
	if target == nil {
		t.Fatal("按 bgmId 应命中「冰之城墙」")
	}
	if target.BgmID != "665390" {
		t.Fatalf("命中了错误的番剧: %+v", target)
	}
}

// 订阅标题带年份与 tmdbid，只能包含匹配；写中文名或日文名都要能命中。
func TestMatchTargetByTitleContains(t *testing.T) {
	cfg := &config.Config{Targets: []config.Target{
		{Title: "冰之城墙", Season: intPtr(2), Offset: intPtr(-12), StandbyOffset: intPtr(-12)},
	}}
	ani := assapi.Ani{
		"title":   "冰之城墙 (2026) [tmdbid=283428]",
		"jpTitle": "氷の城壁 第2期",
	}

	target := matchTarget(cfg, "主库", ani)
	if target == nil || target.Title != "冰之城墙" {
		t.Fatalf("标题包含匹配应命中，实际 %+v", target)
	}

	jp := &config.Config{Targets: []config.Target{{Title: "氷の城壁", Season: intPtr(2)}}}
	if matchTarget(jp, "主库", ani) == nil {
		t.Fatal("日文标题也应能命中")
	}
}

func TestMatchTargetRespectsInstance(t *testing.T) {
	cfg := &config.Config{Targets: []config.Target{
		{Title: "冰之城墙", Instances: []string{"备用库"}, Season: intPtr(2)},
	}}
	ani := assapi.Ani{"title": "冰之城墙 (2026) [tmdbid=283428]"}

	if matchTarget(cfg, "主库", ani) != nil {
		t.Fatal("限定实例为「备用库」时不应在主库命中")
	}
	if matchTarget(cfg, "备用库", ani) == nil {
		t.Fatal("限定的实例上应命中")
	}
}

func TestMatchTargetNoneWhenUnmatched(t *testing.T) {
	ani := assapi.Ani{"title": "冰之城墙 (2026) [tmdbid=283428]"}
	if matchTarget(&config.Config{}, "主库", ani) != nil {
		t.Fatal("没有 targets 时不应命中")
	}
	cfg := &config.Config{Targets: []config.Target{{Title: "魔法骑士雷阿斯", Season: intPtr(1)}}}
	if matchTarget(cfg, "主库", ani) != nil {
		t.Fatal("标题对不上时不应命中")
	}
}

// 已有订阅（不是新增）命中番剧级配置时，季度与主副偏移都要按它来，
// 而不是回落到全局值 —— 这是「和 ASS 一样按番剧规定」的关键。
func TestExistingSubscriptionUsesTargetOffsets(t *testing.T) {
	cfg := &config.Config{
		Sync: config.Sync{Season: intPtr(1), Offset: intPtr(0), StandbyOffset: intPtr(0)},
		Targets: []config.Target{
			{Title: "冰之城墙", Season: intPtr(2), Offset: intPtr(-12), StandbyOffset: intPtr(-12)},
		},
	}
	ani := assapi.Ani{"title": "冰之城墙 (2026) [tmdbid=283428]"}

	off := offsetsOf(cfg, matchTarget(cfg, "主库", ani))
	if off.season == nil || *off.season != 2 {
		t.Fatalf("季度应取番剧级配置的 2，实际 %+v", off.season)
	}
	if off.main == nil || *off.main != -12 {
		t.Fatalf("主 RSS 偏移应取番剧级配置的 -12，实际 %+v", off.main)
	}
	if off.standby == nil || *off.standby != -12 {
		t.Fatalf("副 RSS 偏移应取番剧级配置的 -12，实际 %+v", off.standby)
	}

	// 没命中的番剧仍然按全局兜底，不被别的番剧配置带跑。
	other := assapi.Ani{"title": "世界最强魔女，始动 (2026) [tmdbid=321518]"}
	fallback := offsetsOf(cfg, matchTarget(cfg, "主库", other))
	if fallback.season == nil || *fallback.season != 1 {
		t.Fatalf("未命中番剧应回落到全局季度 1，实际 %+v", fallback.season)
	}
}

// 自动发现靠这份索引判断「实例里是不是真的没有这个番剧」。
// 同一个番剧在不同 RSS 来源下标题可能是中文名或日文原名，只比标题会漏，
// 所以 bgmId 对得上就必须判为已存在，哪怕标题完全不同。
func TestExistingIndexMatchesByBgmID(t *testing.T) {
	ix := newExistingIndex()
	ix.add("主库", assapi.Ani{
		"title":  "冰之城墙 (2026) [tmdbid=283428]",
		"bgmUrl": "https://bgm.tv/subject/665390",
	})

	if !ix.has("主库", config.Target{BgmID: "665390", Title: "完全不同的标题"}) {
		t.Fatal("bgmId 对得上就该判为已存在，标题不同也算")
	}
	if ix.has("备用库", config.Target{BgmID: "665390"}) {
		t.Fatal("索引按实例隔离，另一个实例里没有就不算存在")
	}
	if ix.has("主库", config.Target{BgmID: "999999"}) {
		t.Fatal("bgmId 对不上就不该判为存在")
	}
}

// 没有 bgmId 时退回标题包含匹配，中文名与日文原名都要认。
func TestExistingIndexFallsBackToTitle(t *testing.T) {
	ix := newExistingIndex()
	ix.add("主库", assapi.Ani{
		"title":   "冰之城墙 (2026) [tmdbid=283428]",
		"jpTitle": "氷の城壁 第2期",
	})

	if !ix.has("主库", config.Target{Title: "冰之城墙"}) {
		t.Fatal("没有 bgmId 时应退回标题包含匹配")
	}
	if !ix.has("主库", config.Target{Title: "氷の城壁"}) {
		t.Fatal("日文原名也要能匹配")
	}
	if ix.has("主库", config.Target{Title: "魔法骑士雷阿斯"}) {
		t.Fatal("标题对不上时不应误判为已存在")
	}
}

// 索引里没有的实例要能安全查询，不能因为 map 未初始化就 panic。
func TestExistingIndexEmptyIsSafe(t *testing.T) {
	ix := newExistingIndex()
	if ix.has("主库", config.Target{BgmID: "1", Title: "随便"}) {
		t.Fatal("空索引不应命中任何条目")
	}
}

// 日文原名要参与番剧级配置的匹配：订阅是从 Nyaa 之类来源建的时，标题里只有日文。
func TestMatchTargetByJPTitleField(t *testing.T) {
	cfg := &config.Config{Targets: []config.Target{
		{Title: "赛博朋克：边缘行者 2", JPTitle: "サイバーパンク: エッジランナーズ２", Season: intPtr(2)},
	}}
	ani := assapi.Ani{"title": "サイバーパンク: エッジランナーズ２ (2026) [tmdbid=513878]"}

	target := matchTarget(cfg, "主库", ani)
	if target == nil || target.JPTitle == "" {
		t.Fatalf("应能按 target 里的日文原名命中，实际 %+v", target)
	}
}
