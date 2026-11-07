package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// fakeInstance 是只会回 setAni / deleteAni / listAni 的假实例。
type fakeInstance struct {
	mu       sync.Mutex
	set      []string
	deleted  []string
	items    []map[string]any
	failSet  bool
	listCall int
}

func (f *fakeInstance) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/listAni"):
			f.mu.Lock()
			f.listCall++
			items := f.items
			f.mu.Unlock()
			weeks := []map[string]any{}
			for _, item := range items {
				weeks = append(weeks, map[string]any{"weekLabel": "一", "items": []any{item}})
			}
			payload, _ := json.Marshal(map[string]any{
				"code": 200, "message": "ok",
				"data": map[string]any{"total": len(items), "weekList": weeks},
			})
			_, _ = w.Write(payload)
		case strings.HasSuffix(r.URL.Path, "/api/setAni"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			if f.failSet {
				f.mu.Unlock()
				_, _ = w.Write([]byte(`{"code":500,"message":"上游炸了","data":null}`))
				return
			}
			f.set = append(f.set, fmt.Sprint(body["id"]))
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
		case strings.HasSuffix(r.URL.Path, "/api/deleteAni"):
			var ids []string
			_ = json.NewDecoder(r.Body).Decode(&ids)
			f.mu.Lock()
			f.deleted = append(f.deleted, ids...)
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
		default:
			http.NotFound(w, r)
		}
	}
}

func testConfig(t *testing.T, baseURL string) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	app.SetWorkspace(path, "")
	cfg := config.Default()
	cfg.SetPath(path)
	cfg.Proxy = ""
	cfg.Instances = []config.Instance{{Name: "主库", BaseURL: baseURL, APIKey: "k"}}
	cfg.Normalize()
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return cfg, path
}

func TestCreateWritesArchive(t *testing.T) {
	fake := &fakeInstance{items: []map[string]any{
		{"id": "1", "title": "某番", "url": "https://x/rss", "bgmUrl": "https://bgm.tv/subject/1"},
	}}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg, _ := testConfig(t, server.URL)

	dir := t.TempDir()
	var progress []string
	result, err := Create(context.Background(), cfg, CreateOptions{
		Dir:      dir,
		Progress: func(name string, count int) { progress = append(progress, fmt.Sprintf("%s:%d", name, count)) },
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(progress) != 1 || progress[0] != "主库:1" {
		t.Errorf("应按实例回调进度，实际 %v", progress)
	}
	if !strings.HasPrefix(filepath.Base(result.Path), "anisync-backup-") {
		t.Errorf("存档文件名应是 anisync-backup-<时间>.json，实际 %s", result.Path)
	}
	if result.Instances[0].File != filepath.Base(result.Path) {
		t.Errorf("摘要里应带文件名，实际 %v", result.Instances)
	}

	raw, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version": 1`) {
		t.Errorf("存档里应写明版本号 1:\n%s", raw)
	}
	// 存档存的是 listAni 的原始对象，所以还原时能一条不差地写回去。
	if !strings.Contains(string(raw), `"bgmUrl": "https://bgm.tv/subject/1"`) {
		t.Errorf("存档应保留原始对象:\n%s", raw)
	}

	loaded, err := Read(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Instances) != 1 || len(loaded.Instances[0].Items) != 1 {
		t.Errorf("读回来内容不对: %+v", loaded)
	}
}

func TestCreateRequiresInstances(t *testing.T) {
	cfg, _ := testConfig(t, "https://ass.example.com/")
	cfg.Instances = nil
	if _, err := Create(context.Background(), cfg, CreateOptions{Dir: t.TempDir()}); err == nil {
		t.Fatal("一个实例都没有时应报错，而不是写一份空存档")
	}
}

func TestReadRejectsVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.json")
	if err := os.WriteFile(path, []byte(`{"version": 99, "instances": []}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path); err == nil {
		t.Fatal("版本不匹配必须报错（否则会用错误的字段语义去写实例）")
	} else if !strings.Contains(err.Error(), "99") {
		t.Errorf("错误信息里应带上版本号，实际 %q", err.Error())
	}
}

func TestListIsEmptyWhenDirMissing(t *testing.T) {
	items, err := List(filepath.Join(t.TempDir(), "还没有这个目录"))
	if err != nil {
		t.Fatalf("目录不存在不该报错：界面第一次打开时就是这种状态，实际 %v", err)
	}
	if len(items) != 0 {
		t.Errorf("应回空列表，实际 %v", items)
	}
}

func TestListSortsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"anisync-backup-20260101-010101.json", "anisync-backup-20260301-010101.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"version":1,"createdAt":"2026-01-01T00:00:00Z","instances":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("只该列出 .json 存档，实际 %v", items)
	}
	if items[0].File != "anisync-backup-20260301-010101.json" {
		t.Errorf("最新的应排在最前，实际 %s", items[0].File)
	}
}

func TestRestoreAppliesAndPrunes(t *testing.T) {
	fake := &fakeInstance{items: []map[string]any{
		{"id": "1", "title": "存档里的"},
		{"id": "2", "title": "多出来的"},
	}}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg, _ := testConfig(t, server.URL)

	archive := File{
		Version:   Version,
		CreatedAt: "2026-01-01T00:00:00Z",
		Instances: []Instance{{
			Name:  "主库",
			Items: []assapi.Ani{{"id": "1", "title": "存档里的"}},
		}},
	}
	path, err := Write(t.TempDir(), archive)
	if err != nil {
		t.Fatal(err)
	}

	var (
		restored  []string
		confirmed int
		pruned    []string
	)
	result, err := Restore(context.Background(), cfg, RestoreOptions{Path: path, Prune: true}, RestoreHooks{
		Restored:     func(name string, saved int) { restored = append(restored, fmt.Sprintf("%s:%d", name, saved)) },
		PruneConfirm: func(name string, extras []string) bool { confirmed = len(extras); return true },
		Pruned:       func(name string, deleted int) { pruned = append(pruned, fmt.Sprintf("%s:%d", name, deleted)) },
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if result.Restored != 1 || result.Deleted != 1 || result.Failures != 0 {
		t.Errorf("结果不对: %+v", result)
	}
	if len(restored) != 1 || restored[0] != "主库:1" {
		t.Errorf("应按实例回调进度，实际 %v", restored)
	}
	if confirmed != 1 {
		t.Errorf("prune 之前要报出多余条数，实际 %d", confirmed)
	}
	if len(pruned) != 1 {
		t.Errorf("删完要回调，实际 %v", pruned)
	}
	if len(fake.deleted) != 1 || fake.deleted[0] != "2" {
		t.Errorf("只该删存档里没有的那条，实际 %v", fake.deleted)
	}
}

func TestRestoreCancelKeepsExtras(t *testing.T) {
	fake := &fakeInstance{items: []map[string]any{{"id": "1"}, {"id": "2"}}}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg, _ := testConfig(t, server.URL)

	path, err := Write(t.TempDir(), File{Version: Version, Instances: []Instance{{
		Name:  "主库",
		Items: []assapi.Ani{{"id": "1"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	result, err := Restore(context.Background(), cfg, RestoreOptions{Path: path, Prune: true}, RestoreHooks{
		PruneConfirm: func(string, []string) bool { return false },
	})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("取消时应回 ErrCanceled，实际 %v", err)
	}
	if len(fake.deleted) != 0 {
		t.Errorf("取消之后一条都不该删，实际 %v", fake.deleted)
	}
	// 取消之前已经还原的那些是真的还原了 —— 只统计，不回滚。
	if result.Restored != 1 {
		t.Errorf("还原计数应保留，实际 %+v", result)
	}
}

func TestRestoreSkipsUnknownInstance(t *testing.T) {
	fake := &fakeInstance{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg, _ := testConfig(t, server.URL)

	path, err := Write(t.TempDir(), File{Version: Version, Instances: []Instance{{
		Name:  "早就删掉的实例",
		Items: []assapi.Ani{{"id": "1"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	result, err := Restore(context.Background(), cfg, RestoreOptions{Path: path}, RestoreHooks{
		MissingInstance: func(name string) { missing = append(missing, name) },
	})
	if err != nil {
		t.Fatalf("配置里没有那个实例不是错误，实际 %v", err)
	}
	if len(missing) != 1 || missing[0] != "早就删掉的实例" {
		t.Errorf("应回调报出跳过了谁，实际 %v", missing)
	}
	if result.Restored != 0 {
		t.Errorf("跳过的实例不该计进还原数，实际 %+v", result)
	}
}

func TestRestoreCountsFailures(t *testing.T) {
	fake := &fakeInstance{failSet: true}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	cfg, _ := testConfig(t, server.URL)

	path, err := Write(t.TempDir(), File{Version: Version, Instances: []Instance{{
		Name:  "主库",
		Items: []assapi.Ani{{"id": "1", "title": "还原不了的"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Restore(context.Background(), cfg, RestoreOptions{Path: path}, RestoreHooks{})
	if err != nil {
		t.Fatalf("单条失败不该让整次还原报错，实际 %v", err)
	}
	if result.Failures != 1 || result.Restored != 0 {
		t.Errorf("失败要计数，实际 %+v", result)
	}
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "主库") {
		t.Errorf("错误里要说清是哪个实例哪一条，实际 %v", result.Errors)
	}
}

func TestExtrasAndHelpers(t *testing.T) {
	current := []assapi.Ani{{"id": "1"}, {"id": "2"}, {"id": ""}}
	saved := []assapi.Ani{{"id": "1"}}
	extras := Extras(current, saved)
	if len(extras) != 1 || extras[0] != "2" {
		t.Errorf("Extras 应只给存档里没有的 id，实际 %v", extras)
	}
	if ID(assapi.Ani{"id": 5}) != "" {
		t.Error("id 不是字符串时应给空串，而不是硬转")
	}
	if Title(assapi.Ani{}) != "（无标题）" {
		t.Errorf("没有标题时给占位，实际 %q", Title(assapi.Ani{}))
	}
	if Title(assapi.Ani{"mikanTitle": "m"}) != "m" {
		t.Error("mikanTitle 也算标题")
	}
}

func TestDirFollowsWorkspace(t *testing.T) {
	dir := t.TempDir()
	app.SetWorkspace(filepath.Join(dir, "config.yaml"), "")
	if want := filepath.Join(dir, "data", "backup"); Dir() != want {
		t.Errorf("存档目录应是 <数据目录>/backup，实际 %q want %q", Dir(), want)
	}
}
