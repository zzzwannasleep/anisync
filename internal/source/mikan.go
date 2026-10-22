package source

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Mikan 适配 mikanani.me。
// 真实结构（对照 ani-rss MikanService.java 与线上页面）：
//
//	搜索页  /Home/Search?searchstr=<kw>      -> <a href="/Home/Bangumi/<id>">标题</a>
//	详情页  /Home/Bangumi/<id>               -> .bangumi-info 里有 bgm.tv/subject/<id> 链接
//	                                          -> .leftbar-item 里 <a class="subgroup-name" data-anchor="#<gid>">字幕组</a>
//	RSS:   /RSS/Bangumi?bangumiId=<id>&subgroupid=<gid>
//
// ANI-RSS 自己靠付费缓存做 mikanId<->bgmId 映射，这里不依赖它：用标题搜索 + 详情页反查 bgm 链接。
type Mikan struct{ client *http.Client }

// Name 返回站点标识。
func (m *Mikan) Name() string { return "mikan" }

// MikanHost 是 Mikan 主站，可被测试替换。
var MikanHost = "https://mikanani.me"

const mikanMaxCandidates = 5

var (
	// 搜索结果里的锚点内容常常是 <img>，不能要求有文本，只取 href 里的 id。
	mikanBangumiLink = regexp.MustCompile(`<a[^>]+href="(?:https?://[^"]+)?/Home/Bangumi/(\d+)/?"`)
	mikanBgmLink     = regexp.MustCompile(`(?:bgm\.tv|bangumi\.tv|chii\.in)/subject/(\d+)`)
	mikanInfoBlock   = regexp.MustCompile(`(?s)<p class="bangumi-info">(.*?)</p>`)
	mikanSubgroup    = regexp.MustCompile(`<a[^>]*subgroup-name[^>]*>([^<]*)</a>`)
	mikanAnchor      = regexp.MustCompile(`data-anchor="#(\d+)"`)
)

// extractBgmID 优先从「作品信息块」里取 bgm 链接，取不到再退回全文第一个。
// 详情页里会有推荐位/侧边栏的 bgm 链接，直接取第一个可能拿到别的作品。
func extractBgmID(html string) string {
	for _, block := range mikanInfoBlock.FindAllStringSubmatch(html, -1) {
		if match := mikanBgmLink.FindStringSubmatch(block[1]); match != nil {
			return match[1]
		}
	}
	if match := mikanBgmLink.FindStringSubmatch(html); match != nil {
		return match[1]
	}
	return ""
}

// Groups 找到该番剧在 Mikan 上的字幕组。
func (m *Mikan) Groups(ctx context.Context, q Query) ([]Candidate, error) {
	bangumiID := q.MikanID
	if bangumiID == "" {
		var err error
		bangumiID, err = m.resolveBangumiID(ctx, q)
		if err != nil {
			return nil, err
		}
	}
	if bangumiID == "" {
		return nil, errf("mikan 未找到匹配「%s」的番剧页", firstNonEmpty(q.Title, q.JPTitle, q.BgmID))
	}

	detailURL := fmt.Sprintf("%s/Home/Bangumi/%s", MikanHost, bangumiID)
	body, status, err := getText(ctx, m.client, detailURL)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, errf("mikan 详情页返回 HTTP %d", status)
	}
	html := string(body)

	bgmURL := ""
	if id := extractBgmID(html); id != "" {
		bgmURL = "https://bgm.tv/subject/" + id
	}

	var candidates []Candidate
	for _, match := range mikanSubgroup.FindAllStringSubmatch(html, -1) {
		anchor := mikanAnchor.FindStringSubmatch(match[0])
		if anchor == nil {
			continue
		}
		name := clean(match[1])
		if name == "" {
			continue
		}
		groupID := anchor[1]
		candidates = append(candidates, Candidate{
			Site:     "mikan",
			Label:    name,
			Subgroup: name,
			Type:     "mikan",
			BgmURL:   bgmURL,
			URL: fmt.Sprintf("%s/RSS/Bangumi?bangumiId=%s&subgroupid=%s",
				MikanHost, url.QueryEscape(bangumiID), url.QueryEscape(groupID)),
		})
	}
	return dedupe(candidates), nil
}

// resolveBangumiID 用标题搜索，再并发抓候选详情页核对 bgm 链接。
// 串行抓详情页会撞上总超时，所以这里并发 + 每个候选单独限时。
func (m *Mikan) resolveBangumiID(ctx context.Context, q Query) (string, error) {
	keyword := CleanKeyword(firstNonEmpty(q.MikanTitle, q.Title, q.JPTitle))
	if keyword == "" {
		return "", nil
	}
	searchURL := MikanHost + "/Home/Search?searchstr=" + url.QueryEscape(keyword)
	searchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	body, status, err := getText(searchCtx, m.client, searchURL)
	if err != nil {
		return "", fmt.Errorf("mikan 搜索「%s」失败: %w", keyword, err)
	}
	if status != http.StatusOK {
		return "", errf("mikan 搜索返回 HTTP %d", status)
	}

	seen := map[string]bool{}
	var ids []string
	for _, match := range mikanBangumiLink.FindAllStringSubmatch(string(body), -1) {
		id := match[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) >= mikanMaxCandidates {
			break
		}
	}
	if len(ids) == 0 {
		return "", nil
	}
	// 没有 bgmId 时无法核对，直接取搜索结果第一个
	if q.BgmID == "" {
		return ids[0], nil
	}

	type result struct {
		id  string
		bgm string
		err error
	}
	results := make([]result, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(index int, bangumiID string) {
			defer wg.Done()
			detailCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			detail, status, err := getText(detailCtx, m.client, fmt.Sprintf("%s/Home/Bangumi/%s", MikanHost, bangumiID))
			if err != nil {
				results[index] = result{id: bangumiID, err: err}
				return
			}
			if status != http.StatusOK {
				results[index] = result{id: bangumiID, err: errf("HTTP %d", status)}
				return
			}
			match := extractBgmID(string(detail))
			if match == "" {
				results[index] = result{id: bangumiID}
				return
			}
			results[index] = result{id: bangumiID, bgm: match}
		}(i, id)
	}
	wg.Wait()

	var firstErr error
	for _, item := range results {
		if item.err != nil && firstErr == nil {
			firstErr = item.err
		}
		if item.bgm == q.BgmID {
			return item.id, nil
		}
	}
	if firstErr != nil {
		return "", fmt.Errorf("mikan 核对详情页失败（关键词「%s」）: %w", keyword, firstErr)
	}
	return "", nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
