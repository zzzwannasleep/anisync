// Package cache 是磁盘缓存：把站点返回的字幕组列表存下来，
// 让「后台每隔几分钟自动爬一遍」不必每次都把上游站点重新翻一遍。
//
// 缓存文件放在 <工作目录>/data/cache.json。它只是加速手段：
// 读不出来、写不进去、过期了，都退化成「没有缓存」，不影响同步结果。
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type entry struct {
	SavedAt time.Time       `json:"savedAt"`
	Value   json.RawMessage `json:"value"`
}

type file struct {
	Version int              `json:"version"`
	Entries map[string]entry `json:"entries"`
}

// Store 是一个带 TTL 的键值缓存。
type Store struct {
	mu      sync.Mutex
	path    string
	ttl     time.Duration
	entries map[string]entry
	dirty   bool
	hits    int
	misses  int
}

// Open 打开（或新建）一个缓存。ttl <= 0 或 path 为空时缓存处于停用状态。
func Open(path string, ttl time.Duration) *Store {
	s := &Store{path: path, ttl: ttl, entries: map[string]entry{}}
	if !s.Enabled() {
		return s
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var parsed file
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return s
	}
	now := time.Now()
	for key, item := range parsed.Entries {
		if now.Sub(item.SavedAt) < ttl {
			s.entries[key] = item
		}
	}
	return s
}

// Enabled 表示缓存是否生效。
func (s *Store) Enabled() bool { return s != nil && s.path != "" && s.ttl > 0 }

// Get 取一个值；未命中或已过期时返回 false。
func (s *Store) Get(key string, out any) bool {
	if !s.Enabled() {
		return false
	}
	s.mu.Lock()
	item, ok := s.entries[key]
	if ok && time.Since(item.SavedAt) >= s.ttl {
		delete(s.entries, key)
		s.dirty = true
		ok = false
	}
	if !ok {
		s.misses++
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	if err := json.Unmarshal(item.Value, out); err != nil {
		return false
	}
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	return true
}

// Put 写入一个值，仅改内存，落盘由 Flush 完成。
func (s *Store) Put(key string, value any) {
	if !s.Enabled() {
		return
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.entries[key] = entry{SavedAt: time.Now(), Value: raw}
	s.dirty = true
	s.mu.Unlock()
}

// Flush 把缓存原子地写回磁盘，并顺手丢掉过期条目。
func (s *Store) Flush() error {
	if !s.Enabled() {
		return nil
	}
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}
	now := time.Now()
	for key, item := range s.entries {
		if now.Sub(item.SavedAt) >= s.ttl {
			delete(s.entries, key)
		}
	}
	// 按键排序，保证文件内容稳定，便于 diff 与排查
	keys := make([]string, 0, len(s.entries))
	for key := range s.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	payload := file{Version: 1, Entries: make(map[string]entry, len(keys))}
	for _, key := range keys {
		payload.Entries[key] = s.entries[key]
	}
	path := s.path
	s.dirty = false
	s.mu.Unlock()

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Stats 返回命中数、未命中数与条目数。
func (s *Store) Stats() (hits, misses, size int) {
	if s == nil {
		return 0, 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits, s.misses, len(s.entries)
}

// Path 返回缓存文件路径。
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}
