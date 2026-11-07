package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// 这组测试钉的是 t20 对齐后的**语义**（契约 §7.2），与既有那批「注释/键序/方案往返」
// 测试互补：那些测的是**写什么**，这里测的是**什么情况下写、什么情况下不写**。
//
//	t20 之前的 handleConfig：校验不过 → 400，什么都不写
//	t20 之后：校验类问题 → **落盘** + 200 {valid:false, errors}；规则文件读不出来 → 仍 400 不落盘

// newSaveServer 造一个指向临时目录的服务。
func newSaveServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.SetPath(path)
	return New(cfg), path
}

// putConfig 调一次 handleConfig 的 PUT 路径，返回状态码与解析后的响应体。
func putConfig(t *testing.T, srv *Server, cfg *config.Config) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化配置失败: %v", err)
	}
	rec := httptest.NewRecorder()
	srv.handleConfig(rec, httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body)))
	var parsed map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			t.Fatalf("响应不是 JSON: %v\n%s", err, rec.Body.String())
		}
	}
	return rec.Code, parsed
}

// 合法配置：落盘 + valid:true + 没有 errors。
func TestSaveConfigValidWritesAndReportsValid(t *testing.T) {
	srv, path := newSaveServer(t)
	cfg := config.Default()
	cfg.SetPath(path)
	cfg.Proxy = "http://127.0.0.1:7890"
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: "https://ass.example.com/", APIKey: "k"}}
	cfg.Normalize()

	code, body := putConfig(t, srv, cfg)
	if code != http.StatusOK {
		t.Fatalf("合法配置应回 200，实际 %d：%v", code, body)
	}
	if body["valid"] != true {
		t.Errorf("合法配置应报 valid:true，实际 %v（errors=%v）", body["valid"], body["errors"])
	}
	if errs, _ := body["errors"].([]any); len(errs) != 0 {
		t.Errorf("合法配置不该有 errors，实际 %v", errs)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("应已落盘：%v", err)
	}
	if !strings.Contains(string(raw), "ass.example.com") {
		t.Errorf("落盘内容不对:\n%s", raw)
	}
}

// 零实例配置：**落盘** + valid:false + errors 非空。
//
// 这是 t20 真正要修的那个首发体验：新建工作区之后用户可能只想先设代理 / 先改规则，
// 还没加实例 —— 老行为会把这次保存整个拒掉，等于连一个 config.yaml 都建不出来。
func TestSaveConfigZeroInstancesStillWrites(t *testing.T) {
	srv, path := newSaveServer(t)
	cfg := config.Default()
	cfg.SetPath(path)
	cfg.Proxy = "http://127.0.0.1:7890"
	cfg.Normalize()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("测试前提：保存前不该有 config.yaml，实际 err=%v", err)
	}

	code, body := putConfig(t, srv, cfg)
	if code != http.StatusOK {
		t.Fatalf("零实例配置也应当落盘并回 200，实际 %d：%v", code, body)
	}
	if body["valid"] != false {
		t.Errorf("零实例过不了校验，valid 应为 false，实际 %v", body["valid"])
	}
	errs, _ := body["errors"].([]any)
	if len(errs) == 0 {
		t.Fatalf("valid:false 时必须给出 errors（界面靠它显示红条），实际 %v", body)
	}
	msg, _ := errs[0].(map[string]any)["message"].(string)
	if !strings.Contains(msg, "实例") {
		t.Errorf("错误信息应是那句「还没有配置任何 ANI-RSS 实例」，实际 %q", msg)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("零实例配置必须真的落盘（这正是 t20 要修的那条），实际：%v", err)
	}
	if !strings.Contains(string(raw), "7890") {
		t.Errorf("落盘的应当是刚提交的那份:\n%s", raw)
	}
	// 落盘之后内存里那份也要换掉，否则紧接着的 GET 会给出旧配置。
	if srv.cfg.Proxy != "http://127.0.0.1:7890" {
		t.Errorf("服务端内存里的配置没跟着更新：%q", srv.cfg.Proxy)
	}
}

// 引用了读不出来的外置规则文件：**硬错误、不落盘**（契约 §7.2 的那条线）。
//
// 区别在于「不完整」与「会静默变形」：前者用户看得见（errors 摆着、同步会被拦），
// 后者会让后续 plan / sync 少一套规则地跑而配置文本看上去毫无问题。
func TestSaveConfigMissingRuleFileIsHardError(t *testing.T) {
	srv, path := newSaveServer(t)

	// 先落一份好配置，作为「没被改坏」的基准。
	good := config.Default()
	good.SetPath(path)
	good.Instances = []config.Instance{{Name: "主库", BaseURL: "https://ass.example.com/", APIKey: "k"}}
	good.Rules.Primary = []config.Rule{{Name: "简日", Match: "简日", Priority: 10, Scope: "main", In: "title"}}
	good.Normalize()
	if code, body := putConfig(t, srv, good); code != http.StatusOK {
		t.Fatalf("基准保存失败：%d %v", code, body)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	bad := config.Default()
	bad.SetPath(path)
	bad.Instances = []config.Instance{{Name: "主库", BaseURL: "https://ass.example.com/", APIKey: "k"}}
	bad.Rules.PrimaryFiles = []string{"不存在的规则文件.yaml"}
	bad.Normalize()

	code, body := putConfig(t, srv, bad)
	if code != http.StatusBadRequest {
		t.Fatalf("规则文件读不出来必须回 400（硬错误），实际 %d：%v", code, body)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Errorf("硬错误要把原因写进 error 字段，实际 %v", body)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("硬错误时不许落盘：文件被改动了\n之前:\n%s\n之后:\n%s", before, after)
	}
}
