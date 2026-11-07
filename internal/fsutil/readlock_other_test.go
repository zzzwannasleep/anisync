//go:build !windows

package fsutil

import (
	"os"
	"testing"
)

// holdExclusiveLock 在 POSIX 上只会打开一个普通句柄。
//
// 为什么不做成真锁：POSIX 没有强制锁语义 —— `flock` 是建议锁，而
// `os.ReadFile` 根本不看它；rename 也是原子的、不会因为目标被打开而失败。
// 也就是说**这个窗口在 POSIX 上不存在**，生产里读日志不会因为并发写而失败。
//
// 所以这里返回 blocksReads = false：测试只覆盖「最终必须读成功」这条契约，
// 不去伪造一个 POSIX 上不存在的失败。伪造出来的红/绿只会骗自己。
func holdExclusiveLock(t *testing.T, path string) (func(), bool) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("打开文件失败: %v", err)
	}
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		_ = file.Close()
	}, false
}

// missingIsTransient 在 POSIX 上为 false：rename 是原子的，替换文件时
// 目标名字不会消失，所以「不存在」就是真的不存在（不该重试）。
func missingIsTransient() bool { return false }
