// Package planner 把「站点候选 + 规则」变成「每个实例每个订阅应该长什么样」，并与现状对比出动作。
//
// 关键设计：ANI-RSS 的 setAni 是「整对象覆盖」语义（BeanUtil.copyProperties 连 null 一起拷），
// 所以这里从不构造局部对象，而是把 listAni 拿到的原始 map 改完再整份回传，避免把别的字段洗掉。
package planner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
	"github.com/zzzwannasleep/anisync/internal/rules"
	"github.com/zzzwannasleep/anisync/internal/source"
)

// Options 控制计划生成。
type Options struct {
	OnlyInstance string // 只处理某个实例
	AddMissing   bool   // 实例里没有的 target 是否自动添加
	// Discover 打开自动发现：先扫一遍现在有什么番剧在放，把实例里还没有的补进来。
	// 打开它就隐含了 AddMissing 的语义——发现出来不新增就没有意义。
	Discover bool
	// MaxNew 是每个实例单次最多新增多少条，0 表示不限。
	// 第一次跑自动发现时防止一口气灌进几百部番剧。
	MaxNew      int
	MaxStandby  int
	Concurrency int
	Limit       int    // 每个实例最多处理多少条订阅（调试用，0 表示不限）
	CachePath   string // 站点数据缓存文件；留空则不缓存
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
	// Target 是命中的番剧级配置名（targets 里的 title / bgmId），
	// 用来在计划里说明「这条订阅的季度与偏移是按哪个番剧规定的」。
	Target string `json:"target,omitempty"`
	// Profile 是这条订阅实际生效的方案名；空表示用顶层 rules。
	//
	// 单独列出来是因为 targets 可以按番剧覆盖方案：只看实例的话，
	// 「这条为什么全收了」在计划里查不出来。
	Profile string `json:"profile,omitempty"`

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
	// Discovered 是本次自动发现扫到的番剧，用来交代「新增是从哪来的」。
	Discovered []discover.Item `json:"discovered,omitempty"`
	// DiscoverNotes 是发现过程的降级信息，例如某个来源挂了但其余来源仍然可用。
	DiscoverNotes []string `json:"discoverNotes,omitempty"`
	// Schemes 说明每个实例这次用的是哪套订阅方案。
	Schemes []SchemeSummary `json:"schemes,omitempty"`
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

// SchemeSummary 说明这次计划里每个实例实际用了哪套方案。
//
// 无人值守时这是唯一能回答「这个实例到底按哪套口味在跑」的地方：
// 计划里只有一条条变更，看不出规则来自哪个方案。
type SchemeSummary struct {
	Instance   string                `json:"instance"`
	Profile    string                `json:"profile,omitempty"`
	MaxStandby int                   `json:"maxStandby"`
	SiteOrder  []string              `json:"siteOrder"`
	RuleCount  int                   `json:"ruleCount"`
	DenyCount  int                   `json:"denyCount"`
	RuleFiles  []config.RuleFileInfo `json:"ruleFiles,omitempty"`
}

// Build 生成计划。所有网络访问都是真实的。
func Build(ctx context.Context, cfg *config.Config, opts Options) (*Plan, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
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

	// 收集所有需要打点的订阅，同时建一份「每个实例里已经有什么」的索引：
	// 自动发现要靠它判断哪些番剧是真的还没有，不能只看标题（中/日文名对不上会误判）。
	type job struct {
		instance config.Instance
		ani      assapi.Ani
	}
	var jobs []job
	existing := newExistingIndex()
	// 每个启用实例生效的方案。先收集再建 registry / engine：
	// 这两样都只跟方案有关，跑之前就能固定下来，不必在并发里加锁懒建。
	schemeByInstance := map[string]config.Scheme{}
	for _, instance := range cfg.Instances {
		if !instance.IsEnabled() {
			continue
		}
		schemeByInstance[instance.Name] = cfg.SchemeFor(instance.Name)
	}
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
			// 索引要建全：Limit 只影响打点范围，不影响「实例里有什么」的判断
			existing.add(instance.Name, ani)
			if opts.Limit > 0 && len(jobs) >= opts.Limit {
				break
			}
			jobs = append(jobs, job{instance: instance, ani: ani})
		}
	}

	b, err := newBuilder(cfg, opts, store, siteClient, httpClient, schemeByInstance)
	if err != nil {
		return nil, err
	}

	plan := &Plan{SiteInstance: siteInstance}
	for _, instance := range cfg.Instances {
		if !instance.IsEnabled() {
			continue
		}
		scheme := schemeByInstance[instance.Name]
		plan.Schemes = append(plan.Schemes, SchemeSummary{
			Instance:   instance.Name,
			Profile:    scheme.Name,
			MaxStandby: b.maxStandbyOf(scheme),
			SiteOrder:  scheme.SourceOrder,
			// 报实际会参与匹配的条数：空的 match 会被 rules.New 跳过，
			// 报总条数会让人以为配了 3 条、其实只有 1 条生效。
			RuleCount: scheme.EffectivePrimary(),
			DenyCount: scheme.EffectiveDeny(),
			RuleFiles: scheme.RuleFiles,
		})
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

			action := b.planOneGuarded(ctx, item.instance, item.ani)
			mu.Lock()
			defer mu.Unlock()
			if action.Kind == "error" {
				skipped = append(skipped, SkippedReason{Instance: item.instance.Name, Title: action.Title, Reason: action.Reason})
				return
			}
			results = append(results, action)
		}()
	}
	wg.Wait()

	// 自动发现：扫一遍现在有什么番剧在放，把实例里还没有的补进来。
	// 发现结果被当成自动生成的 targets，走的是下面同一条新增链路。
	targets := cfg.Targets
	if opts.Discover {
		found, err := discover.Discover(ctx, discover.Options{
			Sources:   cfg.Discover.SourceList(),
			BgmUser:   cfg.Discover.BgmUser,
			Days:      cfg.Discover.WindowDays(),
			JapanOnly: cfg.Discover.JapanOnlyEnabled(),
			Proxy:     cfg.ResolvedProxy(),
			Timeout:   timeoutOf(cfg),
			Store:     store,
			Exclude:   cfg.Discover.Exclude,
		})
		if err != nil {
			return nil, fmt.Errorf("自动发现失败: %w", err)
		}
		plan.Discovered = found.Items
		plan.DiscoverNotes = found.Warnings

		// 手写的 targets 说了算：同一个番剧两边都有时，只保留手写的那条，
		// 否则用户为它规定的季度与偏移会被自动发现生成的空值顶掉。
		declared := map[string]bool{}
		for _, target := range cfg.Targets {
			if target.BgmID != "" {
				declared[target.BgmID] = true
			}
		}
		enabled := cfg.Discover.DiscoverEnabled()
		for _, item := range found.Items {
			if declared[item.BgmID] {
				continue
			}
			declared[item.BgmID] = true
			targets = append(targets, item.Target(enabled))
		}
	}

	// targets：实例里没有的就添加
	if opts.AddMissing || opts.Discover {
		type addJob struct {
			instance config.Instance
			target   config.Target
		}

		// 先做不需要网络的筛除（已有订阅、已有动作），再并发去爬站点。
		// 自动发现一次会带出上百部番剧，逐个串行爬四个站点要十几分钟，那样没法用。
		// MaxNew 限制的是「尝试条数」而不是「成功条数」：发现结果按首播时间倒序，
		// 所以截断等于「先处理最新的那批」，同时把网络开销钉死在上限内。
		var addJobs []addJob
		for _, instance := range cfg.Instances {
			if !instance.IsEnabled() {
				continue
			}
			if opts.OnlyInstance != "" && instance.Name != opts.OnlyInstance {
				continue
			}
			picked := 0
			for _, target := range targets {
				if opts.MaxNew > 0 && picked >= opts.MaxNew {
					break
				}
				if !targetAppliesTo(target, instance.Name) {
					continue
				}
				// 已经有这个番剧就跳过：先按 bgmId 精确比对，再退回标题包含
				if existing.has(instance.Name, target) || hasTarget(results, instance.Name, target) {
					continue
				}
				addJobs = append(addJobs, addJob{instance: instance, target: target})
				picked++
			}
		}

		addSem := make(chan struct{}, opts.Concurrency)
		var addWg sync.WaitGroup
		for _, item := range addJobs {
			item := item
			addWg.Add(1)
			go func() {
				defer addWg.Done()
				addSem <- struct{}{}
				defer func() { <-addSem }()

				action := b.planTargetGuarded(ctx, item.instance, item.target)
				if action == nil {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if action.Kind == "error" {
					skipped = append(skipped, SkippedReason{
						Instance: item.instance.Name,
						Title:    firstNonEmpty(item.target.Title, item.target.JPTitle, item.target.BgmID),
						Reason:   action.Reason,
					})
					return
				}
				results = append(results, *action)
			}()
		}
		addWg.Wait()
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
	for _, name := range b.siteOrder() {
		b.statsMu.Lock()
		summary := b.stats[name]
		b.statsMu.Unlock()
		if summary == nil {
			summary = &SiteSummary{Site: name}
		}
		plan.Sites = append(plan.Sites, *summary)
	}
	return plan, nil
}

// builder 把一次 Build 里的共享物收在一处：每个方案的规则引擎、站点注册表、
// 候选缓存与统计。放进结构体是因为这些参数已经在 planOne 上堆到八个了。
type builder struct {
	cfg  *config.Config
	opts Options

	siteClient *assapi.Client
	httpClient *assapi.Client

	engines map[string]*rules.Engine    // 方案名 -> 引擎（"" 是默认方案）
	regists map[string]*source.Registry // 站点顺序签名 -> 注册表

	memoMu sync.Mutex
	memo   map[string][]source.Candidate // 签名|查询 -> 候选
	// inflight 记录正在爬的键，避免同一番剧被并发打多份。
	//
	// 没有它的时候，两个实例同时处理同一个番剧会各自判定「缓存里没有」、
	// 各自去爬一遍：多实例场景下每个番剧的站点请求数等于实例数。
	// 这既是浪费，也更容易撞上游限流。
	inflight map[string]*inflightCall

	statsMu sync.Mutex
	stats   map[string]*SiteSummary

	orderMu sync.Mutex
	orders  []string
}

func newBuilder(cfg *config.Config, opts Options, store *cache.Store, siteClient, httpClient *assapi.Client, schemes map[string]config.Scheme) (*builder, error) {
	b := &builder{
		cfg:        cfg,
		opts:       opts,
		siteClient: siteClient,
		httpClient: httpClient,
		engines:    map[string]*rules.Engine{},
		regists:    map[string]*source.Registry{},
		memo:       map[string][]source.Candidate{},
		inflight:   map[string]*inflightCall{},
		stats:      map[string]*SiteSummary{},
	}
	// 默认方案也要建：没挂方案的实例走它。
	// 另外把**所有**方案都建一遍，而不只是当前挂到实例上的那些：
	// targets 可以给单个番剧覆盖方案，那套方案未必挂在任何实例上。
	all := []config.Scheme{cfg.SchemeForProfile("")}
	for i := range cfg.Profiles {
		all = append(all, cfg.SchemeForProfile(cfg.Profiles[i].Name))
	}
	for _, name := range sortedSchemeNames(schemes) {
		all = append(all, schemes[name])
	}
	built := map[string]bool{}
	for _, scheme := range all {
		if built[scheme.Name] {
			continue
		}
		built[scheme.Name] = true
		if _, ok := b.engines[scheme.Name]; !ok {
			engine, err := buildEngine(scheme, b.maxStandbyOf(scheme))
			if err != nil {
				// 这里**不能**兜成空引擎。空引擎的后果是：黑名单全部失效、
				// 所有候选同档按热度排、副 RSS 因为没人被点名而被清空 ——
				// 也就是说一次静默的错误会把主副 RSS 一起换成错的，而且照样写回实例。
				// 校验阶段本应拦下非法正则；走到这里说明有调用点绕过了 Validate，
				// 那就当场失败，让调用方看见。
				label := scheme.Name
				if label == "" {
					label = "默认方案（顶层 rules）"
				}
				return nil, fmt.Errorf("%s的规则无法编译（配置没过校验？）: %w", label, err)
			}
			b.engines[scheme.Name] = engine
		}
		key := orderKey(scheme.SourceOrder)
		if _, ok := b.regists[key]; !ok {
			registry := source.NewRegistry(siteClient, httpClient.HTTP, timeoutOf(cfg), scheme.SourceOrder, store)
			b.regists[key] = registry
			b.orderMu.Lock()
			b.orders = append(b.orders, key)
			b.orderMu.Unlock()
		}
	}
	// 兜底：一个实例都没有时也要有一张注册表，否则 plan 直接空转。
	if len(b.regists) == 0 {
		registry := source.NewRegistry(siteClient, httpClient.HTTP, timeoutOf(cfg), cfg.Sources.Order, store)
		b.regists[orderKey(cfg.Sources.Order)] = registry
	}
	return b, nil
}

// maxStandbyOf 取生效的副 RSS 上限：命令行覆盖优先，其次方案自己的值。
func (b *builder) maxStandbyOf(scheme config.Scheme) int {
	if b.opts.MaxStandby > 0 {
		return b.opts.MaxStandby
	}
	if scheme.MaxStandby > 0 {
		return scheme.MaxStandby
	}
	return 1
}

// forScheme 取某个番剧要用的引擎、注册表与副 RSS 上限。
//
// profile 非空表示「这条订阅按 targets 覆盖了方案」，此时以覆盖为准：
// 番剧级覆盖的意义就是「这一次别按实例那套来」。
func (b *builder) forScheme(profile, instanceName string) (*rules.Engine, *source.Registry, string, int) {
	var scheme config.Scheme
	if profile != "" {
		scheme = b.cfg.SchemeForProfile(profile)
	} else {
		scheme = b.cfg.SchemeFor(instanceName)
	}
	engine := b.engines[scheme.Name]
	if engine == nil {
		engine = b.engines[""]
	}
	key := orderKey(scheme.SourceOrder)
	registry := b.regists[key]
	if registry == nil {
		for _, candidate := range b.regists {
			registry = candidate
			break
		}
	}
	return engine, registry, registryKey(registry), b.maxStandbyOf(scheme)
}

// siteOrder 返回要写进计划里的站点清单（按注册表的实际生效顺序，去重）。
func (b *builder) siteOrder() []string {
	b.orderMu.Lock()
	defer b.orderMu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, key := range b.orders {
		registry := b.regists[key]
		if registry == nil {
			continue
		}
		for _, name := range registry.Order() {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		for _, name := range config.DefaultSourceOrder {
			out = append(out, name)
		}
	}
	return out
}

// planOneGuarded 兜住 planOne 的 panic。
//
// 常驻进程里一个订阅把某条规则喂炸（比如正则回溯炸栈），
// 未捕获的 panic 会带走整个进程 —— 无人值守时这意味着「再也不会自己跑」，
// 而且没有任何提示。所以这里必须变成一个「这条跳过」。
func (b *builder) planOneGuarded(ctx context.Context, instance config.Instance, ani assapi.Ani) (action Action) {
	defer func() {
		if r := recover(); r != nil {
			action = Action{
				Instance: instance.Name,
				AniID:    stringField(ani, "id"),
				Title:    titleOf(ani),
				Kind:     "error",
				Reason:   fmt.Sprintf("内部错误（已跳过这一条）: %v", r),
			}
		}
	}()
	return b.planOne(ctx, instance, ani)
}

func (b *builder) planTargetGuarded(ctx context.Context, instance config.Instance, target config.Target) (action *Action) {
	defer func() {
		if r := recover(); r != nil {
			action = &Action{
				Instance: instance.Name,
				Title:    firstNonEmpty(target.Title, target.JPTitle, target.BgmID),
				Kind:     "error",
				Reason:   fmt.Sprintf("内部错误（已跳过这一条）: %v", r),
			}
		}
	}()
	return b.planTarget(ctx, instance, target)
}

func (b *builder) planOne(ctx context.Context, instance config.Instance, ani assapi.Ani) Action {
	cfg := b.cfg

	action := Action{Instance: instance.Name, AniID: stringField(ani, "id"), Title: titleOf(ani), Kind: "noop"}
	if action.AniID == "" {
		action.Kind = "error"
		action.Reason = "订阅缺少 id"
		return action
	}

	// 番剧级配置（targets）对已存在的订阅同样生效：ASS 里季度是按番剧给的，
	// 集偏移按 RSS 分开给，所以这条订阅命中哪个番剧，就用那个番剧的季度与偏移。
	target := matchTarget(cfg, instance.Name, ani)
	if target != nil {
		action.Target = targetLabel(*target)
	}
	// 番剧级覆盖方案：这一条按 target 上写的方案挑，不按实例那套。
	override := ""
	if target != nil {
		override = target.Profile
	}
	engine, registry, regKey, maxStandby := b.forScheme(override, instance.Name)
	action.Profile = schemeLabel(b.cfg, override, instance.Name)

	query := queryOf(ani)
	if query.BgmID == "" && query.MikanID == "" && query.Title == "" {
		action.Kind = "error"
		action.Reason = "无法确定番剧标识（bgmUrl / RSS URL / 标题都取不到）"
		return action
	}

	candidates := b.collect(ctx, regKey, registry, query, cfg.Sources.StopOnFirstMatch)
	ranked := engine.Rank(candidates)
	if len(ranked) == 0 {
		action.Kind = "error"
		action.Reason = fmt.Sprintf("按规则没有可用候选（爬取到 %d 个）", len(candidates))
		return action
	}

	primary := ranked[0]
	standby := engine.Standby(ranked[1:])
	if maxStandby < len(standby) {
		standby = standby[:maxStandby]
	}

	off := offsetsOf(cfg, target)
	// 备用 RSS 列表每次都要整体重建，但 offset 不能跟着一起重置：
	// ANI-RSS 会按每个 RSS 的实际集数自动推断它，先按 URL 继承订阅里已有的值。
	desiredStandby := standbyList(standby, ani, off.standby)

	changes := diff(ani, primary, desiredStandby, off)
	if len(changes) == 0 {
		action.Kind = "noop"
		action.Reason = "主副 RSS 已符合规则"
		return action
	}

	body := applyDesired(ani, primary, desiredStandby, off)
	action.Kind = "update"
	action.Changes = changes
	action.body = body
	return action
}

func (b *builder) planTarget(ctx context.Context, instance config.Instance, target config.Target) *Action {
	cfg := b.cfg
	engine, registry, _, maxStandby := b.forScheme(target.Profile, instance.Name)

	// 站点查询：bgmId 是最可靠的锚点（AniBT / AnimeGarden 都要它）；
	// Nyaa 只认日文/罗马音，所以搜索词优先用日文原名，中文名在那边搜不到东西。
	query := source.Query{
		BgmID:     target.BgmID,
		MikanID:   target.MikanID,
		Title:     target.Title,
		JPTitle:   target.JPTitle,
		NyaaQuery: firstNonEmpty(target.JPTitle, target.Title),
	}
	results := registry.Collect(ctx, query, cfg.Sources.StopOnFirstMatch)
	b.recordStats(results)
	ranked := engine.Rank(source.AllCandidates(results))
	if len(ranked) == 0 {
		return nil
	}
	primary := ranked[0]
	standby := engine.Standby(ranked[1:])
	if maxStandby < len(standby) {
		standby = standby[:maxStandby]
	}

	enable := false
	if target.Enable != nil {
		enable = *target.Enable
	}
	// 新增订阅没有「原值」可继承：季度与偏移优先取 target 上的，其次取全局配置
	off := offsetsOf(cfg, &target)
	desiredStandby := standbyList(standby, nil, off.standby)

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
		"standbyRssList": desiredStandby,
	})
	if err != nil {
		return &Action{Instance: instance.Name, Title: target.Title, Kind: "error", Reason: "rssToAni 解析失败: " + err.Error()}
	}
	if draft == nil {
		return &Action{Instance: instance.Name, Title: target.Title, Kind: "error", Reason: "rssToAni 没有返回订阅草稿"}
	}
	// 草稿里的 enable 由我们兜底，避免实例默认值把下载打开；
	// 季度与偏移也由配置说了算，不能被实例的默认推断盖掉。
	draft["enable"] = enable
	draft["standbyRssList"] = desiredStandby
	applyOffsets(draft, off)

	changes := []Change{{Field: "url", To: primary.URL}, {Field: "standbyRssList", To: standbyText(desiredStandby)}}
	if off.season != nil {
		changes = append(changes, Change{Field: "season", To: strconv.Itoa(*off.season)})
	}
	if off.main != nil {
		changes = append(changes, Change{Field: "offset", To: strconv.Itoa(*off.main)})
	}
	if off.standby != nil {
		changes = append(changes, Change{Field: "standbyOffset", To: strconv.Itoa(*off.standby)})
	}

	title := firstNonEmpty(stringField(draft, "title"), target.Title, primary.Label)
	return &Action{
		Instance: instance.Name,
		Title:    title,
		Kind:     "add",
		Reason:   "实例中没有该订阅，按规则新建（默认不启用，避免触发下载）",
		Changes:  changes,
		// 新增订阅同样要交代方案：不写的话 plan --json 里这一条没有 profile，
		// 「这条为什么按全收挑的」就查不出来。
		Profile: schemeLabel(cfg, target.Profile, instance.Name),
		body:    draft,
	}
}

// inflightCall 是一次正在进行的爬取，其余同一个键的等待者会在 done 上等它。
type inflightCall struct {
	done chan struct{}
	val  []source.Candidate
	// err 是 leader 崩掉时留下的原因。等待者要拿到它，否则会被记成
	// 「按规则没有可用候选（爬取到 0 个）」—— 那会把人引去查站点，
	// 而真相是同番剧的另一条订阅把爬取搞崩了。
	err any
}

// collect 爬取并缓存候选。
//
// 缓存键必须带上站点顺序签名：不同方案可以用不同的站点集合，
// 只按番剧缓存会让「第二个方案复用第一个方案的候选」—— 规则看着生效了，
// 其实候选根本不是它那几家站点给的。
//
// 同一个键并发到达时只有第一个真的去爬，其余等它的结果（单飞）。
func (b *builder) collect(ctx context.Context, regKey string, registry *source.Registry, query source.Query, stopOnFirst bool) (candidates []source.Candidate) {
	key := regKey + "|" + query.BgmID + "|" + query.MikanID + "|" + query.Title

	b.memoMu.Lock()
	if cached, ok := b.memo[key]; ok {
		b.memoMu.Unlock()
		return cached
	}
	if waiting, ok := b.inflight[key]; ok {
		b.memoMu.Unlock()
		select {
		case <-waiting.done:
			// leader 崩了就把原因原样抛上去：让上层报「内部错误」，
			// 而不是「没有可用候选」。后者会把人引去查站点。
			if waiting.err != nil {
				panic(waiting.err)
			}
			return waiting.val
		case <-ctx.Done():
			// 一轮被取消时不陪 leader 走完：单站点超时 25 秒，
			// 多个站点串起来足够把整轮拖到超时之后。
			return nil
		}
	}
	wait := &inflightCall{done: make(chan struct{})}
	b.inflight[key] = wait
	b.memoMu.Unlock()

	// 不管正常返回还是 panic 都要把等待者放出去：planOne 的 panic 是被上层 recover 的，
	// 如果不在这里兜底收尾，同一番剧的其余订阅就会永远挂在 channel 上，整轮计划卡死。
	defer func() {
		crashed := recover()
		b.memoMu.Lock()
		if candidates != nil {
			b.memo[key] = candidates
		}
		delete(b.inflight, key)
		wait.val = candidates
		wait.err = crashed
		b.memoMu.Unlock()
		close(wait.done)
		// 收尾之后继续把 panic 往上传：leader 这条订阅要被记成「内部错误」，
		// 而不是悄悄退化成「按规则没有可用候选」。
		if crashed != nil {
			panic(crashed)
		}
	}()

	results := registry.Collect(ctx, query, stopOnFirst)
	b.recordStats(results)
	return source.AllCandidates(results)
}

// recordStats 累计每个站点的请求数、候选数与失败数。
func (b *builder) recordStats(results []source.SiteResult) {
	b.statsMu.Lock()
	defer b.statsMu.Unlock()
	for _, result := range results {
		summary := b.stats[result.Site]
		if summary == nil {
			summary = &SiteSummary{Site: result.Site}
			b.stats[result.Site] = summary
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
}

// orderKey 是站点顺序的稳定签名，用作注册表与候选缓存的键。
func orderKey(order []string) string {
	cleaned := make([]string, 0, len(order))
	for _, name := range order {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			cleaned = append(cleaned, name)
		}
	}
	if len(cleaned) == 0 {
		cleaned = append(cleaned, config.DefaultSourceOrder...)
	}
	return strings.Join(cleaned, ">")
}

// registryKey 用注册表实际生效的顺序做键：认不出的站点会被它丢掉，
// 两套写法不同但实际相同的顺序应该共用一份候选缓存。
func registryKey(registry *source.Registry) string {
	if registry == nil {
		return orderKey(nil)
	}
	return strings.Join(registry.Order(), ">")
}

// sortedSchemeNames 让方案按名字有序处理，注册表与引擎的建立顺序可复现。
func sortedSchemeNames(schemes map[string]config.Scheme) []string {
	names := make([]string, 0, len(schemes))
	for name := range schemes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Apply 执行计划。只处理 update / add。
//
// 只有 update 会重试。理由：`setAni` 是**全量覆盖**，把同一个目标状态再写一遍
// 结果不变，天然幂等；而 `add` 走的是 `addAni?override=false`，
// 「传输层超时」并不等于「服务端没处理」—— 重试可能就是第二次新增。
// 宁可让这一条留到下一个周期再试，也不要在一个不确定的前提下重复创建订阅。
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
			if err := withRetry(ctx, func() error { return client.SetAni(ctx, action.body) }); err != nil {
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

// applyRetries 是一条写入的总尝试次数（含首次）。
const applyRetries = 3

// withRetry 对可重试的错误退避重试。
//
// 判据：传输层错误（连不上、EOF、超时）与 5xx / 429 重试；
// 4xx（apiKey 错、参数错）重试只是白等，直接抛。
func withRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < applyRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 400 * time.Millisecond
			select {
			case <-ctx.Done():
				if lastErr != nil {
					return lastErr
				}
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable(err) {
			return err
		}
	}
	return lastErr
}

// retryable 判断一个错误是否值得再试一次。
func retryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *assapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr.HTTP >= 500 || apiErr.HTTP == http.StatusTooManyRequests
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

// ---------- 内部工具 ----------

// buildEngine 把一套方案编译成规则引擎。
func buildEngine(scheme config.Scheme, maxStandby int) (*rules.Engine, error) {
	primary := make([]rules.Rule, 0, len(scheme.Primary))
	for _, rule := range scheme.Primary {
		primary = append(primary, rules.Rule{
			Name:     rule.Name,
			Match:    rule.Match,
			Priority: rule.Priority,
			Site:     rule.Site,
			Scope:    rule.ScopeOr(),
			In:       rule.InOr(),
		})
	}
	deny := make([]rules.DenyRule, 0, len(scheme.Deny))
	for _, rule := range scheme.Deny {
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
	resolved := instance.Resolve()
	return assapi.New(resolved.BaseURL, resolved.APIKey, cfg.ResolvedProxy(), 0)
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

// offsets 描述本次要写入的季度与剧集偏移。
// 字段为 nil 表示「配置里没写」，此时保持订阅里已有的值不动。
type offsets struct {
	season  *int
	main    *int
	standby *int
}

// matchTarget 找出作用于这条订阅的番剧级配置。
//
// 匹配分两轮：先按 bgmId 精确匹配（订阅的 bgmUrl 或 RSS 参数里的 bgmId），
// 再按标题包含匹配。订阅标题带年份与 tmdbid（例如「冰之城墙 (2026) [tmdbid=283428]」），
// 所以只能包含匹配，不能等值比较。标题同时看 title / jpTitle / mikanTitle，
// 写「冰之城墙」或「氷の城壁」都能命中。
func matchTarget(cfg *config.Config, instance string, ani assapi.Ani) *config.Target {
	if len(cfg.Targets) == 0 {
		return nil
	}
	bgmID := queryOf(ani).BgmID
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		if target.BgmID == "" || bgmID == "" || target.BgmID != bgmID {
			continue
		}
		if targetAppliesTo(*target, instance) {
			return target
		}
	}

	titles := []string{stringField(ani, "title"), stringField(ani, "jpTitle"), stringField(ani, "mikanTitle")}
	for i := range cfg.Targets {
		target := &cfg.Targets[i]
		if !targetAppliesTo(*target, instance) {
			continue
		}
		// 中文名与日文原名都试：订阅里存的是哪个取决于它的 RSS 来源
		for _, raw := range []string{target.Title, target.JPTitle} {
			needle := strings.ToLower(strings.TrimSpace(raw))
			if needle == "" {
				continue
			}
			for _, title := range titles {
				if title != "" && strings.Contains(strings.ToLower(title), needle) {
					return target
				}
			}
		}
	}
	return nil
}

// targetLabel 给番剧级配置起个可读名字，用于计划里说明偏移是谁规定的。
func targetLabel(target config.Target) string {
	if target.Title != "" {
		return target.Title
	}
	return target.BgmID
}

// schemeLabel 返回一条订阅实际生效的方案名；空表示用顶层 rules。
//
// 番剧级覆盖优先：target 上写了方案，这条订阅就按它走，实例那套不参与。
func schemeLabel(cfg *config.Config, override, instanceName string) string {
	if override != "" {
		return override
	}
	return cfg.SchemeFor(instanceName).Name
}

// offsetsOf 取出生效的季度与偏移：番剧级配置（targets）优先，其次全局兜底。
// 两处都没写时字段为 nil，表示「不碰订阅里已有的值」。
func offsetsOf(cfg *config.Config, target *config.Target) offsets {
	off := offsets{season: cfg.Sync.Season, main: cfg.Sync.Offset, standby: cfg.Sync.StandbyOffset}
	if target != nil {
		if target.Season != nil {
			off.season = target.Season
		}
		if target.Offset != nil {
			off.main = target.Offset
		}
		if target.StandbyOffset != nil {
			off.standby = target.StandbyOffset
		}
	}
	return off
}

// applyOffsets 把季度与主 RSS 偏移写进订阅对象；nil 表示不碰。
func applyOffsets(body map[string]any, off offsets) {
	if off.season != nil {
		body["season"] = *off.season
	}
	if off.main != nil {
		body["offset"] = *off.main
	}
}

// diff 比较现状与目标，产出字段级差异。
// desiredStandby 是已经算好的备用 RSS 列表（含继承或覆盖后的 offset）。
func diff(ani assapi.Ani, primary source.Candidate, desiredStandby []any, off offsets) []Change {
	var changes []Change
	if current := stringField(ani, "url"); current != primary.URL {
		changes = append(changes, Change{Field: "url", From: current, To: primary.URL})
	}
	if current := stringField(ani, "subgroup"); primary.Subgroup != "" && current != primary.Subgroup {
		changes = append(changes, Change{Field: "subgroup", From: current, To: primary.Subgroup})
	}
	if off.season != nil {
		if current, _ := intField(ani, "season"); current != *off.season {
			changes = append(changes, Change{Field: "season", From: strconv.Itoa(current), To: strconv.Itoa(*off.season)})
		}
	}
	if off.main != nil {
		if current, _ := intField(ani, "offset"); current != *off.main {
			changes = append(changes, Change{Field: "offset", From: strconv.Itoa(current), To: strconv.Itoa(*off.main)})
		}
	}
	current := standbySignature(ani["standbyRssList"])
	desired := standbySignature(desiredStandby)
	if strings.Join(current, "\n") != strings.Join(desired, "\n") {
		changes = append(changes, Change{
			Field: "standbyRssList",
			From:  standbyText(ani["standbyRssList"]),
			To:    standbyText(desiredStandby),
		})
	}
	return changes
}

func applyDesired(ani assapi.Ani, primary source.Candidate, desiredStandby []any, off offsets) map[string]any {
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
	body["standbyRssList"] = desiredStandby
	applyOffsets(body, off)
	return body
}

// standbyList 重建备用 RSS 列表。
//
// ANI-RSS 会按每个 RSS 的实际集数自动推断 offset（见 AniUtil.autoOffset），
// 所以整体重建列表时不能把它当成 0：先按 URL 继承订阅里已有的值，
// 只有配置显式写了 standbyOffset 才覆盖。新出现的 RSS 没有原值，取 0 或配置值。
func standbyList(standby []source.Candidate, existing assapi.Ani, override *int) []any {
	inherited := map[string]int{}
	if raw, ok := existing["standbyRssList"].([]any); ok {
		for _, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			url := stringField(m, "url")
			if url == "" {
				continue
			}
			if offset, ok := intField(m, "offset"); ok {
				inherited[url] = offset
			}
		}
	}

	list := make([]any, 0, len(standby))
	for _, item := range standby {
		offset := inherited[item.URL]
		if override != nil {
			offset = *override
		}
		list = append(list, map[string]any{
			"label":  item.Label,
			"url":    item.URL,
			"offset": offset,
		})
	}
	return list
}

// standbyText 把备用 RSS 列表渲染成可读文本：字幕组名，偏移非 0 时带上偏移。
func standbyText(raw any) string {
	items, ok := raw.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := stringField(m, "label")
		if label == "" {
			label = stringField(m, "url")
		}
		if offset, ok := intField(m, "offset"); ok && offset != 0 {
			label += fmt.Sprintf("(偏移%d)", offset)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, " , ")
}

// standbySignature 把备用 RSS 列表压成可比对的字符串：url 与 offset 都要参与，
// 否则「只改了偏移」的变更会被判成无需变更。
func standbySignature(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		offset, _ := intField(m, "offset")
		out = append(out, fmt.Sprintf("%s#%d", stringField(m, "url"), offset))
	}
	return out
}

// intField 从 map 里取整数。JSON 解出来的数字是 float64，本地构造的是 int。
func intField(m map[string]any, key string) (int, bool) {
	switch value := m[key].(type) {
	case float64:
		return int(value), true
	case int:
		return value, true
	case int64:
		return int(value), true
	default:
		return 0, false
	}
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

// existingIndex 记录每个实例里已经有哪些番剧。
//
// 自动发现必须靠它判断「这个番剧是不是真的还没有」：只比标题会漏，
// 因为同一个番剧在不同 RSS 来源下标题可能是中文名、也可能是日文原名。
// 所以优先按 bgmId 精确比对，标题包含只作为兜底。
type existingIndex struct {
	bgm    map[string]map[string]bool
	titles map[string][]string
}

func newExistingIndex() *existingIndex {
	return &existingIndex{bgm: map[string]map[string]bool{}, titles: map[string][]string{}}
}

func (ix *existingIndex) add(instance string, ani assapi.Ani) {
	if id := queryOf(ani).BgmID; id != "" {
		if ix.bgm[instance] == nil {
			ix.bgm[instance] = map[string]bool{}
		}
		ix.bgm[instance][id] = true
	}
	for _, raw := range []string{stringField(ani, "title"), stringField(ani, "jpTitle"), stringField(ani, "mikanTitle")} {
		if title := strings.ToLower(strings.TrimSpace(raw)); title != "" {
			ix.titles[instance] = append(ix.titles[instance], title)
		}
	}
}

func (ix *existingIndex) has(instance string, target config.Target) bool {
	if target.BgmID != "" && ix.bgm[instance][target.BgmID] {
		return true
	}
	for _, raw := range []string{target.Title, target.JPTitle} {
		needle := strings.ToLower(strings.TrimSpace(raw))
		if needle == "" {
			continue
		}
		for _, title := range ix.titles[instance] {
			if strings.Contains(title, needle) {
				return true
			}
		}
	}
	return false
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
