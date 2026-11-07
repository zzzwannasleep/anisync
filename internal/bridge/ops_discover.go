package bridge

import (
	"context"
	"strconv"
	"strings"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

// ---------- 7.6 discover ----------

// discoverListPayload 是 discover.list 的 payload。
type discoverListPayload struct {
	JapanOnly *bool `json:"japanOnly"`
	Force     bool  `json:"force"`
}

// opDiscoverList 与设置页的 /api/season 同形。
//
// 判定那一套（subscribedBgmIDs / bgmIDOfAni / foldTitle / listAniWithRetry）在
// internal/app 里只有一份，这里只是把结果套上信封。
func opDiscoverList(c *Call) (any, error) {
	var payload discoverListPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	result, err := app.Season(c.Ctx(), cfg, app.SeasonOptions{
		JapanOnly: payload.JapanOnly,
		Force:     payload.Force,
	})
	if err != nil {
		return nil, Classify(err)
	}
	return result.Payload(), nil
}

// discoverItemPayload 是 discover.apply 里的一条番剧。
type discoverItemPayload struct {
	BgmID   string `json:"bgmId"`
	Title   string `json:"title"`
	JPTitle string `json:"jpTitle"`
}

// discoverApplyPayload 是 discover.apply 的 payload。
type discoverApplyPayload struct {
	BgmIDs    []string              `json:"bgmIds"`
	Items     []discoverItemPayload `json:"items"`
	AsTargets *bool                 `json:"asTargets"`
}

// opDiscoverApply 把选中的番剧写进 targets。
//
// 写 targets 而不是直接写实例：发现结果本来就该先落成配置，
// 再由 plan / sync 走那条已经被验证过的链路 —— 跳过这一步等于让界面
// 拥有一套自己的新增逻辑，而「用了界面的新增」与「用了命令行」结果不一样，
// 是最难查的一类不一致。
func opDiscoverApply(c *Call) (any, error) {
	var payload discoverApplyPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	if len(payload.BgmIDs) == 0 {
		return nil, BadRequest("payload.bgmIds 不能为空")
	}

	meta := map[string]discoverItemPayload{}
	for _, item := range payload.Items {
		if id := strings.TrimSpace(item.BgmID); id != "" {
			meta[id] = item
		}
	}
	// 标题缺省时才回查一次季节表：调用方（界面）通常把 items 一起带过来，
	// 那就不必为了补标题再打一轮上游。
	needLookup := false
	for _, raw := range payload.BgmIDs {
		if _, ok := meta[strings.TrimSpace(raw)]; !ok {
			needLookup = true
			break
		}
	}
	if needLookup {
		if season, seasonErr := app.Season(c.Ctx(), cfg, app.SeasonOptions{}); seasonErr == nil {
			for _, item := range season.Items {
				if _, ok := meta[item.BgmID]; !ok {
					meta[item.BgmID] = discoverItemPayload{BgmID: item.BgmID, Title: item.Title, JPTitle: item.JPTitle}
				}
			}
		}
	}

	added := 0
	next, err := c.mutateConfig(func(next *config.Config) error {
		existing := map[string]bool{}
		for _, target := range next.Targets {
			if target.BgmID != "" {
				existing[target.BgmID] = true
			}
		}
		for _, raw := range payload.BgmIDs {
			id := strings.TrimSpace(raw)
			if id == "" || existing[id] {
				continue
			}
			item := meta[id]
			next.Targets = append(next.Targets, config.Target{
				Title:   item.Title,
				JPTitle: item.JPTitle,
				BgmID:   id,
			})
			existing[id] = true
			added++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.Log("info", "已把 "+itoa(added)+" 部番剧写进 targets")
	return map[string]any{"config": next, "added": added}, nil
}

// discoverAutoAddPayload 是 discover.autoAdd 的 payload。
type discoverAutoAddPayload struct {
	DryRun *bool `json:"dryRun"`
}

// opDiscoverAutoAdd 自动发现并新增订阅（异步）。
func opDiscoverAutoAdd(c *Call) (any, error) {
	var payload discoverAutoAddPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	return c.StartJob("discover.autoAdd", func(ctx context.Context, job *Job) (any, error) {
		cfg, err := currentConfig(c)
		if err != nil {
			return nil, err
		}
		if err := cfg.Validate(); err != nil {
			return nil, InvalidConfig(err)
		}
		dryRun := payload.DryRun != nil && *payload.DryRun

		job.SetMessage("正在扫描本季在放的番剧")
		opts := planner.Options{
			AddMissing: true,
			Discover:   true,
			MaxNew:     cfg.Discover.MaxNew(),
			CachePath:  app.CachePath(),
			Progress: func(done, total int, label string) {
				job.SetMessage(label)
				if total > 0 {
					c.Progress(done, total, label)
				}
			},
		}
		plan, err := planner.Build(ctx, cfg, opts)
		if err != nil {
			return nil, Classify(err)
		}
		planned := 0
		for _, action := range plan.Actions {
			if action.Kind == "add" {
				planned++
			}
		}
		added := 0
		errs := []string{}
		if !dryRun && planned > 0 {
			job.SetMessage("正在写入实例")
			updated, addedCount, applyErrs := planner.Apply(ctx, cfg, plan)
			_ = updated
			added = addedCount
			errs = append(errs, applyErrs...)
		}
		c.Log("info", "自动发现完成：计划新增 "+itoa(planned)+" 条，实际写成 "+itoa(added)+" 条")
		return map[string]any{"planned": planned, "added": added, "errors": errs}, nil
	})
}

// itoa 是 strconv.Itoa 的短别名，让上面的日志拼接读起来短一点。
func itoa(n int) string { return strconv.Itoa(n) }
