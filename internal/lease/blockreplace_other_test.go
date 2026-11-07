//go:build !windows

package lease

import (
	"os"
	"testing"
)

// blockReplace 在 POSIX 上只会打开一个普通句柄。
//
// POSIX 的 rename(2) 是原子的：目标被谁打开着都不影响改名，
// 所以**这条失败路径在那边根本不存在**。返回 blocks=false，
// 调用方据此跳过「写失败」的断言（不伪造一个 POSIX 上不存在的失败）。
func blockReplace(t *testing.T, path string) (release func(), blocks bool) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开租约文件失败: %v", err)
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
