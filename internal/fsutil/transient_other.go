//go:build !windows

package fsutil

import (
	"errors"
	"io/fs"
)

// transientLock 判断一个错误是不是「文件正被另一个进程短暂占用」。
//
// POSIX 上 rename 是原子的、不会因为目标被打开而失败，也不会出现「目标暂时
// 不存在」的窗口，所以真正需要重试的只剩「目标存在」这一种；
// 把它留着是为了让上层的重试逻辑在两个平台上走同一条代码路径，
// 而不是各写一份。
//
// **刻意不把 fs.ErrNotExist 算进来**：Windows 上它是「rename 覆盖的中间态」，
// 在 POSIX 上就只是「这个文件真的不在」——那边没有窗口可等，
// 等下去只会让 status 白花几百毫秒。
func transientLock(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrExist)
}

// lockFamily 在 POSIX 上没有对应物：rename 是原子的，打开一个被别的进程
// 打开着的文件也不会失败，所以不存在「对方拿着句柄」这一类错误。
//
// 保留这个名字是为了让重试循环在两个平台上走同一条代码路径
// （都按「长预算 / 短预算」分档），而不是各写一份。
func lockFamily(error) bool { return false }
