//go:build windows

package lease

import (
	"syscall"
	"testing"
)

// blockReplace 用一个「只许读」的句柄按住租约文件。
//
// 为什么不是独占句柄：我们要让 **读成功**（Holder 与 Renew 的第一步）、
// 只让**替换失败**。Windows 的 MoveFileEx(REPLACE_EXISTING) 需要目标句柄
// 带 FILE_SHARE_DELETE；这里只给 FILE_SHARE_READ，于是：
//
//	Read    -> 允许（共享模式包含 READ）
//	Rename  -> 失败（ERROR_SHARING_VIOLATION / ACCESS_DENIED）
//
// 这正是「租约文件被别的进程短暂打开」时真实发生的事。
func blockReplace(t *testing.T, path string) (release func(), blocks bool) {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("路径转 UTF-16 失败: %v", err)
	}
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("打开租约文件失败: %v", err)
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
