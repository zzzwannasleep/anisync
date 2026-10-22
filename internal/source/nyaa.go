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
)

// Nyaa 适配 nyaa.si。
// Nyaa 没有「字幕组」概念，只有种子标题里的 [放流组] 前缀，所以：
//  1. 用番剧标题搜一次 RSS；
//  2. 从标题里提取 [组名]，按出现次数排序；
//  3. 每个组生成一个「限定搜索词」的 RSS 地址，直接可以喂给 ANI-RSS（type=other）。
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

	feedURL := n.searchURL(keyword, "")
	body, status, err := getText(ctx, n.client, feedURL)
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
	for _, item := range feed.Channel.Items {
		if match := nyaaGroupPrefix.FindStringSubmatch(item.Title); match != nil {
			counts[clean(match[1])]++
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
		})
	}
	return dedupe(candidates), nil
}

// searchURL 拼出限定放流组的搜索 RSS。
// 分类用 0_0（全部）：实测 c=1_2（英译）对中文字幕组返回空结果，
// 按分类过滤没有意义——后面还要按 [组名] 精确聚合。
func (n *Nyaa) searchURL(keyword, group string) string {
	query := keyword
	if group != "" {
		query = keyword + " " + group
	}
	values := url.Values{}
	values.Set("page", "rss")
	values.Set("q", query)
	values.Set("c", "0_0")
	values.Set("f", "0")
	return n.host() + "/?" + strings.ReplaceAll(values.Encode(), "+", "%20")
}

func (n *Nyaa) host() string {
	if NyaaHost == "" {
		return "https://nyaa.si"
	}
	return strings.TrimRight(NyaaHost, "/")
}
