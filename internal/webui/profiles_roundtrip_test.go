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

// 设置页保存一次，不能把「订阅方案」弄丢。
//
// 前端的契约是「改同一个 cfg 对象再整体回传」（collect()），服务端 POST/PUT 是
// 整对象替换（见 TestSaveReplacesOmittedKeysWithDefaults）。两头任何一头漏掉
// profiles，用户保存一次就丢掉整套方案 —— 而这件事在界面上看不出来：
// 方案区直接变空，用户只会以为自己没写，然后重写一遍。
//
// 所以这条测试盯的是完整往返：
//   - profiles[].name / rules（primary、deny、maxStandby、primaryFiles）/ sources.order
//   - instances[].profile
//   - 方案卡要显示的 fileRules（GET 回填时就该在）
//   - 没挂方案的实例不会在 yaml 里多出一行空的 profile:
func TestSaveKeepsProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// 方案引用的外置规则文件必须真的存在：Load 读不到会直接报错，
	// 那和「profiles 丢了」是两回事，测试要能分开这两种失败。
	if err := os.WriteFile(filepath.Join(dir, "rules-primary.yaml"),
		[]byte("- name: 文件里的\n  match: (?i)简日\n  priority: 30\n  scope: main\n  in: title\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := `listen: 127.0.0.1:51837
sources:
  instance: main
  order:
    - anibt
    - nyaa
rules:
  primary:
    - name: 默认简日
      match: (?i)简日
      priority: 10
      scope: main
      in: title
  deny:
    - match: 合集
      why: 不要合集
  maxStandby: 1
profiles:
  - name: 追番
    rules:
      primary:
        - name: 快
          match: (?i)简日
          priority: 5
          scope: main
          in: group
      deny:
        - match: BDRip
          why: 抢速度
      primaryFiles:
        - rules-primary.yaml
      maxStandby: 2
    sources:
      order:
        - anibt
  - name: 收藏
    rules:
      primary:
        - name: 全都要
          match: (?i)简繁
          priority: 8
          scope: both
          in: any
instances:
  - name: main
    baseUrl: https://ass.example.com/
    apiKey: k
    profile: 追番
  - name: backup
    baseUrl: https://ass2.example.com/
    apiKey: k2
targets:
  - title: 老番
    bgmId: "975"
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv := New(cfg)

	// 1) GET：前端就是拿 config 这个字段回填的，方案与实例上的方案名必须都在。
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

	profiles, ok := got.Config["profiles"].([]any)
	if !ok || len(profiles) != 2 {
		t.Fatalf("GET 回来的 profiles 应该是 2 条，实际 %v", got.Config["profiles"])
	}
	first, ok := profiles[0].(map[string]any)
	if !ok || first["name"] != "追番" {
		t.Fatalf("第一个方案应是「追番」，实际 %v", profiles[0])
	}
	// 方案卡标题旁边那串 fileRules 是只读展示，靠 GET 带回来。
	files, ok := first["fileRules"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("方案的 fileRules 没跟着 GET 回来: %v", first["fileRules"])
	}
	fileInfo := files[0].(map[string]any)
	if fileInfo["kind"] != "primary" || fileInfo["count"] != 1.0 {
		t.Errorf("fileRules 内容不对: %v", fileInfo)
	}
	instances, ok := got.Config["instances"].([]any)
	if !ok || len(instances) != 2 {
		t.Fatalf("GET 回来的 instances 应该是 2 条，实际 %v", got.Config["instances"])
	}
	if name := instances[0].(map[string]any)["profile"]; name != "追番" {
		t.Errorf("实例 main 的 profile 应该是 追番，实际 %v", name)
	}

	// 2) 模拟 collect()：只改一个无关字段，其余原样回传（整对象替换）。
	sources, ok := got.Config["sources"].(map[string]any)
	if !ok {
		t.Fatalf("GET 回来的 sources 不是对象: %v", got.Config["sources"])
	}
	sources["timeoutSeconds"] = 33.0
	body, err := json.Marshal(got.Config)
	if err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	srv.handleConfig(rec2, httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST 应回 200，实际 %d，body=%s", rec2.Code, rec2.Body.String())
	}

	// 3) 落盘的文件必须还能读回同一套方案。
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("保存后的文件:\n%s", raw)

	after, err := config.Load(path)
	if err != nil {
		t.Fatalf("重新 Load: %v", err)
	}
	if len(after.Profiles) != 2 {
		t.Fatalf("保存后方案数应该还是 2，实际 %d", len(after.Profiles))
	}
	p0 := after.Profiles[0]
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"profiles[0].name", p0.Name, "追番"},
		{"profiles[0].rules.primary 条数", fmt.Sprint(len(p0.Rules.Primary)), "1"},
		{"profiles[0].rules.primary[0].name", p0.Rules.Primary[0].Name, "快"},
		{"profiles[0].rules.primary[0].match", p0.Rules.Primary[0].Match, "(?i)简日"},
		{"profiles[0].rules.primary[0].priority", fmt.Sprint(p0.Rules.Primary[0].Priority), "5"},
		{"profiles[0].rules.primary[0].scope", p0.Rules.Primary[0].Scope, "main"},
		{"profiles[0].rules.primary[0].in", p0.Rules.Primary[0].In, "group"},
		{"profiles[0].rules.deny[0].match", p0.Rules.Deny[0].Match, "BDRip"},
		{"profiles[0].rules.deny[0].why", p0.Rules.Deny[0].Why, "抢速度"},
		{"profiles[0].rules.maxStandby", fmt.Sprint(p0.Rules.MaxStandby), "2"},
		{"profiles[0].rules.primaryFiles", fmt.Sprint(p0.Rules.PrimaryFiles), "[rules-primary.yaml]"},
		{"profiles[0].sources.order", fmt.Sprint(p0.Sources.Order), "[anibt]"},
		{"profiles[0].fileRules 条数", fmt.Sprint(len(p0.FileRules)), "1"},
		{"profiles[1].name", after.Profiles[1].Name, "收藏"},
		{"profiles[1].sources（没写就该是 nil）", fmt.Sprint(after.Profiles[1].Sources), "<nil>"},
		{"instances[0].profile", after.Instances[0].Profile, "追番"},
		{"instances[1].profile（没挂就是空）", after.Instances[1].Profile, ""},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s 没保住：want %q got %q", c.name, c.want, c.got)
			continue
		}
		t.Logf("OK %s = %s", c.name, c.got)
	}

	// omitempty 的语义：只有真正挂了方案的实例才写 profile:。
	// 没挂的那个实例要是也写一行 profile: ""，配置文件就多出一堆空字段。
	if n := strings.Count(string(raw), "profile:"); n != 1 {
		t.Errorf("yaml 里 profile: 应该只出现 1 次（只给挂了方案的实例），实际 %d 次", n)
	}
	if !strings.Contains(string(raw), "profile: 追番") {
		t.Error("yaml 里没有 profile: 追番")
	}

	// 4) 再 GET 一次：用户刷新页面看到的就是这个响应。
	rec3 := httptest.NewRecorder()
	srv.handleConfig(rec3, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rec3.Code != http.StatusOK {
		t.Fatalf("保存后 GET 应回 200，实际 %d", rec3.Code)
	}
	var again struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(rec3.Body.Bytes(), &again); err != nil {
		t.Fatalf("保存后 GET 响应不是 JSON: %v", err)
	}
	againProfiles, ok := again.Config["profiles"].([]any)
	if !ok || len(againProfiles) != 2 {
		t.Fatalf("保存后 GET 的 profiles 应该还是 2 条，实际 %v", again.Config["profiles"])
	}
	againInstances := again.Config["instances"].([]any)
	if name := againInstances[0].(map[string]any)["profile"]; name != "追番" {
		t.Errorf("保存后实例 main 的 profile 应该是 追番，实际 %v", name)
	}
	againFirst := againProfiles[0].(map[string]any)
	if f, ok := againFirst["fileRules"].([]any); !ok || len(f) != 1 {
		t.Errorf("保存后 GET 的方案 fileRules 丢了: %v", againFirst["fileRules"])
	}
}
