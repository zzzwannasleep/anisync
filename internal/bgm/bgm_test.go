package bgm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// useServer 把接口根地址临时指向测试服务器。
func useServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	old := API
	API = server.URL
	t.Cleanup(func() { API = old })
}

// writeJSON 按给定状态码写一个 JSON 响应。
func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("写响应失败: %v", err)
	}
}

// 搜索结果的字段映射、封面协议规范化、简介压平，外加「id 或原名缺失就丢掉」。
func TestSearchMapsSubjects(t *testing.T) {
	var gotPath string
	var gotUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		writeJSON(t, w, http.StatusOK, map[string]any{
			"results": 4,
			"list": []map[string]any{
				{
					"id": 535669, "type": 2, "name": "氷の城壁", "name_cn": "冰之城墙",
					"air_date": "2026-04-02", "eps_count": 12, "rank": 1234,
					"images": map[string]any{
						"large":  "http://lain.bgm.tv/pic/cover/l/93/8f/535669_4zoHG.jpg",
						"common": "http://lain.bgm.tv/pic/cover/c/93/8f/535669_4zoHG.jpg",
					},
					"rating":  map[string]any{"total": 3256, "score": 7.2},
					"summary": "  第一行\n\n第二行\t带制表符  \n第三行  ",
				},
				{
					"id": 8, "type": 2, "name": "Cowboy Bebop", "name_cn": "",
					"air_date": "1998-04-03", "eps_count": 26, "rank": 1,
					"images":  map[string]any{"medium": "https://lain.bgm.tv/pic/cover/m/8.jpg"},
					"rating":  map[string]any{"total": 100, "score": 9.1},
					"summary": "单行简介",
				},
				// id 为 0：没有条目可指向，丢掉
				{"id": 0, "name": "没有 id 的"},
				// 原名为空：设置页里认不出，丢掉
				{"id": 42, "name": "", "name_cn": "只有中文名"},
			},
		})
	}))
	defer server.Close()
	useServer(t, server)

	items, err := Search(context.Background(), server.Client(), "氷の城壁", 8)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("应解析出 2 条（id 或原名缺失的要丢掉），实际 %d 条: %+v", len(items), items)
	}

	first := items[0]
	if first.ID != "535669" || first.Name != "氷の城壁" || first.NameCN != "冰之城墙" {
		t.Errorf("基本字段映射不对: %+v", first)
	}
	if first.AirDate != "2026-04-02" || first.EpsCount != 12 || first.Rank != 1234 {
		t.Errorf("日期/集数/排名映射不对: %+v", first)
	}
	if first.Score != 7.2 {
		t.Errorf("评分应为 7.2，实际 %v", first.Score)
	}
	if first.Cover != "https://lain.bgm.tv/pic/cover/l/93/8f/535669_4zoHG.jpg" {
		t.Errorf("http 封面没有被规范化成 https: %q", first.Cover)
	}
	if first.Summary != "第一行 第二行 带制表符 第三行" {
		t.Errorf("简介换行/空白没有压平: %q", first.Summary)
	}

	// 只给了 medium 档封面时也要取到，且已经是 https 就不动
	if items[1].Cover != "https://lain.bgm.tv/pic/cover/m/8.jpg" {
		t.Errorf("缺档封面没有回退到 medium: %q", items[1].Cover)
	}
	if items[1].NameCN != "" || items[1].Summary != "单行简介" {
		t.Errorf("空中文名/单行简介处理不对: %+v", items[1])
	}

	if !strings.Contains(gotPath, "氷の城壁") {
		t.Errorf("关键字没有进请求路径: %q", gotPath)
	}
	// 必须自报家门：Bangumi 会给浏览器 UA 限流
	if !strings.HasPrefix(gotUA, "zzzwannasleep/anisync") {
		t.Errorf("请求没有用 Bangumi 要求的 UA，实际 %q", gotUA)
	}
}

// limit 要收敛到 [1, 20]：999 -> 20，0（或负数）-> 默认 8。
func TestSearchClampsLimit(t *testing.T) {
	cases := []struct {
		name  string
		limit int
		want  string
	}{
		{"超上限收敛到 20", 999, "20"},
		{"零用默认 8", 0, "8"},
		{"负数用默认 8", -3, "8"},
		{"合法值原样透传", 5, "5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query().Get("max_results")
				if typ := r.URL.Query().Get("type"); typ != "2" {
					t.Errorf("type 应为 2（动画），实际 %q", typ)
				}
				writeJSON(t, w, http.StatusOK, map[string]any{"results": 0, "list": []any{}})
			}))
			defer server.Close()
			useServer(t, server)

			if _, err := Search(context.Background(), server.Client(), "测试", tc.limit); err != nil {
				t.Fatalf("搜索失败: %v", err)
			}
			if got != tc.want {
				t.Errorf("limit=%d 时 max_results 应为 %q，实际 %q", tc.limit, tc.want, got)
			}
		})
	}
}

// 关键字为空必须就地短路：一个请求都不该发出去。
func TestSearchEmptyKeywordSkipsRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("关键字为空时不该发请求，实际请求了 %s", r.URL.String())
		writeJSON(t, w, http.StatusOK, map[string]any{"results": 0, "list": []any{}})
	}))
	defer server.Close()
	useServer(t, server)

	for _, keyword := range []string{"", "   ", "\n\t "} {
		items, err := Search(context.Background(), server.Client(), keyword, 8)
		if err != nil {
			t.Fatalf("关键字 %q 应返回空结果而不是错误: %v", keyword, err)
		}
		if items == nil {
			t.Errorf("关键字 %q 应返回空切片而不是 nil", keyword)
		}
		if len(items) != 0 {
			t.Errorf("关键字 %q 应返回空切片，实际 %d 条", keyword, len(items))
		}
	}
}

// 上游非 200 要报出状态码，不能笼统地说一句「失败」。
func TestSearchUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()
	useServer(t, server)

	_, err := Search(context.Background(), server.Client(), "测试", 8)
	if err == nil {
		t.Fatal("上游 500 时应返回错误")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("错误信息应带上状态码 500，实际 %q", err.Error())
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != KindStatus || typed.Status != 500 {
		t.Errorf("应返回 Kind=status、Status=500 的 *Error，实际 %#v", err)
	}
}

// 响应体不是 JSON 时要能和「上游挂了」区分开。
func TestSearchMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("<html>这不是 JSON</html>"))
	}))
	defer server.Close()
	useServer(t, server)

	_, err := Search(context.Background(), server.Client(), "测试", 8)
	if err == nil {
		t.Fatal("响应体不是 JSON 时应返回错误")
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != KindDecode {
		t.Errorf("应返回 Kind=decode 的 *Error，实际 %#v", err)
	}
}
