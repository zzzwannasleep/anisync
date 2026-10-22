package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// AnimeGarden 适配 api.animes.garden。
// 真实接口（对照 ani-rss AnimeGardenService.java 与线上响应）：
//
//	GET /resources?subject=<bgmId>&pageSize=200&duplicate=false
//	    -> {"resources":[{"title":"[桜都字幕组] xxx","fansub":{"id":9,"name":"桜都字幕组"},...}]}
//	RSS: https://api.animes.garden/feed.xml?subject=<bgmId>&fansub=<name>
type AnimeGarden struct{ client *http.Client }

// Name 返回站点标识。
func (a *AnimeGarden) Name() string { return "animegarden" }

const animeGardenHost = "https://api.animes.garden"

// Groups 按 fansub 聚合资源。
func (a *AnimeGarden) Groups(ctx context.Context, q Query) ([]Candidate, error) {
	if q.BgmID == "" {
		return nil, errf("animegarden 需要 bgmId，当前订阅没有可用的 bgm 条目")
	}
	query := url.Values{}
	query.Set("subject", q.BgmID)
	query.Set("pageSize", "200")
	query.Set("duplicate", "false")

	body, status, err := getJSON(ctx, a.client, animeGardenHost+"/resources?"+query.Encode())
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errf("animegarden 返回 HTTP %d", status)
	}

	var payload struct {
		Resources []struct {
			Title  string `json:"title"`
			Fansub *struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"fansub"`
			CreatedAt string `json:"createdAt"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("animegarden 响应解析失败: %w", err)
	}

	type agg struct {
		name  string
		count int
		last  string
	}
	byName := map[string]*agg{}
	for _, res := range payload.Resources {
		if res.Fansub == nil || strings.TrimSpace(res.Fansub.Name) == "" {
			continue
		}
		name := clean(res.Fansub.Name)
		item := byName[name]
		if item == nil {
			item = &agg{name: name}
			byName[name] = item
		}
		item.count++
		if res.CreatedAt > item.last {
			item.last = res.CreatedAt
		}
	}

	groups := make([]*agg, 0, len(byName))
	for _, item := range byName {
		groups = append(groups, item)
	}
	// 资源多的排前面，作为同优先级下的次序
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].last > groups[j].last
	})

	candidates := make([]Candidate, 0, len(groups))
	for _, group := range groups {
		candidates = append(candidates, Candidate{
			Site:     "animegarden",
			Label:    group.name,
			Subgroup: group.name,
			Type:     "anime-garden",
			BgmURL:   "https://bgm.tv/subject/" + q.BgmID,
			Weight:   group.count,
			// ANI-RSS 源码里对 & 做了转义
			URL: fmt.Sprintf("%s/feed.xml?subject=%s&fansub=%s",
				animeGardenHost, url.QueryEscape(q.BgmID), strings.ReplaceAll(url.QueryEscape(group.name), "+", "%20")),
		})
	}
	return dedupe(candidates), nil
}
