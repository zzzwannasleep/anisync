package app

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/daemon"
	"github.com/zzzwannasleep/anisync/internal/journal"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

// 租约参数。
//
// TTL 必须明显大于一轮的最长耗时（同步上下文给的是 15 分钟），
// 否则「正在认真跑的那一轮」会被别人判成已死并接管，两个进程同时写。
// 续约周期压到分钟级，则进程被强杀后最多等一个 TTL 就能被别人接管。
const (
	LeaseTTL        = 10 * time.Minute
	LeaseRenewEvery = 2 * time.Minute
)

// SyncTimeout 是一轮同步的时间上限。
const SyncTimeout = 15 * time.Minute

// SyncResult 是一次同步的结果，形状与设置页 /api/sync 的响应一致。
type SyncResult struct {
	Updated int
	Added   int
	Errors  []string
	Plan    *planner.Plan
}

// SyncFunc 是常驻进程注入的同步入口。
//
// 为什么需要注入：设置页的「立即同步」和后台定时那一轮都在写同一批实例，
// 而 ANI-RSS 的 setAni 是全量覆盖 —— 两条路径各自算一份目标状态再各自写回，
// 结果是在两个值之间反复横跳。注入之后它们排同一把锁、共用同一份租约。
type SyncFunc func(ctx context.Context) (SyncResult, error)

// LoadFunc 重新读一份配置。调用方负责在这里做校验，
// 好让「页面上点同步」与「后台定时那一轮」拿到同一份判断。
type LoadFunc func() (*config.Config, error)

// Load 读配置（不做校验）。
func Load(path string) (*config.Config, error) { return config.Load(path) }

// LoadChecked 读配置并校验，错误信息与既有 CLI / 常驻进程逐字一致。
func LoadChecked(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置校验失败: %w", err)
	}
	return cfg, nil
}

// BuildAndApply 是「读实例 → 建计划 → 写入」这一整轮，托盘、定时任务与设置页共用。
//
// 返回计划是为了设置页：它要把「这次到底改了什么」画出来，
// 而那条路径和定时任务走的是同一段代码，不能自己再实现一遍。
func BuildAndApply(ctx context.Context, cfg *config.Config) (daemon.Outcome, *planner.Plan, error) {
	return BuildAndApplyWith(ctx, cfg, planner.Options{
		AddMissing: cfg.Sync.AddMissing,
		Discover:   cfg.Discover.Enabled,
		MaxNew:     cfg.Discover.MaxNew(),
		CachePath:  CachePath(),
	}, nil)
}

// BuildAndApplyWith 与 BuildAndApply 同一条链路，但允许指定计划选项与进度回调。
//
// 桥接层要它：plan.build / plan.apply / sync.run 的进度事件都是从这一层冒上来的，
// 而进度回调为 nil 时行为与 BuildAndApply 完全一致。
func BuildAndApplyWith(ctx context.Context, cfg *config.Config, opts planner.Options, progress planner.Progress) (daemon.Outcome, *planner.Plan, error) {
	if opts.CachePath == "" {
		opts.CachePath = CachePath()
	}
	opts.Progress = progress
	plan, err := planner.Build(ctx, cfg, opts)
	if err != nil {
		return daemon.Outcome{}, nil, err
	}
	outcome := daemon.Outcome{Writable: CountWritable(plan), Skipped: len(plan.Skipped)}
	if outcome.Writable == 0 {
		return outcome, plan, nil
	}
	updated, added, errs := planner.Apply(ctx, cfg, plan)
	outcome.Updated, outcome.Added, outcome.Errors = updated, added, errs
	return outcome, plan, nil
}

// CountWritable 数一条计划里真正要写入的条数。
func CountWritable(plan *planner.Plan) int {
	count := 0
	if plan == nil {
		return 0
	}
	for _, action := range plan.Actions {
		if action.Kind == "update" || action.Kind == "add" {
			count++
		}
	}
	return count
}

// NewSyncEndpoint 造一个「设置页点同步」用的入口。
//
// 三件事必须和定时任务保持一致，否则同一个按钮在两种模式下行为不同：
//   - 重新读一遍配置（页面上的 cfg 可能还没落盘，也可能被别的进程改过）；
//   - 用调用方给的锁串行化（页面上连点两下不该并发写同一批订阅）；
//   - 写一条运行日志。
func NewSyncEndpoint(load LoadFunc, mu *sync.Mutex, mode string) SyncFunc {
	return func(ctx context.Context) (SyncResult, error) {
		fresh, err := load()
		if err != nil {
			return SyncResult{}, err
		}
		if !mu.TryLock() {
			return SyncResult{Errors: []string{"上一轮还没跑完，这次没有执行"}}, nil
		}
		defer mu.Unlock()
		outcome, plan, err := BuildAndApply(ctx, fresh)
		RecordRun(mode, outcome, err)
		return SyncResult{
			Updated: outcome.Updated,
			Added:   outcome.Added,
			Errors:  outcome.Errors,
			Plan:    plan,
		}, err
	}
}

// IntervalOf 把配置里的间隔换算成 Duration；0 表示不自动跑。
func IntervalOf(cfg *config.Config) time.Duration {
	if cfg.Sync.IntervalMinutes <= 0 {
		return 0
	}
	return time.Duration(cfg.Sync.IntervalMinutes) * time.Minute
}

// RecordRun 给不走调度循环的触发路径（托盘菜单、命令行、桥接层）补一条运行日志。
func RecordRun(mode string, outcome daemon.Outcome, err error) {
	entry := journal.Entry{
		At:       time.Now(),
		Mode:     mode,
		Writable: outcome.Writable,
		Updated:  outcome.Updated,
		Added:    outcome.Added,
		Skipped:  outcome.Skipped,
		Errors:   outcome.Errors,
		Note:     outcome.Note,
		OK:       err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	// 写日志失败要说出来：运行日志是无人值守时唯一的证据，
	// 而「证据写不进去」这件事本身必须有人知道。吞掉错误的后果是
	// 「记录凭空少了」——读者只会以为那几轮根本没跑。
	if appendErr := journal.Append(JournalPath(), entry, journal.DefaultMaxEntries); appendErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ 运行日志写入失败（这一轮的结果没被记下来）: %v\n", appendErr)
	}
}

// SummarizeOutcome 把一轮结果压成一句话，给托盘菜单用。
func SummarizeOutcome(outcome daemon.Outcome) string {
	if outcome.Note != "" {
		return outcome.Note
	}
	if outcome.Writable == 0 {
		return "没有需要写入的变更"
	}
	text := fmt.Sprintf("更新 %d 条，新增 %d 条", outcome.Updated, outcome.Added)
	if len(outcome.Errors) > 0 {
		text += fmt.Sprintf("，失败 %d 条", len(outcome.Errors))
	}
	return text
}
