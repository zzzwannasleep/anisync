package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// lockHoldDuration 比修复前的重试总预算（约 50ms）长一个数量级。
//
// 这个数是刻意选的：它比「旧预算」明显长，所以修复前一定失败；
// 又远短于交互式命令的耐心上限，所以修复后不该出现「用户等不下去」。
const lockHoldDuration = 1500 * time.Millisecond

// TestReadFileWaitsOutALongLock 是这条修复的**因果证明**，不是「跑了不红」。
//
// 场景就是生产里的那一个：`journal.Tail` 读 data/runs.jsonl 时，常驻进程正在
// `journal.Append` 同一份文件。Windows 上 rename 覆盖的那一瞬间，打开目标文件
// 会拿到 ERROR_SHARING_VIOLATION；若这个占用持续得比重试预算长，
// 读的人就会拿到「运行日志: 读取失败」——而重跑一次就好了，
// 属于最难被报告、也最容易让人失去信任的那类故障。
//
// 测试的做法：用一个**真的独占句柄**把文件按住 lockHoldDuration，
// 然后调 ReadFile，断言它等到锁释放后**读成功**。
//
//	修复前（总预算约 50ms）：稳定失败 —— 预算耗尽时锁还在。
//	修复后（预算按「等得住」重设）：稳定通过，且耗时接近持锁时长。
//
// 这条测试与 TestAtomicWriteFileNeverLeavesPartialContent 分工不同：
// 后者是概率性的守门人（并发读写不许读到半截内容），
// 前者是确定性的因果证明（预算必须真的等得住）。
func TestReadFileWaitsOutALongLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	want := "{\"a\":1}\n"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	release, blocksReads := holdExclusiveLock(t, path)
	t.Cleanup(release)

	go func() {
		time.Sleep(lockHoldDuration)
		release()
	}()

	started := time.Now()
	raw, err := ReadFile(path)
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("被占用 %s 之后 ReadFile 必须等到锁放开再读成功，"+
			"实际等了 %s 就放弃：%v", lockHoldDuration, elapsed.Round(time.Millisecond), err)
	}
	if string(raw) != want {
		t.Errorf("内容不对：want %q got %q", want, string(raw))
	}
	if !blocksReads {
		// POSIX 上 rename 是原子的、打开被占用的文件不会失败，
		// 所以这个窗口在那边根本不存在：断言只覆盖契约（最终必须读成功）。
		t.Logf("本平台没有强制锁语义，只验证了「最终读成功」（耗时 %s）", elapsed.Round(time.Millisecond))
		return
	}
	if elapsed < lockHoldDuration/2 {
		t.Errorf("只等了 %s 就成功，说明没有真的等到锁放开 —— 重试预算还是太短",
			elapsed.Round(time.Millisecond))
	}
	if elapsed > 3*lockHoldDuration {
		t.Errorf("等了 %s，比持锁时间（%s）长太多：预算应当有上限，不能无限等",
			elapsed.Round(time.Millisecond), lockHoldDuration)
	}
	t.Logf("被占用 %s，ReadFile 等了 %s 后读成功", lockHoldDuration, elapsed.Round(time.Millisecond))
}

// missingWindow 是「文件暂时不存在」的模拟时长。
//
// 真实窗口是微秒级（MoveFileEx 让目标让位之后立刻改名），这里取 150ms
// 是为了让它**明显长于一次调度停顿、又短于短预算**（AccessRetryBudget=250ms）：
// 修复前这条测试必须在 0ms 就失败（连一次重试都没有），修复后必须等到文件回来。
const missingWindow = 150 * time.Millisecond

// TestReadFileWaitsOutAMissingWindow 是第二条确定性证据，针对**另一个错误码**。
//
// Windows 的 MoveFileEx(REPLACE_EXISTING) 是「先让目标让位、再把新文件改名过去」，
// 于是目标名字会有一小段时间**根本不存在**。读的人拿到的是
// `The system cannot find the file specified.`（ERROR_FILE_NOT_FOUND），
// 而不是 SHARING_VIOLATION —— 只修「等得更久」是不够的：
// 这个错误码**根本不在可重试集合里**，重试预算再长也是 0ms 就放弃。
//
// 实测到了它：修好预算之后，并发读写那条守门人测试仍以约 5% 的概率失败，
// 报的就是这一句。
func TestReadFileWaitsOutAMissingWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runs.jsonl")
	old := "{\"old\":1}\n"
	fresh := "{\"new\":2}\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatalf("准备文件失败: %v", err)
	}

	// 模拟「另一个进程正在替换这个文件」：先让它消失，过一会儿再放回来。
	//
	// removed 这一等是必须的：不等就可能在文件还好好待着的时候读，
	// 于是读到旧内容、测试当场变成「什么都证明不了」。
	removed := make(chan struct{})
	restore := make(chan struct{})
	go func() {
		_ = os.Remove(path)
		close(removed)
		time.Sleep(missingWindow)
		_ = os.WriteFile(path, []byte(fresh), 0o600)
		close(restore)
	}()
	<-removed

	started := time.Now()
	raw, err := ReadFile(path)
	elapsed := time.Since(started)
	<-restore

	if !missingIsTransient() {
		// POSIX 上 rename 是原子的，目标名字不会消失：那边没有这个窗口，
		// 读不到就是真的读不到（transientLock 刻意不认 ErrNotExist）。
		if err == nil {
			t.Logf("本平台没有替换窗口，读到了 %q", string(raw))
			return
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("POSIX 上应当直接把「不存在」报上来，实际 %v", err)
		}
		t.Logf("POSIX 无替换窗口：立刻报 ErrNotExist（耗时 %s），符合预期", elapsed.Round(time.Millisecond))
		return
	}

	if err != nil {
		t.Fatalf("文件只是暂时被换掉（%s），ReadFile 必须等到它回来："+
			"实际等了 %s 就放弃：%v", missingWindow, elapsed.Round(time.Millisecond), err)
	}
	if string(raw) != fresh {
		t.Errorf("内容不对：want %q got %q", fresh, string(raw))
	}
	if elapsed < missingWindow/2 {
		t.Errorf("只等了 %s 就成功，说明没等到文件被放回来", elapsed.Round(time.Millisecond))
	}
	if elapsed > AccessRetryBudget+500*time.Millisecond {
		t.Errorf("等了 %s：替换窗口属于短预算那一档（%s），不该花到长预算（%s）",
			elapsed.Round(time.Millisecond), AccessRetryBudget, LockRetryBudget)
	}
	t.Logf("文件消失了 %s，ReadFile 等了 %s 后读到新内容",
		missingWindow, elapsed.Round(time.Millisecond))
}

// TestReadFileGivesUpOnPermanentErrors 钉住重试的**上界**，以及「短预算那一档只花短预算」。
//
// 「等得住」不等于「永远等」：真错了必须在有界时间内返回，
// 否则 status 这种交互式命令会挂在一次打不开的日志上。
//
// 两个用例都落在 AccessRetryBudget（250ms）那一档，而不是 LockRetryBudget（2s）：
//
//   - 「文件不存在」在 Windows 上是**可重试**的：MoveFileEx 更换目标时，
//     那个名字会有一小段时间根本不存在，读者拿到的就是「找不到文件」。
//     分不出「真的没有」和「正在被换掉」，所以它只能拿短预算。
//   - 「读目录」拿到的是 fs.ErrPermission，同样与「别人正在 rename」共用一个错误码。
//
// 这条断言钉的就是「它们没有掉进长预算」：真错了最多花掉 250ms 左右。
func TestReadFileGivesUpOnPermanentErrors(t *testing.T) {
	dir := t.TempDir()
	budget := AccessRetryBudget + 500*time.Millisecond

	started := time.Now()
	if _, err := ReadFile(filepath.Join(dir, "没有这个文件")); err == nil {
		t.Error("不存在的文件必须报错（而不是无限等）")
	}
	elapsed := time.Since(started)
	if elapsed > budget {
		t.Errorf("不存在的文件花了 %s：它落进了长预算那一档（LockRetryBudget=%s）",
			elapsed.Round(time.Millisecond), LockRetryBudget)
	}
	t.Logf("不存在的文件耗时 %s（短预算 %s，长预算 %s）",
		elapsed.Round(time.Millisecond), AccessRetryBudget, LockRetryBudget)

	// 目录冒充文件：读它必定失败，而且这个失败没法靠重试变好。
	started = time.Now()
	if _, err := ReadFile(dir); err == nil {
		t.Error("读目录必须报错")
	}
	elapsed = time.Since(started)
	if elapsed > budget {
		t.Errorf("读目录花了 %s：它落进了长预算那一档（LockRetryBudget=%s）",
			elapsed.Round(time.Millisecond), LockRetryBudget)
	}
	t.Logf("读目录耗时 %s", elapsed.Round(time.Millisecond))
}

// TestReadFileWithinHonoursTheCap 钉住「调用方可以要求一个更短的预算」。
//
// config.Load 就在用这条：它在 status / check 的关键路径上，
// 而「配置真的不存在」是完全正常的状态 —— 那里等不起 2 秒。
func TestReadFileWithinHonoursTheCap(t *testing.T) {
	dir := t.TempDir()
	cap := 60 * time.Millisecond

	started := time.Now()
	if _, err := ReadFileWithin(filepath.Join(dir, "没有这个文件"), cap); err == nil {
		t.Error("不存在的文件必须报错")
	}
	elapsed := time.Since(started)
	if elapsed > cap+400*time.Millisecond {
		t.Errorf("给了 %s 的上限，实际花了 %s", cap, elapsed.Round(time.Millisecond))
	}
	t.Logf("短预算 %s 下，不存在的文件耗时 %s", cap, elapsed.Round(time.Millisecond))
}
