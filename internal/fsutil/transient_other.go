//go:build !windows

package fsutil

import (
	"errors"
	"io/fs"
)

// transientLock 判断一个错误是不是「文件正被另一个进程短暂占用」。
//
// POSIX 上 rename 是原子的、不会因为目标被打开而失败，所以真正需要重试的
// 只剩「目标存在」这一种；把它留着是为了让上层的重试逻辑在两个平台上
// 走同一条代码路径，而不是各写一份。
func transientLock(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrExist)
}
