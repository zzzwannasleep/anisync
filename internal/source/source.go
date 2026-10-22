// Package source 是站点适配层：把「一个番剧」翻译成「各站点的字幕组/放流组 RSS 候选」。
//
// AniBT / AnimeGarden / Mikan 三家不自己爬网页，而是直接调用 ANI-RSS 实例的
// aniBTGroup / animeGardenGroup / mikanGroup 接口——实例怎么认这个站点，我们就怎么认，
// 拿到的是它自己拼好的 RSS 地址，不会因为站点改版而各写一份解析。
// Nyaa 没有对应的 ASS 接口（ANI-RSS 本身也不集成 Nyaa），由本包按关键字搜索 RSS 自行适配。
package source

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/cache"
)

// Query 描述要查找的番剧。
type Query struct {
	BgmID      string // bgm.tv 条目 id，最可靠的锚点
	MikanID    string // 已知的 mikan bangumi id（可从订阅 URL 里拿到）
	Title      string // 中文标题，用于搜索
	JPTitle    string // 日文标题，用于搜索
	NyaaQuery  string // 给 nyaa 用的搜索词
	MikanTitle string
	RSSURL     string // 订阅当前的 RSS 地址
}

// Candidate 是一个可用的 RSS 候选。
type Candidate struct {
	Site     string `json:"site"`
	Label    string `json:"label"`
	URL      string `json:"url"`
	Type     string `json:"type"`
	BgmURL   string `json:"bgmUrl,omitempty"`
	Subgroup string `json:"subgroup,omitempty"`

	// Titles 是该组最近的资源标题。字幕类型（简日/繁日/内嵌/内封）只写在标题里，
	// 规则引擎靠它来排「简日内嵌 > 繁日内嵌 > 简繁日内封」这类优先级。
	Titles []string `json:"-"`

	// 以下字段由规则引擎填充
	Priority        int    `json:"priority"`
	StandbyPriority int    `json:"standbyPriority"`
	SiteOrder       int    `json:"siteOrder"`
	RuleName        string `json:"ruleName,omitempty"`
	StandbyRuleName string `json:"standbyRuleName,omitempty"`
	// Weight 是站点给出的「热度」（最近的发布条数），用于同优先级下的兜底排序。
	Weight int `json:"weight,omitempty"`
}

// Source 是站点适配器。
type Source interface {
	Name() string
	Groups(ctx context.Context, q Query) ([]Candidate, error)
}

// SiteType 把站点标识映射成 ANI-RSS 的 Ani.type 取值。
func SiteType(site string) string {
	switch strings.ToLower(site) {
	case "anibt":
		return "ani-bt"
	case "animegarden":
		return "anime-garden"
	case "mikan":
		return "mikan"
	default:
		return "other"
	}
}

// Registry 按配置的优先级顺序调度各站点。
type Registry struct {
	timeout time.Duration
	order   []string
	sources map[string]Source
}

// NewRegistry 构建注册表；order 为站点优先级（从高到低）。
// site 是借用站点数据的 ANI-RSS 实例，为 nil 时仅 Nyaa 可用。
func NewRegistry(site *assapi.Client, httpClient *http.Client, timeout time.Duration, order []string, store *cache.Store) *Registry {
	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	all := map[string]Source{
		"anibt":       &assSource{site: "anibt", client: site, store: store},
		"animegarden": &assSource{site: "animegarden", client: site, store: store},
		"mikan":       &assSource{site: "mikan", client: site, store: store},
		"nyaa":        &Nyaa{client: httpClient},
	}
	r := &Registry{timeout: timeout, sources: all}
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

// clean 去掉多余空白。
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

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

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
