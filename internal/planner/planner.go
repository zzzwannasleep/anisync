// Package planner 把「站点候选 + 规则」变成「每个实例每个订阅应该长什么样」，并与现状对比出动作。
//
// 关键设计：ANI-RSS 的 setAni 是「整对象覆盖」语义（BeanUtil.copyProperties 连 null 一起拷），
// 所以这里从不构造局部对象，而是把 listAni 拿到的原始 map 改完再整份回传，避免把别的字段洗掉。
package planner

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/rules"
	"github.com/zzzwannasleep/anisync/internal/source"
)

// Options 控制计划生成。
type Options struct {
	OnlyInstance string // 只处理某个实例
	AddMissing   bool   // 实例里没有的 target 是否自动添加
	MaxStandby   int
	Concurrency  int
	Limit        int    // 每个实例最多处理多少条订阅（调试用，0 表示不限）
	CachePath    string // 站点数据缓存文件；留空则不缓存
}

// Change 是一条字段级差异。
type Change struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// Action 是对一个订阅要做的事。
type Action struct {
	Instance string   `json:"instance"`
	AniID    string   `json:"aniId"`
	Title    string   `json:"title"`
	Kind     string   `json:"kind"` // update / add / noop / error
	Reason   string   `json:"reason,omitempty"`
	Changes  []Change `json:"changes,omitempty"`

	body map[string]any // 完整 Ani 对象（update/add 用）
}

// Body 暴露要提交的对象（供 Web 设置页预览）。
func (a Action) Body() map[string]any { return a.body }

// Plan 是一次完整的打点计划。
type Plan struct {
	Actions []Action        `json:"actions"`
	Sites   []SiteSummary   `json:"sites"`
	Skipped []SkippedReason `json:"skipped"`
	// SiteInstance 是本次借用站点数据的实例名。
	SiteInstance string `json:"siteInstance,omitempty"`
}

// SiteSummary 记录每个站点在本次计划里的产出，便于排查「为什么没有候选」。
type SiteSummary struct {
	Site       string `json:"site"`
	Candidates int    `json:"candidates"`
	Requests   int    `json:"requests"`
	Failures   int    `json:"failures"`
	Error      string `json:"error,omitempty"`
}

// SkippedReason 记录跳过的订阅及原因。
type SkippedReason struct {
	Instance string `json:"instance"`
	Title    string `json:"title"`
	Reason   string `json:"reason"`
}

// Build 生成计划。所有网络访问都是真实的。
func Build(ctx context.Context, cfg *config.Config, opts Options) (*Plan, error) {
	if opts.MaxStandby <= 0 {
		opts.MaxStandby = cfg.Rules.MaxStandby
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}

	engine, err := buildEngine(cfg, opts.MaxStandby)
	if err != nil {
		return nil, err
	}

	// 站点数据统一由某一个实例提供：它怎么认这些站点，我们就怎么认
	siteClient, siteInstance, err := siteDataClient(cfg)
	if err != nil {
		return nil, err
	}
	httpClient, err := newClient(cfg, "")
	if err != nil {
		return nil, err
	}

	store := cache.Open(opts.CachePath, time.Duration(cfg.Sources.CacheMinutes)*time.Minute)
	defer func() { _ = store.Flush() }()

	registry := source.NewRegistry(siteClient, httpClient.HTTP, timeoutOf(cfg), cfg.Sources.Order, store)

	plan := &Plan{SiteInstance: siteInstance}
	// 同一番剧在多个实例里重复出现时只爬一次
	candidateCache := map[string][]source.Candidate{}
	var cacheMu sync.Mutex
	siteStats := map[string]*SiteSummary{}
	var statsMu sync.Mutex

	// 收集所有需要打点的订阅
	type job struct {
		instance config.Instance
		ani      assapi.Ani
	}
	var jobs []job
	for _, instance := range cfg.Instances {
		if !instance.IsEnabled() {
			continue
		}
		if opts.OnlyInstance != "" && instance.Name != opts.OnlyInstance {
			continue
		}
		apiClient, err := newClient(cfg, instance.Name)
		if err != nil {
			return nil, err
		}
		items, _, err := apiClient.ListAni(ctx)
		if err != nil {
			return nil, fmt.Errorf("[%s] 拉取订阅失败: %w", instance.Name, err)
		}
		for _, ani := range items {
			if opts.Limit > 0 && len(jobs) >= opts.Limit {
				break
			}
			jobs = append(jobs, job{instance: instance, ani: ani})
		}
	}

	// 并发爬取
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]Action, 0, len(jobs))
	skipped := make([]SkippedReason, 0)

	for _, item := range jobs {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			action := planOne(ctx, cfg, engine, registry, item.instance, item.ani, opts,
				candidateCache, &cacheMu, siteStats, &statsMu)
			mu.Lock()
			defer mu.Unlock()
			if action.Kind == "error" {
				skipped = append(skipped, SkippedReason{Instance: item.instance.Name, Title: titleOf(item.ani), Reason: action.Reason})
				return
			}
			results = append(results, action)
		}()
	}
	wg.Wait()

	// targets：实例里没有的就添加
	if opts.AddMissing {
		for _, instance := range cfg.Instances {
			if !instance.IsEnabled() {
				continue
			}
			if opts.OnlyInstance != "" && instance.Name != opts.OnlyInstance {
				continue
			}
			for _, target := range cfg.Targets {
				if !targetAppliesTo(target, instance.Name) {
					continue
				}
				if hasTarget(results, instance.Name, target) {
					continue
				}
				action := planTarget(ctx, cfg, engine, registry, instance, target, candidateCache, &cacheMu)
				if action == nil {
					continue
				}
				if action.Kind == "error" {
					skipped = append(skipped, SkippedReason{Instance: instance.Name, Title: target.Title, Reason: action.Reason})
					continue
				}
				results = append(results, *action)
			}
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Instance != results[j].Instance {
			return results[i].Instance < results[j].Instance
		}
		if results[i].Kind != results[j].Kind {
			return results[i].Kind < results[j].Kind
		}
		return results[i].Title < results[j].Title
	})

	plan.Actions = results
	plan.Skipped = skipped
	for _, name := range registry.Order() {
		summary := siteStats[name]
		if summary == nil {
			summary = &SiteSummary{Site: name}
		}
		plan.Sites = append(plan.Sites, *summary)
	}
	return plan, nil
}

func planOne(ctx context.Context, cfg *config.Config, engine *rules.Engine, registry *source.Registry,
	instance config.Instance, ani assapi.Ani, opts Options, cache map[string][]source.Candidate,
	cacheMu *sync.Mutex, siteStats map[string]*SiteSummary, statsMu *sync.Mutex) Action {

	action := Action{Instance: instance.Name, AniID: stringField(ani, "id"), Title: titleOf(ani), Kind: "noop"}
	if action.AniID == "" {
		action.Kind = "error"
		action.Reason = "订阅缺少 id"
		return action
	}

	query := queryOf(ani)
	if query.BgmID == "" && query.MikanID == "" && query.Title == "" {
		action.Kind = "error"
		action.Reason = "无法确定番剧标识（bgmUrl / RSS URL / 标题都取不到）"
		return action
	}

	candidates := collect(ctx, registry, query, cfg.Sources.StopOnFirstMatch, cache, cacheMu, siteStats, statsMu)
	ranked := engine.Rank(candidates)
	if len(ranked) == 0 {
		action.Kind = "error"
		action.Reason = fmt.Sprintf("按规则没有可用候选（爬取到 %d 个）", len(candidates))
		return action
	}

	primary := ranked[0]
	standby := engine.Standby(ranked[1:])

	changes := diff(ani, primary, standby)
	if len(changes) == 0 {
		action.Kind = "noop"
		action.Reason = "主副 RSS 已符合规则"
		return action
	}

	body := applyDesired(ani, primary, standby)
	action.Kind = "update"
	action.Changes = changes
	action.body = body
	return action
}

func planTarget(ctx context.Context, cfg *config.Config, engine *rules.Engine, registry *source.Registry,
	instance config.Instance, target config.Target, cache map[string][]source.Candidate, cacheMu *sync.Mutex) *Action {

	query := source.Query{BgmID: target.BgmID, MikanID: target.MikanID, Title: target.Title, NyaaQuery: target.Title}
	results := registry.Collect(ctx, query, cfg.Sources.StopOnFirstMatch)
	ranked := engine.Rank(source.AllCandidates(results))
	if len(ranked) == 0 {
		return nil
	}
	primary := ranked[0]
	standby := engine.Standby(ranked[1:])

	enable := false
	if target.Enable != nil {
		enable = *target.Enable
	}

	// 让实例自己解析这个 RSS：标题、bgmUrl、季集、字幕组都由 ANI-RSS 决定，
	// 我们只提供 RSS 地址、字幕组、副 RSS 与启用开关，省得在本地猜它的字段规则。
	client, err := newClient(cfg, instance.Name)
	if err != nil {
		return &Action{Instance: instance.Name, Title: target.Title, Kind: "error", Reason: err.Error()}
	}
	draft, err := client.RssToAni(ctx, map[string]any{
		"url":            primary.URL,
		"type":           primary.Type,
		"subgroup":       primary.Subgroup,
		"bgmUrl":         firstNonEmpty(primary.BgmURL, bgmURLOfID(target.BgmID)),
		"enable":         enable,
		"standbyRssList": standbyList(standby),
	})
	if err != nil {
		return &Action{Instance: instance.Name, Title: target.Title, Kind: "error", Reason: "rssToAni 解析失败: " + err.Error()}
	}
	if draft == nil {
		return &Action{Instance: instance.Name, Title: target.Title, Kind: "error", Reason: "rssToAni 没有返回订阅草稿"}
	}
	// 草稿里的 enable 由我们兜底，避免实例默认值把下载打开
	draft["enable"] = enable
	draft["standbyRssList"] = standbyList(standby)

	title := firstNonEmpty(stringField(draft, "title"), target.Title, primary.Label)
	return &Action{
		Instance: instance.Name,
		Title:    title,
		Kind:     "add",
		Reason:   "实例中没有该订阅，按规则新建（默认不启用，避免触发下载）",
		Changes:  []Change{{Field: "url", To: primary.URL}, {Field: "standbyRssList", To: standbyText(standby)}},
		body:     draft,
	}
}

func collect(ctx context.Context, registry *source.Registry, query source.Query, stopOnFirst bool,
	cache map[string][]source.Candidate, cacheMu *sync.Mutex, siteStats map[string]*SiteSummary, statsMu *sync.Mutex) []source.Candidate {

	key := query.BgmID + "|" + query.MikanID + "|" + query.Title
	cacheMu.Lock()
	if cached, ok := cache[key]; ok {
		cacheMu.Unlock()
		return cached
	}
	cacheMu.Unlock()

	results := registry.Collect(ctx, query, stopOnFirst)
	candidates := source.AllCandidates(results)

	cacheMu.Lock()
	cache[key] = candidates
	cacheMu.Unlock()

	statsMu.Lock()
	for _, result := range results {
		summary := siteStats[result.Site]
		if summary == nil {
			summary = &SiteSummary{Site: result.Site}
			siteStats[result.Site] = summary
		}
		summary.Requests++
		summary.Candidates += len(result.Candidates)
		if result.Err != nil {
			summary.Failures++
			if summary.Error == "" {
				summary.Error = result.Err.Error()
			}
		}
	}
	statsMu.Unlock()
	return candidates
}

// Apply 执行计划。只处理 update / add。
func Apply(ctx context.Context, cfg *config.Config, plan *Plan) (updated, added int, errs []string) {
	clients := map[string]*assapi.Client{}
	for _, action := range plan.Actions {
		if action.Kind != "update" && action.Kind != "add" {
			continue
		}
		client, ok := clients[action.Instance]
		if !ok {
			var err error
			client, err = newClient(cfg, action.Instance)
			if err != nil {
				errs = append(errs, fmt.Sprintf("[%s] %v", action.Instance, err))
				continue
			}
			clients[action.Instance] = client
		}
		switch action.Kind {
		case "update":
			if err := client.SetAni(ctx, action.body); err != nil {
				errs = append(errs, fmt.Sprintf("[%s] %s 更新失败: %v", action.Instance, action.Title, err))
				continue
			}
			updated++
		case "add":
			if err := client.AddAni(ctx, action.body, false); err != nil {
				errs = append(errs, fmt.Sprintf("[%s] %s 添加失败: %v", action.Instance, action.Title, err))
				continue
			}
			added++
		}
	}
	return updated, added, errs
}

// ---------- 内部工具 ----------

func buildEngine(cfg *config.Config, maxStandby int) (*rules.Engine, error) {
	primary := make([]rules.Rule, 0, len(cfg.Rules.Primary))
	for _, rule := range cfg.Rules.Primary {
		primary = append(primary, rules.Rule{
			Name:     rule.Name,
			Match:    rule.Match,
			Priority: rule.Priority,
			Site:     rule.Site,
			Scope:    rule.ScopeOr(),
		})
	}
	deny := make([]rules.DenyRule, 0, len(cfg.Rules.Deny))
	for _, rule := range cfg.Rules.Deny {
		deny = append(deny, rules.DenyRule{Match: rule.Match, Why: rule.Why})
	}
	return rules.New(primary, deny, maxStandby)
}

// siteDataClient 返回提供站点数据的实例客户端（mikanGroup / aniBTGroup / animeGardenGroup 走它）。
func siteDataClient(cfg *config.Config) (*assapi.Client, string, error) {
	name := cfg.SiteInstance()
	if name == "" {
		return nil, "", fmt.Errorf("没有启用的实例，无法获取站点数据")
	}
	client, err := newClient(cfg, name)
	if err != nil {
		return nil, "", err
	}
	return client, name, nil
}

func newClient(cfg *config.Config, instanceName string) (*assapi.Client, error) {
	var instance config.Instance
	if instanceName == "" {
		if len(cfg.Instances) == 0 {
			return nil, fmt.Errorf("没有配置实例")
		}
		instance = cfg.Instances[0]
	} else {
		found, ok := cfg.InstanceByName(instanceName)
		if !ok {
			return nil, fmt.Errorf("找不到实例 %s", instanceName)
		}
		instance = found
	}
	return assapi.New(instance.BaseURL, instance.APIKey, cfg.Proxy, 0)
}

func timeoutOf(cfg *config.Config) time.Duration {
	return time.Duration(cfg.Sources.TimeoutSeconds) * time.Second
}

func queryOf(ani assapi.Ani) source.Query {
	rawURL := stringField(ani, "url")
	query := source.Query{
		Title:      firstNonEmpty(stringField(ani, "title"), stringField(ani, "mikanTitle")),
		JPTitle:    stringField(ani, "jpTitle"),
		MikanTitle: stringField(ani, "mikanTitle"),
		NyaaQuery:  firstNonEmpty(stringField(ani, "jpTitle"), stringField(ani, "title")),
		RSSURL:     rawURL,
	}
	query.BgmID = source.BgmIDFromURL(stringField(ani, "bgmUrl"))
	query.MikanID = source.MikanIDFromURL(rawURL)
	if parsed, err := url.Parse(rawURL); err == nil {
		params := parsed.Query()
		if query.BgmID == "" {
			query.BgmID = firstNonEmpty(params.Get("bgmId"), params.Get("subject"))
		}
		if id := params.Get("bangumiId"); id != "" {
			query.MikanID = id
		}
	}
	return query
}

// diff 比较现状与目标，产出字段级差异。
func diff(ani assapi.Ani, primary source.Candidate, standby []source.Candidate) []Change {
	var changes []Change
	if current := stringField(ani, "url"); current != primary.URL {
		changes = append(changes, Change{Field: "url", From: current, To: primary.URL})
	}
	if current := stringField(ani, "subgroup"); primary.Subgroup != "" && current != primary.Subgroup {
		changes = append(changes, Change{Field: "subgroup", From: current, To: primary.Subgroup})
	}
	current := standbyURLs(ani)
	desired := candidateURLs(standby)
	if strings.Join(current, "\n") != strings.Join(desired, "\n") {
		changes = append(changes, Change{Field: "standbyRssList", From: strings.Join(current, " , "), To: strings.Join(desired, " , ")})
	}
	return changes
}

func applyDesired(ani assapi.Ani, primary source.Candidate, standby []source.Candidate) map[string]any {
	body := make(map[string]any, len(ani)+3)
	for key, value := range ani {
		body[key] = value
	}
	body["url"] = primary.URL
	if primary.Subgroup != "" {
		body["subgroup"] = primary.Subgroup
	}
	if primary.BgmURL != "" && stringField(ani, "bgmUrl") == "" {
		body["bgmUrl"] = primary.BgmURL
	}
	body["standbyRssList"] = standbyList(standby)
	return body
}

func standbyList(standby []source.Candidate) []any {
	list := make([]any, 0, len(standby))
	for _, item := range standby {
		list = append(list, map[string]any{
			"label":  item.Label,
			"url":    item.URL,
			"offset": 0,
		})
	}
	return list
}

func standbyText(standby []source.Candidate) string {
	parts := make([]string, 0, len(standby))
	for _, item := range standby {
		parts = append(parts, item.Label)
	}
	return strings.Join(parts, " , ")
}

func standbyURLs(ani assapi.Ani) []string {
	raw, ok := ani["standbyRssList"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, stringField(m, "url"))
		}
	}
	return out
}

func candidateURLs(candidates []source.Candidate) []string {
	out := make([]string, 0, len(candidates))
	for _, item := range candidates {
		out = append(out, item.URL)
	}
	return out
}

func titleOf(ani assapi.Ani) string {
	return firstNonEmpty(stringField(ani, "title"), stringField(ani, "mikanTitle"), stringField(ani, "id"))
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func bgmURLOfID(id string) string {
	if id == "" {
		return ""
	}
	return "https://bgm.tv/subject/" + id
}

func targetAppliesTo(target config.Target, instance string) bool {
	if len(target.Instances) == 0 {
		return true
	}
	for _, name := range target.Instances {
		if name == instance {
			return true
		}
	}
	return false
}

func hasTarget(actions []Action, instance string, target config.Target) bool {
	for _, action := range actions {
		if action.Instance != instance {
			continue
		}
		if target.Title != "" && strings.Contains(action.Title, target.Title) {
			return true
		}
		if target.BgmID != "" && action.body != nil && strings.Contains(stringField(action.body, "bgmUrl"), target.BgmID) {
			return true
		}
	}
	return false
}
