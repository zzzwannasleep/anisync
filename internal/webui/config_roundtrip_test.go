package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// 设置页保存必须原样带回它不认识的那部分配置。
//
// 前端 collect() 只写它渲染得出的字段（sources.timeoutSeconds、discover.limit 等），
// 刻意不碰 sources.order / sources.instance / stopOnFirstMatch / discover.sources /
// bgmUser / days。这些键靠「整份对象回传」活着，所以这条测试盯的是：
// 老配置读进来、改一个无关字段、PUT 回去之后，上面每一个键都还在，注释也没丢。
func TestSaveKeepsLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `# 顶部注释：不能被冲掉
listen: 127.0.0.1:51837
proxy: ""
sources:
  # 站点顺序注释
  instance: main
  order:
    - animegarden
    - mikan
  stopOnFirstMatch: true
  timeoutSeconds: 25
  cacheMinutes: 720
rules:
  primary: []
  deny: []
  maxStandby: 1
instances:
  - name: main
    baseUrl: https://ass.example.com/
    apiKey: k
targets:
  - title: 老番
    bgmId: "975"
discover:
  enabled: true
  sources:
    - animegarden
  bgmUser: alice
  days: 30
  limit: 50
  exclude:
    - 合集
sync:
  intervalMinutes: 0
  preview: false
  addMissing: false
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv := New(cfg)

	// 1) GET：前端拿到的就是这份对象
	rec := httptest.NewRecorder()
	srv.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 应回 200，实际 %d", rec.Code)
	}
	var got struct {
		Path   string         `json:"path"`
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET 响应不是 JSON: %v", err)
	}
	t.Logf("GET config.sources = %v", got.Config["sources"])
	t.Logf("GET config.discover = %v", got.Config["discover"])

	// 2) 模拟设置页 collect()：只改一个无关字段（超时），其余原样回传。
	//    collect() 不写 sources.order / discover.sources / bgmUser / days，
	//    但 cfg 是同一个对象，这些键会随整个对象一起回去。
	sources := got.Config["sources"].(map[string]any)
	sources["timeoutSeconds"] = 33.0
	body, err := json.Marshal(got.Config)
	if err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	srv.handleConfig(rec2, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("PUT 应回 200，实际 %d，body=%s", rec2.Code, rec2.Body.String())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("保存后的文件:\n%s", raw)

	after, err := config.Load(path)
	if err != nil {
		t.Fatalf("重新 Load: %v", err)
	}
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"sources.order", fmt.Sprint(after.Sources.Order), "[animegarden mikan]"},
		{"sources.instance", after.Sources.Instance, "main"},
		{"sources.stopOnFirstMatch", fmt.Sprint(after.Sources.StopOnFirstMatch), "true"},
		{"sources.timeoutSeconds", fmt.Sprint(after.Sources.TimeoutSeconds), "33"},
		{"discover.sources", fmt.Sprint(after.Discover.SourceList()), "[animegarden]"},
		{"discover.bgmUser", after.Discover.BgmUser, "alice"},
		{"discover.days", fmt.Sprint(after.Discover.Days), "30"},
		{"discover.exclude", fmt.Sprint(after.Discover.Exclude), "[合集]"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s 被改动了：want %q got %q", c.name, c.want, c.got)
		} else {
			t.Logf("OK %s = %s", c.name, c.got)
		}
	}
	if !strings.Contains(string(raw), "# 顶部注释：不能被冲掉") || !strings.Contains(string(raw), "# 站点顺序注释") {
		t.Error("注释没有保留")
	}
	if !strings.Contains(string(raw), "bgmUser: alice") || !strings.Contains(string(raw), "days: 30") {
		t.Error("落盘文本里没有 bgmUser / days")
	}
}

// PUT /api/config 是整对象替换，不是 merge patch：客户端没发的键会被默认值填上。
//
// 这条不是「现在的 bug」，是把接口契约钉死，免得以后有人以为它是增量更新：
// 设置页之所以安全，是因为 collect() 回传的是 GET 拿到的同一个对象，
// 而不是因为它只发了改动的那几个字段。哪天有人把 collect() 改成「只发变化的部分」，
// 这条测试会立刻变红，而不是等用户发现自己的 sources.order 被悄悄换成了默认值。
func TestSaveReplacesOmittedKeysWithDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := `sources:
  order: [animegarden, mikan]
instances:
  - name: main
    baseUrl: https://ass.example.com/
    apiKey: k
discover:
  sources: [animegarden]
  bgmUser: alice
  days: 30
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg)
	// 客户端只发最小的 JSON，不带 sources.order / discover.sources / bgmUser / days
	minimal := `{"listen":"127.0.0.1:51837","sources":{"timeoutSeconds":25,"cacheMinutes":720},"rules":{"maxStandby":1},"instances":[{"name":"main","baseUrl":"https://ass.example.com/","apiKey":"k"}],"sync":{}}`
	rec := httptest.NewRecorder()
	srv.handleConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader(minimal)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT 应回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// 漏传 = 回落到默认值，而不是保留原值。这正是「整对象替换」的含义。
	if got := fmt.Sprint(after.Sources.Order); got != "[anibt nyaa]" {
		t.Errorf("漏传 sources.order 后应回落默认值 [anibt nyaa]，实际 %s", got)
	}
	if got := fmt.Sprint(after.Discover.SourceList()); got != "[yuc]" {
		t.Errorf("漏传 discover.sources 后应回落默认值 [yuc]，实际 %s", got)
	}
	if after.Discover.BgmUser != "" {
		t.Errorf("漏传 discover.bgmUser 后应为空，实际 %q", after.Discover.BgmUser)
	}
	if after.Discover.Days != config.DefaultDiscoverDays {
		t.Errorf("漏传 discover.days 后应为 %d，实际 %d", config.DefaultDiscoverDays, after.Discover.Days)
	}
}
