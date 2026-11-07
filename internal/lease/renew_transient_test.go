package lease

import (
	"errors"
	"testing"
	"time"
)

// 两个时长都刻意这么选：
//
//	longBlock    比 fsutil 的最长重试预算（LockRetryBudget = 2s）还长，
//	             所以写心跳**一定失败** —— 而「写失败」正是要分类的那件事。
//	             不这么选就得赌 Windows 回的是 5 还是 32（两档预算不同），
//	             而这条测试必须是确定性的。
//	shortBlock   明显短于预算，用来钉住「瞬时占用会被 fsutil 等掉」。
const (
	longBlock  = 2600 * time.Millisecond
	shortBlock = 150 * time.Millisecond
)

// TestRenewAbsorbsAShortBlock 钉住「预算之内的占用不该被看见」。
//
// 这一条是 t11 的成果（fsutil 的两档预算），在这里作为回归：
// 占用短于预算时，Renew 应当**根本不报错**。
func TestRenewAbsorbsAShortBlock(t *testing.T) {
	path := tempPath(t)
	held, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	defer func() { _ = held.Release() }()

	release, blocks := blockReplace(t, path)
	defer release()
	if !blocks {
		t.Skip("本平台没有「目标被打开则改名失败」的语义，这条路径在那边不存在")
	}
	go func() {
		time.Sleep(shortBlock)
		release()
	}()

	started := time.Now()
	if err := held.Renew(); err != nil {
		t.Fatalf("占用只有 %s（短于预算），Renew 不该报错，实际等 %s 后：%v",
			shortBlock, time.Since(started).Round(time.Millisecond), err)
	}
	t.Logf("占用 %s，Renew 等 %s 后成功（预算内的占用被等掉了）",
		shortBlock, time.Since(started).Round(time.Millisecond))
}

// TestHeartbeatWriteFailureIsNotLeaseLoss 是 A 的**确定性因果证明**。
//
// 场景：租约文件被别的进程按住，按住的时间**超过重试预算** ——
// 于是这一次心跳真的没写进去。但租约还是我的（文件里那个 token 没变）。
//
// 正确反应是「下一拍接着续」，**不是**「我被人接管了，停写」。
// 这条测试钉住分类本身：
//
//	修复前：Renew 把裸的写错误直接返回，errors.Is(err, ErrHeartbeat) 为 false → 红
//	修复后：返回 &HeartbeatError{}，既不是 ErrLost、又能被 ErrHeartbeat 判出来 → 绿
func TestHeartbeatWriteFailureIsNotLeaseLoss(t *testing.T) {
	path := tempPath(t)
	held, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	defer func() { _ = held.Release() }()

	release, blocks := blockReplace(t, path)
	if !blocks {
		t.Skip("本平台没有「目标被打开则改名失败」的语义，这条路径在那边不存在")
	}

	go func() {
		time.Sleep(longBlock)
		release()
	}()

	started := time.Now()
	renewErr := held.Renew()
	elapsed := time.Since(started)
	release()

	if renewErr == nil {
		t.Fatalf("文件被按住 %s（长于预算），心跳不可能写成功，实际却没报错", longBlock)
	}
	if errors.Is(renewErr, ErrLost) {
		t.Errorf("写心跳失败**不是**丢了租约（token 没变）：把它报成 ErrLost 会让常驻进程"+
			"错判自己被接管然后静默停止写入。实际错误：%v", renewErr)
	}
	if !errors.Is(renewErr, ErrHeartbeat) {
		t.Errorf("这次失败必须能被 ErrHeartbeat 判出来，实际：%v", renewErr)
	}
	t.Logf("按住 %s：Renew 等 %s 后报 %q（ErrLost=%v / ErrHeartbeat=%v）",
		longBlock, elapsed.Round(time.Millisecond), renewErr.Error(),
		errors.Is(renewErr, ErrLost), errors.Is(renewErr, ErrHeartbeat))

	// 占用消失之后必须能立刻恢复：这正是「下一拍接着续」的前提。
	recovered := time.Now()
	if err := held.Renew(); err != nil {
		t.Fatalf("占用消失之后应能立刻续约，实际：%v", err)
	}
	t.Logf("占用消失后 %s 就续约成功", time.Since(recovered).Round(time.Millisecond))
}

// TestKeepAliveDoesNotStopOnHeartbeatFailure 是 A 的调用侧证明。
//
// 要求里的原话是「onLost 只在真的丢失时触发」。这里制造一次**写失败**
// （不是丢租约），断言：
//
//	onLost      -> 一次都不能被调用（修复前会被调用一次，然后停止续约）
//	onTransient -> 至少被调用一次（这次心跳没写成，但租约还是我的）
//	之后        -> 心跳恢复正常（占用消失后下一次续约成功）
func TestKeepAliveDoesNotStopOnHeartbeatFailure(t *testing.T) {
	path := tempPath(t)
	held, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	defer func() { _ = held.Release() }()

	release, blocks := blockReplace(t, path)
	if !blocks {
		t.Skip("本平台没有「目标被打开则改名失败」的语义，这条路径在那边不存在")
	}

	lost := make(chan error, 8)
	transient := make(chan error, 8)
	stop := held.KeepAlive(20*time.Millisecond,
		func(err error) { lost <- err },
		func(err error) { transient <- err },
	)
	defer stop()

	// 等到「心跳没写成」被报出来。占用由本句柄一直按着（rename 会一直失败），
	// 所以这里不需要另开 goroutine 计时。
	select {
	case err := <-transient:
		if errors.Is(err, ErrLost) {
			t.Fatalf("onTransient 不该收到 ErrLost：%v", err)
		}
	case err := <-lost:
		t.Fatalf("写心跳失败被当成了丢租约（onLost 被调用）：%v", err)
	case <-time.After(longBlock + 3*time.Second):
		t.Fatal("等不到 onTransient：心跳失败没有被报出来")
	}

	// 关键断言：这一刻绝不能再有 onLost —— 常驻进程会因此静默停写。
	select {
	case err := <-lost:
		t.Fatalf("写失败之后 onLost 被调用了（常驻进程会停写）：%v", err)
	case <-time.After(300 * time.Millisecond):
	}

	// 占用消失之后心跳要能恢复：下一拍续约成功，文件里的心跳时间往前走。
	before := holderHeartbeat(t, path)
	release()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if holderHeartbeat(t, path).After(before) {
			t.Log("占用消失之后心跳恢复正常")
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("占用消失之后心跳没有恢复：KeepAlive 已经停了")
}

// holderHeartbeat 读租约文件里当前的心跳时间。
func holderHeartbeat(t *testing.T, path string) time.Time {
	t.Helper()
	info, ok := Holder(path)
	if !ok {
		t.Fatalf("读不到租约文件：%s", path)
	}
	return info.Heartbeat
}
