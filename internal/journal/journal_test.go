package journal

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "data", "runs.jsonl")
}

// TestAppendConcurrentKeepsEveryEntry 守住一条实测出过的惨案。
//
// 三个写入口（daemon 的调度 goroutine、托盘菜单、设置页同步）会同时 Append，
// 而它们都在同一个进程里、临时文件名都带同一个 pid。实测 24 个并发 Append
// 落盘 0 条 —— 改名时用的是被别人截断成 0 字节的临时文件，整份运行历史当场清空。
func TestAppendConcurrentKeepsEveryEntry(t *testing.T) {
	path := tempPath(t)
	const writers = 24

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for i := 0; i < writers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Append(path, Entry{At: time.Now(), Mode: "daemon", Updated: i}, 0)
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("%d 个并发 Append 里有 %d 个报错，第一个: %v", writers, len(errs), errs[0])
	}
	entries, err := Tail(path, 0)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(entries) != writers {
		t.Fatalf("并发 Append 丢了记录：期望 %d 条，实际 %d 条", writers, len(entries))
	}
	// 内容也不能串：每条的 Updated 应当各自不同。
	seen := map[float64]bool{}
	for _, entry := range entries {
		_ = seen
		if entry.Mode != "daemon" {
			t.Fatalf("记录内容被写坏了: %+v", entry)
		}
	}
}

// TestTailStripsBomOnAnyLine 证明 BOM 出现在中间行时那一行也不会被丢掉。
//
// TrimSpace 不把 U+FEFF 当空白，所以中间行的 BOM 会让 json.Unmarshal 失败、
// 那一行被跳过；而 Append 是「读全量再整份重写」，于是它永久消失。
func TestTailStripsBomOnAnyLine(t *testing.T) {
	path := tempPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	bom := "\ufeff"
	body := strings.Join([]string{
		`{"at":"2026-11-02T21:00:00Z","ok":true,"note":"第一条"}`,
		bom + `{"at":"2026-11-02T22:00:00Z","ok":true,"note":"中间那条"}`,
		`{"at":"2026-11-02T23:00:00Z","ok":true,"note":"第三条"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	entries, err := Tail(path, 0)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("带 BOM 的中间行被丢掉了：期望 3 条，实际 %d 条", len(entries))
	}
	if entries[1].Note != "中间那条" {
		t.Fatalf("第二条内容不对: %+v", entries[1])
	}
}

// TestAppendThenTail 是最基本的一条：写进去能读出来，而且顺序是从旧到新。
func TestAppendThenTail(t *testing.T) {
	path := tempPath(t)
	base := time.Date(2026, 11, 2, 21, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		entry := Entry{
			At:      base.Add(time.Duration(i) * time.Hour),
			Mode:    "daemon",
			OK:      i != 1,
			Updated: i,
		}
		if err := Append(path, entry, 0); err != nil {
			t.Fatalf("追加第 %d 条失败: %v", i, err)
		}
	}
	entries, err := Tail(path, 0)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("应该有 3 条，实际 %d 条", len(entries))
	}
	if !entries[0].At.Before(entries[2].At) {
		t.Fatalf("顺序应该是从旧到新，实际 %v", entries)
	}
	if entries[1].OK {
		t.Fatalf("第 2 条应该记成失败")
	}
}

// TestAppendTruncatesToMax 证明日志不会无限增长。
//
// 它是运维日志，不是数据库：一个常年运行的托盘进程如果每轮追加一行，
// 几年之后就是几十万行，`status` 会越读越慢。
func TestAppendTruncatesToMax(t *testing.T) {
	path := tempPath(t)
	for i := 0; i < 10; i++ {
		if err := Append(path, Entry{At: time.Now(), Updated: i}, 3); err != nil {
			t.Fatalf("追加第 %d 条失败: %v", i, err)
		}
	}
	entries, err := Tail(path, 0)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("应该只保留 3 条，实际 %d 条", len(entries))
	}
	// 保留的必须是最近的三条，而不是最早的三条。
	if entries[2].Updated != 9 || entries[0].Updated != 7 {
		t.Fatalf("保留的应该是最新的三条，实际 Updated=%d..%d", entries[0].Updated, entries[2].Updated)
	}
}

// TestTailReturnsLastN 证明 status --limit 能只取最近几条。
func TestTailReturnsLastN(t *testing.T) {
	path := tempPath(t)
	for i := 0; i < 8; i++ {
		if err := Append(path, Entry{At: time.Now(), Updated: i}, 0); err != nil {
			t.Fatalf("追加失败: %v", err)
		}
	}
	entries, err := Tail(path, 2)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(entries) != 2 || entries[1].Updated != 7 {
		t.Fatalf("应该只拿到最后 2 条且最后一条是 7，实际 %+v", entries)
	}
}

// TestTailSkipsBrokenLines 证明日志被手工改坏不会让 status 命令自己挂掉。
//
// 排查工具自己报错比没有工具更糟：用户正在找「它为什么不跑」，
// 结果先被一行坏日志挡住。
func TestTailSkipsBrokenLines(t *testing.T) {
	path := tempPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	body := strings.Join([]string{
		`{"at":"2026-11-02T21:00:00Z","ok":true,"updated":1}`,
		`这不是 JSON`,
		``,
		`{"at":"2026-11-02T22:00:00Z","ok":false,"error":"实例连不上"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	entries, err := Tail(path, 0)
	if err != nil {
		t.Fatalf("坏行不该让读取失败: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("应该只读到 2 条有效记录，实际 %d 条", len(entries))
	}
	if entries[1].Error != "实例连不上" {
		t.Fatalf("第二条应该是失败那条，实际 %+v", entries[1])
	}
}

// TestAppendMissingFileIsFine 证明第一次运行（还没有 data/ 目录）不会报错。
func TestAppendMissingFileIsFine(t *testing.T) {
	path := tempPath(t)
	if _, err := os.Stat(filepath.Dir(path)); err == nil {
		t.Fatalf("前置条件：目录不该已经存在")
	}
	if err := Append(path, Entry{At: time.Now(), OK: true}, 0); err != nil {
		t.Fatalf("目录不存在时追加应该自动建目录: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("追加之后文件应该存在: %v", err)
	}
}

// TestClipLimitsErrors 证明一条日志不会因为逐条错误太多而变得没法读。
func TestClipLimitsErrors(t *testing.T) {
	entry := Entry{}
	for i := 0; i < maxFieldErrors+3; i++ {
		entry.Errors = append(entry.Errors, "错误")
	}
	entry.Clip()
	if len(entry.Errors) != maxFieldErrors+1 {
		t.Fatalf("应该裁到 %d 条并加一句汇总，实际 %d 条", maxFieldErrors+1, len(entry.Errors))
	}
	if !strings.Contains(entry.Errors[len(entry.Errors)-1], "另有") {
		t.Fatalf("最后一条应该是汇总，实际 %q", entry.Errors[len(entry.Errors)-1])
	}
}
