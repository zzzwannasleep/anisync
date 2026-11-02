package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/journal"
)

// harness 是一个可控时钟 + 可控睡眠的假世界。
//
// 调度循环的所有时间判断都走 Options.Now / Options.Sleep，
// 所以这里不需要真的等一个间隔：睡多久由 sleep 自己推进假时钟。
type harness struct {
	start  time.Time
	now    time.Time
	cancel context.CancelFunc

	maxSleeps int
	sleeps    []time.Duration

	loads  int
	loaded []string // 每次 Load 生成的代号
	runs   []string // 每次 Run 看到的代号
	// runTimes / runSleeps 记录每一轮发生的时刻与当时已经睡过几段，
	// 用来判断「第一轮等了多久」「退避阶梯是不是按预期走的」。
	runTimes  []time.Time
	runSleeps []int

	interval time.Duration
	sleepOn  func(*harness) // 每次 Load 时回调，用来在「运行过程中」改配置

	failRun bool
	panicOn bool
	// busyOn 让 Run 报「这轮让开了」（同一进程里上一轮还没结束）。
	busyOn bool

	records []journal.Entry
	logs    []string
}

func newHarness(maxSleeps int) *harness {
	start := time.Date(2026, 11, 2, 21, 0, 0, 0, time.UTC)
	return &harness{start: start, now: start, maxSleeps: maxSleeps, interval: 2 * DefaultIdle}
}

func (h *harness) options(ctx context.Context) Options {
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel
	return Options{
		Load: func() (*config.Config, error) {
			h.loads++
			code := fmt.Sprintf("gen-%d", h.loads)
			h.loaded = append(h.loaded, code)
			cfg := config.Default()
			// Proxy 在这里被当成「这一代配置」的标记：Run 里读它就能证明
			// 自己拿到的是不是刚读上来的那一份，而不是启动时抓的旧指针。
			cfg.Proxy = code
			if h.sleepOn != nil {
				h.sleepOn(h)
			}
			return cfg, nil
		},
		Run: func(ctx context.Context, cfg *config.Config) (Outcome, error) {
			if h.panicOn {
				panic("规则引擎炸了")
			}
			h.runs = append(h.runs, cfg.Proxy)
			h.runTimes = append(h.runTimes, h.now)
			h.runSleeps = append(h.runSleeps, len(h.sleeps))
			if h.busyOn {
				return Outcome{Busy: true, Note: "上一轮还没跑完，这轮跳过"}, nil
			}
			if h.failRun {
				return Outcome{}, fmt.Errorf("实例连不上")
			}
			return Outcome{Writable: 1, Updated: 1}, nil
		},
		Interval: func(*config.Config) time.Duration { return h.interval },
		Idle:     DefaultIdle,
		Now:      func() time.Time { return h.now },
		Sleep: func(ctx context.Context, d time.Duration) bool {
			h.sleeps = append(h.sleeps, d)
			if len(h.sleeps) > h.maxSleeps {
				h.cancel()
				return false
			}
			h.now = h.now.Add(d)
			return true
		},
		Record: func(entry journal.Entry) { h.records = append(h.records, entry) },
		Log:    writerFunc(func(p []byte) (int, error) { h.logs = append(h.logs, string(p)); return len(p), nil }),
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestRunReloadsConfigEveryCycle 是这次改动最要紧的一条。
//
// 旧实现只在进程启动时读一次配置，之后永久持有那个指针；而 Web 设置页保存时
// 构造的是一份**全新**的 Config（server.go 里 `s.cfg = next`）。
// 于是「在设置页改完规则」这件事对后台调度器完全不可见 —— 改完不重启，永远跑旧规则。
// 这条测试钉住：每一轮都必须看到刚读上来的那一代配置。
func TestRunReloadsConfigEveryCycle(t *testing.T) {
	h := newHarness(20)
	// 跑完第一轮之后改一次配置：第二轮必须看到新一代。
	firstDone := make(chan struct{}, 4)
	h.sleepOn = func(h *harness) {
		if len(h.runs) > 0 && len(h.runs) < 2 {
			select {
			case firstDone <- struct{}{}:
			default:
			}
		}
	}
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) < 2 {
		t.Fatalf("循环没跑够两轮（实际 %d 轮），无法判断是否重读配置", len(h.runs))
	}
	if h.runs[0] == h.runs[1] {
		t.Fatalf("两轮拿到了同一代配置（%s）：配置没有每轮重读", h.runs[0])
	}
	if len(h.loaded) < len(h.runs) {
		t.Fatalf("Load 调用 %d 次少于 Run 的 %d 次，说明有轮次用了缓存下来的配置",
			len(h.loaded), len(h.runs))
	}
}

// TestRunWaitsOneIntervalBeforeFirstRun 记录启动语义：不配置 RunOnStart 时先等一个间隔。
//
// 这是为了不改变已有用户的行为 —— 双击启动托盘就立刻重写一百多条订阅，
// 是个会让人措手不及的惊喜。
func TestRunWaitsOneIntervalBeforeFirstRun(t *testing.T) {
	h := newHarness(6)
	h.interval = 3 * DefaultIdle
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) == 0 {
		t.Fatalf("一个间隔之后应该跑一轮，实际一轮都没跑（睡眠 %v）", h.sleeps)
	}
	// 第一轮必须等满一个间隔：3 段 30 秒 = 90 秒。
	if got := h.runTimes[0].Sub(h.start); got < h.interval {
		t.Fatalf("第一轮在启动后 %v 就跑了，应该先等满一个间隔 %v", got, h.interval)
	}
	if h.runSleeps[0] < 3 {
		t.Fatalf("第一轮之前只睡了 %d 段，不足一个间隔需要的 3 段", h.runSleeps[0])
	}
}

// TestRunResumesImmediatelyWhenIntervalTurnsOn 证明「把间隔从 0 改成非 0 会立刻跑一轮」。
//
// 旧实现里 intervalMinutes = 0 时后台 goroutine 根本不会启动，
// 之后改成非 0 要重启才生效；无人值守场景下这是个死结。
func TestRunResumesImmediatelyWhenIntervalTurnsOn(t *testing.T) {
	h := newHarness(12)
	h.interval = 0
	turnedOn := false
	h.sleepOn = func(h *harness) {
		// 睡够 5 段之后把间隔打开。
		if !turnedOn && len(h.sleeps) >= 5 {
			turnedOn = true
			h.interval = 6 * DefaultIdle
		}
	}
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) == 0 {
		t.Fatalf("把间隔改成非 0 之后应该立刻跑一轮，实际一轮都没跑（睡眠 %v）", h.sleeps)
	}
	// 「立刻」的意思是：打开之后不再等一个完整间隔。
	var afterOn []time.Duration
	for i, d := range h.sleeps {
		if i >= 5 {
			afterOn = append(afterOn, d)
		}
	}
	if len(afterOn) > 0 && afterOn[0] >= 6*DefaultIdle {
		t.Fatalf("打开间隔后第一段睡眠就是 %v，说明还在等一个完整间隔", afterOn[0])
	}
}

// TestRunBacksOffOnFailure 证明失败走的是退避阶梯，而不是「这一轮作废、干等一整个间隔」。
//
// 6 小时间隔下一次代理抖动等于半天不修；这里按 1/2/5/10 分钟退避。
func TestRunBacksOffOnFailure(t *testing.T) {
	h := newHarness(200)
	h.interval = 60 * DefaultIdle // 半小时的间隔
	h.failRun = true
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) < 5 {
		t.Fatalf("半小时间隔下失败退避应该让它在假时钟里跑多轮，实际只跑了 %d 轮", len(h.runs))
	}
	wantGaps := []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute}
	for i, want := range wantGaps {
		if i+1 >= len(h.runTimes) {
			break
		}
		got := h.runTimes[i+1].Sub(h.runTimes[i])
		if got != want {
			t.Fatalf("第 %d 次重试的间隔应该是 %v，实际 %v（退避阶梯没生效）", i+1, want, got)
		}
	}
	if len(h.records) == 0 || h.records[0].OK {
		t.Fatalf("失败的一轮应该记成 OK=false，实际 %+v", h.records)
	}
	if h.records[0].Attempt != 1 {
		t.Fatalf("第一次失败应该记 Attempt=1，实际 %d", h.records[0].Attempt)
	}
	if h.records[0].NextAt.IsZero() {
		t.Fatalf("失败的一轮也应该写出下次运行时间，实际 %+v", h.records[0])
	}
}

// TestRunSurvivesPanic 证明一轮里的崩溃不会带走整个常驻进程。
//
// 未捕获的 panic 在 goroutine 里会直接终止进程：无人值守时这意味着
// 「它再也不会自己跑了」，而且没人看见。所以必须是「这一轮记下来、继续下一轮」。
func TestRunSurvivesPanic(t *testing.T) {
	h := newHarness(10)
	h.panicOn = true
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) != 0 {
		t.Fatalf("panic 的那一轮不该算作跑成功")
	}
	if len(h.records) < 2 {
		t.Fatalf("panic 之后应该继续跑下一轮，实际只记了 %d 条", len(h.records))
	}
	if h.records[0].Panic == "" {
		t.Fatalf("panic 应该被记进日志，实际 %+v", h.records[0])
	}
	if h.records[0].OK || h.records[0].Error == "" {
		t.Fatalf("panic 那一轮应该记成失败并带上原因，实际 %+v", h.records[0])
	}
}

// TestRunBusyDoesNotConsumeAnInterval 证明「这轮让开了」不会被当成成功。
//
// 托盘手点同步和定时那一轮撞在一起时，调度器拿到的是「让开」。
// 如果把它记成成功，lastRun 会被推后一整个间隔 —— 6 小时间隔下，
// 这一次真该跑的同步就此消失 6 小时，而日志写着一切正常。
func TestRunBusyDoesNotConsumeAnInterval(t *testing.T) {
	h := newHarness(20)
	h.interval = 20 * DefaultIdle // 10 分钟的间隔
	h.busyOn = true
	h.runs = nil
	// 立刻跑一轮，好让第一轮就走「让开」分支。
	opts := h.options(context.Background())
	opts.RunOnStart = true
	_ = Run(context.Background(), opts)

	if len(h.runs) < 3 {
		t.Fatalf("让开之后应该每分钟再来一次（而不是等一整个间隔），实际只跑了 %d 轮", len(h.runs))
	}
	for i := 1; i < len(h.runTimes); i++ {
		if gap := h.runTimes[i].Sub(h.runTimes[i-1]); gap != busyRetry {
			t.Fatalf("第 %d 次重试的间隔应该是 %v，实际 %v（让开被当成成功了？）", i, busyRetry, gap)
		}
	}
	for _, entry := range h.records {
		if entry.Busy && (!entry.OK || entry.Error != "") {
			t.Fatalf("让开的记录应该是 OK=true 且没有 error，实际 %+v", entry)
		}
	}
}

// TestRunFlappingWithFailuresDoesNotSpin 是「抖动」的最坏组合：
// 间隔在 0 / 非 0 之间反复变，而且每轮都失败。
//
// 第一版防抖判据用 lastRun，可失败的一轮根本不写 lastRun，于是判据恒真，
// 每次翻转都抢跑 —— 实测 59/59 次间隔都是 30 秒（配置是 30 分钟）。
// 现在判据用 lastAttempt（成功、失败、让开都写），并且不清失败计数。
func TestRunFlappingWithFailuresDoesNotSpin(t *testing.T) {
	h := newHarness(40)
	const configured = 30 * DefaultIdle // 15 分钟
	h.interval = configured
	h.failRun = true
	flip := false
	h.sleepOn = func(h *harness) {
		flip = !flip
		if flip {
			h.interval = 0
		} else {
			h.interval = configured
		}
	}
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) < 2 {
		t.Fatalf("应该至少跑过两轮才能判断间隔，实际 %d 轮", len(h.runs))
	}
	// 允许失败退避（1/2/5/10/30 分钟），但不允许「每 30 秒一轮」。
	for i := 1; i < len(h.runTimes); i++ {
		gap := h.runTimes[i].Sub(h.runTimes[i-1])
		if gap < time.Minute {
			t.Fatalf("第 %d 次与上一次只隔了 %v（配置间隔 %v）：抖动 + 失败被放大成狂跑了",
				i, gap, configured)
		}
	}
}

// TestRunIntervalFlappingDoesNotSpin 证明间隔在 0 / 非 0 之间抖动时不会被放大成狂跑。
//
// 触发条件很现实：两台机共享同一份配置目录、或者编辑器和设置页交替保存。
// 如果每次「重新打开」都无条件抢跑，实测 30 分钟的间隔会被跑成 30 秒一轮
// —— 60 倍放大，足够把上游打到限流。
func TestRunIntervalFlappingDoesNotSpin(t *testing.T) {
	h := newHarness(40)
	h.interval = 30 * DefaultIdle // 15 分钟
	flip := false
	h.sleepOn = func(h *harness) {
		flip = !flip
		if flip {
			h.interval = 0
		} else {
			h.interval = 30 * DefaultIdle
		}
	}
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.runs) > 2 {
		t.Fatalf("间隔在 0/非 0 之间抖动时跑了 %d 轮（睡眠 %v），被放大成狂跑了", len(h.runs), h.sleeps)
	}
}

// TestRunSleepsInSmallSlices 证明长间隔被切成小段轮询。
//
// 不切段的话，把间隔从 12 小时改成 5 分钟要等 12 小时才生效 ——
// 「改了配置没反应」正是这次要修的那类问题。
func TestRunSleepsInSmallSlices(t *testing.T) {
	h := newHarness(3)
	h.interval = 24 * time.Hour
	_ = Run(context.Background(), h.options(context.Background()))

	if len(h.sleeps) == 0 {
		t.Fatalf("应该有睡眠记录")
	}
	for i, d := range h.sleeps {
		if d > DefaultIdle {
			t.Fatalf("第 %d 段睡眠是 %v，超过了 Idle 上限 %v：配置改动要等这么久才被发现", i, d, DefaultIdle)
		}
	}
	if h.loads < len(h.sleeps) {
		t.Fatalf("每段睡眠醒来都应该重读配置：睡 %d 段、只读了 %d 次", len(h.sleeps), h.loads)
	}
}
