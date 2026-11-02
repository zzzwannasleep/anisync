// Package fsutil 放几个所有包都需要的文件小工具。
//
// 存在的理由很具体：`lease` 与 `journal` 各自手写了一遍「临时文件 + 改名」，
// 而且都把临时文件命名成 `<path>.<pid>.tmp` —— 同一个进程里两个 goroutine
// 同时写就会互相截断、抢改名，最后落盘的是被别人 truncate 过的空文件。
// 这不是两个地方各写错一次，是同一个模式被抄了两遍，所以抽出来只写一遍。
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// writeSeq 保证同一纳秒内的两次写也能拿到不同的临时文件名。
var writeSeq atomic.Uint64

// renameRetries 是改名失败后的重试次数。
//
// Windows 上 `MoveFileEx(REPLACE_EXISTING)` 在目标文件正被另一个进程替换或
// 打开时返回 ERROR_ACCESS_DENIED，而不是像 POSIX 那样直接成功。实测 512 个
// 并发写同一路径时 486~498 次失败 —— 而调用方（运行日志）把这个错误吞掉了，
// 表现出来是「记录凭空少了」。重试几次就够：真正持有目标文件的那次写都是微秒级。
const renameRetries = 5

// AtomicWriteFile 原子地覆盖写一个文件，权限为 perm。
//
// 做法是先写临时文件、fsync、再改名：读的人要么看到旧内容、要么看到新内容，
// 不会读到写了一半的文件；临时文件名带 pid + 纳秒 + 自增序号，同进程并发也不会撞名。
//
// **同一个路径只应该有一个写者。** 这里对 Windows 的 rename 失败做了重试兜底，
// 但那只是把「偶发争抢」抹平，不是并发写的许可：需要多写者时，在调用方加锁
// （`journal.Append` 就是这么做的），否则会以很低概率丢掉某一次写入。
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := fmt.Sprintf("%s.%d.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano(), writeSeq.Add(1))

	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	// 先落盘再改名：不然掉电之后可能留下一个 0 字节的目标文件
	// ——「运行日志突然空了」是最难解释的一种现象。
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return renameWithRetry(tmp, path)
}

// renameWithRetry 改名；Windows 上被占用时退避重试。
func renameWithRetry(from, to string) error {
	var lastErr error
	for attempt := 0; attempt < renameRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
		if err := os.Rename(from, to); err == nil {
			return nil
		} else if lastErr = err; !transientLock(err) {
			break
		}
	}
	_ = os.Remove(from)
	return lastErr
}

// ReadFile 读一个文件，对 Windows 的短暂占用做几次重试。
//
// 为什么需要：在 Windows 上，文件正被 rename 覆盖的那一瞬间，**打开它也会失败**
// （`The process cannot access the file because it is being used by another process`）。
// 这把「读」变成了会偶发失败的操作，而调用方通常把读失败当成确定结论 ——
// 比如 `lease.Holder` 读不出来就返回「没有持有者」，于是合法持有者续约时
// 被判成 ErrLost 而停写。重试几次就够：rename 是微秒级的。
func ReadFile(path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < renameRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
		raw, err := os.ReadFile(path)
		if err == nil {
			return raw, nil
		}
		lastErr = err
		if !transientLock(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// Open 打开一个文件，对 Windows 的短暂占用做几次重试。
func Open(path string) (*os.File, error) {
	var lastErr error
	for attempt := 0; attempt < renameRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 5 * time.Millisecond)
		}
		file, err := os.Open(path)
		if err == nil {
			return file, nil
		}
		lastErr = err
		if !transientLock(err) {
			return nil, err
		}
	}
	return nil, lastErr
}
