package workdir

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyDir 是 SetRoot 出现**之前**的 Dir() 实现，逐字抄自 8f9eac0。
//
// 存在的唯一理由：钉住「没人调用 SetRoot 时行为逐字节不变」。
// 把旧实现抄到这里，等于把旧行为变成一份可以机器比对的事实 ——
// 而不是靠人读 diff 说「应该没变」。
func legacyDir() string {
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		if root, ok := bundleRoot(dir); ok {
			return root
		}
		if !isTempDir(dir) {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// 没设 root 时，Dir() / ConfigPath() / DataDir() 必须与旧实现完全一致。
//
// 这条测试是「默认路径承诺」的守门人：桌面端「删掉文件夹即卸载干净」
// 靠的就是 Dir() 落在可执行文件同目录，任何一处提前返回都会当场把它打破。
func TestDefaultBehaviorUnchangedWithoutRoot(t *testing.T) {
	if got, ok := Root(); ok {
		t.Fatalf("测试进程里不该有人设过 root，实际 = %q", got)
	}
	if got, want := Dir(), legacyDir(); got != want {
		t.Fatalf("Dir() 与 SetRoot 之前的实现不一致：got %q want %q", got, want)
	}
	if got, want := ConfigPath(), filepath.Join(legacyDir(), ConfigName); got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
	if got, want := DataDir(), filepath.Join(legacyDir(), DataDirName); got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
	// 常量本身也是承诺的一部分：改名字等于改磁盘布局。
	if ConfigName != "config.yaml" || DataDirName != "data" {
		t.Errorf("磁盘布局常量被改了：ConfigName=%q DataDirName=%q", ConfigName, DataDirName)
	}
}

// SetRoot 之后 Dir() 与由它派生的两个路径都必须跟着走。
func TestSetRootOverridesDir(t *testing.T) {
	dir := t.TempDir()
	SetRoot(dir)
	t.Cleanup(func() { SetRoot("") })

	got, ok := Root()
	if !ok || got != dir {
		t.Fatalf("Root() = %q,%v want %q,true", got, ok, dir)
	}
	if Dir() != dir {
		t.Errorf("设了 root 之后 Dir() 应返回它，实际 %q", Dir())
	}
	if want := filepath.Join(dir, ConfigName); ConfigPath() != want {
		t.Errorf("ConfigPath() = %q, want %q", ConfigPath(), want)
	}
	if want := filepath.Join(dir, DataDirName); DataDir() != want {
		t.Errorf("DataDir() = %q, want %q", DataDir(), want)
	}

	// 语义是「切换工作区」而不是「叠加」：第二次调用必须整体替换。
	next := t.TempDir()
	SetRoot(next)
	if Dir() != next {
		t.Errorf("第二次 SetRoot 应整体替换，实际 %q", Dir())
	}

	// 空串 = 恢复默认行为，并且要回到与旧实现一致的那条路。
	SetRoot("   ")
	if _, ok := Root(); ok {
		t.Error("SetRoot 空白串应等价于清除 root")
	}
	if got, want := Dir(), legacyDir(); got != want {
		t.Errorf("清除 root 后应回到默认实现：got %q want %q", got, want)
	}
}

// 显式 root 必须原样使用，不做 EvalSymlinks / 临时目录判定。
//
// 移动端传进来的就是应用私有目录；如果这里「顺手」把临时目录当成 go run 回退掉，
// 工作区会落到进程当前目录 —— 在 Android 上那是只读的 APK 目录。
func TestSetRootKeepsTempLikePath(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "anisync-workdir-should-be-kept")
	SetRoot(dir)
	t.Cleanup(func() { SetRoot("") })
	got := Dir()
	if got != dir {
		t.Errorf("显式 root 应原样返回，实际 %q（想要 %q）", got, dir)
	}
	if strings.Contains(got, "go-build") {
		t.Errorf("显式 root 不该被 go run 回退规则改写，实际 %q", got)
	}
}
