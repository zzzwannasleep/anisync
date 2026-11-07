//go:build windows

package fsutil

import (
	"errors"
	"io/fs"
	"syscall"
)

// transientLock 判断一个错误是不是「文件正被另一个进程短暂占用」。
//
// Windows 上这类错误有三个来源，而且 Go 的映射完全不一样：
//
//	ERROR_SHARING_VIOLATION (32) -> 原样保留为 syscall.Errno，**不是** fs.ErrPermission
//	ERROR_ACCESS_DENIED (5)      -> fs.ErrPermission（能 errors.Is 到）
//	ERROR_FILE_NOT_FOUND (2)     -> fs.ErrNotExist（不是 fs.ErrNotExist 吗？是它）
//
// 前两个是「rename 覆盖的瞬间去打开目标文件」时拿到的；**第三个是同一个
// 物理事件的后半段**：Windows 的 MoveFileEx(REPLACE_EXISTING) 是先让目标让位
// 再改名，于是目标名字会有一小段时间**根本不存在**。实测（6 个写者并发、
// 同一个路径）：只认前两个错误码时，读者拿到的是
// `The system cannot find the file specified.` —— 而它在 0ms 就返回了，
// 连一次重试都没有。
//
// 只认 fs.ErrPermission 会漏掉 32；只认 32 会漏掉这个。三个都要。
func transientLock(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrExist) || errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return lockFamily(err)
}

// lockFamily 判断「另一个句柄正拿着这个文件」这一类错误。
//
// 它比 transientLock 窄，意义也不同：这一类**只可能靠等解决**（对方放手之前
// 我们再试一万次也一样），所以它配得上一个长预算（lockRetryBudget）。
// transientLock 里另外那两种（权限 / 已存在）既可能是瞬时的、也可能是真的错了，
// 从错误码上分不出来，只能配短预算。
func lockFamily(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.Errno(32)) || // ERROR_SHARING_VIOLATION
		errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
}
