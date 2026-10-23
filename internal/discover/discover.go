// Package discover 负责「自动发现该订阅什么」。
//
// 这是 AniSync 存在的理由：用户不该为了加一个订阅去打开 ANI-RSS 的 WebUI。
// 本包扫一遍「现在有什么番剧在放」，产出可以直接当新增列表用的 config.Target，
// 交给 planner 走它已有的新增流程（rssToAni 解析 → addAni 写入）。
//
// 默认来源是 Bangumi 每日放送表。选它的唯一理由：它返回的每一条**自带 bgmId**，
// 而 ANI-RSS 的订阅正是按 bgmId 认番剧的——整条链路上没有一步需要「拿名字猜番剧」。
// 任何只给标题的数据源都得补一步模糊检索，中文译名在 Bangumi 上的命中率低到不能用。
//
//	animegarden          AnimeGarden 的主题列表；只列出真的有字幕组在做的番剧
//	bangumi-calendar     Bangumi 每日放送表；当季新番全集，自带 bgmId，默认用它
//	bangumi-collections  Bangumi 某个用户的「在看」；需要 bgmUser
//
// 发现出来的条目不写季度与偏移：ANI-RSS 会按每个 RSS 的实际集数自己推断，
// 这也正是「不用打开 ASS」的前提——绝大多数番剧本来就不需要手工指定。
package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/httpx"
)

// AnimeGardenAPI 是 AnimeGarden 的接口根地址。
var AnimeGardenAPI = "https://api.animes.garden"

// BangumiAPI 是 Bangumi 的接口根地址。
var BangumiAPI = "https://api.bgm.tv"

// bangumiUA 是给 Bangumi 用的 UA。它要求调用方自报家门，用浏览器 UA 可能被限流。
const bangumiUA = "zzzwannasleep/anisync (https://github.com/zzzwannasleep/anisync)"

// collectionsPageSize 是 Bangumi 收藏接口的分页大小（该接口 limit 上限为 50）。
const collectionsPageSize = 50

// collectionsMaxPages 是收藏列表最多翻多少页，防止收藏几千部时把内存和配额吃光。
const collectionsMaxPages = 4

// Item 是一条发现结果。
type Item struct {
	BgmID   string    `json:"bgmId"`
	Title   string    `json:"title"`
	JPTitle string    `json:"jpTitle,omitempty"`
	Source  string    `json:"source"`
	AirAt   time.Time `json:"airAt,omitempty"`
	// Cover 是封面地址，已统一成 https；前端必须经 /api/bgm/cover 中转，
	// 直连 lain.bgm.tv 在浏览器里取不到图。
	Cover string `json:"cover,omitempty"`
	// Summary 是简介，列表里只取第一行当副标题。
	Summary string `json:"summary,omitempty"`
	// Doing 是 Bangumi 上的「在看」人数，用来把冷门番排到后面。
	Doing int `json:"doing,omitempty"`
	// Score 是 Bangumi 评分，没人评分时为 0。
	Score float64 `json:"score,omitempty"`
	// Weekday 是放送星期，1 = 周一，0 = 未知。
	Weekday int `json:"weekday,omitempty"`
}

// Target 把发现结果转成一条番剧级配置。
// enable 决定新增出来的订阅是否直接启用；季度与偏移留空，交给 ANI-RSS 推断。
func (i Item) Target(enable bool) config.Target {
	flag := enable
	return config.Target{
		Title:   i.Title,
		JPTitle: i.JPTitle,
		BgmID:   i.BgmID,
		Enable:  &flag,
	}
}

// Result 是一次发现的结果。
type Result struct {
	Items []Item `json:"items"`
	// Sources 是本次实际生效的来源顺序。
	Sources []string `json:"sources"`
	// Filtered 是被地区/平台过滤掉（JapanOnly）的条数。
	// 报出来是为了让界面能解释「为什么只剩这些」—— 不报的话用户只会看到列表变短了。
	Filtered int `json:"filtered,omitempty"`
	// Unmatched 是来源里有、但在 Bangumi 上认不出条目的条数（Yuc 来源会产生）。
	Unmatched int `json:"unmatched,omitempty"`
	// Warnings 记录单个来源的失败。只有全部来源都失败才算致命错误。
	Warnings []string `json:"warnings,omitempty"`
}

// sourceStats 是一次来源拉取的副产物，用来跟用户解释「为什么列表比源站少」。
type sourceStats struct {
	// Filtered 是被地区/平台过滤掉的条数（只有放送表来源会产生）。
	Filtered int
	// Unmatched 是来源里有、但在 Bangumi 上认不出条目的条数（只有 Yuc 来源会产生）。
	Unmatched int
}

// Options 控制一次发现。
type Options struct {
	Sources []string
	BgmUser string
	// Days 是「首播日期在最近多少天内」的窗口。
	Days int
	// JapanOnly 只保留日本 TV 番剧与剧场版；放送表来源才会用到。
	JapanOnly bool
	Proxy     string
	Timeout   time.Duration
	Store     *cache.Store
	Exclude   []string
}

// Discover 依次扫各来源并合并去重。
//
// 去重按 bgmId：同一个番剧先被高优先级来源发现就用它，来源顺序即优先级。
func Discover(ctx context.Context, opts Options) (*Result, error) {
	sources := normalizeSources(opts.Sources)
	excludes, err := compileExcludes(opts.Exclude)
	if err != nil {
		return nil, err
	}
	client, err := httpx.New(opts.Proxy, timeoutOf(opts))
	if err != nil {
		return nil, err
	}

	result := &Result{Sources: sources}
	seen := map[string]bool{}
	failures := 0

	for _, name := range sources {
		items, stats, err := fetchSource(ctx, client, opts, name)
		if err != nil {
			failures++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s：%v", name, err))
			continue
		}
		result.Filtered += stats.Filtered
		result.Unmatched += stats.Unmatched
		for _, item := range items {
			item.Source = name
			if item.BgmID == "" || item.Title == "" {
				continue
			}
			if seen[item.BgmID] || excluded(excludes, item) {
				continue
			}
			seen[item.BgmID] = true
			result.Items = append(result.Items, item)
		}
	}
	if failures == len(sources) {
		return nil, fmt.Errorf("所有发现来源都失败了：%s", strings.Join(result.Warnings, "；"))
	}
	return result, nil
}

// cachedSource 是缓存里存的东西：过滤后的条目 + 当时的统计。
//
// 只存 []Item 的话，缓存命中时「已滤掉 N 部」会变成 0 —— 界面上看起来像什么都没过滤，
// 而列表明明短了一截。把统计一起缓存，冷热两条路径报出来的数字才一致。
type cachedSource struct {
	Items []Item      `json:"items"`
	Stats sourceStats `json:"stats"`
}

// fetchSource 按名字取一个来源，命中缓存就不再请求。
func fetchSource(ctx context.Context, client *http.Client, opts Options, name string) ([]Item, sourceStats, error) {
	key := "discover:" + name
	if name == config.DiscoverBgmCollections {
		key += ":" + opts.BgmUser
	}
	// 过滤开关会改变结果，键里必须带上：否则切换开关之后拿到的是另一份配置下的旧列表。
	if name == config.DiscoverBgmCalendar && opts.JapanOnly {
		key += ":japan"
	}
	if opts.Store != nil {
		var cached cachedSource
		if opts.Store.Get(key, &cached) {
			return cached.Items, cached.Stats, nil
		}
	}

	var (
		items []Item
		stats sourceStats
		err   error
	)
	switch name {
	case config.DiscoverYuc:
		items, stats, err = yucSubjects(ctx, client, opts)
	case config.DiscoverAnimeGarden:
		items, err = animeGardenSubjects(ctx, client, opts.Days)
	case config.DiscoverBgmCalendar:
		items, stats, err = bangumiCalendar(ctx, client, opts)
	case config.DiscoverBgmCollections:
		items, err = bangumiCollections(ctx, client, opts.BgmUser)
	default:
		return nil, sourceStats{}, fmt.Errorf("未知的发现来源 %q", name)
	}
	if err != nil {
		return nil, sourceStats{}, err
	}
	if opts.Store != nil {
		opts.Store.Put(key, cachedSource{Items: items, Stats: stats})
	}
	return items, stats, nil
}

// ---------- AnimeGarden ----------

// agSubjects 是 AnimeGarden /subjects 的响应。
type agSubjects struct {
	Status   string `json:"status"`
	Subjects []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		ActivedAt  string `json:"activedAt"`
		IsArchived bool   `json:"isArchived"`
		Search     struct {
			Include []string `json:"include"`
		} `json:"search"`
	} `json:"subjects"`
}

// animeGardenSubjects 拉 AnimeGarden 的主题列表。
//
// 这个列表的价值在于「真的有资源」：AnimeGarden 只给有字幕组在做、并且已经聚合到资源的
// 番剧建主题，所以拿它当发现来源，不会像放送表那样把一堆没人做的番剧灌进实例。
func animeGardenSubjects(ctx context.Context, client *http.Client, days int) ([]Item, error) {
	body, status, err := httpx.Get(ctx, client, AnimeGardenAPI+"/subjects", map[string]string{
		"Accept": "application/json",
	})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("AnimeGarden 返回 HTTP %d", status)
	}
	var payload agSubjects
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("AnimeGarden 主题列表解析失败: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	items := make([]Item, 0, len(payload.Subjects))
	for _, subject := range payload.Subjects {
		if subject.IsArchived {
			continue
		}
		// activedAt 是首播时间，可能是未来（还没开播的新番）：未来的照收，
		// 先把订阅建好，开播时 ANI-RSS 自己就会抓到。
		airAt := parseTime(subject.ActivedAt)
		if !airAt.IsZero() && airAt.Before(cutoff) {
			continue
		}
		items = append(items, Item{
			BgmID:   strconv.Itoa(subject.ID),
			Title:   strings.TrimSpace(subject.Name),
			JPTitle: pickJPTitle(subject.Search.Include),
			AirAt:   airAt,
		})
	}
	sortByAirAt(items)
	return items, nil
}

// ---------- Bangumi ----------

// bangumiCalendar 拉每日放送表，按需过滤地区与平台，再按热度排序。
func bangumiCalendar(ctx context.Context, client *http.Client, opts Options) ([]Item, sourceStats, error) {
	items, err := bangumiCalendarRaw(ctx, client)
	if err != nil {
		return nil, sourceStats{}, err
	}
	// 只要日本 TV 番剧与剧场版时，得逐条查详情才能判断 —— 放送表本身不带地区与平台。
	var stats sourceStats
	if opts.JapanOnly {
		before := len(items)
		items = filterJapaneseTV(ctx, client, opts.Store, items)
		stats.Filtered = before - len(items)
	}
	sortByDoing(items)
	return items, stats, nil
}

// bangumiCalendarRaw 只把放送表拉下来解析成条目，不做任何过滤、不排序。
//
// 拆出来是因为 Yuc 那条路也要它：Yuc 给的是「当季有哪些番」，bgmId 得靠放送表补，
// 而建索引要的是全量（过滤是另一回事），所以这里不能顺手把地区过滤做掉。
func bangumiCalendarRaw(ctx context.Context, client *http.Client) ([]Item, error) {
	body, status, err := httpx.Get(ctx, client, BangumiAPI+"/calendar", bangumiHeaders())
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Bangumi 放送表返回 HTTP %d", status)
	}
	var days []struct {
		Weekday struct {
			ID int `json:"id"`
		} `json:"weekday"`
		Items []struct {
			ID      int               `json:"id"`
			Name    string            `json:"name"`
			NameCN  string            `json:"name_cn"`
			AirDate string            `json:"air_date"`
			Summary string            `json:"summary"`
			Images  map[string]string `json:"images"`
			Rating  struct {
				Score float64 `json:"score"`
			} `json:"rating"`
			Collection struct {
				Doing int `json:"doing"`
			} `json:"collection"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &days); err != nil {
		return nil, fmt.Errorf("Bangumi 放送表解析失败: %w", err)
	}

	var items []Item
	for _, day := range days {
		for _, subject := range day.Items {
			items = append(items, Item{
				BgmID:   strconv.Itoa(subject.ID),
				Title:   firstNonEmpty(subject.NameCN, subject.Name),
				JPTitle: strings.TrimSpace(subject.Name),
				AirAt:   parseTime(subject.AirDate),
				Cover:   bgm.NormalizeCover(pickCover(subject.Images)),
				Summary: strings.TrimSpace(subject.Summary),
				Doing:   subject.Collection.Doing,
				Score:   subject.Rating.Score,
				Weekday: day.Weekday.ID,
			})
		}
	}
	return items, nil
}

// sortByDoing 按在看人数倒序：设置页第一屏该是有人在追的番，而不是接口给的原始顺序。
// 人数相同再按首播时间，保证顺序稳定可复现（测试要能断言）。
func sortByDoing(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Doing != items[j].Doing {
			return items[i].Doing > items[j].Doing
		}
		if !items[i].AirAt.Equal(items[j].AirAt) {
			return items[i].AirAt.Before(items[j].AirAt)
		}
		return items[i].BgmID < items[j].BgmID
	})
}

// coverPriority 是 Bangumi 封面尺寸的取用顺序。
// large 是 1000px 级别，列表里用不上；common 够清晰又省流量。
var coverPriority = []string{"common", "large", "medium", "small", "grid"}

// pickCover 按优先级挑一张封面，都没有就返回空串。
func pickCover(images map[string]string) string {
	for _, size := range coverPriority {
		if url := strings.TrimSpace(images[size]); url != "" {
			return url
		}
	}
	return ""
}

// ---------- 地区与平台过滤 ----------

// subjectWorkers 是查条目详情时的并发数。
// 单条约 55ms，110 条约 6 秒；并发再高就是拿 Bangumi 的限流风险换几秒钟，不划算。
const subjectWorkers = 6

// subjectMeta 是 /v0/subjects/{id} 里判断「这是什么番」需要的两个信号。
type subjectMeta struct {
	Platform string   `json:"platform"`
	MetaTags []string `json:"meta_tags"`
}

// japaneseRegionTag 是 Bangumi 给日本作品打的地区标签。
const japaneseRegionTag = "日本"

// isJapaneseTV 判断一条详情是不是「日本 TV 番剧或剧场版」。
//
// 两个条件都要满足，缺一不可：
//   - 平台必须是 TV 或剧场版。OVA / OAD / WEB / 短片这些直接排除 ——
//     用户要的是能按周追的番剧，不是「黑暗召唤师迷你动画剧场」这种附赠短片。
//   - 地区标签必须含「日本」。只按平台过滤挡不住国漫美漫：大王饶命、罗小黑是 WEB，
//     但极道寿司次郎是 TV + 美国，降世神通是 TV + 法国，光看平台会漏进来。
//
// 已知代价：Bangumi 上有些日本作品没打地区标签（例如「屁屁侦探」「数码宝贝30周年」），
// 会被一起滤掉。宁缺毋滥 —— 混进一部国漫比漏掉一部冷门日漫更让人难受。
func isJapaneseTV(meta subjectMeta) bool {
	switch meta.Platform {
	case "TV", "剧场版":
	default:
		return false
	}
	for _, tag := range meta.MetaTags {
		if tag == japaneseRegionTag {
			return true
		}
	}
	return false
}

// filterJapaneseTV 把放送表收敛成日本 TV 番剧与剧场版。
//
// 逐条查 /v0/subjects/{id} 并按 bgmId 缓存：第一次约 6 秒，之后 12 小时内是瞬时的。
// 查不到的条目**保留**：一次网络抖动不该把用户本来想订阅的番静默吞掉，
// 宁可多列一条让他自己划掉。
func filterJapaneseTV(ctx context.Context, client *http.Client, store *cache.Store, items []Item) []Item {
	keep := make([]bool, len(items))
	var wg sync.WaitGroup
	sem := make(chan struct{}, subjectWorkers)
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			meta, err := subjectDetail(ctx, client, store, items[i].BgmID)
			keep[i] = err != nil || isJapaneseTV(meta)
		}(i)
	}
	wg.Wait()

	out := make([]Item, 0, len(items))
	for i, item := range items {
		if keep[i] {
			out = append(out, item)
		}
	}
	return out
}

// subjectDetail 取一个条目的地区与平台，优先读缓存。
func subjectDetail(ctx context.Context, client *http.Client, store *cache.Store, id string) (subjectMeta, error) {
	key := "bgm:subject:" + id
	if store != nil {
		var cached subjectMeta
		if store.Get(key, &cached) {
			return cached, nil
		}
	}
	body, status, err := httpx.Get(ctx, client, BangumiAPI+"/v0/subjects/"+url.PathEscape(id), bangumiHeaders())
	if err != nil {
		return subjectMeta{}, err
	}
	if status != http.StatusOK {
		return subjectMeta{}, fmt.Errorf("Bangumi 条目 %s 返回 HTTP %d", id, status)
	}
	var meta subjectMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return subjectMeta{}, fmt.Errorf("Bangumi 条目 %s 解析失败: %w", id, err)
	}
	if store != nil {
		store.Put(key, meta)
	}
	return meta, nil
}

// bangumiCollections 拉某个用户「在看」的番剧，也就是扫他的追番列表。
func bangumiCollections(ctx context.Context, client *http.Client, user string) ([]Item, error) {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil, fmt.Errorf("缺少 Bangumi 用户名")
	}
	var items []Item
	for page := 0; page < collectionsMaxPages; page++ {
		query := url.Values{}
		query.Set("subject_type", "2") // 2 = 动画
		query.Set("type", "3")         // 3 = 在看
		query.Set("limit", strconv.Itoa(collectionsPageSize))
		query.Set("offset", strconv.Itoa(page*collectionsPageSize))

		endpoint := BangumiAPI + "/v0/users/" + url.PathEscape(user) + "/collections?" + query.Encode()
		body, status, err := httpx.Get(ctx, client, endpoint, bangumiHeaders())
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			return nil, fmt.Errorf("Bangumi 用户 %s 不存在，或收藏没有公开", user)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("Bangumi 收藏返回 HTTP %d", status)
		}
		var payload struct {
			Total int `json:"total"`
			Data  []struct {
				Subject struct {
					ID     int    `json:"id"`
					Name   string `json:"name"`
					NameCN string `json:"name_cn"`
				} `json:"subject"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("Bangumi 收藏解析失败: %w", err)
		}
		for _, row := range payload.Data {
			items = append(items, Item{
				BgmID:   strconv.Itoa(row.Subject.ID),
				Title:   firstNonEmpty(row.Subject.NameCN, row.Subject.Name),
				JPTitle: strings.TrimSpace(row.Subject.Name),
			})
		}
		if len(payload.Data) < collectionsPageSize || len(items) >= payload.Total {
			break
		}
	}
	return items, nil
}

func bangumiHeaders() map[string]string {
	return map[string]string{
		"Accept":     "application/json",
		"User-Agent": bangumiUA,
	}
}

// ---------- 工具 ----------

// kanaPattern 用来从别名里认出日文原名：只有日文标题会带假名。
// 中文标题同样是汉字，光看「有没有汉字」分不出来，必须看假名。
var kanaPattern = regexp.MustCompile(`[\p{Hiragana}\p{Katakana}]`)

// pickJPTitle 从 AnimeGarden 的多语言别名里挑出日文原名。
//
// Nyaa 这类站点只认日文/罗马音，中文名搜不到东西，所以这个字段直接决定
// 自动发现出来的番剧能不能在 Nyaa 上匹配到放流组。挑不到就留空。
func pickJPTitle(aliases []string) string {
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias != "" && kanaPattern.MatchString(alias) {
			return alias
		}
	}
	return ""
}

// parseTime 宽松解析时间；解析不了返回零值，调用方按「没有时间」处理。
func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// sortByAirAt 按首播时间倒序，越新的番剧越靠前。
func sortByAirAt(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].AirAt.Equal(items[j].AirAt) {
			return items[i].Title < items[j].Title
		}
		return items[i].AirAt.After(items[j].AirAt)
	})
}

func compileExcludes(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("discover.exclude 的正则不合法 %q: %w", pattern, err)
		}
		out = append(out, compiled)
	}
	return out, nil
}

// excluded 判断这条发现结果是否命中黑名单。标题与日文名都算。
func excluded(patterns []*regexp.Regexp, item Item) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(item.Title) || (item.JPTitle != "" && pattern.MatchString(item.JPTitle)) {
			return true
		}
	}
	return false
}

func normalizeSources(sources []string) []string {
	out := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, name := range sources {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return append([]string(nil), config.DefaultDiscoverSources...)
	}
	return out
}

func timeoutOf(opts Options) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	return 30 * time.Second
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
