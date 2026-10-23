package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// 默认站点顺序改成 [anibt, nyaa] 之后，老配置里写着的 animegarden / mikan 必须照旧能用。
//
// 这两家不再进 DefaultSourceOrder，但仍在 NewRegistry 的注册表和 SiteType 映射里。
// 删掉它们的诱惑很大（看着像死代码），代价却是所有写了这两个站点的老配置直接失效，
// 所以这里端到端钉一遍：能加载、能校验、注册表认、真的取得到候选。
func TestLegacyOrderAndDiscoverSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	legacy := `
listen: 127.0.0.1:51837
proxy: ""
sources:
  order:
    - animegarden
    - mikan
  stopOnFirstMatch: false
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
targets: []
discover:
  enabled: true
  sources:
    - animegarden
  days: 30
  limit: 50
  exclude: []
sync:
  intervalMinutes: 0
  preview: false
  addMissing: false
`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load 失败: %v", err)
	}
	t.Logf("sources.order = %v", cfg.Sources.Order)
	t.Logf("discover.SourceList() = %v days=%d", cfg.Discover.SourceList(), cfg.Discover.Days)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate 失败: %v", err)
	}
	if err := cfg.ValidateDiscover(); err != nil {
		t.Fatalf("ValidateDiscover 失败: %v", err)
	}

	// 假的 ANI-RSS：按 animeGardenGroup / mikanGroup / mikan 应答。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ep := strings.TrimPrefix(r.URL.Path, "/api/")
		var data any
		switch ep {
		case "animeGardenGroup":
			data = []map[string]any{{
				"name":  "Lilith-Raws",
				"rss":   "https://animegarden.example/rss/1",
				"items": []map[string]any{{"title": "葬送的芙莉莲 - 01 [简日内嵌]"}},
			}}
		case "mikanGroup":
			data = []map[string]any{{
				"label": "桜都字幕组",
				"rss":   "https://mikanani.me/RSS/Bangumi?bangumiId=3301",
				"items": []map[string]any{{"title": "葬送的芙莉莲 - 01 [繁日内封]"}},
			}}
		case "mikan":
			data = map[string]any{"weeks": []map[string]any{{
				"items": []map[string]any{{"title": "葬送的芙莉莲", "url": "https://mikanani.me/Home/Bangumi/3301", "bgmId": "400602"}},
			}}}
		default:
			data = []any{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "ok", "data": data})
	}))
	defer upstream.Close()

	client, err := assapi.New(upstream.URL, "k", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(client, upstream.Client(), 0, cfg.Sources.Order, nil)
	t.Logf("registry.Order() = %v", reg.Order())
	if strings.Join(reg.Order(), ",") != "animegarden,mikan" {
		t.Fatalf("老配置的站点顺序没有生效，实际 %v", reg.Order())
	}

	results := reg.Collect(context.Background(), Query{
		BgmID:   "400602",
		Title:   "葬送的芙莉莲",
		JPTitle: "葬送のフリーレン",
	}, false)
	for _, res := range results {
		t.Logf("站点 %s -> 候选 %d 个, err=%v", res.Site, len(res.Candidates), res.Err)
		if res.Err != nil {
			t.Errorf("站点 %s 报错: %v", res.Site, res.Err)
		}
		if len(res.Candidates) == 0 {
			t.Errorf("站点 %s 一个候选都没拿到（老配置会静默失效）", res.Site)
		}
	}
}

// discover.sources 的取值校验：老来源仍然合法，非法来源要报错并列出可选项。
func TestDiscoverSourceValidation(t *testing.T) {
	cases := []struct {
		name    string
		sources []string
		user    string
		wantErr bool
	}{
		{"animegarden 仍然合法", []string{"animegarden"}, "", false},
		{"yuc 合法", []string{"yuc"}, "", false},
		{"bangumi-collections 有 bgmUser", []string{"bangumi-collections"}, "alice", false},
		{"bangumi-collections 没 bgmUser", []string{"bangumi-collections"}, "", true},
		{"大小写与空格", []string{"  AnimeGarden  "}, "", false},
		{"四个来源混合", []string{"yuc", "animegarden", "bangumi-calendar", "bangumi-collections"}, "alice", false},
		{"不认识的来源", []string{"anilist"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Discover.Sources = tc.sources
			cfg.Discover.BgmUser = tc.user
			err := cfg.ValidateDiscover()
			t.Logf("sources=%v bgmUser=%q -> err=%v", tc.sources, tc.user, err)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v 实际 err=%v", tc.wantErr, err)
			}
		})
	}
}

// 一份完全没有 sources.order / discover 段的老配置，应该拿到新默认值而不是空。
func TestMinimalLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("instances:\n  - name: main\n    baseUrl: https://a/\n    apiKey: k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	t.Logf("order=%v discover=%v days=%d", cfg.Sources.Order, cfg.Discover.SourceList(), cfg.Discover.Days)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	reg := NewRegistry(nil, nil, 0, cfg.Sources.Order, nil)
	t.Logf("registry order=%v", reg.Order())
	if strings.Join(reg.Order(), ",") != "anibt,nyaa" {
		t.Errorf("默认顺序应为 anibt,nyaa，实际 %v", reg.Order())
	}
}
