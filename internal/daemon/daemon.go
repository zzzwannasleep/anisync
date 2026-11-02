// Package daemon 是常驻的定时同步循环。
//
// 它解决的是「无人值守」这一档需求里最容易做错的四件事：
//
//  1. **每轮重读配置。** 设置页保存时构造的是一份全新的 Config 对象
//     （internal/webui 里 `s.cfg = next`），启动时抓一次指针的调度器
//     会永远跑旧规则 —— 改完规则不重启，看上去生效了，其实没有。
//  2. **间隔可热改。** 启动时按当时的值建一个 ticker，之后改间隔要重启才生效；
//     启动时是 0 的话后台更是再也不会醒。这里每轮都重新取间隔，
//     并把长睡眠切成小段轮询，配置一改就按新节奏走。
//  3. **崩溃不带走进程。** 一个订阅把规则喂炸，未捕获的 panic 会让整个常驻进程退出。
//     无人值守时这意味着「它再也不会自己跑了」，而且没人看见。
//  4. **失败要退避，但不能放弃。** 代理抖一下就让这一轮彻底作废、等下一个完整间隔，
//     在 6 小时间隔下等于半天不修。这里按 1/2/5/10/30 分钟退避重试。
package daemon

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/journal"
)

// Outcome 是一轮同步的结果，由调用方（main）填。
type Outcome struct {
	// Writable 是计划里本可以写入的条数。
	Writable int
	// Updated / Added 是真正写成功的条数。
	Updated int
	Added   int
	// Skipped 是按规则没有可用候选、或被跳过的订阅条数。
	Skipped int
	// Errors 是逐条写入失败的原因。
	Errors []string
	// Note 是一句人话总结；为空时由本包按数字拼一句。
	Note string
	// Busy 表示这一轮其实没跑：同一进程里上一轮还没结束，所以让开了。
	//
	// 它必须和「成功」分开：当成成功会让调度器把 lastRun 推后一整个间隔，
	// 于是「真该跑的这一次」被吞掉，而日志还写着一切正常。
	Busy bool
}

// busyRetry 是「让开」之后多久再来。
//
// 撞车只说明此刻有人在写，不是配置或网络的问题，所以不该走失败退避，
// 也不该等到下一个完整间隔。
const busyRetry = time.Minute

// Options 是循环的依赖，全部可注入，方便测试。
type Options struct {
	// Load 重新读一遍配置。每轮都会调用。
	Load func() (*config.Config, error)
	// Run 跑一轮：读实例 → 建计划 → 写入。
	Run func(ctx context.Context, cfg *config.Config) (Outcome, error)
	// Interval 从配置里取间隔；返回 0 表示「不自动跑」。
	Interval func(*config.Config) time.Duration
	// RunOnStart 为 true 时进程启动后立刻跑一轮，而不是先等一个间隔。
	RunOnStart bool
	// Idle 是「等下次运行」时的最大睡眠粒度，也是「不自动跑」时的复查间隔。
	//
	// 它的作用不是省电，而是让配置改动在一个很短的窗口内生效：
	// 睡 6 小时和睡 30 秒再算一次「现在到点了吗」，后者能在半分钟内
	// 发现你把间隔从 6 小时改成了 5 分钟。
	Idle time.Duration
	// Backoff 返回连续失败 n 次后等多久再试。
	Backoff func(attempt int) time.Duration
	// Log 是给人看的日志出口；为空则不打印。
	Log io.Writer
	// Record 每跑完一轮（成功、失败、崩溃）都会收到一条记录。
	Record func(journal.Entry)
	// Sleep 睡一段时间，返回 false 表示 ctx 结束。
	Sleep func(ctx context.Context, d time.Duration) bool
	// Now 取当前时间。
	Now func() time.Time
}

// DefaultIdle 是默认的睡眠粒度。
const DefaultIdle = 30 * time.Second

// DefaultBackoff 是默认的失败退避阶梯：1 / 2 / 5 / 10 / 30 分钟，之后一直 30 分钟。
func DefaultBackoff(attempt int) time.Duration {
	switch {
	case attempt <= 0:
		return 0
	case attempt == 1:
		return time.Minute
	case attempt == 2:
		return 2 * time.Minute
	case attempt == 3:
		return 5 * time.Minute
	case attempt == 4:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// Run 跑调度循环，直到 ctx 结束。
//
// 返回值就是 ctx.Err()：正常退出（Ctrl-C / 托盘退出）不算错误，
// 调用方按「退出原因」处理即可。
func Run(ctx context.Context, opts Options) error {
	opts = withDefaults(opts)
	state := loopState{start: opts.Now()}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		cfg, err := opts.Load()
		if err != nil {
			// 配置读不出来时不退出：文件可能正在被编辑器保存，下一轮就好。
			state.failures++
			state.lastFail = opts.Now()
			next := opts.Now().Add(opts.Backoff(state.failures))
			opts.record(journal.Entry{
				At:      opts.Now(),
				Mode:    "daemon",
				OK:      false,
				Error:   "读取配置失败: " + err.Error(),
				NextAt:  next,
				Attempt: state.failures,
			})
			opts.logf("[自动同步] 读取配置失败（第 %d 次）：%v；%s 后重试",
				state.failures, err, opts.Backoff(state.failures).Round(time.Second))
			if !opts.Sleep(ctx, waitFor(opts, next)) {
				return ctx.Err()
			}
			continue
		}

		interval := opts.Interval(cfg)
		if interval <= 0 {
			// 配置说「不自动跑」：进入暂停，定期复查。
			// 关掉期间不累积欠账，重新打开时立刻跑一轮（见 loopState.due）。
			if !state.paused {
				state.paused = true
				opts.logf("[自动同步] 配置里 sync.intervalMinutes = 0，暂不自动同步；改成非 0 会立刻生效")
			}
			if !opts.Sleep(ctx, opts.Idle) {
				return ctx.Err()
			}
			continue
		}
		if state.paused {
			// 刚被重新打开：正常情况下立刻跑一轮，而不是再等一个新间隔。
			state.paused = false
			state.start = opts.Now()
			// 但不清失败计数：配置里改个间隔不等于「实例已经恢复」，
			// 清掉退避记忆会让一个还连不上的实例立刻被重试一轮。
			//
			// 也不无条件抢跑：如果 interval 在 0 与非 0 之间反复变（两台机共享
			// 同一份配置、编辑器和设置页交替保存），每次都抢跑就会变成每 Idle
			// 跑一轮 —— 实测 30 分钟的间隔被跑成 30 秒一轮。
			// 判据用「上次尝试」而不是「上次成功」：一直失败时 lastRun 永远是零，
			// 拿它做判据等于判据恒真，抖动 + 实例挂掉就是最坏的那种组合。
			if state.lastAttempt.IsZero() || opts.Now().Sub(state.lastAttempt) >= interval {
				state.runNow = true
			}
			opts.logf("[自动同步] 已按配置开启：每 %s 一轮", interval)
		}

		if due := state.due(interval, opts); opts.Now().Before(due) {
			if !opts.Sleep(ctx, waitFor(opts, due)) {
				return ctx.Err()
			}
			continue
		}

		state = runOnce(ctx, opts, cfg, interval, state)
	}
}

// loopState 是循环自己的记忆：上次跑完是什么时候、连续失败几次、是否暂停。
type loopState struct {
	start    time.Time
	lastRun  time.Time
	lastFail time.Time
	// lastAttempt 是上一次**尝试**本轮的时间（成功、失败、让开都算）。
	//
	// 和 lastRun 分开是因为「一直失败」时 lastRun 永远是零，
	// 任何拿它当判据的防抖逻辑都会退化成判据恒真。
	lastAttempt time.Time
	failures    int
	paused      bool
	// runNow 表示「下一轮立刻跑」，用于刚被重新启用的那一刻。
	runNow bool
	// snooze 是「这一轮让开了，过一会儿再来」的时间点。
	snooze time.Time
}

// due 算出下一轮应该跑的时间点。
func (s loopState) due(interval time.Duration, opts Options) time.Time {
	if s.runNow {
		return s.start
	}
	// 让开之后按 snooze 重来，而不是等一整个间隔：撞车只说明此刻有人在写。
	if !s.snooze.IsZero() {
		return s.snooze
	}
	if s.failures > 0 {
		return s.lastFail.Add(opts.Backoff(s.failures))
	}
	if !s.lastRun.IsZero() {
		return s.lastRun.Add(interval)
	}
	if opts.RunOnStart {
		return s.start
	}
	return s.start.Add(interval)
}

// runOnce 跑一轮并记一条日志。崩溃在这里被兜住。
func runOnce(ctx context.Context, opts Options, cfg *config.Config, interval time.Duration, state loopState) loopState {
	started := opts.Now()
	state.lastAttempt = started
	var (
		outcome  Outcome
		err      error
		panicked string
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = fmt.Sprint(r)
				err = fmt.Errorf("同步过程崩溃（已兜住，进程继续）: %v", r)
			}
		}()
		outcome, err = opts.Run(ctx, cfg)
	}()
	finished := opts.Now()

	entry := journal.Entry{
		At:       started,
		Mode:     "daemon",
		Millis:   finished.Sub(started).Milliseconds(),
		Writable: outcome.Writable,
		Updated:  outcome.Updated,
		Added:    outcome.Added,
		Skipped:  outcome.Skipped,
		Errors:   outcome.Errors,
		Note:     outcome.Note,
		Panic:    panicked,
	}

	// 进程正在退出时把这一轮记成失败没有意义，也没有人会看。
	if ctxErr := ctx.Err(); ctxErr != nil && err != nil {
		opts.record(entry)
		return state
	}

	// 「上一轮还没跑完，这轮让开」既不是成功也不是失败：
	// 不能推后 lastRun（那会把真该跑的这一次吞掉一整个间隔），
	// 也不该升级成失败退避（那只说明此刻有人在写）。
	if err == nil && outcome.Busy {
		state.runNow = false
		state.snooze = finished.Add(busyRetry)
		entry.OK = true
		entry.Busy = true
		entry.Note = outcome.Note
		entry.NextAt = state.snooze
		opts.record(entry)
		opts.logf("[自动同步] %s；%s 后重试", nonEmpty(outcome.Note, "上一轮还没跑完"), busyRetry)
		return state
	}
	state.snooze = time.Time{}

	if err != nil {
		state.failures++
		state.lastFail = finished
		state.runNow = false
		entry.OK = false
		entry.Error = err.Error()
		entry.Attempt = state.failures
		entry.NextAt = finished.Add(opts.Backoff(state.failures))
		opts.record(entry)
		opts.logf("[自动同步] 失败（连续第 %d 次）：%v；%s 后重试",
			state.failures, err, opts.Backoff(state.failures).Round(time.Second))
		return state
	}

	state.failures = 0
	state.lastRun = finished
	state.runNow = false
	entry.OK = true
	entry.NextAt = finished.Add(interval)
	opts.record(entry)
	opts.logf("[自动同步] %s", summarize(outcome, finished.Sub(started), interval))
	return state
}

// summarize 把一轮结果说成一句话。
func summarize(outcome Outcome, elapsed, interval time.Duration) string {
	note := outcome.Note
	if note == "" {
		switch {
		case outcome.Writable == 0:
			note = "没有需要写入的变更"
		default:
			note = fmt.Sprintf("更新 %d 条，新增 %d 条", outcome.Updated, outcome.Added)
			if len(outcome.Errors) > 0 {
				note += fmt.Sprintf("，失败 %d 条", len(outcome.Errors))
			}
		}
		if outcome.Skipped > 0 {
			note += fmt.Sprintf("；%d 条按规则无候选", outcome.Skipped)
		}
	}
	return fmt.Sprintf("%s（耗时 %s，下次 %s 后）", note, elapsed.Round(time.Millisecond), interval)
}

// waitFor 把「还有多久到点」压到 Idle 以内。
func waitFor(opts Options, due time.Time) time.Duration {
	wait := due.Sub(opts.Now())
	if wait <= 0 {
		return 0
	}
	if opts.Idle > 0 && wait > opts.Idle {
		return opts.Idle
	}
	return wait
}

// nonEmpty 返回第一个非空字符串，都为空时给一个占位。
func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// withDefaults 补齐可选依赖。
func withDefaults(opts Options) Options {
	if opts.Idle <= 0 {
		opts.Idle = DefaultIdle
	}
	if opts.Backoff == nil {
		opts.Backoff = DefaultBackoff
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepCtx
	}
	if opts.Interval == nil {
		opts.Interval = func(*config.Config) time.Duration { return 0 }
	}
	if opts.Record == nil {
		opts.Record = func(journal.Entry) {}
	}
	return opts
}

// sleepCtx 睡 d，ctx 结束时提前返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// logf 是带 nil 保护的日志输出。
func (o Options) logf(format string, args ...any) {
	if o.Log == nil {
		return
	}
	fmt.Fprintf(o.Log, format+"\n", args...)
}

// record 是带 nil 保护的日志落盘。
func (o Options) record(entry journal.Entry) {
	if o.Record == nil {
		return
	}
	o.Record(entry)
}
