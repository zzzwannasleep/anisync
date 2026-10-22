package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPutGetFlushRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store := Open(path, time.Hour)
	store.Put("k", []string{"a", "b"})
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("缓存文件应落盘: %v", err)
	}

	reopened := Open(path, time.Hour)
	var got []string
	if !reopened.Get("k", &got) {
		t.Fatal("重新打开后应命中缓存")
	}
	if len(got) != 2 || got[0] != "a" {
		t.Fatalf("缓存内容不对: %v", got)
	}
}

func TestExpiredEntryIsMiss(t *testing.T) {
	store := Open(filepath.Join(t.TempDir(), "cache.json"), time.Millisecond)
	store.Put("k", 1)
	time.Sleep(20 * time.Millisecond)
	var got int
	if store.Get("k", &got) {
		t.Fatal("过期条目不应命中")
	}
}

func TestDisabledWhenTTLZero(t *testing.T) {
	store := Open(filepath.Join(t.TempDir(), "cache.json"), 0)
	if store.Enabled() {
		t.Fatal("ttl=0 时缓存应停用")
	}
	store.Put("k", 1)
	if err := store.Flush(); err != nil {
		t.Fatalf("停用时 Flush 应安全返回: %v", err)
	}
	var got int
	if store.Get("k", &got) {
		t.Fatal("停用时不应命中")
	}
}

// planner 在没配缓存路径时会传 nil，所有方法都必须能安全地空跑。
func TestNilStoreIsSafe(t *testing.T) {
	var store *Store
	if store.Enabled() {
		t.Fatal("nil store 不应启用")
	}
	var got int
	if store.Get("k", &got) {
		t.Fatal("nil store 不应命中")
	}
	store.Put("k", 1)
	if err := store.Flush(); err != nil {
		t.Fatalf("nil store Flush 应安全返回: %v", err)
	}
	if store.Path() != "" {
		t.Fatalf("nil store 路径应为空，实际 %q", store.Path())
	}
}

func TestFlushDropsExpiredEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	store := Open(path, time.Millisecond)
	store.Put("old", 1)
	time.Sleep(20 * time.Millisecond)
	store.Put("new", 2)
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush 失败: %v", err)
	}
	reopened := Open(path, time.Hour)
	if _, _, size := reopened.Stats(); size != 1 {
		t.Fatalf("过期条目应在落盘时被丢弃，实际剩 %d 条", size)
	}
}
