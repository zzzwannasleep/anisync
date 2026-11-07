package bridge

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
)

// ---------- 7.9 status / journal / lease ----------

// statusLimitDefault 是 status.get / journal.tail 的默认条数。
const statusLimitDefault = 5

// statusGetPayload 是 status.get 的 payload。
type statusGetPayload struct {
	Limit int `json:"limit"`
}

// opStatusGet 汇总实例与方案、租约、最近几轮。**不发任何网络请求。**
//
// 这条承诺是刻意的：无人值守时它是唯一一个「随时能问、不问出事」的入口。
// 一旦它开始发请求，断网时它就会变成另一个转圈圈的界面。
func opStatusGet(c *Call) (any, error) {
	var payload statusGetPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	limit := payload.Limit
	if limit <= 0 {
		limit = statusLimitDefault
	}

	summaries := app.SchemeSummaries(cfg)
	views := make([]SchemeView, 0, len(summaries))
	for _, summary := range summaries {
		views = append(views, schemeViewOf(summary))
	}

	entries, tailErr := app.TailJournal(limit)
	if entries == nil {
		entries = []RunEntry{}
	}
	info, held := app.CurrentLease(app.LeasePath())

	data := map[string]any{
		"version":    app.Version,
		"platform":   app.Platform(),
		"configPath": cfg.Path(),
		"dataDir":    app.DataDir(),
		"instances":  views,
		"lease":      leaseViewOf(info, held, leaseTTLSeconds),
		"runs":       entries,
	}
	if tailErr != nil {
		data["journalError"] = tailErr.Error()
	}
	if next, ok := app.NextRunAt(entries); ok {
		data["nextRunAt"] = next.Format("2006-01-02T15:04:05Z07:00")
	}
	return data, nil
}

// journalTailPayload 是 journal.tail 的 payload。
type journalTailPayload struct {
	Limit int `json:"limit"`
}

func opJournalTail(c *Call) (any, error) {
	var payload journalTailPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	limit := payload.Limit
	if limit <= 0 {
		limit = statusLimitDefault
	}
	entries, err := app.TailJournal(limit)
	if err != nil {
		return nil, Classify(err)
	}
	if entries == nil {
		entries = []RunEntry{}
	}
	return map[string]any{"entries": entries, "path": app.JournalPath()}, nil
}

// journalClearPayload 是 journal.clear 的 payload（危险动作，需要 confirm:true）。
type journalClearPayload struct {
	Confirm bool `json:"confirm"`
}

func opJournalClear(c *Call) (any, error) {
	path := app.JournalPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, Classify(err)
	}
	c.Log("warn", "运行日志已清空："+path)
	return map[string]any{"cleared": true, "path": path}, nil
}

// leaseTakeoverPayload 是 lease.takeover 的 payload（危险动作，需要 confirm:true）。
type leaseTakeoverPayload struct {
	Force   bool `json:"force"`
	Confirm bool `json:"confirm"`
}

// opLeaseTakeover 接管单飞租约。
//
// 接管之后**本进程持有它**：这正是「抢了别人的写权」的语义。
// 立刻交还的话，下一个进程照样抢不到（文件还在），
// 用户会觉得「点了接管，什么也没发生」。
func opLeaseTakeover(c *Call) (any, error) {
	var payload leaseTakeoverPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	// payload.force 是契约里的入口（给 UI 的 --force 语义）；
	// 走到这一步已经过了 confirm 闸门，所以默认就是接管。
	_ = payload.Force
	hold, err := c.Core.claimLease("takeover", true)
	if err != nil {
		return nil, Classify(err)
	}
	c.Log("warn", "已接管租约")
	return map[string]any{"ok": true, "lease": leaseViewOf(hold.Info(), true, leaseTTLSeconds)}, nil
}

// ---------- 7.12 log / events ----------

// logRecentPayload 是 log.recent 的 payload。
type logRecentPayload struct {
	Limit int `json:"limit"`
}

func opLogRecent(c *Call) (any, error) {
	var payload logRecentPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	limit := payload.Limit
	if limit <= 0 {
		limit = logCapacity
	}
	lines := c.Core.RecentLogs(limit)
	if lines == nil {
		lines = []LogLine{}
	}
	return map[string]any{"lines": lines}, nil
}

// log.write 的硬限制。
const (
	// maxClientLogText 是一行客户端日志的长度上限。超了**截断**而不是拒收：
	// 一条被截断的诊断信息仍然有用，而被丢掉的那条永远不会再出现。
	maxClientLogText = 2000
	// clientLogPrefix 让客户端写的行与引擎自己写的行在日志里**可区分**。
	// 排查时「这行是谁写的」决定了可信度，不该靠猜。
	clientLogPrefix = "[client] "
)

var clientLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// logWritePayload 是 log.write 的 payload。
type logWritePayload struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

// opLogWrite 让客户端把一行诊断信息写进引擎日志。
//
// 存在的理由：引擎日志是排查时唯一会被人翻的地方，而在此之前
// **客户端侧的任何诊断都没有通道进来**（`log.recent` 是只读的）。
//
// 三条边界，都是刻意的：
//   - **只进内存环形缓冲**：不落盘、不进 data/runs.jsonl ——
//     后者是「每轮同步的结果」，混进客户端行会把「最近 200 轮」淹掉；
//   - **限速**：超出的行丢掉并回 {ok:true, dropped:1}。客户端不能把引擎的
//     日志缓冲刷掉（那是排查资产）；
//   - **不推事件**：只写缓冲，不 publish 回事件流。否则「客户端收到 log 事件
//     → 再 log.write 一行」就是一条没有尽头的回环，而限速只会把回环变成噪音。
//     客户端要回看自己写过的行，走 log.recent。
func opLogWrite(c *Call) (any, error) {
	var payload logWritePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	level := strings.ToLower(strings.TrimSpace(payload.Level))
	if !clientLogLevels[level] {
		return nil, BadRequest("level 只能是 debug / info / warn / error，实际 %q", payload.Level)
	}

	text := payload.Text
	if len([]rune(text)) > maxClientLogText {
		text = string([]rune(text)[:maxClientLogText]) + "…（已截断）"
	}

	allowed, firstDrop := c.Core.clientLogs.allow(time.Now())
	if !allowed {
		if firstDrop {
			// 只喊一次：被限速这件事本身要留痕（否则「我的日志怎么少了」没人能回答），
			// 但每个被丢掉的行都喊一次，就成了用噪音解释噪音。
			c.Core.logs.Add("warn", "客户端日志被限速（每秒上限 "+strconv.Itoa(clientLogBurst)+" 行），后续丢弃")
		}
		return map[string]any{"ok": true, "dropped": 1}, nil
	}

	c.Core.logs.Add(level, clientLogPrefix+text)
	return map[string]any{"ok": true}, nil
}
