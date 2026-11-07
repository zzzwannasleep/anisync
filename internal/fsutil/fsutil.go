// Package fsutil 放几个所有包都需要的文件小工具。
//
// 存在的理由很具体：`lease` 与 `journal` 各自手写了一遍「临时文件 + 改名」，
// 而且都把临时文件命名成 `<path>.<pid>.tmp` —— 同一个进程里两个 goroutine
// 同时写就会互相截断、抢改名，最后落盘的是被别人 truncate 过的空文件。
// 这不是两个地方各写错一次，是同一个模式被抄了两遍，所以抽出来只写一遍。
package fsutil

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// writeSeq 保证同一纳秒内的两次写也能拿到不同的临时文件名。
var writeSeq atomic.Uint64

// 重试预算。**这里有两个数，不是一个** —— 因为它们回答的是两个不同的问题。
//
// lockRetryBudget —— 「对方正拿着句柄」：
// Windows 的 ERROR_SHARING_VIOLATION(32) / ERROR_LOCK_VIOLATION(33)。
// 这类错误里我们唯一能做的是等，而对方放手的时机不由我们决定：
// rename 覆盖的瞬间、杀毒软件对新文件的扫描、云盘同步代理、另一个进程的 fsync，
// 都能把窗口按到几十到几百毫秒。**实测**：整套测试并行跑时，
// 旧预算（5 次 × 5ms ≈ 50ms）会被吃干，于是读到「运行日志: 读取失败」——
// 而重跑一次就好了（见 readwait_test.go 那条确定性测试，修复前 52ms 就放弃）。
//
// 2s 的依据：
//   - 正常窗口是微秒级（rename 原子，读的人打开→读完→关掉）；
//     只有杀毒/云盘/调度停顿会把它拉长，而这些都是「几百毫秒内会走」的量级。
//   - 40 倍于被实测吃干的 50ms；退避上限 100ms 意味着这不是「再多试几次」，
//     而是真的等到对方放手。
//   - 上界必须存在：真被一个编辑器独占锁住时，宁可 2s 后报错，也不能永远挂着。
//   - 交互代价可接受：这条路径**只在确实有争抢时**才会等。
//     现状是 50ms 就失败、用户看到「日志读不出来」还得自己重跑一次；
//     等 200ms 拿到结果，永远好过立刻失败。
//
// accessRetryBudget —— ERROR_ACCESS_DENIED(5) / 目标已存在：
// 同一个错误既可能来自「别人正在 rename 覆盖」，也可能是「你真的读不了、路径写错了」，
// 从错误码上分不出来。所以给一个**短**预算：把瞬时那部分抹平，
// 但不让一个真实的权限错误把交互式命令（status）拖住。
// 250ms = 被实测吃干的 50ms 的 5 倍，仍远低于人能察觉「它卡了」的阈值。
//
// 这两个数**只管文件锁**。网络侧的重试刻意不是这个量级：
// `planner.withRetry`（3 次 / 400·800ms）与 `app.ListAniWithRetry`（2 次 / 300ms）
// 是**后台重试**——失败一次就留到下一轮，没人盯着看；文件锁在这里则是
// **交互式等待**（status 读完就打印）。把秒级预算照抄到网络侧是错的：
// 那会让一次失败请求把同步整体拖住。看到「这里 2s、那里 400ms」不是遗漏，是分工。
const (
	LockRetryBudget   = 2 * time.Second
	AccessRetryBudget = 250 * time.Millisecond
)

// retryFirstWait / retryMaxWait 是退避的起点与上限（配合 ±25% 抖动使用）。
//
// 起点 2ms：第一次重试几乎无感 —— 绝大多数占用在第 1~2 次就消失了，
// 不该为了「万一要等 2 秒」让每次正常读都先睡一觉。
// 上限 100ms：到顶之后就是「每 100ms 看一眼」，2s 预算下约 20 次机会，
// 比死等一次要稳（对方放手的时机本身是抖的）。
const (
	retryFirstWait = 2 * time.Millisecond
	retryMaxWait   = 100 * time.Millisecond
)

// retryBudgetFor 判断一个错误值不值得重试，以及值得等多久。
//
// 分档的理由见上面两个常量：锁类值得等满长预算，
// 权限/存在类只给短预算，其余立即返回。
func retryBudgetFor(err error) (time.Duration, bool) {
	switch {
	case lockFamily(err):
		return LockRetryBudget, true
	case transientLock(err):
		return AccessRetryBudget, true
	default:
		return 0, false
	}
}

// retryTransient 反复执行 op，直到成功、预算用尽，或遇到不可重试的错误。
//
// 预算从**第一次失败**开始算，且按第一次失败的错误类别定，
// 中途不换档：一个操作从「被占用」变成「权限不对」意味着局面已经变了，
// 那时候继续穷追没有意义。
//
// maxBudget > 0 时把总预算压到它以内：给那些「等不起 2 秒」的调用方用 ——
// 例如 config.Load，它在 status / check / 每一次 daemon 轮次的关键路径上。
func retryTransient(op func() error, maxBudget time.Duration) error {
	var (
		lastErr  error
		deadline time.Time
		wait     = retryFirstWait
	)
	for {
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		budget, retryable := retryBudgetFor(err)
		if !retryable {
			return err
		}
		if maxBudget > 0 && budget > maxBudget {
			budget = maxBudget
		}
		if deadline.IsZero() {
			deadline = time.Now().Add(budget)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return lastErr
		}
		sleep := wait
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(withJitter(sleep))
		wait *= 2
		if wait > retryMaxWait {
			wait = retryMaxWait
		}
	}
}

// withJitter 给等待时间加 ±25% 抖动。
//
// 为什么需要：多个进程/goroutine 用同一套退避计划重试同一个路径时，
// 它们会在同一瞬间再次撞上彼此。抖动把这种同步打散 ——
// 代价只是「等得稍不整齐」，收益是「不再集体撞点」。
func withJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	delta := int64(d) / 4
	if delta <= 0 {
		return d
	}
	return d + time.Duration(rand.Int64N(2*delta+1)) - time.Duration(delta)
}

// AtomicWriteFile 原子地覆盖写一个文件，权限为 perm。
//
// 做法是先写临时文件、fsync、再改名：读的人要么看到旧内容、要么看到新内容，
// 不会读到写了一半的文件；临时文件名带 pid + 纳秒 + 自增序号，同进程并发也不会撞名。
//
// **同一个路径只应该有一个写者。** 这里对 Windows 的 rename 失败做了重试兜底，
// 但那只是把「偶发争抢」抹平，不是并发写的许可：需要多写者时，在调用方加锁
// （`journal.Append` 就是这么做的），否则会以很低概率丢掉某一次写入。
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := fmt.Sprintf("%s.%d.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano(), writeSeq.Add(1))

	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	// 先落盘再改名：不然掉电之后可能留下一个 0 字节的目标文件
	// ——「运行日志突然空了」是最难解释的一种现象。
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return renameWithRetry(tmp, path)
}

// renameWithRetry 改名；被占用时按重试预算退避重试。
//
// 与读路径共用同一套预算：写这一侧失败的表现是「运行日志少了一条记录」，
// 而那条记录正是无人值守时唯一的证据 —— 宁可让这次写入多花几百毫秒，
// 也不能静默少一行。预算上界（2s）保证它不会把调用方（journal.Append
// 持有进程内互斥锁）无限拖住。
func renameWithRetry(from, to string) error {
	err := retryTransient(func() error { return os.Rename(from, to) }, 0)
	if err != nil {
		// 放弃时把临时文件清掉，不给下次留下一个「不认识的文件」。
		_ = os.Remove(from)
	}
	return err
}

// ReadFile 读一个文件，对 Windows 的短暂占用按重试预算等待。
//
// 为什么需要：在 Windows 上，文件正被 rename 覆盖的那一瞬间，**打开它也会失败**
// （`The process cannot access the file because it is being used by another process`）。
// 这把「读」变成了会偶发失败的操作，而调用方通常把读失败当成确定结论 ——
// 比如 `lease.Holder` 读不出来就返回「没有持有者」，于是合法持有者续约时
// 被判成 ErrLost 而停写；`journal.Tail` 读不出来则让 `status` 报
// 「运行日志: 读取失败」，而用户重跑一次就好了。
//
// 所以这里等的不是一个「够用的次数」，而是一个**够用的时间**：见文件顶部两个预算。
func ReadFile(path string) ([]byte, error) {
	return ReadFileWithin(path, 0)
}

// ReadFileWithin 与 ReadFile 一样重试，但把总预算压到 maxBudget 以内。
//
// 给「等不起长预算」的调用方用：`config.Load` 在 status / check /
// 每一次 daemon 轮次的关键路径上，而「配置真的不存在（首次运行）」是完全正常的状态。
// 那边要抹平的是「正在被替换」，不是「真的没有」——
// 这两件事从错误码上分不出来，所以只能用短预算一起覆盖
// （见 internal/config 里那句注释）。
//
// maxBudget <= 0 表示按错误类别用默认预算。
func ReadFileWithin(path string, maxBudget time.Duration) ([]byte, error) {
	var out []byte
	err := retryTransient(func() error {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out = raw
		return nil
	}, maxBudget)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Open 打开一个文件，对 Windows 的短暂占用按重试预算等待。
func Open(path string) (*os.File, error) {
	var file *os.File
	err := retryTransient(func() error {
		opened, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		file = opened
		return nil
	}, 0)
	if err != nil {
		return nil, err
	}
	return file, nil
}
