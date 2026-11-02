//go:build windows

package fsutil

import (
	"errors"
	"io/fs"
	"syscall"
)

// transientLock 判断一个错误是不是「文件正被另一个进程短暂占用」。
//
// Windows 上这类错误有两个来源，而且 Go 的映射不一样：
//
//	ERROR_ACCESS_DENIED (5)         -> fs.ErrPermission（能 errors.Is 到）
//	ERROR_SHARING_VIOLATION (32)    -> 原样保留为 syscall.Errno，**不是** fs.ErrPermission
//
// 后者就是「rename 覆盖的瞬间去打开目标文件」时拿到的那个，也是本包两个重试
// 真正要处理的那一类。只认 fs.ErrPermission 会漏掉它 —— 实测 512 并发写同一路径时
// 486~498 次失败，全是 32。
func transientLock(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrExist) {
		return true
	}
	return errors.Is(err, syscall.Errno(32)) || // ERROR_SHARING_VIOLATION
		errors.Is(err, syscall.Errno(33)) // ERROR_LOCK_VIOLATION
}
