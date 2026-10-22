// Package source 是站点适配层：把「一个番剧」翻译成「各站点的字幕组/放送组 RSS 候选」。
// 所有接口路径都对照 ani-rss v3.2.52 源码与线上真实响应实现，不做猜测。
package source

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Query 描述要查找的番剧。
type Query struct {
	BgmID      string // bgm.tv 条目 id，最可靠的锚点
	MikanID    string // 已知的 mikan bangumi id（可从订阅 URL 里拿到）
	Title      string // 中文标题，用于搜索
	JPTitle    string // 日文标题，用于搜索
	NyaaQuery  string // 给 nyaa 用的搜索词
	MikanTitle string
}

// Candidate 是一个可用的 RSS 候选。
type Candidate struct {
	Site     string `json:"site"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	BgmURL   string `json:"bgmUrl,omitempty"`
	Subgroup string `json:"subgroup,omitempty"`

	// 以下字段由规则引擎填充
	Priority  int    `json:"priority"`
	SiteOrder int    `json:"siteOrder"`
	RuleName  string `json:"ruleName,omitempty"`
	// Weight 是站点给出的「热度」（发布数/资源数），用于同优先级下的兜底排序。
	Weight int `json:"weight,omitempty"`
}

// Source 是站点适配器。
type Source interface {
	Name() string
	Groups(ctx context.Context, q Query) ([]Candidate, error)
}

// Registry 按配置的优先级顺序调度各站点。
type Registry struct {
	client  *http.Client
	timeout time.Duration
	order   []string
	sources map[string]Source
}

// NewRegistry 构建注册表；order 为站点优先级（从高到低）。
func NewRegistry(client *http.Client, timeout time.Duration, order []string) *Registry {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	all := map[string]Source{
		"anibt":       &AniBT{client: client},
		"animegarden": &AnimeGarden{client: client},
		"mikan":       &Mikan{client: client},
		"nyaa":        &Nyaa{client: client},
	}
	r := &Registry{client: client, timeout: timeout, sources: all}
	seen := map[string]bool{}
	for _, name := range order {
		name = strings.ToLower(strings.TrimSpace(name))
		if _, ok := all[name]; ok && !seen[name] {
			r.order = append(r.order, name)
			seen[name] = true
		}
	}
	if len(r.order) == 0 {
		r.order = []string{"anibt", "animegarden", "mikan", "nyaa"}
	}
	return r
}

// Order 返回生效的站点顺序。
func (r *Registry) Order() []string { return r.order }

// SiteResult 记录单个站点的爬取结果，便于在计划里展示「为什么没有候选」。
type SiteResult struct {
	Site       string
	Candidates []Candidate
	Err        error
	Skipped    bool
}

// Collect 按优先级顺序爬取；stopOnFirst 为 true 时命中即停。
func (r *Registry) Collect(ctx context.Context, q Query, stopOnFirst bool) []SiteResult {
	results := make([]SiteResult, 0, len(r.order))
	for index, name := range r.order {
		src := r.sources[name]
		itemCtx, cancel := context.WithTimeout(ctx, r.timeout)
		candidates, err := src.Groups(itemCtx, q)
		cancel()

		for i := range candidates {
			candidates[i].SiteOrder = index
			if candidates[i].Site == "" {
				candidates[i].Site = name
			}
		}
		results = append(results, SiteResult{Site: name, Candidates: candidates, Err: err})
		if stopOnFirst && len(candidates) > 0 {
			break
		}
	}
	return results
}

// AllCandidates 汇总所有站点的候选。
func AllCandidates(results []SiteResult) []Candidate {
	var out []Candidate
	for _, r := range results {
		out = append(out, r.Candidates...)
	}
	return out
}

// bgmSubjectPattern 从任意 URL 中取 bgm 条目 id。
var bgmSubjectPattern = regexp.MustCompile(`(?:bgm\.tv|bangumi\.tv|chii\.in)/subject/(\d+)`)

// BgmIDFromURL 从 bgmUrl 里取条目 id，取不到返回空串。
func BgmIDFromURL(raw string) string {
	if m := bgmSubjectPattern.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return ""
}

// BgmIDFromAny 与 BgmIDFromURL 等价，语义上表示「任意 URL」。
func BgmIDFromAny(raw string) string { return BgmIDFromURL(raw) }

// clean 去掉多余空白。
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

var (
	keywordBrackets = regexp.MustCompile(`[\[【][^\]】]*[\]】]`)
	keywordParens   = regexp.MustCompile(`[（(][^)）]*[)）]`)
	keywordNoise    = regexp.MustCompile(`(?i)\b(tmdbid|tmdb|bgmid|subject)\s*=\s*\w+`)
	keywordSpaces   = regexp.MustCompile(`\s{2,}`)
)

// CleanKeyword 把 ANI-RSS 的标题洗成能拿去搜索的关键词。
// 例如 "铁旅！邂逅铁道少女 (2026) [tmdbid=302020]" -> "铁旅！邂逅铁道少女"。
func CleanKeyword(title string) string {
	title = keywordNoise.ReplaceAllString(title, " ")
	title = keywordBrackets.ReplaceAllString(title, " ")
	title = keywordParens.ReplaceAllString(title, " ")
	title = strings.NewReplacer("【", " ", "】", " ", "[", " ", "]", " ").Replace(title)
	title = keywordSpaces.ReplaceAllString(title, " ")
	return strings.TrimSpace(title)
}

// dedupe 按 URL 去重，保留先出现的（即优先级更高的站点）。
func dedupe(list []Candidate) []Candidate {
	seen := map[string]bool{}
	out := list[:0]
	for _, c := range list {
		if c.URL == "" || seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		out = append(out, c)
	}
	return out
}

// escapeQuery 把搜索词编码进 query。
func escapeQuery(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), " ", "+")
}

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
