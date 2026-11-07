package bridge

import (
	"context"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/daemon"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

// ---------- 7.7 plan ----------

// planBuildPayload 是 plan.build 的 payload。
type planBuildPayload struct {
	Discover bool   `json:"discover"`
	Limit    int    `json:"limit"`
	Instance string `json:"instance"`
}

func opPlanBuild(c *Call) (any, error) {
	var payload planBuildPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, InvalidConfig(err)
	}

	// `instance` 是**具名资源**：给了名字，那个实例就得存在（契约 §7.7）。
	//
	// 为什么必须在这里拦：planner 对 `OnlyInstance` 只是 `continue`
	// （名字对不上就跳过），于是**拼错一个实例名会得到一份与「真的没什么可做」
	// 一模一样的空计划** —— 而「这次没什么可做的」是一个合法且重要的结论。
	// 一个合法的结论不该能通过 typo 到达：那样「我打错了」和「确实没事」就无法区分。
	//
	// 与同族的其它 op 同形：instance.test / instance.subscriptions / subscription.* /
	// probe.run 全都在实例名不存在时拒绝，plan.build 曾是唯一那个例外。
	//
	// 注意空串**不变**：那是「所有实例」的意思（省略/空串都走这条路）。
	// `limit` 也不受影响：它就是个数上限，匹配不到是正确结果。
	if name := firstNonEmpty(payload.Instance); name != "" {
		if _, err := requireInstance(cfg, name); err != nil {
			return nil, err
		}
		// 用规整后的名字往下走：requireInstance 会去掉首尾空白，
		// planner 那边是逐字比较的 —— 传原文会让「校验通过但计划仍是空的」。
		payload.Instance = name
	}

	opts := planOptions(cfg, payload.Discover, payload.Limit, payload.Instance)
	opts.Progress = func(done, total int, label string) {
		// 进度只推事件，不改计划本身；plan.build 的取消靠 callId。
		c.Progress(done, total, label)
	}

	plan, err := planner.Build(c.Ctx(), cfg, opts)
	if err != nil {
		return nil, Classify(err)
	}
	return map[string]any{"plan": plan}, nil
}

// planOptions 由配置与 payload 组装计划选项。
func planOptions(cfg *config.Config, discover bool, limit int, instance string) planner.Options {
	return planner.Options{
		OnlyInstance: instance,
		AddMissing:   cfg.Sync.AddMissing || discover,
		Discover:     discover || cfg.Discover.Enabled,
		MaxNew:       cfg.Discover.MaxNew(),
		Limit:        limit,
		CachePath:    app.CachePath(),
	}
}

// planApplyPayload 是 plan.apply 的 payload。
type planApplyPayload struct {
	Discover bool `json:"discover"`
	Force    bool `json:"force"`
}

func opPlanApply(c *Call) (any, error) {
	var payload planApplyPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	return c.StartJob("plan.apply", func(ctx context.Context, job *Job) (any, error) {
		cfg, err := currentConfig(c)
		if err != nil {
			return nil, err
		}
		if err := cfg.Validate(); err != nil {
			return nil, InvalidConfig(err)
		}
		// 写实例之前先拿租约：只保护「写」不够，读-改-写整段都要在锁里。
		// 跑完就还：这一层不是常驻进程，攥着租约会把命令行的 sync 挡在门外。
		job.SetMessage("正在生成计划")
		var outcome daemon.Outcome
		var plan *planner.Plan
		leaseErr := c.Core.withLease("plan.apply", payload.Force, func() error {
			var applyErr error
			outcome, plan, applyErr = app.BuildAndApplyWith(ctx, cfg, planOptions(cfg, payload.Discover, 0, ""),
				func(done, total int, label string) {
					job.SetMessage(label)
					c.Progress(done, total, label)
				})
			return applyErr
		})
		if leaseErr != nil {
			return nil, Classify(leaseErr)
		}
		app.RecordRun("bridge:plan.apply", outcome, nil)
		return map[string]any{
			"updated": outcome.Updated,
			"added":   outcome.Added,
			"errors":  outcome.Errors,
			"plan":    plan,
		}, nil
	})
}

// ---------- 7.8 sync / job ----------

// syncRunPayload 是 sync.run 的 payload。
type syncRunPayload struct {
	Discover   bool `json:"discover"`
	AddMissing bool `json:"addMissing"`
	Preview    bool `json:"preview"`
	Force      bool `json:"force"`
}

func opSyncRun(c *Call) (any, error) {
	var payload syncRunPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	return c.StartJob("sync.run", func(ctx context.Context, job *Job) (any, error) {
		cfg, err := currentConfig(c)
		if err != nil {
			return nil, err
		}
		if err := cfg.Validate(); err != nil {
			return nil, InvalidConfig(err)
		}

		willWrite := !payload.Preview && !cfg.Sync.Preview

		job.SetMessage("正在生成计划")
		opts := planOptions(cfg, payload.Discover, 0, "")
		opts.AddMissing = payload.AddMissing || cfg.Sync.AddMissing || payload.Discover

		var outcome daemon.Outcome
		var plan *planner.Plan
		run := func() error {
			var runErr error
			outcome, plan, runErr = app.BuildAndApplyWith(ctx, cfg, opts, func(done, total int, label string) {
				job.SetMessage(label)
				c.Progress(done, total, label)
			})
			return runErr
		}

		var runErr error
		if willWrite {
			// 与 CLI 的 `sync -y` 同一条纪律：写入前就要拿到租约，跑完交还。
			runErr = c.Core.withLease("sync.run", payload.Force, run)
		} else {
			runErr = run()
		}
		if runErr != nil {
			app.RecordRun("bridge:sync.run", outcome, runErr)
			return nil, Classify(runErr)
		}
		app.RecordRun("bridge:sync.run", outcome, nil)
		return syncResultPayload(outcome, plan), nil
	})
}

// syncResultPayload 是一轮同步的结果形状。
//
// 同时给出「数字摘要」与完整 plan：界面既要知道「改了几条」，
// 也要能展开看「改了哪几条」，而后者只有 plan 里才有。
func syncResultPayload(outcome daemon.Outcome, plan *planner.Plan) map[string]any {
	return map[string]any{
		"writable": outcome.Writable,
		"updated":  outcome.Updated,
		"added":    outcome.Added,
		"skipped":  outcome.Skipped,
		"errors":   nonNilStrings(outcome.Errors),
		"busy":     outcome.Busy,
		"note":     outcome.Note,
		"plan":     plan,
	}
}

func opJobList(c *Call) (any, error) {
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	jobs := c.Core.jobs.List()
	if jobs == nil {
		jobs = []JobView{}
	}
	return map[string]any{"jobs": jobs}, nil
}

// jobIDPayload 是 job.get / job.cancel 的 payload。
type jobIDPayload struct {
	ID string `json:"id"`
}

func opJobGet(c *Call) (any, error) {
	var payload jobIDPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if firstNonEmpty(payload.ID) == "" {
		return nil, BadRequest("payload.id 不能为空")
	}
	view, ok := c.Core.jobs.Get(payload.ID)
	if !ok {
		return nil, NotFoundNamed(payload.ID, "没有这个任务: %s", payload.ID)
	}
	return map[string]any{"job": view}, nil
}

func opJobCancel(c *Call) (any, error) {
	var payload jobIDPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if firstNonEmpty(payload.ID) == "" {
		return nil, BadRequest("payload.id 不能为空")
	}
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	ok := c.Core.jobs.Cancel(payload.ID)
	return map[string]any{"ok": ok}, nil
}

// leaseTTLSeconds 是 LeaseView 里 expired 的判据。
var leaseTTLSeconds = app.LeaseTTL.Seconds()
