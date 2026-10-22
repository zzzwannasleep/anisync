package source

import (
	"context"
	"regexp"
	"strings"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/cache"
)

// maxSampleTitles 每个字幕组最多取多少条最近标题参与规则匹配。
const maxSampleTitles = 12

// MikanHost 是 mikan 站点根地址。
const MikanHost = "https://mikanani.me"

// assSource 把 AniBT / AnimeGarden / Mikan 三家统一交给 ANI-RSS 实例去解析。
// 我们不自己爬这三个站点：实例返回什么字幕组、什么 RSS 地址，我们就用什么。
type assSource struct {
	site   string
	client *assapi.Client
	store  *cache.Store
}

func (a *assSource) Name() string { return a.site }

func (a *assSource) Groups(ctx context.Context, q Query) ([]Candidate, error) {
	if a.client == nil {
		return nil, errf("%s 需要 ANI-RSS 实例提供站点数据：配置里至少启用一个实例", a.site)
	}
	var (
		groups []assapi.Group
		err    error
	)
	switch a.site {
	case "anibt":
		if q.BgmID == "" {
			return nil, errf("anibt 需要 bgmId")
		}
		groups, err = a.cached(ctx, "anibt:"+q.BgmID, func() ([]assapi.Group, error) {
			return a.client.AniBTGroup(ctx, q.BgmID)
		})
	case "animegarden":
		if q.BgmID == "" {
			return nil, errf("animegarden 需要 bgmId")
		}
		groups, err = a.cached(ctx, "animegarden:"+q.BgmID, func() ([]assapi.Group, error) {
			return a.client.AnimeGardenGroup(ctx, q.BgmID)
		})
	case "mikan":
		page, pageErr := a.mikanPage(ctx, q)
		if pageErr != nil {
			return nil, pageErr
		}
		if page == "" {
			// 搜索没有结果，不算错误：上层会把它记为「该站点 0 个候选」
			return nil, nil
		}
		groups, err = a.cached(ctx, "mikan:"+page, func() ([]assapi.Group, error) {
			return a.client.MikanGroup(ctx, page)
		})
	default:
		return nil, errf("未知站点 %s", a.site)
	}
	if err != nil {
		return nil, err
	}
	return toCandidates(a.site, groups), nil
}

// cached 先查缓存，未命中再真正请求。
func (a *assSource) cached(ctx context.Context, key string, load func() ([]assapi.Group, error)) ([]assapi.Group, error) {
	var groups []assapi.Group
	if a.store.Get(key, &groups) {
		return groups, nil
	}
	groups, err := load()
	if err != nil {
		return nil, err
	}
	a.store.Put(key, trimGroups(groups))
	return groups, nil
}

// trimGroups 只留参与规则匹配所需的标题样本。
// 规则最多读 maxSampleTitles 条标题，多存的都是白占地方；热度改由 Total 承载，不会丢。
func trimGroups(groups []assapi.Group) []assapi.Group {
	out := make([]assapi.Group, 0, len(groups))
	for _, group := range groups {
		if group.Total == 0 {
			group.Total = len(group.Items)
		}
		if len(group.Items) > maxSampleTitles {
			group.Items = group.Items[:maxSampleTitles]
		}
		out = append(out, group)
	}
	return out
}

// mikanPage 得到 mikan 番剧页地址。
// mikanGroup 接口吃的是番剧页 URL（实例侧按页面解析），不是 RSS 地址。
func (a *assSource) mikanPage(ctx context.Context, q Query) (string, error) {
	if page := mikanPageFromID(q.MikanID); page != "" {
		return page, nil
	}
	if page := mikanPageFromID(MikanIDFromURL(q.RSSURL)); page != "" {
		return page, nil
	}
	return a.searchMikanPage(ctx, q)
}

// searchMikanPage 用实例的 mikan 搜索接口反查番剧页：
// 结果里带 bgmId，能和订阅的 bgmUrl 精确对上，比按标题猜可靠。
func (a *assSource) searchMikanPage(ctx context.Context, q Query) (string, error) {
	text := CleanKeyword(firstNonEmpty(q.MikanTitle, q.Title, q.JPTitle))
	if text == "" {
		return "", errf("mikan 需要标题或 bangumiId")
	}
	items, err := a.client.MikanSearch(ctx, text)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", nil
	}
	if q.BgmID != "" {
		for _, item := range items {
			if strings.TrimSpace(item.BgmID) == q.BgmID && item.URL != "" {
				return item.URL, nil
			}
		}
	}
	return items[0].URL, nil
}

var mikanBangumiID = regexp.MustCompile(`(?i)bangumiId=(\d+)`)

// MikanIDFromURL 从 mikan RSS 地址里取番剧 id。
func MikanIDFromURL(raw string) string {
	if m := mikanBangumiID.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return ""
}

func mikanPageFromID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return MikanHost + "/Home/Bangumi/" + id
}

// toCandidates 把实例返回的字幕组转成候选，并带上最近的资源标题供规则匹配。
func toCandidates(site string, groups []assapi.Group) []Candidate {
	out := make([]Candidate, 0, len(groups))
	for _, group := range groups {
		name := clean(group.DisplayName())
		rss := strings.TrimSpace(group.RSS)
		if name == "" || rss == "" {
			continue
		}
		titles := make([]string, 0, maxSampleTitles)
		for _, item := range group.Items {
			if len(titles) >= maxSampleTitles {
				break
			}
			if title := clean(item.Title); title != "" {
				titles = append(titles, title)
			}
		}
		weight := group.Total
		if weight == 0 {
			weight = len(group.Items)
		}
		out = append(out, Candidate{
			Site:     site,
			Label:    name,
			Subgroup: name,
			URL:      rss,
			Type:     SiteType(site),
			BgmURL:   strings.TrimSpace(group.BgmURL),
			Titles:   titles,
			Weight:   weight,
		})
	}
	return dedupe(out)
}
