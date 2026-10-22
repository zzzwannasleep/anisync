package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// AniBT 适配 anibt.net。
// 真实接口（对照 ani-rss AniBTService.java）：
//
//	GET /api/anime/groups?bgmId=<id>  -> {"data":{"groups":[{"slug":"sakurato","name":"桜都字幕组",...}]}}
//	RSS: https://anibt.net/rss/anime.xml?bgmId=<id>&groupSlug=<slug>
type AniBT struct{ client *http.Client }

// Name 返回站点标识。
func (a *AniBT) Name() string { return "anibt" }

// Host 是站点根地址。
const aniBTHost = "https://anibt.net"

// Groups 拉取字幕组。
func (a *AniBT) Groups(ctx context.Context, q Query) ([]Candidate, error) {
	if q.BgmID == "" {
		return nil, errf("anibt 需要 bgmId，当前订阅没有可用的 bgm 条目")
	}
	endpoint := aniBTHost + "/api/anime/groups?bgmId=" + url.QueryEscape(q.BgmID)
	body, status, err := getJSON(ctx, a.client, endpoint)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errf("anibt 返回 HTTP %d", status)
	}

	var payload struct {
		Data struct {
			Groups []struct {
				Slug            string `json:"slug"`
				Name            string `json:"name"`
				Status          string `json:"status"`
				TotalReleases   int    `json:"totalReleases"`
				PublishedEpisod []int  `json:"publishedEpisodes"`
			} `json:"groups"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("anibt 响应解析失败: %w", err)
	}

	candidates := make([]Candidate, 0, len(payload.Data.Groups))
	for _, group := range payload.Data.Groups {
		if group.Slug == "" {
			continue
		}
		candidates = append(candidates, Candidate{
			Site:     "anibt",
			Label:    clean(group.Name),
			Subgroup: clean(group.Name),
			Type:     "ani-bt",
			BgmURL:   "https://bgm.tv/subject/" + q.BgmID,
			Weight:   group.TotalReleases,
			URL: fmt.Sprintf("%s/rss/anime.xml?bgmId=%s&groupSlug=%s",
				aniBTHost, url.QueryEscape(q.BgmID), url.QueryEscape(group.Slug)),
		})
	}
	return dedupe(candidates), nil
}
