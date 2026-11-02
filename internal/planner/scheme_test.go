package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// fakeASS 是一个最小的 ANI-RSS 假实例：只实现 planner 真正会打的三个接口。
//
// 刻意不用 mock 框架：这里要证明的是「真实 HTTP + 真实解析 + 真实规则引擎」
// 这条链路，任何一层被替身换掉，结论就不成立了。
type fakeASS struct {
	groups []map[string]any
	ani    map[string]any
	// hits 记录每个接口被打了多少次，用来验证「两个方案共用候选」是否成立。
	hits map[string]int
}

func newFakeASS() *fakeASS {
	return &fakeASS{
		hits: map[string]int{},
		ani: map[string]any{
			"id":             "1",
			"title":          "测试番剧",
			"bgmUrl":         "https://bgm.tv/subject/975",
			"url":            "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=old",
			"subgroup":       "old",
			"season":         1,
			"offset":         0,
			"standbyRssList": []any{},
		},
	}
}

func (f *fakeASS) serve() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/listAni", func(w http.ResponseWriter, r *http.Request) {
		f.hits["listAni"]++
		writeData(w, map[string]any{
			"weekList": []any{map[string]any{"items": []any{f.ani}}},
			"total":    1,
		})
	})
	mux.HandleFunc("/api/aniBTGroup", func(w http.ResponseWriter, r *http.Request) {
		f.hits["aniBTGroup"]++
		writeData(w, f.groups)
	})
	mux.HandleFunc("/api/about", func(w http.ResponseWriter, r *http.Request) {
		writeData(w, map[string]any{"version": "test"})
	})
	return httptest.NewServer(mux)
}

func writeData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	raw, _ := json.Marshal(map[string]any{"code": 200, "message": "", "data": data})
	_, _ = w.Write(raw)
}

// group 造一个字幕组。
func group(name, rss string, total int, titles ...string) map[string]any {
	items := make([]any, 0, len(titles))
	for _, title := range titles {
		items = append(items, map[string]any{"title": title})
	}
	return map[string]any{
		"label": name,
		"rss":   rss,
		"total": total,
		"items": items,
	}
}

// TestBuildPicksPerInstanceScheme 是这次改动的验收主证据：
// 两个实例挂不同方案，同一条订阅必须挑出不同的主 RSS 与不同的副 RSS 条数。
//
// 改动之前的实现只有一个全局 engine，两个实例必然拿到同一个结果 ——
// 也就是说这条测试在旧代码上不可能通过。
func TestBuildPicksPerInstanceScheme(t *testing.T) {
	fake := newFakeASS()
	fake.groups = []map[string]any{
		group("LoliHouse", "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=lolihouse", 5,
			"[LoliHouse] 测试番剧 - 01 [简日][1080p][WEBRip]"),
		group("ToonsHub", "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=toonshub", 50,
			"[ToonsHub] 测试番剧 - 01 [简繁日内封][1080p]"),
		group("Nix-Raws", "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=nix", 30,
			"[Nix-Raws] 测试番剧 - 01 [简繁内封][1080p]"),
	}
	server := fake.serve()
	defer server.Close()

	cfg := config.Default()
	cfg.Sources.Order = []string{"anibt"}
	cfg.Sources.CacheMinutes = 0
	cfg.Instances = []config.Instance{
		{Name: "追番", BaseURL: server.URL, APIKey: "k1", Profile: "追番"},
		{Name: "收藏", BaseURL: server.URL, APIKey: "k2", Profile: "收藏"},
	}
	cfg.Profiles = []config.Profile{
		{
			Name: "追番",
			Rules: config.Rules{
				Primary: []config.Rule{
					{Name: "只要 LoliHouse", Match: `(?i)lolihouse`, Priority: 10, In: config.InGroup},
				},
				MaxStandby: 1,
			},
			Sources: &config.ProfileSources{Order: []string{"anibt"}},
		},
		{
			Name: "收藏",
			Rules: config.Rules{
				Primary: []config.Rule{
					{Name: "全收", Match: `.`, Priority: 500, Scope: config.ScopeBoth},
				},
				MaxStandby: 5,
			},
			Sources: &config.ProfileSources{Order: []string{"anibt"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("配置校验失败: %v", err)
	}

	plan, err := Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatalf("生成计划失败: %v", err)
	}
	if len(plan.Skipped) > 0 {
		t.Fatalf("不应该有被跳过的订阅: %+v", plan.Skipped)
	}

	urlOf := func(instance string) (string, int) {
		t.Helper()
		for _, action := range plan.Actions {
			if action.Instance != instance {
				continue
			}
			var url string
			var standby int
			for _, change := range action.Changes {
				switch change.Field {
				case "url":
					url = change.To
				case "standbyRssList":
					if change.To != "" {
						standby = len(strings.Split(change.To, " , "))
					}
				}
			}
			return url, standby
		}
		t.Fatalf("实例 %s 没有产出动作", instance)
		return "", 0
	}

	chaseURL, chaseStandby := urlOf("追番")
	if !strings.Contains(chaseURL, "groupSlug=lolihouse") {
		t.Fatalf("「追番」方案的主 RSS 应该是 LoliHouse，实际 %s", chaseURL)
	}
	if chaseStandby != 0 {
		t.Fatalf("「追番」方案没给任何 scope: standby 规则，副 RSS 应该为 0，实际 %d 条", chaseStandby)
	}

	collectURL, collectStandby := urlOf("收藏")
	if !strings.Contains(collectURL, "groupSlug=toonshub") {
		t.Fatalf("「收藏」方案没有规则约束，主 RSS 应该按热度选 ToonsHub，实际 %s", collectURL)
	}
	if collectStandby != 2 {
		t.Fatalf("「收藏」方案 maxStandby=5 且全收，副 RSS 应该是 2 条（Nix-Raws 与 LoliHouse），实际 %d 条", collectStandby)
	}

	if chaseURL == collectURL {
		t.Fatalf("两个方案挑出了同一个主 RSS（%s）：方案没生效", chaseURL)
	}

	// 同一条订阅在两个实例里只该爬一次站点。
	if fake.hits["aniBTGroup"] != 1 {
		t.Fatalf("同一番剧在两个实例里应该只爬一次站点，实际打了 %d 次", fake.hits["aniBTGroup"])
	}

	// 计划里要能看出每个实例挂的是哪套方案，否则无人值守时没法核对。
	if len(plan.Schemes) != 2 {
		t.Fatalf("计划里应该有 2 条方案摘要，实际 %d 条", len(plan.Schemes))
	}
	got := map[string]string{}
	for _, scheme := range plan.Schemes {
		got[scheme.Instance] = scheme.Profile
	}
	if got["追番"] != "追番" || got["收藏"] != "收藏" {
		t.Fatalf("方案摘要不对: %+v", got)
	}
}

// TestBuildTargetProfileOverridesInstance 证明「按番剧覆盖方案」真的生效。
//
// 真实用法：整库照常追番，但那几部本命要全收 —— 为 3 部番另开一个实例
// 是过度设计，按番剧覆盖正好对上「规则本来就是按番剧挑的」这件事。
func TestBuildTargetProfileOverridesInstance(t *testing.T) {
	fake := newFakeASS()
	fake.groups = []map[string]any{
		group("LoliHouse", "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=lolihouse", 5,
			"[LoliHouse] 测试番剧 - 01 [简日][1080p]"),
		group("ToonsHub", "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=toonshub", 50,
			"[ToonsHub] 测试番剧 - 01 [简繁日内封][1080p]"),
	}
	server := fake.serve()
	defer server.Close()

	cfg := config.Default()
	cfg.Sources.Order = []string{"anibt"}
	cfg.Sources.CacheMinutes = 0
	// 实例用顶层规则（没有规则 = 全靠热度挑），番剧级覆盖成「只要 LoliHouse」。
	cfg.Rules.Primary = []config.Rule{{Name: "全收", Match: `.`, Priority: 900, Scope: config.ScopeBoth}}
	cfg.Rules.MaxStandby = 1
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: server.URL, APIKey: "k1"}}
	cfg.Profiles = []config.Profile{{
		Name: "本命全收",
		Rules: config.Rules{
			Primary: []config.Rule{
				{Name: "只要 LoliHouse", Match: `(?i)lolihouse`, Priority: 1, In: config.InGroup},
			},
			MaxStandby: 4,
		},
	}}
	cfg.Targets = []config.Target{{BgmID: "975", Profile: "本命全收"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("配置校验失败: %v", err)
	}

	plan, err := Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatalf("生成计划失败: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("应该只处理 1 条订阅，实际 %d 条", len(plan.Actions))
	}
	action := plan.Actions[0]
	if action.Profile != "本命全收" {
		t.Fatalf("这条订阅应该记成覆盖了方案，实际 Profile=%q", action.Profile)
	}
	var picked string
	for _, change := range action.Changes {
		if change.Field == "url" {
			picked = change.To
		}
	}
	if !strings.Contains(picked, "groupSlug=lolihouse") {
		t.Fatalf("番剧级覆盖没生效：应该按覆盖方案挑 LoliHouse（热度更低），实际 %s", picked)
	}

	// 反向对照：把覆盖去掉，同一条订阅就该按实例那套（热度优先）挑 ToonsHub。
	cfg.Targets = nil
	plan, err = Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatalf("生成计划失败: %v", err)
	}
	picked = ""
	for _, change := range plan.Actions[0].Changes {
		if change.Field == "url" {
			picked = change.To
		}
	}
	if !strings.Contains(picked, "groupSlug=toonshub") {
		t.Fatalf("没有覆盖时应该按实例方案（热度）挑 ToonsHub，实际 %s", picked)
	}
}

// TestValidateRejectsUnknownTargetProfile 证明番剧级覆盖写错方案名会被拦下。
func TestValidateRejectsUnknownTargetProfile(t *testing.T) {
	cfg := config.Default()
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: "https://a.example.com/", APIKey: "k"}}
	cfg.Profiles = []config.Profile{{Name: "追番"}}
	cfg.Targets = []config.Target{{BgmID: "975", Profile: "不存在的方案"}}
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("番剧级覆盖指向不存在的方案，应该报错")
	}
	if !strings.Contains(err.Error(), "追番") {
		t.Fatalf("报错里应该列出可选方案名，实际: %v", err)
	}
}

// TestBuildRefusesToRunWithBrokenRules 证明「规则编译不了」不会被兜成空引擎。
//
// 这条守的是一个很危险的兜底：第一版在引擎编译失败时用 `rules.New(nil, nil, ...)`
// 顶上去，理由是「校验阶段会拦」。后果是——黑名单全部失效、所有候选同档按热度排、
// 副 RSS 因为没人被点名而被清空，也就是说一次静默的错误会把主副 RSS 一起换成
// 错的，而且还照样写回实例。绕过 Validate 的调用点（常驻循环、设置页注入的同步、
// status）恰恰是真实存在的，所以这里必须当场失败。
func TestBuildRefusesToRunWithBrokenRules(t *testing.T) {
	cfg := config.Default()
	cfg.Sources.Order = []string{"anibt"}
	cfg.Sources.CacheMinutes = 0
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: "http://127.0.0.1:1/", APIKey: "k"}}
	cfg.Profiles = []config.Profile{{
		Name: "追番",
		Rules: config.Rules{
			Primary: []config.Rule{{Name: "坏正则", Match: `(`, Priority: 1}},
		},
	}}
	cfg.Instances[0].Profile = "追番"

	// 刻意不调 Validate：模拟「某个调用点忘了校验」。
	if _, err := Build(context.Background(), cfg, Options{}); err == nil {
		t.Fatalf("规则编译不了时 Build 必须报错，不能拿空引擎顶上")
	}
}

// TestBuildProfilesWithDifferentSiteOrderFetchSeparately 证明「两套方案用不同站点」
// 时各自真的去爬了自己的站点，而不是复用对方的候选。
//
// 这条守的是一个隐蔽 bug：planner 的候选缓存在 Build 内存里、跨实例共享，
// 键如果只按番剧做，第二个方案会拿到第一个方案的候选 —— 规则看着生效，
// 候选却来自它根本没配的站点。所以键里必须带站点顺序签名。
func TestBuildProfilesWithDifferentSiteOrderFetchSeparately(t *testing.T) {
	fake := newFakeASS()
	fake.groups = []map[string]any{
		group("LoliHouse", "https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=lolihouse", 5,
			"[LoliHouse] 测试番剧 - 01 [简日][1080p]"),
	}
	server := fake.serve()
	defer server.Close()

	cfg := config.Default()
	cfg.Sources.Order = []string{"anibt"}
	cfg.Sources.CacheMinutes = 0
	cfg.Instances = []config.Instance{
		{Name: "甲", BaseURL: server.URL, APIKey: "k1", Profile: "甲方案"},
		{Name: "乙", BaseURL: server.URL, APIKey: "k2", Profile: "乙方案"},
	}
	cfg.Profiles = []config.Profile{
		{
			Name:    "甲方案",
			Rules:   config.Rules{Primary: []config.Rule{{Name: "全收", Match: `.`, Priority: 10}}},
			Sources: &config.ProfileSources{Order: []string{"anibt"}},
		},
		{
			Name:    "乙方案",
			Rules:   config.Rules{Primary: []config.Rule{{Name: "全收", Match: `.`, Priority: 10}}},
			Sources: &config.ProfileSources{Order: []string{"animegarden", "anibt"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("配置校验失败: %v", err)
	}

	plan, err := Build(context.Background(), cfg, Options{})
	if err != nil {
		t.Fatalf("生成计划失败: %v", err)
	}
	// 甲方案只配 anibt，乙方案配了 animegarden + anibt：
	// anibt 会被打两次（两个方案各一次），animegarden 只被乙方案打一次。
	// 若缓存键没带站点顺序，第二次 anibt 会被缓存命中，总次数就变成 2。
	if fake.hits["aniBTGroup"] < 2 {
		t.Fatalf("两套方案用了不同的站点集合，anibt 应该被爬两次以上，实际 %d 次（缓存键没带站点顺序？）",
			fake.hits["aniBTGroup"])
	}

	// 站点摘要要覆盖两个方案用到的全部站点。
	sites := map[string]bool{}
	for _, item := range plan.Sites {
		sites[item.Site] = true
	}
	if !sites["anibt"] {
		t.Fatalf("站点摘要里应该有 anibt，实际 %+v", plan.Sites)
	}
}
