package bridge

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/lease"
)

// Core 是桥接层的运行时状态。
//
// 一个进程一个：它持有当前工作区、当前配置、事件总线、任务表与取消注册表。
// 所有入口（FFI 的 AniSyncCall、HTTP 节点的 /api/v1/call）都只是往它投一次 Handle。
type Core struct {
	bus   *EventBus
	jobs  *JobManager
	calls *CallRegistry
	logs  *LogRing
	// clientLogs 是 log.write 的限速器（见 clientLogLimit）。
	clientLogs clientLogLimit

	// writeMu 串行化「会写实例 / 会写配置」的动作。
	//
	// 为什么需要：ANI-RSS 的 setAni 是全量覆盖，两条写路径各自算一份目标状态
	// 再各自写回，结果是在两个值之间反复横跳且不报错。
	writeMu sync.Mutex

	mu   sync.Mutex
	cfg  *config.Config
	list map[string]cachedList

	leaseMu sync.Mutex
	held    *lease.Lease
}

// cachedList 是某个实例订阅列表的内存快照（instance.subscriptions 的 refresh=false）。
type cachedList struct {
	at    time.Time
	items []assapi.Ani
	total int
}

// listCacheTTL 是订阅列表内存缓存的时限。
//
// 一条 listAni 响应 1.8MB。界面上的「刷新」按钮点是点得动的，
// 但翻页、切标签、重连事件流不该每次都把它重新拉一遍。
const listCacheTTL = 60 * time.Second

// New 建一个桥接容器。
func New() *Core {
	bus := NewEventBus(DefaultEventBuffer)
	return &Core{
		bus:   bus,
		jobs:  NewJobManager(bus),
		calls: NewCallRegistry(),
		logs:  NewLogRing(logCapacity),
		list:  map[string]cachedList{},
	}
}

// Events 返回事件总线，供 FFI 回调 / SSE 订阅。
func (c *Core) Events() *EventBus { return c.bus }

// Jobs 返回任务管理器。
func (c *Core) Jobs() *JobManager { return c.jobs }

// RecentLogs 返回最近的日志行。
func (c *Core) RecentLogs(limit int) []LogLine { return c.logs.Recent(limit) }

// Cancel 取消一次在飞调用；返回 false 表示没有这个 callId。
func (c *Core) Cancel(callID string) bool { return c.calls.Cancel(callID) }

// Close 释放这个容器持有的资源：在飞任务、事件订阅者、租约。
func (c *Core) Close() {
	c.jobs.CancelAll()
	c.bus.Close()
	c.releaseLease()
}

// Init 切换工作区并重新读配置（core.init）。
//
// 可以重复调用：Flutter 侧靠它换工作区（换一份 config.yaml = 换一个工作区）。
// 第二次调用会释放上一个工作区持有的租约 —— 不释放的话，
// 换了工作区之后 sync.run 会以为自己还持有旧那份配置的写权。
func (c *Core) Init(configPath, dataDir string) (*config.Config, error) {
	c.releaseLease()
	path := app.SetWorkspace(configPath, dataDir)
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cfg = cfg
	c.list = map[string]cachedList{}
	c.mu.Unlock()
	return cfg, nil
}

// Config 返回当前配置；没有显式 init 过时按默认工作区懒加载一次。
func (c *Core) Config() (*config.Config, error) {
	c.mu.Lock()
	cfg := c.cfg
	c.mu.Unlock()
	if cfg != nil {
		return cfg, nil
	}

	loaded, err := config.Load(app.ConfigPath())
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cfg = loaded
	c.mu.Unlock()
	return loaded, nil
}

// SetConfig 换掉当前配置（保存配置之后）。
func (c *Core) SetConfig(cfg *config.Config) {
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
}

// ConfigPath 返回当前配置文件路径。
func (c *Core) ConfigPath() string {
	c.mu.Lock()
	cfg := c.cfg
	c.mu.Unlock()
	if cfg != nil && cfg.Path() != "" {
		return cfg.Path()
	}
	return app.ConfigPath()
}

// beginWrite / endWrite 是写闸门。
func (c *Core) beginWrite() bool { return c.writeMu.TryLock() }

func (c *Core) endWrite() { c.writeMu.Unlock() }

// Write 同步执行一段写操作；拿不到闸门时返回 busy。
func (c *Core) write(fn func() error) error {
	if !c.beginWrite() {
		return Busy("已有一个写操作在飞，请等它结束")
	}
	defer c.endWrite()
	return fn()
}

// log 记一行日志并推一帧事件。
func (c *Core) log(level, text string) {
	c.logs.Add(level, text)
	c.bus.Publish(NewEvent(EventLog, nil, nil, map[string]any{"level": level, "text": text}))
}

// Log 是给 cmd/anisync-core 用的日志入口。
func (c *Core) Log(level, text string) { c.log(level, text) }

// listAni 读一个实例的订阅，带一层很短的缓存（refresh=false 时吃它）。
func (c *Core) listAni(ctx context.Context, cfg *config.Config, name string, refresh bool) ([]assapi.Ani, int, error) {
	if !refresh {
		c.mu.Lock()
		hit, ok := c.list[name]
		c.mu.Unlock()
		if ok && time.Since(hit.at) < listCacheTTL {
			return hit.items, hit.total, nil
		}
	}
	items, total, err := app.ListAni(ctx, cfg, name)
	if err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	c.list[name] = cachedList{at: time.Now(), items: items, total: total}
	c.mu.Unlock()
	return items, total, nil
}

// withLease 在一段写操作期间持有单飞租约，结束后交还。
//
// 为什么是「用一段就还」而不是「一直拿着」：这一层不是常驻进程。
// 一次 sync.run 结束后如果还攥着租约，用户接着在命令行跑 `AniSync sync -y`
// 就会撞上 lease_held —— 而那个进程其实什么都没在写。
// 想长期持有写权的是 lease.takeover，那条路走 claimLease。
func (c *Core) withLease(note string, force bool, fn func() error) error {
	c.leaseMu.Lock()
	if c.held != nil {
		// 自己已经拿着（接管过）：续一下心跳就算数，不去抢自己手里的文件。
		if err := c.held.Renew(); err == nil {
			c.leaseMu.Unlock()
			return fn()
		}
		c.held = nil
	}
	c.leaseMu.Unlock()

	hold, err := lease.Acquire(app.LeasePath(), app.LeaseTTL, note, force)
	if err != nil {
		return err
	}
	defer func() { _ = hold.Release() }()
	return fn()
}

// claimLease 抢单飞租约并**长期持有**（lease.takeover 用）。
func (c *Core) claimLease(note string, force bool) (*lease.Lease, error) {
	c.leaseMu.Lock()
	defer c.leaseMu.Unlock()
	if c.held != nil {
		// 自己已经拿着了：续一下心跳就算数，不去抢自己手里的文件。
		if err := c.held.Renew(); err == nil {
			return c.held, nil
		}
		c.held = nil
	}
	hold, err := lease.Acquire(app.LeasePath(), app.LeaseTTL, note, force)
	if err != nil {
		return nil, err
	}
	c.held = hold
	return hold, nil
}

// releaseLease 交还租约。
func (c *Core) releaseLease() {
	c.leaseMu.Lock()
	hold := c.held
	c.held = nil
	c.leaseMu.Unlock()
	if hold != nil {
		_ = hold.Release()
	}
}

// LeaseRef 返回当前持有的租约（可能为 nil）。
func (c *Core) LeaseRef() *lease.Lease {
	c.leaseMu.Lock()
	defer c.leaseMu.Unlock()
	return c.held
}

// handleJSON 是给 HTTP 层用的便捷入口：payload 已经是解析过的 JSON 值。
func (c *Core) handleJSON(ctx context.Context, opName string, payload any, callID string) []byte {
	raw, err := json.Marshal(payload)
	if err != nil {
		return envelope(nil, BadRequest("payload 无法序列化: %v", err))
	}
	return c.Handle(ctx, opName, raw, callID)
}

// HandleJSON 是 handleJSON 的导出形式（webui 节点模式用）。
func (c *Core) HandleJSON(ctx context.Context, opName string, payload any, callID string) []byte {
	return c.handleJSON(ctx, opName, payload, callID)
}

// ensureDir 保证某个目录存在（存档目录等）。
func ensureDir(dir string) error { return os.MkdirAll(dir, 0o755) }

// currentConfig 取当前配置；读取失败按契约映射错误码。
func currentConfig(c *Call) (*config.Config, error) {
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	cfg, err := c.Core.Config()
	if err != nil {
		return nil, Classify(err)
	}
	return cfg, nil
}

// cloneConfig 深拷贝一份配置。
//
// 走 JSON 往返而不是手写字段拷贝：配置结构体每加一个字段就漏一处拷贝，
// 而漏掉的表现是「在界面上改了某栏、保存后没了」。走 JSON 则永远与
// json tag 同步 —— 而 json tag 正是界面读写配置用的那份契约。
//
// 拷贝之后必须重新读外置规则文件与注释布局：这两样都不参与序列化。
func cloneConfig(cfg *config.Config) (*config.Config, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	next := &config.Config{}
	if err := json.Unmarshal(raw, next); err != nil {
		return nil, err
	}
	next.SetPath(cfg.Path())
	if err := next.LoadRuleFiles(); err != nil {
		return nil, err
	}
	if err := next.ReloadLayout(); err != nil {
		return nil, err
	}
	next.Normalize()
	return next, nil
}

// mutateConfig 是「改一份配置副本 → 校验 → 落盘 → 换内存里那份」的统一流程。
//
// 三件事的顺序是刻意的：先改副本（改坏了不污染内存里那份）、
// 再校验（校验不过就不落盘）、最后才落盘并换掉内存里的指针。
func (c *Call) mutateConfig(mutate func(next *config.Config) error) (*config.Config, error) {
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	next, err := cloneConfig(cfg)
	if err != nil {
		return nil, InvalidConfig(err)
	}
	if err := mutate(next); err != nil {
		return nil, err
	}
	next.Normalize()
	if err := next.Validate(); err != nil {
		return nil, InvalidConfig(err)
	}
	var saveErr error
	if werr := c.Core.write(func() error {
		saveErr = next.Save()
		return saveErr
	}); werr != nil {
		return nil, werr
	}
	if saveErr != nil {
		return nil, Internal(saveErr)
	}
	c.Core.SetConfig(next)
	return next, nil
}
