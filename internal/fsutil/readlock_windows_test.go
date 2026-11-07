//go:build windows

package fsutil

import (
	"syscall"
	"testing"
)

// holdExclusiveLock 用一个**真的独占句柄**按住文件：dwShareMode = 0。
//
// 这就是 Windows 上 ERROR_SHARING_VIOLATION 的来源，也是本包两个重试
// 真正要处理的那一类错误 —— 用假错误注入只能证明「策略函数会重试」，
// 证明不了「同一个真实场景下 ReadFile 会等到锁放开」。
//
// Go 的 os.Open 默认带 FILE_SHARE_READ|WRITE|DELETE，**不会**制造占用，
// 所以这里必须直接下到 syscall 才能把窗口造出来。
//
// 返回的 release 可以重复调用。
func holdExclusiveLock(t *testing.T, path string) (func(), bool) {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("路径转 UTF-16 失败: %v", err)
	}
	// access = GENERIC_READ，share = 0（谁都不许开），OPEN_EXISTING。
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("独占打开失败: %v", err)
	}
	var once bool
	return func() {
		if once {
			return
		}
		once = true
		_ = syscall.CloseHandle(handle)
	}, true
}

// missingIsTransient 在 Windows 上为 true：替换文件时目标名字会短暂消失
// （ERROR_FILE_NOT_FOUND），所以「不存在」也是可重试的。
func missingIsTransient() bool { return true }
