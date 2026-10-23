// Package bgm 按关键字搜 Bangumi（bgm.tv）的动画条目。
//
// 设置页要的是「搜一部番剧，从卡片里挑一条」，而不是让用户自己去别处
// 复制一条链接再贴回来。本包只做搜索这一件事：不缓存、不鉴权、不写配置，
// 缓存与代理交给调用方（WebUI 走它自己那份磁盘缓存）。
//
// 用的是 search/subject 这个老接口，不是 /v0/search/subjects：
// 后者对匿名调用回 HTTP 400，前者免鉴权即可用。
package bgm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zzzwannasleep/anisync/internal/httpx"
)

// API 是 Bangumi 的接口根地址。
//
// 声明成变量是为了测试能把它指向 httptest.Server；发现功能里那份
// internal/discover.BangumiAPI 是不导出的，这里不共用、也不去改它。
var API = "https://api.bgm.tv"

// userAgent 是给 Bangumi 用的 UA。
//
// 它要求调用方自报家门，用浏览器 UA 可能被限流，所以必须显式覆盖
// httpx.Get 默认塞进去的那条 Chrome UA。
const userAgent = "zzzwannasleep/anisync (https://github.com/zzzwannasleep/anisync)"

// 搜索条数的默认值与上限。上限按 Bangumi 自己的 max_results 语义取 20。
const (
	DefaultLimit = 8
	MaxLimit     = 20
)

// Subject 是一条 Bangumi 条目。
type Subject struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`   // 原名（日文）
	NameCN   string  `json:"nameCn"` // 中文名，可能为空
	AirDate  string  `json:"airDate"`
	Summary  string  `json:"summary"`
	Cover    string  `json:"cover"` // 已规范化成 https
	Score    float64 `json:"score"` // 0 表示无人评分
	Rank     int     `json:"rank"`  // 0 表示未上榜
	EpsCount int     `json:"epsCount"`
}

// 失败原因分类。WebUI 靠它区分「上游挂了」和「上游返回的东西看不懂」。
const (
	KindRequest = "request" // 请求根本没发出去 / 没收到响应
	KindStatus  = "status"  // 上游返回非 200
	KindDecode  = "decode"  // 响应体不是预期的 JSON
)

// Error 是搜索失败的原因。
//
// 消息刻意写得简短（「上游返回 HTTP 500」），完整语境由调用方补：
// 设置页会把它拼成「Bangumi 搜索失败: 上游返回 HTTP 500」。
type Error struct {
	Kind   string
	Status int // KindStatus 时的上游状态码
	Err    error
}

func (e *Error) Error() string {
	switch e.Kind {
	case KindStatus:
		return fmt.Sprintf("上游返回 HTTP %d", e.Status)
	case KindDecode:
		return "响应解析失败: " + e.Err.Error()
	default:
		return "请求失败: " + e.Err.Error()
	}
}

// Unwrap 让 errors.Is / errors.As 能继续往下钻。
func (e *Error) Unwrap() error { return e.Err }

// ClampLimit 把条数收敛到 [1, MaxLimit]；limit <= 0 用 DefaultLimit。
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// Search 按关键字搜动画条目。
//
// keyword 为空（或只有空白）时返回空切片，且不发任何请求。
// limit <= 0 用 DefaultLimit，超过 MaxLimit 收敛到 MaxLimit。
func Search(ctx context.Context, client *http.Client, keyword string, limit int) ([]Subject, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []Subject{}, nil
	}
	limit = ClampLimit(limit)

	query := url.Values{
		"type":          {"2"}, // 2 = 动画
		"responseGroup": {"large"},
		"max_results":   {strconv.Itoa(limit)},
	}
	endpoint := API + "/search/subject/" + url.PathEscape(keyword) + "?" + query.Encode()

	// httpx.Get 会先塞一条浏览器 UA；这里用 headers 覆盖成 Bangumi 要的自报家门 UA。
	body, status, err := httpx.Get(ctx, client, endpoint, map[string]string{"User-Agent": userAgent})
	if err != nil {
		return nil, &Error{Kind: KindRequest, Err: err}
	}
	if status != http.StatusOK {
		return nil, &Error{Kind: KindStatus, Status: status}
	}

	var parsed searchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, &Error{Kind: KindDecode, Err: err}
	}

	out := make([]Subject, 0, len(parsed.List))
	for _, raw := range parsed.List {
		// id 或原名缺失的条目在设置页里既选不中也认不出，直接丢掉。
		if raw.ID == 0 || strings.TrimSpace(raw.Name) == "" {
			continue
		}
		out = append(out, raw.subject())
	}
	return out, nil
}

// searchResponse 是 search/subject 的响应外形。
type searchResponse struct {
	Results int          `json:"results"`
	List    []rawSubject `json:"list"`
}

type rawSubject struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	NameCN   string `json:"name_cn"`
	AirDate  string `json:"air_date"`
	EpsCount int    `json:"eps_count"`
	Rank     int    `json:"rank"`
	Images   struct {
		Large  string `json:"large"`
		Common string `json:"common"`
		Medium string `json:"medium"`
		Small  string `json:"small"`
		Grid   string `json:"grid"`
	} `json:"images"`
	Rating struct {
		Total int     `json:"total"`
		Score float64 `json:"score"`
	} `json:"rating"`
	Summary string `json:"summary"`
}

// subject 把上游条目转成给设置页用的形状。
func (r rawSubject) subject() Subject {
	return Subject{
		ID:       strconv.Itoa(r.ID),
		Name:     strings.TrimSpace(r.Name),
		NameCN:   strings.TrimSpace(r.NameCN),
		AirDate:  strings.TrimSpace(r.AirDate),
		Summary:  flatten(r.Summary),
		Cover:    NormalizeCover(r.coverURL()),
		Score:    r.Rating.Score,
		Rank:     r.Rank,
		EpsCount: r.EpsCount,
	}
}

// coverURL 取第一个非空的封面地址；上游给的是五档尺寸，缺档时往下退。
func (r rawSubject) coverURL() string {
	for _, candidate := range []string{
		r.Images.Large, r.Images.Common, r.Images.Medium, r.Images.Small, r.Images.Grid,
	} {
		if strings.TrimSpace(candidate) != "" {
			return strings.TrimSpace(candidate)
		}
	}
	return ""
}

// NormalizeCover 把封面地址统一成 https。
//
// 导出是因为 discover 包也要用：Bangumi 放送表返回的封面同样是 http 原链，
// 两个包对同一个 CDN 必须写出同一种规范，否则设置页里一半封面能显示一半不能。
//
// 上游返回的是明文 http://，而 lain.bgm.tv 会 301 到 https；
// 设置页的 <img> 若先走一次明文请求，浏览器可能按混合内容拦掉。
//
// 大小写不敏感：scheme 按 RFC 3986 本来就不区分大小写，上游哪天回一个
// HTTP:// 不该就漏掉这次规范化。
func NormalizeCover(raw string) string {
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "http://") {
		return "https://" + raw[7:]
	}
	return raw
}

// flatten 把简介里的换行与连续空白压成单个空格。
//
// 只压空白，不截断：截多短是视觉问题，交给前端按卡片宽度钳制。
func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
