// Package journal 把每一轮同步的结果落成一份可追溯的运行日志。
//
// 为什么需要它：无人值守时「它今天到底跑过没有」是唯一真正重要的问题。
// 常驻进程的 stdout 在托盘模式下没人看（Windows 上双击运行根本没有控制台），
// 于是「跑了但一直失败」和「根本没在跑」在用户眼里一模一样。
//
// 文件是 <工作目录>/data/runs.jsonl，一行一条，只保留最近若干条：
// 它是运维日志，不是数据库，不需要无限增长，也不参与同步判断。
package journal

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/fsutil"
)

// DefaultMaxEntries 是保留的最近条数。
//
// 200 条 × 一条几十字节 ≈ 十几 KB，一个正常间隔（6 小时）下能回溯一个多月，
// 而文件小到可以随手打开看。
const DefaultMaxEntries = 200

// Entry 是一轮同步的结果。
type Entry struct {
	At time.Time `json:"at"`
	// Mode 说明这轮是谁触发的：daemon（常驻调度）/ cli / web。
	Mode string `json:"mode,omitempty"`
	// OK 表示这轮完整跑完且没有写入失败。
	OK     bool  `json:"ok"`
	Millis int64 `json:"ms,omitempty"`
	// Writable 是计划里本可以写入的条数；Updated / Added 是真正写成功的条数。
	Writable int `json:"writable,omitempty"`
	Updated  int `json:"updated,omitempty"`
	Added    int `json:"added,omitempty"`
	// Skipped 是按规则没有候选、或被判为无法处理的订阅条数。
	Skipped int `json:"skipped,omitempty"`
	// Errors 是逐条写入失败的原因（最多留前几条，避免日志被刷爆）。
	Errors []string `json:"errors,omitempty"`
	// Error 是整轮失败的原因（拉配置、连不上实例等）。
	Error string `json:"error,omitempty"`
	// Panic 记录被兜住的崩溃，用于「它还在跑，但某一轮炸了」。
	Panic string `json:"panic,omitempty"`
	// Busy 表示这一轮到点时上一轮还没跑完，于是让开了。
	//
	// 单独一个字段而不是混进 OK/Error：它不是成功也不是失败，
	// 是「这次没跑，过一会儿再来」。混成成功会让一次真正该跑的同步
	// 无声地等掉一整个间隔。
	Busy bool `json:"busy,omitempty"`
	// NextAt 是这一轮结束后计划的下次运行时间，便于确认调度还活着。
	NextAt time.Time `json:"nextAt,omitempty"`
	// Attempt 是这一轮是第几次连续失败后的重试。
	Attempt int `json:"attempt,omitempty"`
	// Note 是一句人话总结，直接展示给用户。
	Note string `json:"note,omitempty"`
}

// maxFieldErrors 是一条日志里最多保留多少条逐条错误。
const maxFieldErrors = 5

// Clip 把逐条错误裁到可展示的长度。
func (e *Entry) Clip() {
	if len(e.Errors) > maxFieldErrors {
		e.Errors = append(append([]string(nil), e.Errors[:maxFieldErrors]...),
			fmt.Sprintf("…另有 %d 条", len(e.Errors)-maxFieldErrors))
	}
}

// appendMu 串行化本进程内的所有追加。
//
// 不加它的话，三个写入口（daemon 的调度 goroutine、托盘菜单、设置页同步）
// 会同时「读全量 → 追一行 → 写临时文件 → 改名」，而它们的临时文件名
// 都带同一个 pid。实测 24 个并发 Append 会落盘 0 条：改名时用的是
// 一个已经被别人截断成 0 字节的临时文件，整份运行历史当场被清空。
//
// 而运行日志恰恰是无人值守时「它到底跑没跑」的唯一证据 ——
// 证据链突然空白会被误读成「根本没在跑」。
var appendMu sync.Mutex

// Append 追加一条日志，并把文件裁到 maxEntries 条以内。
//
// 整份重写而不是纯追加：截断需要知道行数，而 200 行的读写成本可以忽略。
// 写盘走临时文件 + 改名，读的人永远看不到写了一半的文件。
func Append(path string, entry Entry, maxEntries int) error {
	if path == "" {
		return nil
	}
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	entry.Clip()

	appendMu.Lock()
	defer appendMu.Unlock()

	entries, _ := Tail(path, maxEntries)
	entries = append(entries, entry)
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}

	var b strings.Builder
	for _, item := range entries {
		raw, err := json.Marshal(item)
		if err != nil {
			continue
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return fsutil.AtomicWriteFile(path, []byte(b.String()), 0o600)
}

// Tail 读最近 n 条，按时间从旧到新。
//
// 坏行直接跳过：日志文件被手工改坏不该让 status 命令报错 ——
// 它是排查工具，自己挂掉比没有更糟。
func Tail(path string, n int) ([]Entry, error) {
	if path == "" {
		return nil, nil
	}
	file, err := fsutil.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []Entry
	scanner := bufio.NewScanner(file)
	// 一行可能带几条错误文本，放宽到 1MB，避免超长行让 Scanner 直接放弃。
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		// 每行都去一次 UTF-8 BOM：用户用记事本另存过、或者拼接文件时，
		// 坏行可能落在任意位置。`strings.TrimSpace` 不把 U+FEFF 当空白，
		// 所以中间行的 BOM 会让 json.Unmarshal 失败 —— 那一行被静默丢掉，
		// 而 Append 是「读全量再整份重写」，于是它永久消失。
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return entries, err
	}
	if n > 0 && len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries, nil
}
