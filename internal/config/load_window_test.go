package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/fsutil"
)

// replaceWindow 是「文件暂时不存在」的模拟时长。
//
// 真实窗口是微秒级（改名的中间态），这里取 120ms 是为了让测试确定性：
// 它明显短于 config 用的短预算（fsutil.AccessRetryBudget = 250ms），
// 所以修复后一定能等到文件回来；而修复前是 0ms 就放弃。
const replaceWindow = 120 * time.Millisecond

// TestLoadDoesNotMistakeATransientReplaceForAMissingFile 是 B 的**确定性因果证明**。
//
// 场景：另一个进程正在原子替换 config.yaml（`Config.Save()` 走的就是
// 临时文件 + 改名）。Windows 上那个名字会短暂消失 ——
// 裸 os.ReadFile 撞上它拿到的是「文件不存在」，而 `Load` 把「不存在」
// 当成**首次运行**，于是静默返回一份**默认配置**。
//
// 代价不只是「拿错表」：`status` 会拿回落后的表继续输出，而用户看 status
// 就是为了确认「它还活着、跑对了」。一个静默退化成默认配置的 status 比报错更坏。
//
//	修复前：窗口内 Load 直接返回默认配置（0 个实例）→ 这条测试红
//	修复后：Load 用短预算等过窗口，读到真实内容 → 绿
//
// 注意这条**没有**改变「文件真的不存在 → 返回默认配置」的既有行为：
// 它改的是「正在被替换」不再被误判成「不存在」。
func TestLoadDoesNotMistakeATransientReplaceForAMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	real := "listen: 127.0.0.1:51837\n" +
		"proxy: http://real.example:1\n" +
		"instances:\n  - name: 主库\n    baseUrl: https://ass.example.com/\n    apiKey: k\n"
	if err := os.WriteFile(path, []byte(real), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	// 复刻一次原子替换：先让目标让位，过一会儿再放回来（用 fsutil 的原子写，
	// 与 Config.Save 走的是同一条路径 —— 放回来本身也必须是原子的，
	// 否则读到半截 YAML 会让这条测试测成别的东西）。
	removed := make(chan struct{})
	restored := make(chan struct{})
	go func() {
		_ = os.Remove(path)
		close(removed)
		time.Sleep(replaceWindow)
		if err := fsutil.AtomicWriteFile(path, []byte(real), 0o600); err != nil {
			t.Errorf("放回文件失败: %v", err)
		}
		close(restored)
	}()
	<-removed

	started := time.Now()
	cfg, err := Load(path)
	elapsed := time.Since(started)
	<-restored

	if err != nil {
		t.Fatalf("Load 不该报错（窗口里读不到应当重试，而不是把错抛给调用方）：%v", err)
	}

	if !replaceWindowIsTransient() {
		// POSIX：rename 原子，目标名字不会消失，没有这个窗口可等。
		t.Logf("本平台没有替换窗口（耗时 %s）：Load 读到了 %d 个实例",
			elapsed.Round(time.Millisecond), len(cfg.Instances))
		return
	}

	if len(cfg.Instances) != 1 {
		t.Fatalf("Load 在替换窗口里把文件当成了「不存在」，回落到默认配置"+
			"（%d 个实例，耗时 %s）—— status 会因此输出一张假的表",
			len(cfg.Instances), elapsed.Round(time.Millisecond))
	}
	if cfg.Proxy != "http://real.example:1" {
		t.Errorf("读回来的不是磁盘上那份：proxy=%q", cfg.Proxy)
	}
	if elapsed < replaceWindow/2 {
		t.Errorf("只用了 %s 就读到，说明没有真的等过窗口（%s）",
			elapsed.Round(time.Millisecond), replaceWindow)
	}
	t.Logf("文件消失 %s，Load 等了 %s 后读到真实配置（1 个实例）",
		replaceWindow, elapsed.Round(time.Millisecond))
}

// TestLoadStillReturnsDefaultWhenFileIsReallyMissing 钉住**没有**被改变的那条既有行为。
//
// init 流程依赖它：第一次运行时配置文件就是不存在，这不是错误。
// t15 改的只是「正在被替换」不再被误判成「不存在」，不是把「不存在」变成错误。
func TestLoadStillReturnsDefaultWhenFileIsReallyMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	started := time.Now()
	cfg, err := Load(path)
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("文件不存在是正常状态，不该报错：%v", err)
	}
	if cfg == nil || cfg.Path() != path {
		t.Fatalf("应当返回默认配置且 path 指向该位置，实际 %+v", cfg)
	}
	if len(cfg.Instances) != 0 {
		t.Errorf("默认配置不该有实例，实际 %d 个", len(cfg.Instances))
	}
	// 短预算的上界：真不存在时最多花掉 AccessRetryBudget 左右，而不是 2 秒。
	if elapsed > fsutil.AccessRetryBudget+800*time.Millisecond {
		t.Errorf("「真的不存在」花了 %s：它落进了长预算那一档，"+
			"而 status / check 在关键路径上等不起", elapsed.Round(time.Millisecond))
	}
	t.Logf("真的不存在时耗时 %s（短预算 %s）",
		elapsed.Round(time.Millisecond), fsutil.AccessRetryBudget)
}
