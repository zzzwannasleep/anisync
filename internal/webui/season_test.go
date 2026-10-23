package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
)

// 蓝军验证 C1：/api/season 在畸形 JSON / 非 200 / 超大响应下必须给出可读错误，而不是挂住或 OOM。
func TestSeasonUpstreamFailures(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"畸形 JSON", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"weekday": [ this is not json`))
		}},
		{"非 200", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
		}},
		{"429 限流", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
		}},
		{"HTML 登录页", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body>login</body></html>"))
		}},
		{"超大响应（20MiB）", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			chunk := strings.Repeat("A", 1<<20)
			for i := 0; i < 20; i++ {
				if _, err := w.Write([]byte(chunk)); err != nil {
					return
				}
			}
		}},
		{"空响应体", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(tc.handler)
			defer upstream.Close()
			old := discover.BangumiAPI
			discover.BangumiAPI = upstream.URL
			defer func() { discover.BangumiAPI = old }()

			t.Chdir(t.TempDir())
			t.Setenv("ANISYNC_PROXY", "")
			cfg := config.Default()
			cfg.Proxy = ""
			// 这些用例测的是放送表那条路，所以把来源钉死。默认来源是 yuc，
			// 不钉的话它们会去请求真的 raw.githubusercontent.com。
			cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
			srv := New(cfg)
			rec := httptest.NewRecorder()
			srv.handleSeason(rec, httptest.NewRequest(http.MethodGet, "/api/season", nil))
			t.Logf("状态码 %d body=%.200s", rec.Code, rec.Body.String())
			if rec.Code == http.StatusOK {
				t.Fatalf("上游坏了不该回 200：%s", rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "error") {
				t.Errorf("错误响应里没有 error 字段: %s", rec.Body.String())
			}
		})
	}
}

// cfg.Sources.CacheMinutes 对 /api/season 到底有没有生效。
//
// 只数放送表那一次请求。地区过滤会顺带按 bgmId 打 /v0/subjects/{id}，
// 那是另一条缓存线（逐条缓存），混在一起数就分不清是哪个缓存没生效了。
func TestSeasonCacheMinutesIsEffective(t *testing.T) {
	var calendarHits, subjectHits int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v0/subjects/") {
			atomic.AddInt64(&subjectHits, 1)
			_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","日本"]}`))
			return
		}
		atomic.AddInt64(&calendarHits, 1)
		_, _ = w.Write([]byte(`[{"weekday":{"id":1},"items":[{"id":1,"name":"x","name_cn":"x"}]}]`))
	}))
	defer upstream.Close()
	old := discover.BangumiAPI
	discover.BangumiAPI = upstream.URL
	defer func() { discover.BangumiAPI = old }()

	t.Chdir(t.TempDir())
	t.Setenv("ANISYNC_PROXY", "")
	cfg := config.Default() // cacheMinutes = 720
	cfg.Proxy = ""
	// 这些用例测的是放送表那条路，所以把来源钉死。默认来源是 yuc，
	// 不钉的话它们会去请求真的 raw.githubusercontent.com。
	cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
	srv := New(cfg)
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		srv.handleSeason(rec, httptest.NewRequest(http.MethodGet, "/api/season", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次请求失败: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	t.Logf("3 次 /api/season：放送表 %d 次、条目详情 %d 次（cacheMinutes=720，各期望 1 次）", calendarHits, subjectHits)
	if calendarHits != 1 {
		t.Errorf("放送表缓存没生效：3 次请求打了上游 %d 次", calendarHits)
	}
	if subjectHits != 1 {
		t.Errorf("条目详情缓存没生效：3 次请求打了上游 %d 次", subjectHits)
	}
}

// 封面地址规范化：http 原链要升级成 https，其余原样返回。
//
// 大小写不敏感 —— scheme 按 RFC 3986 本来就不区分大小写，上游回一个 HTTP://
// 不该就漏掉这次规范化。
func TestCoverNormalization(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://lain.bgm.tv/pic/cover/c/1.jpg", "https://lain.bgm.tv/pic/cover/c/1.jpg"},
		{"HTTP://lain.bgm.tv/pic/cover/c/1.jpg", "https://lain.bgm.tv/pic/cover/c/1.jpg"},
		{"HtTp://lain.bgm.tv/pic/cover/c/1.jpg", "https://lain.bgm.tv/pic/cover/c/1.jpg"},
		{"https://lain.bgm.tv/pic/cover/c/1.jpg", "https://lain.bgm.tv/pic/cover/c/1.jpg"},
		{"//lain.bgm.tv/pic/cover/c/1.jpg", "//lain.bgm.tv/pic/cover/c/1.jpg"}, // 协议相对，不猜
		{"ftp://lain.bgm.tv/1.jpg", "ftp://lain.bgm.tv/1.jpg"},
		{"", ""},
		{"http://", "https://"}, // 退化输入也不该 panic
	}
	for _, tc := range cases {
		got := bgm.NormalizeCover(tc.in)
		t.Logf("NormalizeCover(%q) = %q", tc.in, got)
		if got != tc.want {
			t.Errorf("NormalizeCover(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 非 http(s) 的封面地址必须在拨号之前就被封面代理挡掉。
//
// 注意「大写协议头」不是威胁：Go 的 url.Parse 会把 scheme 归一成小写，
// 主机名仍是白名单里的 lain.bgm.tv，放行它是对的 —— 白名单管的是主机，不是写法。
// 真正要挡住的是协议相对（scheme 为空）和非 http(s) 协议。
func TestCoverProxyRejectsNonHTTPSchemes(t *testing.T) {
	srv := newTestServer(t)
	for _, raw := range []string{"//lain.bgm.tv/x.jpg", "ftp://lain.bgm.tv/x.jpg", "file:///etc/passwd", "lain.bgm.tv/x.jpg"} {
		rec := httptest.NewRecorder()
		srv.handleBgmCover(rec, httptest.NewRequest(http.MethodGet, "/api/bgm/cover?u="+url.QueryEscape(raw), nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q 应被挡在拨号之前（400），实际 %d", raw, rec.Code)
		}
	}
}

// 蓝军验证 C4：/api/season 返回的封面即使被上游换成白名单外的地址，
// 也只能让 <img> 拿到 400，不会让服务端去访问它。
func TestSeasonCoverStaysBehindWhitelist(t *testing.T) {
	var trapHits int64
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&trapHits, 1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer trap.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload, _ := json.Marshal([]map[string]any{{
			"weekday": map[string]any{"id": 1},
			"items": []map[string]any{{
				"id": 42, "name": "x", "name_cn": "x",
				"images": map[string]string{"common": trap.URL + "/evil.jpg"},
			}},
		}})
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()
	old := discover.BangumiAPI
	discover.BangumiAPI = upstream.URL
	defer func() { discover.BangumiAPI = old }()

	t.Chdir(t.TempDir())
	t.Setenv("ANISYNC_PROXY", "")
	cfg := config.Default()
	cfg.Proxy = ""
	// 这些用例测的是放送表那条路，所以把来源钉死。默认来源是 yuc，
	// 不钉的话它们会去请求真的 raw.githubusercontent.com。
	cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
	srv := New(cfg)
	rec := httptest.NewRecorder()
	srv.handleSeason(rec, httptest.NewRequest(http.MethodGet, "/api/season", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("season 应回 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			Cover string `json:"cover"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(got.Items))
	}
	t.Logf("上游塞进来的封面: %q", got.Items[0].Cover)

	// 前端会把它拼成 /api/bgm/cover?u=...
	coverRec := httptest.NewRecorder()
	srv.handleBgmCover(coverRec, httptest.NewRequest(http.MethodGet, "/api/bgm/cover?u="+url.QueryEscape(got.Items[0].Cover), nil))
	t.Logf("封面代理状态码 %d body=%s", coverRec.Code, coverRec.Body.String())
	if coverRec.Code != http.StatusBadRequest {
		t.Errorf("白名单外的封面应被拒，实际 %d", coverRec.Code)
	}
	if trapHits != 0 {
		t.Errorf("陷阱服务器被访问了 %d 次，SSRF 面被打开", trapHits)
	}
}

// 本季新番列表要同时尊重两件事：标题黑名单，以及「已添加」的标题匹配。
//
// 黑名单：面板上写着「命中的不列出来也不自动加」，不把 exclude 传下去的话，
// 用户能用「全选」把黑名单里的番一次性灌进 targets。
// 标题匹配：只写 title、没写 bgmId 的手写 target 也算已添加，否则用户会给
// 同一部番再加一条带 bgmId 的。
func TestSeasonRouteAppliesExcludeAndTitleMatch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
		  {"weekday":{"id":1},"items":[
		    {"id":1,"name":"A","name_cn":"被黑名单挡住的番","collection":{"doing":900}},
		    {"id":2,"name":"B","name_cn":"手写标题命中","collection":{"doing":800}},
		    {"id":3,"name":"C","name_cn":"没加过的番","collection":{"doing":700}}
		  ]}
		]`))
	}))
	defer upstream.Close()

	old := discover.BangumiAPI
	discover.BangumiAPI = upstream.URL
	defer func() { discover.BangumiAPI = old }()

	t.Chdir(t.TempDir())
	t.Setenv("ANISYNC_PROXY", "")

	cfg := config.Default()
	cfg.Proxy = ""
	// 这些用例测的是放送表那条路，所以把来源钉死。默认来源是 yuc，
	// 不钉的话它们会去请求真的 raw.githubusercontent.com。
	cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
	cfg.Discover.Exclude = []string{"黑名单"}
	// 只有 title，没有 bgmId —— 老配置里很常见，planner 照样能按标题包含匹配命中。
	cfg.Targets = []config.Target{{Title: "手写标题命中"}}
	srv := New(cfg)

	rec := httptest.NewRecorder()
	srv.handleSeason(rec, httptest.NewRequest(http.MethodGet, "/api/season", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("应回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Total int `json:"total"`
		Added int `json:"added"`
		Items []struct {
			BgmID string `json:"bgmId"`
			Title string `json:"title"`
			Added bool   `json:"added"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}

	for _, it := range got.Items {
		if it.Title == "被黑名单挡住的番" {
			t.Error("黑名单命中的番不该出现在列表里；否则「全选」能把它灌进 targets")
		}
	}
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("黑名单应滤掉 1 条，剩 2 条，实际 total=%d len=%d", got.Total, len(got.Items))
	}
	if got.Added != 1 {
		t.Errorf("应只有 1 条标记为已添加，实际 %d", got.Added)
	}
	for _, it := range got.Items {
		if it.BgmID == "2" && !it.Added {
			t.Error("targets 里只写了 title 的番也要算已添加，否则用户会给同一部番再加一遍")
		}
		if it.BgmID == "3" && it.Added {
			t.Error("没加过的番不该标成已添加")
		}
	}
}

// 「已添加」必须问实例，不能只看配置里的 targets。
//
// 这是真实踩到的：实例里明明订了 71 部，界面显示一部都没加 ——
// 因为 targets 是用户手写的番剧级配置（规定季度与偏移），而订阅是实例里真实存在的东西，
// 用户在 ANI-RSS 里手动加的订阅根本不会出现在 targets 里。
func TestSeasonMarksAddedFromInstanceSubscriptions(t *testing.T) {
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/api/listAni") {
			http.NotFound(w, r)
			return
		}
		// 实例里订了 1 和 3，没订 2。3 只有 RSS 里的 bgmId，用来验证兜底解析。
		_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"total":2,"weekList":[
		  {"weekLabel":"一","items":[{"bgmUrl":"https://bgm.tv/subject/1"}]},
		  {"weekLabel":"二","items":[{"url":"https://x/rss?bgmId=3"}]}
		]}}`))
	}))
	defer instance.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v0/subjects/") {
			_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","日本"]}`))
			return
		}
		_, _ = w.Write([]byte(`[{"weekday":{"id":1},"items":[
		  {"id":1,"name":"a","name_cn":"已订阅一"},
		  {"id":2,"name":"b","name_cn":"没订阅"},
		  {"id":3,"name":"c","name_cn":"已订阅二"}
		]}]`))
	}))
	defer upstream.Close()

	old := discover.BangumiAPI
	discover.BangumiAPI = upstream.URL
	defer func() { discover.BangumiAPI = old }()

	t.Chdir(t.TempDir())
	t.Setenv("ANISYNC_PROXY", "")

	cfg := config.Default()
	cfg.Proxy = ""
	// 这些用例测的是放送表那条路，所以把来源钉死。默认来源是 yuc，
	// 不钉的话它们会去请求真的 raw.githubusercontent.com。
	cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
	cfg.Targets = nil // 配置里一条都没有 —— 正是用户的情况
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: instance.URL, APIKey: "k"}}
	srv := New(cfg)

	rec := httptest.NewRecorder()
	srv.handleSeason(rec, httptest.NewRequest(http.MethodGet, "/api/season", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("应回 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Added      int `json:"added"`
		Subscribed int `json:"subscribed"`
		Items      []struct {
			BgmID string `json:"bgmId"`
			Added bool   `json:"added"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Subscribed != 2 {
		t.Errorf("应报出实例里有 2 条订阅，实际 %d", got.Subscribed)
	}
	if got.Added != 2 {
		t.Errorf("应标记 2 条已添加，实际 %d", got.Added)
	}
	byID := map[string]bool{}
	for _, it := range got.Items {
		byID[it.BgmID] = it.Added
	}
	if !byID["1"] || byID["2"] || !byID["3"] {
		t.Errorf("已添加标记不对（1 和 3 应为 true、2 应为 false）：%v", byID)
	}
}

// 实例读不到时要说清楚原因，而不是让整张列表退化成「全部未添加」。
func TestSeasonReportsInstanceError(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dead.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v0/subjects/") {
			_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","日本"]}`))
			return
		}
		_, _ = w.Write([]byte(`[{"weekday":{"id":1},"items":[{"id":1,"name":"a","name_cn":"a"}]}]`))
	}))
	defer upstream.Close()

	old := discover.BangumiAPI
	discover.BangumiAPI = upstream.URL
	defer func() { discover.BangumiAPI = old }()

	t.Chdir(t.TempDir())
	t.Setenv("ANISYNC_PROXY", "")
	cfg := config.Default()
	cfg.Proxy = ""
	// 这些用例测的是放送表那条路，所以把来源钉死。默认来源是 yuc，
	// 不钉的话它们会去请求真的 raw.githubusercontent.com。
	cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
	cfg.Instances = []config.Instance{{Name: "挂了的实例", BaseURL: dead.URL, APIKey: "k"}}
	srv := New(cfg)

	rec := httptest.NewRecorder()
	srv.handleSeason(rec, httptest.NewRequest(http.MethodGet, "/api/season", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("实例挂了不该让整个接口失败，实际 %d", rec.Code)
	}
	var got struct {
		InstanceError string `json:"instanceError"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.InstanceError == "" {
		t.Error("实例读不到时必须报 instanceError，否则用户看到「一部都没加」会以为判定逻辑坏了")
	}
}
