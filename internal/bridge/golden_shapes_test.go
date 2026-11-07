package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// shapePlaceholder 是夹具里替代真实工作区路径的占位符。
//
// 夹具必须逐字节稳定：它进版本库、被 Dart 侧断言。
// 临时目录名每次都不一样，httptest 的端口每次也不一样，所以全部替换掉。
const (
	workspacePlaceholder = "<WORKSPACE>"
	instancePlaceholder  = "https://ass.example.com"
)

// TestGoldenShapes 对着 httptest 假实例产出四个 op 的样例响应。
//
// 四个 op 是契约 §9 点名的：config.get / instance.subscriptions / plan.build / status.get。
// Dart 侧拿这些文件跑 model 的 fromJson —— **字段名改了就红**。
//
// 每个 op 写两份文件：
//   - shapes/<name>.json      完整响应（信封 {"ok":true,"data":{...}}）
//   - shapes/<name>.data.json 只有 data（`Model.fromJson(jsonDecode(file))` 直接能用）
//
// 两份都给是刻意的：契约 §3 说响应永远是信封，所以样例也该是信封；
// 而反序列化用的是 data。少给哪一份都会让 Dart 侧自己动手剥一层，
// 而「我自己剥的那层对不对」没人对拍。
func TestGoldenShapes(t *testing.T) {
	dir := t.TempDir()
	instance := newFakeInstance(t)
	defer instance.Close()

	cases := []struct {
		name    string
		op      string
		payload map[string]any
	}{
		{name: "config_get", op: "config.get"},
		{name: "instance_subscriptions", op: "instance.subscriptions", payload: map[string]any{"name": "追番库", "refresh": true}},
		{name: "plan_build", op: "plan.build"},
		{name: "status_get", op: "status.get", payload: map[string]any{"limit": 5}},
	}

	core := New()
	defer core.Close()
	cfg := fakeConfig(dir, instance.URL)
	app.SetWorkspace(filepath.Join(dir, "config.yaml"), "")
	core.SetConfig(cfg)

	out := filepath.Join(fixturesDir(t), "shapes")
	for _, tc := range cases {
		envelope := core.HandleJSON(context.Background(), tc.op, tc.payload, "")
		normalized := normalizeFixture(t, envelope, dir, instance.URL)

		var parsed struct {
			OK    bool            `json:"ok"`
			Data  json.RawMessage `json:"data"`
			Error *ErrorPayload   `json:"error"`
		}
		if err := json.Unmarshal(normalized, &parsed); err != nil {
			t.Fatalf("%s 的响应不是合法 JSON: %v\n%s", tc.op, err, normalized)
		}
		if !parsed.OK {
			t.Fatalf("%s 应该成功，实际 %v", tc.op, parsed.Error)
		}
		if len(parsed.Data) == 0 {
			t.Fatalf("%s 的响应里没有 data", tc.op)
		}

		pretty := prettyJSON(t, normalized)
		prettyData := prettyJSON(t, parsed.Data)
		writeFixture(t, filepath.Join(out, tc.name+".json"), pretty)
		writeFixture(t, filepath.Join(out, tc.name+".data.json"), prettyData)
		t.Logf("%s → %d 字节（信封），%d 字节（data）", tc.op, len(pretty), len(prettyData))
	}
}

// normalizeFixture 把夹具里会变的东西替换成占位符。
func normalizeFixture(t *testing.T, raw []byte, dir, upstream string) []byte {
	t.Helper()
	text := string(raw)
	slashDir := filepath.ToSlash(dir)
	// 长的先替：httptest 的地址可能包含临时目录片段。
	//
	// 每条都要给「JSON 转义后的样子」：Windows 路径在 JSON 里是 C:\\Users\\...，
	// 直接替 C:\Users\... 一个字都匹配不到 —— 这正是夹具里能漏出真实路径的原因。
	pairs := [][2]string{
		{upstream, instancePlaceholder},
		{slashDir, workspacePlaceholder},
		{dir, workspacePlaceholder},
		{jsonEscaped(slashDir), workspacePlaceholder},
		{jsonEscaped(dir), workspacePlaceholder},
	}
	for _, pair := range pairs {
		if pair[0] == "" {
			continue
		}
		text = strings.ReplaceAll(text, pair[0], pair[1])
	}
	// httptest 给的是 127.0.0.1:<随机端口>，端口每次都不一样。
	// 注意别把配置里的 listen: 127.0.0.1:51837 一起判掉 —— 那是固定的。
	if host := strings.TrimPrefix(upstream, "http://"); host != "" && strings.Contains(text, host) {
		t.Fatalf("夹具里还留着 httptest 地址 %s，规范化没覆盖到：\n%s", host, text)
	}
	for _, leftover := range []string{slashDir, dir, jsonEscaped(slashDir), jsonEscaped(dir)} {
		if leftover != "" && strings.Contains(text, leftover) {
			t.Fatalf("夹具里还留着临时目录 %s，规范化没覆盖到：\n%s", leftover, text)
		}
	}
	return []byte(text)
}

// jsonEscaped 返回一个字符串「在 JSON 文本里的样子」（不带引号）。
func jsonEscaped(s string) string {
	if s == "" {
		return ""
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// prettyJSON 缩进一下，让夹具是给人读的（diff 也才有意义）。
//
// 关掉 HTML 转义：Go 默认会把 < > & 写成 \u003c / \u0026，
// 那让夹具里的占位符和 RSS 地址变成没法一眼读懂的乱码 ——
// 而这份文件的唯一用途就是被人对着读、对着 diff。
func prettyJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("夹具内容不是合法 JSON: %v\n%s", err, raw)
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		t.Fatalf("夹具缩进失败: %v", err)
	}
	return buf.Bytes()
}

// fakeConfig 造一份形状夹具用的配置。
//
// 实例指向 httptest；站点顺序只留 anibt，这样 plan.build 不会去打真的 nyaa。
//
// 刻意**真的引用两份外置规则文件**：`ruleFiles` 里那三个字段
// （path / kind / count）只有在真有文件被引用时才会出现在夹具里。
// 引用空的话夹具能过、Dart 侧的 model 却从没被真正检验过 ——
// 「字段名改了就红」这个承诺就落空了。
func fakeConfig(dir, baseURL string) *config.Config {
	if err := os.WriteFile(filepath.Join(dir, "raws.yaml"), []byte(
		"- name: 放流组优先\n  match: (?i)^ani\\b\n  priority: 10\n  scope: both\n  in: group\n"+
			"- name: ToonsHub\n  match: (?i)toonshub\n  priority: 400\n  scope: both\n  in: group\n"+
			"- name: Nix-Raws\n  match: (?i)nix-raws\n  priority: 450\n  scope: both\n  in: group\n"+
			"- name: 喵萌奶茶屋\n  match: (?i)喵萌\n  priority: 500\n  scope: both\n  in: group\n",
	), 0o600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "deny.yaml"), []byte(
		"- {match: 合集, why: 不要合集}\n"+
			"- {match: '(?i)bdrip', why: 追番不等 BDRip}\n",
	), 0o600); err != nil {
		panic(err)
	}

	cfg := config.Default()
	cfg.Proxy = ""
	cfg.Sources.Instance = "追番库"
	cfg.Sources.Order = []string{"anibt"}
	cfg.Sources.TimeoutSeconds = 25
	cfg.Sources.CacheMinutes = 720
	cfg.Rules.MaxStandby = 1
	cfg.Rules.Primary = []config.Rule{
		{Name: "放流组优先", Match: `(?i)^ani\b`, Priority: 10, Scope: "main", In: "group"},
	}
	cfg.Rules.Deny = []config.DenyRule{{Match: "合集", Why: "不要合集"}}
	cfg.Rules.PrimaryFiles = []string{"raws.yaml"}
	cfg.Rules.DenyFiles = []string{"deny.yaml"}
	cfg.Profiles = []config.Profile{
		{
			Name: "收藏",
			Rules: config.Rules{
				MaxStandby:   12,
				Primary:      []config.Rule{{Name: "全都要", Match: `.+`, Priority: 100, Scope: "both", In: "any"}},
				PrimaryFiles: []string{"raws.yaml"},
				DenyFiles:    []string{"deny.yaml"},
			},
			Sources: &config.ProfileSources{Order: []string{"anibt"}},
		},
	}
	cfg.Instances = []config.Instance{
		{Name: "追番库", BaseURL: baseURL, APIKey: "abcdefghijklmnop", Profile: "收藏"},
	}
	cfg.Targets = []config.Target{{Title: "冰之城墙", BgmID: "665390", Season: intPtr(2)}}
	cfg.Sync.IntervalMinutes = 0
	cfg.SetPath(filepath.Join(dir, "config.yaml"))
	cfg.Normalize()
	// 外置规则文件要在 Normalize 之后读：它按主配置所在目录解析相对路径。
	if err := cfg.LoadRuleFiles(); err != nil {
		panic(err)
	}
	return cfg
}

func intPtr(v int) *int { return &v }

// newFakeInstance 起一个只会说 ANI-RSS 协议的假实例。
//
// 只用假实例，不做任何真请求：夹具必须能在没有网络、没有代理的机器上稳定产出 ——
// 而形状对拍本身要证明的正是「真实响应长什么样」。
func newFakeInstance(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/about"):
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"version":"3.2.52"}}`))
		case strings.HasSuffix(r.URL.Path, "/api/listAni"):
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"total":2,"weekList":[
			  {"weekLabel":"四","items":[
			    {"id":"1234","title":"冰之城墙 (2026) [tmdbid=283428]","bgmUrl":"https://bgm.tv/subject/665390",
			     "url":"https://old.example.com/rss.xml","subgroup":"旧组","season":1,"offset":0,
			     "enable":true,"image":"https://lain.bgm.tv/pic/cover/m/xx.jpg",
			     "standbyRssList":[{"label":"ANi","url":"https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=ani","offset":-12}]},
			    {"id":"0","title":"药屋少女的呢喃","bgmUrl":"https://bgm.tv/subject/400602",
			     "url":"https://old.example.com/rss2.xml","season":"第二季","offset":{"nested":true},
			     "standbyRssList":["不是对象"]}
			  ]}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/api/aniBTGroup"):
			// 按 bgmId 给不同的组：夹具是给人读的，
			// 两条不同番剧拿到同一个 RSS 会让人以为右边那个字段没生效。
			if r.URL.Query().Get("bgmId") == "400602" {
				_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":[
				  {"label":"喵萌奶茶屋","name":"喵萌奶茶屋","slug":"mgn","rss":"https://anibt.net/rss/anime.xml?bgmId=400602&groupSlug=mgn",
				   "bgmUrl":"https://bgm.tv/subject/400602","total":17,
				   "items":[{"title":"[喵萌奶茶屋] 药屋少女的呢喃 第二季 - 01 [简繁日内封][1080P]"}]}
				]}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":[
			  {"label":"猎户发布组","name":"猎户发布组","slug":"lh","rss":"https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=lh",
			   "bgmUrl":"https://bgm.tv/subject/665390","total":42,
			   "items":[{"title":"[猎户压制部] 冰之城墙 - 01 [简日内嵌][1080P]"}]}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}
