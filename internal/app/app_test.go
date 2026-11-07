package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/daemon"
	"github.com/zzzwannasleep/anisync/internal/journal"
	"github.com/zzzwannasleep/anisync/internal/planner"
	"github.com/zzzwannasleep/anisync/internal/workdir"
)

// 工作区第一性：configPath / dataDir 定了，四个派生路径必须全部跟着走。
func TestPathsFollowWorkspace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if got := SetWorkspace(path, ""); got != path {
		t.Fatalf("SetWorkspace 应回报生效的 configPath，实际 %q", got)
	}
	if ConfigPath() != path {
		t.Errorf("ConfigPath = %q, want %q", ConfigPath(), path)
	}
	if want := filepath.Join(dir, "data"); DataDir() != want {
		t.Errorf("DataDir = %q, want %q", DataDir(), want)
	}
	if want := filepath.Join(dir, "data", "cache.json"); CachePath() != want {
		t.Errorf("CachePath = %q, want %q", CachePath(), want)
	}
	if want := filepath.Join(dir, "data", "sync.lock"); LeasePath() != want {
		t.Errorf("LeasePath = %q, want %q", LeasePath(), want)
	}
	if want := filepath.Join(dir, "data", "runs.jsonl"); JournalPath() != want {
		t.Errorf("JournalPath = %q, want %q", JournalPath(), want)
	}
	if want := filepath.Join(dir, "data", "backup"); BackupDir() != want {
		t.Errorf("BackupDir = %q, want %q", BackupDir(), want)
	}

	// 显式 dataDir 优先。
	other := t.TempDir()
	SetWorkspace(path, other)
	if DataDir() != other {
		t.Errorf("显式 dataDir 应优先，实际 %q", DataDir())
	}
}

// 没有显式工作区时，一切走 workdir 的既有语义（数据在程序目录，不跟 --config 走）。
func TestPathsDefaultToWorkdir(t *testing.T) {
	SetWorkspace("", "")
	if ConfigPath() != workdirConfigPath() {
		t.Errorf("默认 ConfigPath 应是 workdir.ConfigPath()，实际 %q", ConfigPath())
	}
	if DataDir() != workdirDataDir() {
		t.Errorf("默认 DataDir 应是 workdir.DataDir()，实际 %q", DataDir())
	}
}

func TestInstanceStatusesLabels(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = []config.Profile{{Name: "追番", Rules: config.Rules{MaxStandby: 2}}}
	cfg.Instances = []config.Instance{
		{Name: "挂对了", BaseURL: "https://a/", APIKey: "k", Profile: "追番"},
		{Name: "没挂", BaseURL: "https://b/", APIKey: "k"},
		{Name: "挂错了", BaseURL: "https://c/", APIKey: "k", Profile: "不存在"},
	}
	statuses := InstanceStatuses(cfg)
	if len(statuses) != 3 {
		t.Fatalf("应回 3 个实例，实际 %d", len(statuses))
	}
	cases := []struct {
		index   int
		profile string
		label   string
		missing bool
	}{
		{0, "追番", "追番", false},
		{1, "", "（默认，顶层 rules）", false},
		{2, "", "回落：顶层 rules —— 配置里写的「不存在」不存在", true},
	}
	for _, tc := range cases {
		got := statuses[tc.index]
		if got.Profile != tc.profile || got.ProfileLabel != tc.label || got.Missing != tc.missing {
			t.Errorf("实例 %s 的标签不对: %+v", got.Name, got)
		}
	}
	if statuses[0].MaxStandby != 2 {
		t.Errorf("方案里的 maxStandby 应生效，实际 %d", statuses[0].MaxStandby)
	}
}

func TestCountWritable(t *testing.T) {
	plan := &planner.Plan{Actions: []planner.Action{
		{Kind: "update"}, {Kind: "add"}, {Kind: "noop"}, {Kind: "error"},
	}}
	if got := CountWritable(plan); got != 2 {
		t.Errorf("CountWritable = %d, want 2", got)
	}
	if got := CountWritable(nil); got != 0 {
		t.Errorf("nil 计划应是 0，实际 %d", got)
	}
}

func TestSummarizeOutcome(t *testing.T) {
	cases := []struct {
		outcome daemon.Outcome
		want    string
	}{
		{daemon.Outcome{Note: "上一轮还没跑完"}, "上一轮还没跑完"},
		{daemon.Outcome{Writable: 0}, "没有需要写入的变更"},
		{daemon.Outcome{Writable: 3, Updated: 2, Added: 1}, "更新 2 条，新增 1 条"},
		{daemon.Outcome{Writable: 3, Updated: 2, Added: 1, Errors: []string{"x"}}, "更新 2 条，新增 1 条，失败 1 条"},
	}
	for _, tc := range cases {
		if got := SummarizeOutcome(tc.outcome); got != tc.want {
			t.Errorf("SummarizeOutcome = %q, want %q", got, tc.want)
		}
	}
}

func TestIntervalOf(t *testing.T) {
	cfg := config.Default()
	if got := IntervalOf(cfg); got != 0 {
		t.Errorf("intervalMinutes=0 应给 0，实际 %s", got)
	}
	cfg.Sync.IntervalMinutes = 30
	if got := IntervalOf(cfg).Minutes(); got != 30 {
		t.Errorf("应给 30 分钟，实际 %v", got)
	}
}

// 上一轮还没跑完时，设置页点同步必须「报一句人话」而不是排队等。
func TestNewSyncEndpointBusy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.SetPath(path)
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: "https://a/", APIKey: "k"}}

	var mu sync.Mutex
	mu.Lock() // 模拟「上一轮还在跑」
	endpoint := NewSyncEndpoint(func() (*config.Config, error) { return cfg, nil }, &mu, "web")

	result, err := endpoint(context.Background())
	if err != nil {
		t.Fatalf("让开不是错误，实际 %v", err)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "上一轮还没跑完") {
		t.Errorf("应该说清是让开了，实际 %+v", result)
	}
	if result.Updated != 0 || result.Plan != nil {
		t.Errorf("让开时不该有任何动作，实际 %+v", result)
	}
	mu.Unlock()
}

func TestLoadCheckedWrapsValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if _, err := LoadChecked(path); err == nil {
		t.Fatal("空配置应校验失败")
	} else if !strings.Contains(err.Error(), "配置校验失败") {
		t.Errorf("错误信息应带上「配置校验失败」前缀（CLI 与常驻进程都用它），实际 %q", err.Error())
	}
	// 读不出来的路径是另一回事，不该被包成校验失败。
	if _, err := LoadChecked(filepath.Join(dir, "no-such-dir", "x.yaml")); err == nil {
		t.Error("不存在的目录应报错")
	}
}

func TestRecordRunWritesJournal(t *testing.T) {
	dir := t.TempDir()
	SetWorkspace(filepath.Join(dir, "config.yaml"), "")
	RecordRun("cli", daemon.Outcome{Writable: 2, Updated: 1, Added: 1}, nil)

	entries, err := TailJournal(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("应落 1 条运行记录，实际 %d", len(entries))
	}
	entry := entries[0]
	if entry.Mode != "cli" || !entry.OK || entry.Updated != 1 || entry.Added != 1 {
		t.Errorf("记录内容不对: %+v", entry)
	}
	if entry.At.IsZero() {
		t.Error("记录必须带时间")
	}
}

func TestNextRunAt(t *testing.T) {
	if _, ok := NextRunAt(nil); ok {
		t.Error("没有记录时不该猜一个下次运行时间")
	}
	if _, ok := NextRunAt([]journal.Entry{{OK: true}}); ok {
		t.Error("记录里没有 NextAt 时不该猜")
	}
	entries := []journal.Entry{{OK: true}, {Note: "x", NextAt: time.Now().Add(time.Hour)}}
	got, ok := NextRunAt(entries)
	if !ok || got.IsZero() {
		t.Fatalf("应取最后一条带 NextAt 的记录，实际 %v %v", got, ok)
	}
}

// BuildAndApply 是 CLI / 托盘 / 设置页共用的那一轮：这里对着假实例验证它真的会写回。
func TestBuildAndApplyAgainstFakeInstance(t *testing.T) {
	var (
		mu      sync.Mutex
		setAni  int
		lastSet map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/listAni"):
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"total":1,"weekList":[
			  {"weekLabel":"四","items":[{"id":"1","title":"某番","bgmUrl":"https://bgm.tv/subject/1",
			   "url":"https://old.example.com/rss.xml","season":1,"standbyRssList":[]}]}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/api/setAni"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			setAni++
			lastSet = body
			mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
		case strings.HasSuffix(r.URL.Path, "/api/aniBTGroup"):
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":[
			  {"label":"ANi","name":"ANi","slug":"ani","rss":"https://anibt.net/rss/anime.xml?bgmId=1&groupSlug=ani",
			   "bgmUrl":"https://bgm.tv/subject/1","total":3,"items":[{"title":"[ANi] 某番 - 01"}]}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	SetWorkspace(path, "")
	cfg := config.Default()
	cfg.SetPath(path)
	cfg.Proxy = ""
	cfg.Sources.Instance = "主库"
	cfg.Sources.Order = []string{"anibt"}
	cfg.Rules.MaxStandby = 1
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: server.URL, APIKey: "k"}}
	cfg.Normalize()
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	outcome, plan, err := BuildAndApply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAndApply: %v", err)
	}
	if plan == nil {
		t.Fatal("必须回一份计划（设置页要画它）")
	}
	if outcome.Writable != 1 || outcome.Updated != 1 {
		t.Errorf("应有 1 条更新并写成功，实际 %+v", outcome)
	}
	mu.Lock()
	defer mu.Unlock()
	if setAni != 1 {
		t.Errorf("实例侧应收到 1 次 setAni，实际 %d", setAni)
	}
	if lastSet["url"] != "https://anibt.net/rss/anime.xml?bgmId=1&groupSlug=ani" {
		t.Errorf("写回去的 url 不对: %v", lastSet["url"])
	}
	if lastSet["title"] != "某番" {
		t.Errorf("setAni 是全量覆盖，原字段必须整份带回去，实际 %v", lastSet)
	}
}

// 一份配置都读不出来时，BuildAndApply 要把错误原样抛上来（而不是空转）。
func TestBuildAndApplyRejectsBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	SetWorkspace(filepath.Join(dir, "config.yaml"), "")
	cfg := config.Default()
	if _, _, err := BuildAndApply(context.Background(), cfg); err == nil {
		t.Fatal("没有实例时建计划应失败")
	}
}

// 小工具：直接向 workdir 要默认路径，避免测试里复制一份逻辑。
func workdirConfigPath() string { return workdir.ConfigPath() }

func workdirDataDir() string { return workdir.DataDir() }
