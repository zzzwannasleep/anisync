// Package discover 负责「自动发现该订阅什么」。
//
// 这是 AniSync 存在的理由：用户不该为了加一个订阅去打开 ANI-RSS 的 WebUI。
// 本包扫一遍「现在有什么番剧在放」，产出可以直接当新增列表用的 config.Target，
// 交给 planner 走它已有的新增流程（rssToAni 解析 → addAni 写入）。
//
// 三个来源全部免鉴权，也不需要 ANI-RSS 实例参与：
//
//	animegarden         AnimeGarden 的主题列表；只列出真的有字幕组在做的番剧，默认用它
//	bangumi-calendar    Bangumi 每日放送表；当季新番全集，会带上没人做的番剧
//	bangumi-collections Bangumi 某个用户的「在看」；即「扫我的追番列表」，需要 bgmUser
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
	"time"

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
	// Warnings 记录单个来源的失败。只有全部来源都失败才算致命错误。
	Warnings []string `json:"warnings,omitempty"`
}

// Options 控制一次发现。
type Options struct {
	Sources []string
	BgmUser string
	// Days 是「首播日期在最近多少天内」的窗口。
	Days    int
	Proxy   string
	Timeout time.Duration
	Store   *cache.Store
	Exclude []string
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
		items, err := fetchSource(ctx, client, opts, name)
		if err != nil {
			failures++
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s：%v", name, err))
			continue
		}
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

// fetchSource 按名字取一个来源，命中缓存就不再请求。
func fetchSource(ctx context.Context, client *http.Client, opts Options, name string) ([]Item, error) {
	key := "discover:" + name
	if name == config.DiscoverBgmCollections {
		key += ":" + opts.BgmUser
	}
	if opts.Store != nil {
		var cached []Item
		if opts.Store.Get(key, &cached) {
			return cached, nil
		}
	}

	var (
		items []Item
		err   error
	)
	switch name {
	case config.DiscoverAnimeGarden:
		items, err = animeGardenSubjects(ctx, client, opts.Days)
	case config.DiscoverBgmCalendar:
		items, err = bangumiCalendar(ctx, client)
	case config.DiscoverBgmCollections:
		items, err = bangumiCollections(ctx, client, opts.BgmUser)
	default:
		return nil, fmt.Errorf("未知的发现来源 %q", name)
	}
	if err != nil {
		return nil, err
	}
	if opts.Store != nil {
		opts.Store.Put(key, items)
	}
	return items, nil
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

// bangumiCalendar 拉每日放送表。返回的是当季在放送的番剧全集。
func bangumiCalendar(ctx context.Context, client *http.Client) ([]Item, error) {
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
			ID     int    `json:"id"`
			Name   string `json:"name"`
			NameCN string `json:"name_cn"`
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
			})
		}
	}
	return items, nil
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
