package bridge

import (
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/httpx"
	"github.com/zzzwannasleep/anisync/internal/source"
)

// ---------- 7.11 probe / bgm ----------

// probeCandidate 是探针结果里的一条候选。
type probeCandidate struct {
	Group   string   `json:"group"`
	Title   string   `json:"title"`
	RSS     string   `json:"rss"`
	Weight  int      `json:"weight"`
	Aliases []string `json:"aliases"`
	Matched bool     `json:"matched"`
	Rule    string   `json:"rule,omitempty"`
	Rank    int      `json:"rank"`
}

// probeRunPayload 是 probe.run 的 payload。
type probeRunPayload struct {
	Site     string `json:"site"`
	BgmID    string `json:"bgmId"`
	Title    string `json:"title"`
	Instance string `json:"instance"`
}

// opProbeRun 单站点探针：只跑一个来源，把候选和错误原样交出来。
//
// 它回答的是界面上最常问的那句「这部番为什么没匹配到」：
// 候选有没有、命中哪条规则、按什么顺序排。
func opProbeRun(c *Call) (any, error) {
	var payload probeRunPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	site := firstNonEmpty(payload.Site)
	if site == "" {
		site = "mikan"
	}

	instanceName := firstNonEmpty(payload.Instance, cfg.SiteInstance())
	var siteClient *assapi.Client
	if instanceName != "" {
		siteClient, err = app.Client(cfg, instanceName)
		if err != nil {
			// 探针在「还没配实例」时也该能跑 Nyaa 那一家，所以这里不致命。
			siteClient = nil
			if !instanceExists(cfg, instanceName) {
				// 配了实例名但配置里没有它：detail 就是这个实例名（契约 §3）。
				return nil, withResourceDetail(err, instanceName)
			}
		}
	}

	timeout := time.Duration(cfg.Sources.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	httpClient, err := httpx.New(cfg.ResolvedProxy(), timeout)
	if err != nil {
		return nil, Classify(err)
	}

	query := source.Query{
		BgmID:     config.BgmIDFromText(payload.BgmID),
		Title:     firstNonEmpty(payload.Title),
		JPTitle:   firstNonEmpty(payload.Title),
		NyaaQuery: firstNonEmpty(payload.Title),
	}
	if query.BgmID == "" {
		query.BgmID = firstNonEmpty(payload.BgmID)
	}

	ctx := c.Ctx()
	registry := source.NewRegistry(siteClient, httpClient, timeout, []string{site}, nil)
	results := registry.Collect(ctx, query, false)
	candidates := source.AllCandidates(results)

	errors := []string{}
	for _, result := range results {
		if result.Err != nil {
			errors = append(errors, result.Site+"："+result.Err.Error())
		}
	}

	// 排一遍用来回答「命中哪条规则」。方案取指定实例实际生效的那套，
	// 与真正打点时用的是同一套规则 —— 不然探针会给出一个不存在的答案。
	profile := ""
	if instanceName != "" {
		profile = cfg.SchemeFor(instanceName).Name
	}
	ranked := candidates
	if engine, engineErr := app.SchemeEngine(cfg, profile); engineErr == nil {
		ranked = engine.Rank(candidates)
	} else {
		errors = append(errors, "规则无法编译："+engineErr.Error())
	}

	items := make([]probeCandidate, 0, len(ranked))
	for i, candidate := range ranked {
		title := ""
		if len(candidate.Titles) > 0 {
			title = candidate.Titles[0]
		}
		items = append(items, probeCandidate{
			Group:   candidate.Label,
			Title:   title,
			RSS:     candidate.URL,
			Weight:  candidate.Weight,
			Aliases: nonNilStrings(candidate.GroupAliases),
			Matched: candidate.RuleName != "",
			Rule:    candidate.RuleName,
			Rank:    i + 1,
		})
	}
	return map[string]any{
		"site":       site,
		"candidates": items,
		"errors":     errors,
	}, nil
}

// bgmSearchPayload 是 bgm.search 的 payload。
type bgmSearchPayload struct {
	Q     string `json:"q"`
	Limit int    `json:"limit"`
}

// bgmItem 是一条 Bangumi 搜索结果。
type bgmItem struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	NameCN  string  `json:"nameCN"`
	Image   string  `json:"image"`
	Summary string  `json:"summary"`
	Date    string  `json:"date"`
	Score   float64 `json:"score"`
}

func opBgmSearch(c *Call) (any, error) {
	var payload bgmSearchPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	keyword := firstNonEmpty(payload.Q)
	if keyword == "" {
		return nil, BadRequest("请输入搜索关键字")
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	client, err := httpx.New(cfg.ResolvedProxy(), 20*time.Second)
	if err != nil {
		return nil, Classify(err)
	}
	items, err := bgm.Search(c.Ctx(), client, keyword, bgm.ClampLimit(payload.Limit))
	if err != nil {
		return nil, Classify(err)
	}
	out := make([]bgmItem, 0, len(items))
	for _, item := range items {
		out = append(out, bgmItem{
			ID:      item.ID,
			Name:    item.Name,
			NameCN:  item.NameCN,
			Image:   item.Cover,
			Summary: item.Summary,
			Date:    item.AirDate,
			Score:   item.Score,
		})
	}
	return map[string]any{"items": out}, nil
}
