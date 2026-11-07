// Package bridge 是 Go 引擎的操作契约层：op 注册表、视图形状、错误映射、
// 事件总线、异步任务、取消注册表。
//
// 这一层**纯 Go，不 import "C"**：它必须能被普通测试、webui 节点模式与 c-shared
// 入口同时使用。cgo 只出现在 cmd/anisync-core 那一层，五个导出符号把请求转发到这里。
//
// op 名、payload 字段名、data 字段名、错误码、事件 type 全部来自 docs/ops-api.md，
// Dart 侧照着同一份文件硬编码；两边靠 app/test/fixtures/ops.json 对拍。
package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// Payload 是一次调用的原始 payload。
//
// 保持原始字节而不是先解成 map：每个 op 的字段集是固定的，
// 解成 map 再一个个取值会让「字段名打错」变成静默的空值，
// 而契约要求这种错误当场表现成 bad_request。
type Payload json.RawMessage

// Handler 是 op 的实现。
type Handler func(c *Call) (any, error)

// Op 是一条 op 的元数据 + 实现。
//
// **名字与分组只在这张表里声明一次**（internal/bridge/ops.go）。
// 加一个 op 却忘了告诉 Dart，会在 app/test/contract 的对拍测试里当场炸出来。
type Op struct {
	Name    string `json:"name"`
	Group   string `json:"group"`
	Summary string `json:"summary"`
	Async   bool   `json:"async"`
	// Confirm 为真时，payload 里没有 confirm:true 一律 bad_request，不执行。
	Confirm bool `json:"-"`
	// Handler 是实现；元数据与实现同表声明，避免「注册了但没实现」。
	Handler Handler `json:"-"`
}

// Call 是一次调用的上下文：payload、取消、事件出口。
type Call struct {
	Core   *Core
	Op     *Op
	CallID string
	JobID  string

	ctx     context.Context
	payload Payload
}

// Ctx 返回这次调用的上下文（已挂 callId 的取消）。
func (c *Call) Ctx() context.Context { return c.ctx }

// Raw 返回原始 payload 字节。
func (c *Call) Raw() Payload { return c.payload }

// Bind 把 payload 解成某个结构体；payload 为空等价于 {}。
func (c *Call) Bind(v any) error {
	if len(c.payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(c.payload, v); err != nil {
		return BadRequest("payload 解析失败: %v", err)
	}
	return nil
}

// Log 记一行日志：进环形缓冲（log.recent 兜底），同时推一帧 log 事件。
func (c *Call) Log(level, text string) {
	if c.Core == nil {
		return
	}
	c.Core.logs.Add(level, text)
	c.Event(EventLog, map[string]any{"level": level, "text": text})
}

// Progress 推一帧 progress 事件。
func (c *Call) Progress(done, total int, label string) {
	c.Event(EventProgress, map[string]any{"done": done, "total": total, "label": label})
}

// Event 推一帧任意事件，自动带上 callId / jobId。
func (c *Call) Event(kind string, data map[string]any) {
	if c.Core == nil {
		return
	}
	var callID, jobID *string
	if c.CallID != "" {
		id := c.CallID
		callID = &id
	}
	if c.JobID != "" {
		id := c.JobID
		jobID = &id
	}
	c.Core.bus.Publish(NewEvent(kind, callID, jobID, data))
}

// StartJob 启动一个异步 op 并返回 {jobId}。
//
// 写闸门在这一层拿：拿不到就立刻让这个 job 以 busy 失败，
// 而不是排进队列 —— 「点了一次同步」在界面上应该是立刻有回音的。
func (c *Call) StartJob(kind string, run func(ctx context.Context, job *Job) (any, error)) (any, error) {
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	view := c.Core.jobs.Start(kind, func(ctx context.Context, job *Job) (any, error) {
		if !c.Core.beginWrite() {
			return nil, Busy("已有一个写操作在飞，请等它结束")
		}
		defer c.Core.endWrite()
		return run(ctx, job)
	})
	return map[string]any{"jobId": view.ID}, nil
}

// InternalMsg 是 Internal 的字符串版。
func InternalMsg(message string) *Error { return &Error{Code: CodeInternal, Message: message} }

// Envelope 是响应信封：永远是 {"ok":true,"data":{...}} 或 {"ok":false,"error":{...}}。
type Envelope struct {
	OK    bool          `json:"ok"`
	Data  any           `json:"data,omitempty"`
	Error *ErrorPayload `json:"error,omitempty"`
}

// ops 是注册表，按实现顺序声明（见 ops.go）。
var (
	opsMu sync.RWMutex
)

// Ops 返回注册表的副本，顺序即声明顺序（= 实现顺序 = 契约里的分组顺序）。
func Ops() []Op {
	opsOnce.Do(func() { opsCache = opsTable() })
	opsMu.RLock()
	defer opsMu.RUnlock()
	out := make([]Op, len(opsCache))
	copy(out, opsCache)
	return out
}

// Lookup 按名字找一条 op。
func Lookup(name string) (Op, bool) {
	opsOnce.Do(func() { opsCache = opsTable() })
	opsMu.RLock()
	defer opsMu.RUnlock()
	for _, op := range opsCache {
		if op.Name == name {
			return op, true
		}
	}
	return Op{}, false
}

// OpNames 返回全部 op 名（声明顺序）。
func OpNames() []string {
	opsOnce.Do(func() { opsCache = opsTable() })
	opsMu.RLock()
	defer opsMu.RUnlock()
	out := make([]string, 0, len(opsCache))
	for _, op := range opsCache {
		out = append(out, op.Name)
	}
	return out
}

// envelope 把 data / err 渲染成信封 JSON。
func envelope(data any, err error) []byte {
	env := Envelope{OK: err == nil}
	if err != nil {
		payload := Classify(err)
		env.Error = &ErrorPayload{Code: payload.Code, Message: payload.Message, Detail: payload.Detail}
	} else {
		if data == nil {
			data = map[string]any{}
		}
		env.Data = data
	}
	raw, marshalErr := json.Marshal(env)
	if marshalErr != nil {
		// 连错误都序列化不了说明 data 里有不可序列化的东西：退到一条最小信封，
		// 保证调用方永远拿到合法 JSON —— 契约说响应**永远是**两者之一。
		fallback, _ := json.Marshal(Envelope{
			OK:    false,
			Error: &ErrorPayload{Code: CodeInternal, Message: "响应序列化失败: " + marshalErr.Error()},
		})
		return fallback
	}
	return raw
}

// Handle 执行一次调用，返回信封 JSON。任何 op 都不许让进程死。
func (c *Core) Handle(ctx context.Context, opName string, payload []byte, callID string) []byte {
	op, ok := Lookup(opName)
	if !ok {
		// detail 恰好是那个 op 名：`not_found` 有两种含义（「这个 op 不存在」
		// 与「这个名字的资源不存在」），客户端要能把它们分开，
		// 才能把「我的内核版本比节点新」认出来，而不是当成参数写错。
		return envelope(nil, &Error{
			Code:    CodeNotFound,
			Message: "未知的 op: " + opName,
			Detail:  opName,
		})
	}
	if op.Confirm && !payloadConfirmed(payload) {
		return envelope(nil, BadRequest("%s 是危险动作，payload 里必须带 confirm:true", op.Name))
	}

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if callID != "" && !c.calls.Add(callID, cancel) {
		return envelope(nil, BadRequest("callId %s 已经在飞了", callID))
	}
	if callID != "" {
		defer c.calls.Remove(callID)
	}

	call := &Call{Core: c, Op: &op, CallID: callID, ctx: cctx, payload: payload}
	data, err := invoke(op, call)
	// 调用被取消时要报 canceled，而不是把底层那条「context canceled」当 internal：
	// 界面按 canceled 静默处理，按 internal 会弹一条红条。
	if err != nil && cctx.Err() != nil {
		var already *Error
		if !errors.As(err, &already) {
			err = Canceled(err)
		}
	}
	return envelope(data, err)
}

// invoke 调一次 handler，外层统一 recover。
//
// panic 转成 internal 错误 + 一条 log 事件：任何 op 都不许让进程死，
// 这是现有 daemon 的纪律，扩展到这里。
func invoke(op Op, call *Call) (data any, err error) {
	defer func() {
		if r := recover(); r != nil {
			call.Log("error", "op "+op.Name+" 内部崩溃（已兜住）")
			data = nil
			err = Errorf(CodeInternal, "op %s 内部错误（已兜住）: %v", op.Name, r)
		}
	}()
	if op.Handler == nil {
		return nil, InternalMsg("op " + op.Name + " 没有实现")
	}
	return op.Handler(call)
}

// payloadConfirmed 判断 payload 里是不是写着 confirm:true。
func payloadConfirmed(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	var probe struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return false
	}
	return probe.Confirm
}
