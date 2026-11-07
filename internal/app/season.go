package app

import (
	"context"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
)

// SeasonItem 是一条本季新番。Added 表示实例的 targets / 订阅里已经有它。
type SeasonItem struct {
	discover.Item
	Added bool `json:"added"`
}

// SeasonOptions 控制一次季节表组装。
type SeasonOptions struct {
	// JapanOnly 覆盖配置里的开关；nil 表示按配置走。
	//
	// 设置页的「只要日漫」开关要能按当前勾选状态预览，不必先保存配置 ——
	// 切换过滤条件只是想看看列表长什么样，逼用户先落盘一次没有道理。
	JapanOnly *bool
	// Force 跳过缓存，直接问上游。
	Force bool
}

// SeasonResult 是「本季新番」的业务结果。
type SeasonResult struct {
	Items    []SeasonItem
	Total    int
	Added    int
	Filtered int
	Source   string
	Warnings []string
	// Unmatched 是 Yuc 里有、但 Bangumi 上还没建条目的番剧数。
	Unmatched int
	// InstanceError 是「一个实例都没读到」时的原因；读到了则为空。
	InstanceError string
	// Subscribed 是各启用实例的订阅总数。
	Subscribed int
}

// Payload 把结果渲染成既有的 /api/season 响应形状。
//
// 放在这里是为了让设置页与桥接层报出同一份东西：两份手写的 map 迟早会
// 在某一边多一个键，而界面上看见的差别没人查得出来。
func (r SeasonResult) Payload() map[string]any {
	payload := map[string]any{
		"items":    r.Items,
		"total":    r.Total,
		"added":    r.Added,
		"filtered": r.Filtered,
		"source":   r.Source,
		"warnings": r.Warnings,
	}
	// Yuc 里有、但 Bangumi 上还没建条目的番，认不出来就进不了列表。
	// 报出来，否则用户只会发现列表比 Yuc 上少了几部。
	if r.Unmatched > 0 {
		payload["unmatched"] = r.Unmatched
	}
	// 读实例失败时要说清楚，否则用户看到「一部都没加」会以为是判定逻辑坏了。
	if r.InstanceError != "" {
		payload["instanceError"] = r.InstanceError
	} else {
		payload["subscribed"] = r.Subscribed
	}
	return payload
}

// Season 返回「本季新番」：Bangumi 放送表 / Yuc 季度页里当前在放送的番剧，
// 并标出哪些实例里已经订过。
//
// 这条链路上没有模糊匹配：放送表的每一项都自带 bgmId，加进 targets 就是原样搬运。
// 「已添加」必须问实例，不能只看配置里的 targets —— 见下面那段注释。
func Season(ctx context.Context, cfg *config.Config, opts SeasonOptions) (SeasonResult, error) {
	proxy := cfg.ResolvedProxy()
	timeout := time.Duration(cfg.Sources.TimeoutSeconds) * time.Second
	ttl := time.Duration(cfg.Sources.CacheMinutes) * time.Minute
	if opts.Force {
		ttl = 0
	}
	exclude := append([]string(nil), cfg.Discover.Exclude...)
	japanOnly := cfg.Discover.JapanOnlyEnabled()
	if opts.JapanOnly != nil {
		japanOnly = *opts.JapanOnly
	}
	// 面板只跑「当季番剧表」型的来源。bangumi-collections 是用户自己的追番列表，
	// 不是一季的番剧表，混进来会让面板变成两种东西的并集。
	sources := make([]string, 0, len(cfg.Discover.SourceList()))
	for _, name := range cfg.Discover.SourceList() {
		if name != config.DiscoverBgmCollections {
			sources = append(sources, name)
		}
	}
	if len(sources) == 0 {
		sources = append([]string(nil), config.DefaultDiscoverSources...)
	}

	// 手写 target 的标题也要算「已添加」：配置里允许只写 title 不写 bgmId，
	// planner 照样能按标题包含匹配命中。只认 bgmId 的话，这种番在界面上会被当成
	// 没加过，用户就会给同一部番再加一条带 bgmId 的 —— 两份配置描述同一个番剧。
	existingTitles := make(map[string]bool, len(cfg.Targets))
	for _, target := range cfg.Targets {
		if title := FoldTitle(target.Title); title != "" {
			existingTitles[title] = true
		}
	}

	if timeout <= 0 {
		timeout = 25 * time.Second
	}

	// 「已添加」必须问实例，不能只看配置里的 targets。
	//
	// targets 是用户手写的番剧级配置（规定季度与偏移），订阅是实例里真实存在的东西，
	// 两者本来就不是一回事：用户在 ANI-RSS 里手动加的订阅根本不会出现在 targets 里。
	// 之前只看 targets，结果实例里明明订了 71 部，界面上显示一部都没加。
	existingIDs, subscribed, instanceErr := SubscribedBgmIDs(ctx, cfg)
	for _, target := range cfg.Targets {
		if id := strings.ToLower(strings.TrimSpace(target.BgmID)); id != "" {
			existingIDs[id] = true
		}
	}

	// 缓存要显式 Flush 才会落盘（cache.Put 只改内存）。这里必须开成变量再 defer，
	// 写成 cache.Open(...) 内联传参会得到一个「每次请求新建、请求结束即丢弃」的
	// 内存缓存 —— 看着像有缓存，实际上每次都重新打上游。
	store := cache.Open(CachePath(), ttl)
	defer func() { _ = store.Flush() }()

	result, err := discover.Discover(ctx, discover.Options{
		Sources:   sources,
		JapanOnly: japanOnly,
		Proxy:     proxy,
		Timeout:   timeout,
		Store:     store,
		// 黑名单必须一起生效：界面上写着「命中就不列出来」，不传的话用户能用「全选」
		// 把黑名单里的番一次性灌进 targets。自动添加那条路（planner）本来就传它。
		Exclude: exclude,
	})
	if err != nil {
		return SeasonResult{}, err
	}

	items := make([]SeasonItem, 0, len(result.Items))
	added := 0
	for _, item := range result.Items {
		isAdded := existingIDs[strings.ToLower(item.BgmID)] ||
			existingTitles[FoldTitle(item.Title)] ||
			existingTitles[FoldTitle(item.JPTitle)]
		if isAdded {
			added++
		}
		items = append(items, SeasonItem{Item: item, Added: isAdded})
	}
	out := SeasonResult{
		Items:    items,
		Total:    len(items),
		Added:    added,
		Filtered: result.Filtered,
		Source:   strings.Join(result.Sources, ","),
		Warnings: result.Warnings,
		// Yuc 里有、但 Bangumi 上还没建条目的番，认不出来就进不了列表。
		Unmatched: result.Unmatched,
	}
	if instanceErr != nil {
		out.InstanceError = instanceErr.Error()
	} else {
		out.Subscribed = subscribed
	}
	return out, nil
}
