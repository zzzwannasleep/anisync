// Package assapi 是 ANI-RSS（ASS）的 API 客户端。
// 契约来自 ani-rss v3.2.52 源码：controller/*.java、auth/fun/ApiKey.java。
// 鉴权：请求头 api-key（也接受 x-api-key / s）。
// 响应：{"code":200,"message":"...","data":...,"t":...}
package assapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/httpx"
)

// Client 绑定一个实例。
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// New 创建客户端。
func New(baseURL, apiKey, proxy string, timeout time.Duration) (*Client, error) {
	httpClient, err := httpx.New(proxy, timeout)
	if err != nil {
		return nil, err
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/") + "/", APIKey: apiKey, HTTP: httpClient}, nil
}

// Result 是统一响应包。
type Result struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// APIError 表示服务端返回了非 200 业务码。
type APIError struct {
	HTTP    int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("ANI-RSS 返回错误（HTTP %d，业务码 %d）：%s", e.HTTP, e.Code, e.Message)
	}
	return fmt.Sprintf("ANI-RSS 返回错误（HTTP %d，业务码 %d）", e.HTTP, e.Code)
}

// Post 调用一个接口。body 为 nil 时不带请求体。
func (c *Client) Post(ctx context.Context, endpoint string, query url.Values, body any) (*Result, error) {
	u := c.BaseURL + "api/" + endpoint
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api-key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", httpx.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 %s 失败: %w", c.BaseURL, err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}

	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, &APIError{HTTP: res.StatusCode, Message: fmt.Sprintf("返回的不是 JSON（可能是登录页或反向代理错误）: %.120s", string(raw))}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || result.Code != 200 {
		return &result, &APIError{HTTP: res.StatusCode, Code: result.Code, Message: result.Message}
	}
	return &result, nil
}

// Ani 用 map 承载，避免漏字段：setAni 是全量覆盖语义，必须把原对象改完再整份回传。
type Ani = map[string]any

// Group 是字幕组/放流组。
// 三个站点接口返回的字段名各不相同（mikan 用 label，anibt/animegarden 用 name），
// 这里统一收口；Items 里的标题会参与规则匹配，用来排「简日内嵌 / 繁日内封」这类字幕类型优先级。
type Group struct {
	Label  string      `json:"label"`
	Name   string      `json:"name"`
	Slug   string      `json:"slug"`
	RSS    string      `json:"rss"`
	BgmURL string      `json:"bgmUrl"`
	Items  []GroupItem `json:"items"`
	// Total 是该组的资源总数。缓存只保留少量标题样本，热度要靠它才不丢。
	Total int `json:"total,omitempty"`
}

// GroupItem 是字幕组下的一条资源，只取标题。
type GroupItem struct {
	Title    string   `json:"title"`
	Language []string `json:"language"`
}

// DisplayName 返回可读名字。
func (g Group) DisplayName() string {
	if g.Label != "" {
		return g.Label
	}
	return g.Name
}

// MikanItem 是 mikan 搜索结果里的番剧条目。
type MikanItem struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	BgmID string `json:"bgmId"`
}

// About 测试连接。
func (c *Client) About(ctx context.Context) (map[string]any, error) {
	result, err := c.Post(ctx, "about", nil, nil)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if len(result.Data) > 0 {
		_ = json.Unmarshal(result.Data, &data)
	}
	return data, nil
}

// ListAni 拉取全部订阅（扁平化 weekList）。
func (c *Client) ListAni(ctx context.Context) ([]Ani, int, error) {
	result, err := c.Post(ctx, "listAni", nil, nil)
	if err != nil {
		return nil, 0, err
	}
	var data struct {
		WeekList []struct {
			Items []Ani `json:"items"`
		} `json:"weekList"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return nil, 0, fmt.Errorf("listAni 响应解析失败: %w", err)
	}
	items := make([]Ani, 0, data.Total)
	for _, week := range data.WeekList {
		items = append(items, week.Items...)
	}
	return items, data.Total, nil
}

// SetAni 修改订阅（全量覆盖，必须传完整对象）。
func (c *Client) SetAni(ctx context.Context, ani Ani) error {
	_, err := c.Post(ctx, "setAni", nil, ani)
	return err
}

// AddAni 添加订阅；override=true 时覆盖同名订阅。
func (c *Client) AddAni(ctx context.Context, ani Ani, override bool) error {
	query := url.Values{}
	query.Set("override", fmt.Sprint(override))
	_, err := c.Post(ctx, "addAni", query, ani)
	return err
}

// DeleteAni 删除订阅；deleteFiles=false 时只删订阅与种子缓存，不碰下载器里的任务。
func (c *Client) DeleteAni(ctx context.Context, ids []string, deleteFiles bool) error {
	query := url.Values{}
	query.Set("deleteFiles", fmt.Sprint(deleteFiles))
	_, err := c.Post(ctx, "deleteAni", query, ids)
	return err
}

// RssToAni 把 RSS 解析成订阅草稿。
func (c *Client) RssToAni(ctx context.Context, body map[string]any) (Ani, error) {
	result, err := c.Post(ctx, "rssToAni", nil, body)
	if err != nil {
		return nil, err
	}
	var ani Ani
	if err := json.Unmarshal(result.Data, &ani); err != nil {
		return nil, fmt.Errorf("rssToAni 响应解析失败: %w", err)
	}
	return ani, nil
}

// MikanGroup 由实例侧解析 Mikan 番剧页，拿到字幕组列表。
func (c *Client) MikanGroup(ctx context.Context, mikanURL string) ([]Group, error) {
	query := url.Values{}
	query.Set("url", mikanURL)
	return c.groups(ctx, "mikanGroup", query)
}

// AniBTGroup 取 AniBT 字幕组。
func (c *Client) AniBTGroup(ctx context.Context, bgmID string) ([]Group, error) {
	query := url.Values{}
	query.Set("bgmId", bgmID)
	return c.groups(ctx, "aniBTGroup", query)
}

// AnimeGardenGroup 取 AnimeGarden 字幕组。
func (c *Client) AnimeGardenGroup(ctx context.Context, bgmID string) ([]Group, error) {
	query := url.Values{}
	query.Set("bgmId", bgmID)
	return c.groups(ctx, "animeGardenGroup", query)
}

func (c *Client) groups(ctx context.Context, endpoint string, query url.Values) ([]Group, error) {
	result, err := c.Post(ctx, endpoint, query, nil)
	if err != nil {
		return nil, err
	}
	var groups []Group
	if err := json.Unmarshal(result.Data, &groups); err != nil {
		return nil, fmt.Errorf("%s 响应解析失败: %w", endpoint, err)
	}
	return groups, nil
}

// MikanSearch 按标题搜索 mikan 番剧，返回带番剧页地址与 bgmId 的条目。
// 用来在订阅不是 mikan 来源时，反查出 mikan 番剧页（mikanGroup 需要的是页面地址）。
func (c *Client) MikanSearch(ctx context.Context, text string) ([]MikanItem, error) {
	query := url.Values{}
	query.Set("text", text)
	result, err := c.Post(ctx, "mikan", query, map[string]any{})
	if err != nil {
		return nil, err
	}
	var data struct {
		Weeks []struct {
			Items []MikanItem `json:"items"`
		} `json:"weeks"`
	}
	if err := json.Unmarshal(result.Data, &data); err != nil {
		return nil, fmt.Errorf("mikan 搜索响应解析失败: %w", err)
	}
	items := make([]MikanItem, 0, len(data.Weeks))
	for _, week := range data.Weeks {
		items = append(items, week.Items...)
	}
	return items, nil
}
