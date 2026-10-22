package source

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/zzzwannasleep/anisync/internal/httpx"
)

// Nyaa 适配 nyaa.si。
//
// Nyaa 没有「字幕组」维度，也没有对应的 ANI-RSS 接口，它的 RSS 一律是关键字搜索型，
// 所以这里：搜一次 → 从标题里提取 [放流组] → 每个组生成一个限定搜索词的 RSS。
// 这种搜索型 RSS ANI-RSS 可以直接吃（Ani.type = other）。
type Nyaa struct{ client *http.Client }

// Name 返回站点标识。
func (n *Nyaa) Name() string { return "nyaa" }

// NyaaHost 是站点根地址。
var NyaaHost = "https://nyaa.si"

const nyaaMaxGroups = 8

var nyaaGroupPrefix = regexp.MustCompile(`^\s*\[([^\]]{1,40})\]`)

// Groups 搜一次并聚合放流组。
func (n *Nyaa) Groups(ctx context.Context, q Query) ([]Candidate, error) {
	keyword := CleanKeyword(firstNonEmpty(q.NyaaQuery, q.JPTitle, q.Title))
	if keyword == "" {
		return nil, errf("nyaa 需要标题关键词")
	}

	body, status, err := getText(ctx, n.client, n.searchURL(keyword, ""))
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errf("nyaa 返回 HTTP %d", status)
	}

	var feed struct {
		Channel struct {
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("nyaa RSS 解析失败: %w", err)
	}
	if len(feed.Channel.Items) == 0 {
		return nil, nil
	}

	counts := map[string]int{}
	latest := map[string][]string{}
	for _, item := range feed.Channel.Items {
		match := nyaaGroupPrefix.FindStringSubmatch(item.Title)
		if match == nil {
			continue
		}
		name := clean(match[1])
		counts[name]++
		if len(latest[name]) < maxSampleTitles {
			latest[name] = append(latest[name], clean(item.Title))
		}
	}
	groups := make([]string, 0, len(counts))
	for name := range counts {
		groups = append(groups, name)
	}
	sort.Slice(groups, func(i, j int) bool {
		if counts[groups[i]] != counts[groups[j]] {
			return counts[groups[i]] > counts[groups[j]]
		}
		return groups[i] < groups[j]
	})
	if len(groups) > nyaaMaxGroups {
		groups = groups[:nyaaMaxGroups]
	}

	candidates := make([]Candidate, 0, len(groups))
	for _, name := range groups {
		candidates = append(candidates, Candidate{
			Site:     "nyaa",
			Label:    name,
			Subgroup: name,
			Type:     "other",
			URL:      n.searchURL(keyword, name),
			Titles:   latest[name],
			Weight:   counts[name],
		})
	}
	return dedupe(candidates), nil
}

// searchURL 拼出限定放流组的搜索 RSS，形式与 nyaa.si 搜索页「RSS」按钮给出的链接一致：
//
//	https://nyaa.si/?page=rss&q=%5BToonsHub%5D+White+Cat+Legend+S03&c=0_0&f=0
//
// c=0_0 是全部分类。Nyaa 没有字幕组维度，按分类过滤（例如 1_0 动画）反而会漏掉
// 挂在其它分类下的同组资源，所以统一用全分类，靠标题里的 [组名] 精确聚合。
func (n *Nyaa) searchURL(keyword, group string) string {
	query := strings.TrimSpace(keyword)
	if group != "" {
		query += " " + group
	}
	return n.host() + "/?page=rss&q=" + url.QueryEscape(query) + "&c=0_0&f=0"
}

func (n *Nyaa) host() string {
	if NyaaHost == "" {
		return "https://nyaa.si"
	}
	return strings.TrimRight(NyaaHost, "/")
}

// getText 抓取 RSS/XML 文本。
func getText(ctx context.Context, client *http.Client, rawURL string) ([]byte, int, error) {
	return httpx.Get(ctx, client, rawURL, map[string]string{
		"Accept": "application/xml,text/xml,text/html;q=0.9,*/*;q=0.8",
	})
}
