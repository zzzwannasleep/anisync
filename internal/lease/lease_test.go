package lease

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "data", "sync.lock")
}

// TestAcquireIsMutuallyExclusive 是这套租约真正要证明的一条。
//
// 第一版实现是「读 → 判过期 → 写 → 回读确认文件里是我」。那个回读**不构成互斥**：
// 它在自己写完之后、别人写之前完成，所以每个进程都能看到自己的 token。
// 实测 8 个进程同时抢一个过期租约，8 个全都认为自己抢到了。
//
// 现在靠 O_EXCL 独占创建 + 改名让位，所以「同时抢 N 个，恰好 1 个成功」
// 是可以断言的性质，而不是一句注释。
func TestAcquireIsMutuallyExclusive(t *testing.T) {
	const contenders = 16

	cases := []struct {
		name  string
		setup func(path string)
	}{
		{
			name:  "冷启动（文件不存在）",
			setup: func(string) {},
		},
		{
			name: "接管过期租约",
			setup: func(path string) {
				_ = writeInfo(path, Info{
					PID:       999999,
					Token:     "dead-holder",
					StartedAt: time.Now().Add(-time.Hour),
					Heartbeat: time.Now().Add(-time.Hour),
				})
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := tempPath(t)
			testCase.setup(path)

			var (
				start   = make(chan struct{})
				wg      sync.WaitGroup
				mu      sync.Mutex
				winners []*Lease
				held    int
			)
			for i := 0; i < contenders; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					heldLease, err := Acquire(path, time.Minute, "race", false)
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						winners = append(winners, heldLease)
						return
					}
					held++
				}()
			}
			close(start)
			wg.Wait()

			if len(winners) != 1 {
				t.Fatalf("%d 个并发抢租约，应该有恰好 1 个成功，实际 %d 个成功、%d 个被拒",
					contenders, len(winners), held)
			}
			// 文件里也必须是这一个赢家，不能是某个已经退出的。
			current, ok := Holder(path)
			if !ok || current.Token != winners[0].Info().Token {
				t.Fatalf("租约文件里的持有者和唯一的赢家不一致：文件 %q vs 赢家 %q",
					current.Token, winners[0].Info().Token)
			}
			_ = winners[0].Release()
		})
	}
}

// TestRenewAfterTakeoverReportsLost 证明「我已经被接管」这件事能被发现。
//
// 老进程被挂起/机器休眠超过 TTL 后，租约可能已经被别人抢走。它醒来若无脑续约，
// 会把文件重新写成自己的名字 —— 而新持有者毫不知情，于是又变成两个写者。
// 这里钉住：续约必须先校验归属，发现不是自己就报 ErrLost。
func TestRenewAfterTakeoverReportsLost(t *testing.T) {
	path := tempPath(t)
	old, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	taken, err := Acquire(path, time.Minute, "sync", true)
	if err != nil {
		t.Fatalf("--force 接管应该成功: %v", err)
	}
	defer func() { _ = taken.Release() }()

	if err := old.Renew(); !errors.Is(err, ErrLost) {
		t.Fatalf("被接管之后续约应该报 ErrLost，实际 %v", err)
	}
	// 文件必须还是新持有者的 —— 老进程的续约不能把它抢回去。
	current, ok := Holder(path)
	if !ok || current.Token != taken.Info().Token {
		t.Fatalf("老进程的续约把租约抢回去了：文件 %q，新持有者 %q",
			current.Token, taken.Info().Token)
	}
	// 老进程的 Release 也不能删掉新持有者的文件，而且要明确报错。
	if err := old.Release(); !errors.Is(err, ErrLost) {
		t.Fatalf("被接管之后释放应该报 ErrLost（静默成功会让人以为干净退出了），实际 %v", err)
	}
	if _, ok := Holder(path); !ok {
		t.Fatalf("老进程的释放把新持有者的租约删掉了")
	}
}

// TestKeepAliveReportsLost 证明后台续约发现被接管时会回调并停下来。
func TestKeepAliveReportsLost(t *testing.T) {
	path := tempPath(t)
	old, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	lost := make(chan error, 1)
	stop := old.KeepAlive(10*time.Millisecond, func(err error) { lost <- err })
	defer stop()

	taken, err := Acquire(path, time.Minute, "sync", true)
	if err != nil {
		t.Fatalf("接管失败: %v", err)
	}
	defer func() { _ = taken.Release() }()

	select {
	case err := <-lost:
		if !errors.Is(err, ErrLost) {
			t.Fatalf("回调应该是 ErrLost，实际 %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("被接管之后 KeepAlive 应该回调 onLost")
	}
}

// TestAcquireRejectsSecondHolder 是租约存在的全部理由：
// 同一份配置上只允许一个进程在写。
func TestAcquireRejectsSecondHolder(t *testing.T) {
	path := tempPath(t)
	first, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("第一次获取应该成功: %v", err)
	}
	defer func() { _ = first.Release() }()

	_, err = Acquire(path, time.Minute, "sync", false)
	if err == nil {
		t.Fatalf("第二个持有者应该被拒绝")
	}
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("应该返回 ErrHeld，实际 %v", err)
	}
	// 报错必须能让人判断「要不要 --force」：至少要说清是谁、还剩多久。
	if got := err.Error(); got == "" {
		t.Fatalf("拒绝时应该给出可操作的说明")
	}
}

// TestForceTakesOver 证明强杀上一个进程后能立刻接管，不用等 TTL。
func TestForceTakesOver(t *testing.T) {
	path := tempPath(t)
	first, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("第一次获取应该成功: %v", err)
	}
	second, err := Acquire(path, time.Minute, "sync", true)
	if err != nil {
		t.Fatalf("--force 应该能接管: %v", err)
	}
	defer func() { _ = second.Release() }()

	if second.Info().Token == first.Info().Token {
		t.Fatalf("接管后应该是新的持有者标识")
	}
	// 旧持有者已经不再是持有者：它的 Release 必须明确报 ErrLost，
	// 而不是静默成功 —— 静默会让它以为「我干净退出了」，
	// 可实际上它可能一直在和真正的持有者同时写。
	if err := first.Release(); !errors.Is(err, ErrLost) {
		t.Fatalf("旧持有者释放应该报 ErrLost，实际 %v", err)
	}
	if _, ok := Holder(path); !ok {
		t.Fatalf("旧持有者释放时把新持有者的租约删掉了")
	}
}

// TestStaleLeaseIsTakenOver 证明「进程被强杀、没人释放」不会把程序永久锁死。
//
// 这是心跳租约相对文件锁的好处：锁的释放靠时间，不靠别人来解。
func TestStaleLeaseIsTakenOver(t *testing.T) {
	path := tempPath(t)
	old := Info{
		PID:       999999,
		Token:     "dead-holder",
		StartedAt: time.Now().Add(-time.Hour),
		Heartbeat: time.Now().Add(-time.Hour),
		Note:      "tray",
	}
	if err := writeInfo(path, old); err != nil {
		t.Fatalf("写旧租约失败: %v", err)
	}

	fresh, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("过期的租约应该能被直接接管: %v", err)
	}
	defer func() { _ = fresh.Release() }()
	if fresh.Info().Token == old.Token {
		t.Fatalf("接管后应该是新的持有者标识")
	}
}

// TestFreshLeaseIsNotTakenOver 是对称的一条：没过期就不许抢。
func TestFreshLeaseIsNotTakenOver(t *testing.T) {
	path := tempPath(t)
	freshInfo := Info{
		PID:       1,
		Token:     "alive",
		StartedAt: time.Now(),
		Heartbeat: time.Now(),
	}
	if err := writeInfo(path, freshInfo); err != nil {
		t.Fatalf("写租约失败: %v", err)
	}
	if _, err := Acquire(path, time.Minute, "tray", false); !errors.Is(err, ErrHeld) {
		t.Fatalf("没过期的租约应该拒绝接管，实际 %v", err)
	}
}

// TestReleaseAllowsNextAcquire 证明正常退出后能立刻重启。
func TestReleaseAllowsNextAcquire(t *testing.T) {
	path := tempPath(t)
	first, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("第一次获取应该成功: %v", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("释放失败: %v", err)
	}
	if _, ok := Holder(path); ok {
		t.Fatalf("释放之后租约文件应该没了")
	}
	second, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("释放之后应该能重新获取: %v", err)
	}
	_ = second.Release()
	// 再释放一次不该报错（退出路径可能重复调用）。
	if err := second.Release(); err != nil {
		t.Fatalf("重复释放不该报错: %v", err)
	}
}

// TestKeepAliveRefreshesHeartbeat 证明长任务期间租约不会自己过期。
//
// 一轮同步可能跑十几分钟，而 TTL 只有几分钟：只在两轮之间续约，
// 会让「正在认真干活」的进程被别人判成已死并接管 —— 那正是我们要防的并发写。
func TestKeepAliveRefreshesHeartbeat(t *testing.T) {
	path := tempPath(t)
	held, err := Acquire(path, time.Minute, "tray", false)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	defer func() { _ = held.Release() }()

	before, ok := Holder(path)
	if !ok {
		t.Fatalf("租约文件应该存在")
	}
	time.Sleep(30 * time.Millisecond)

	stop := held.KeepAlive(10*time.Millisecond, nil)
	time.Sleep(60 * time.Millisecond)
	stop()

	after, ok := Holder(path)
	if !ok {
		t.Fatalf("续约期间租约文件不该消失")
	}
	if !after.Heartbeat.After(before.Heartbeat) {
		t.Fatalf("心跳时间没有被推进：before=%v after=%v", before.Heartbeat, after.Heartbeat)
	}
	// 续约后仍是同一个持有者。
	if after.Token != before.Token {
		t.Fatalf("续约不该换持有者标识")
	}
}

// TestBrokenLeaseFileIsHeldUntilTrusted 记录一个刻意的保守取舍。
//
// 「文件存在但读不出内容」有两种可能，从外部看一模一样：
//   - 上一个进程写到一半崩了 / 文件被手改坏了；
//   - 另一个进程刚 O_EXCL 创建成功、还没把内容写进去。
//
// 判据只能退到文件年龄：新文件按「活着」处理。宁可让人 --force 一次，
// 也不能让并发争抢里出现第二个赢家 —— 一把假锁比没有锁更危险。
func TestBrokenLeaseFileIsHeldUntilTrusted(t *testing.T) {
	path := tempPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("{这不是 JSON"), 0o600); err != nil {
		t.Fatalf("写坏文件失败: %v", err)
	}
	if _, ok := Holder(path); ok {
		t.Fatalf("坏文件不应该被当成有效租约")
	}
	// 刚写下的坏文件：按「可能是别人正在创建」处理，拒绝。
	if _, err := Acquire(path, time.Minute, "tray", false); !errors.Is(err, ErrHeld) {
		t.Fatalf("刚写下的坏文件应该被当成占用（保守方向），实际 %v", err)
	}
	// 明确知道没有别人在跑时，--force 能清掉它。
	forced, err := Acquire(path, time.Minute, "tray", true)
	if err != nil {
		t.Fatalf("--force 应该能清掉坏的租约文件: %v", err)
	}
	_ = forced.Release()

	// 文件不存在同样安全。
	if _, ok := Holder(filepath.Join(t.TempDir(), "nope")); ok {
		t.Fatalf("不存在的文件不该被当成租约")
	}
}
