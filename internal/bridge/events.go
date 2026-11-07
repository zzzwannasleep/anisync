package bridge

import (
	"sync"
	"time"
)

// 事件类型。取值由 docs/ops-api.md §8 固定。
const (
	EventLog              = "log"
	EventProgress         = "progress"
	EventJobStart         = "job.start"
	EventJobDone          = "job.done"
	EventJobFailed        = "job.failed"
	EventLeaseLost        = "lease.lost"
	EventInstanceProgress = "instance.progress"
)

// Event 是一条推给客户端的消息（FFI 回调参数 / SSE 的一行 data）。
//
// callId 与 jobId 用指针：契约要求它们**出现在每一帧里**，
// 没有值时是 null 而不是消失 —— 少一个键会让 Dart 侧的反序列化当场炸。
type Event struct {
	Type   string         `json:"type"`
	TS     string         `json:"ts"`
	CallID *string        `json:"callId"`
	JobID  *string        `json:"jobId"`
	Data   map[string]any `json:"data"`
}

// NewEvent 造一条事件，时间戳统一 UTC RFC3339。
func NewEvent(kind string, callID, jobID *string, data map[string]any) Event {
	if data == nil {
		data = map[string]any{}
	}
	return Event{
		Type:   kind,
		TS:     time.Now().UTC().Format(time.RFC3339),
		CallID: callID,
		JobID:  jobID,
		Data:   data,
	}
}

// EventBus 是广播总线。
//
// 投递**永不阻塞**：订阅者的缓冲区满了就丢掉这一帧。
// 原因是这条链的另一端可能是 Flutter 的 UI isolate（FFI 回调）
// 或一条慢速 HTTP 连接（SSE）；让业务 goroutine 在这里等一秒，
// 等于让整个同步卡在一个画不出来的界面上。
type EventBus struct {
	mu     sync.Mutex
	buf    int
	next   int
	subs   map[int]chan Event
	closed bool
}

// DefaultEventBuffer 是每个订阅者的缓冲区大小。
const DefaultEventBuffer = 256

// NewEventBus 建一条总线。buf <= 0 时用 DefaultEventBuffer。
func NewEventBus(buf int) *EventBus {
	if buf <= 0 {
		buf = DefaultEventBuffer
	}
	return &EventBus{buf: buf, subs: map[int]chan Event{}}
}

// Subscribe 订阅事件；返回的 stop 用于退订（可重复调用）。
func (b *EventBus) Subscribe() (<-chan Event, func()) {
	if b == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	b.mu.Lock()
	id := b.next
	b.next++
	ch := make(chan Event, b.buf)
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			if _, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(ch)
			}
			b.mu.Unlock()
		})
	}
}

// Publish 推一帧事件。满了就丢，绝不阻塞。
func (b *EventBus) Publish(ev Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// 丢帧是设计的一部分：见 EventBus 的注释。
		}
	}
}

// Close 关掉所有订阅通道。
func (b *EventBus) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, ch := range b.subs {
		close(ch)
		delete(b.subs, id)
	}
}

// LogLine 是环形缓冲里的一行日志。
type LogLine struct {
	TS    string `json:"ts"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

// logCapacity 是环形缓冲保留的行数。
//
// log.recent 是「事件流断了」的兜底：SSE 重连、App 切前台、isolate 重启之后，
// UI 仍然要能看到刚才发生了什么。500 行足够覆盖一次同步的噪音。
const logCapacity = 500

// LogRing 是固定容量的日志环形缓冲。
type LogRing struct {
	mu    sync.Mutex
	max   int
	lines []LogLine
}

// NewLogRing 建一个环形缓冲。
func NewLogRing(max int) *LogRing {
	if max <= 0 {
		max = logCapacity
	}
	return &LogRing{max: max}
}

// Add 追加一行。
func (r *LogRing) Add(level, text string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, LogLine{
		TS:    time.Now().UTC().Format(time.RFC3339),
		Level: level,
		Text:  text,
	})
	if len(r.lines) > r.max {
		r.lines = append([]LogLine(nil), r.lines[len(r.lines)-r.max:]...)
	}
}

// Recent 返回最近 limit 行（按时间从旧到新）；limit <= 0 表示全部。
func (r *LogRing) Recent(limit int) []LogLine {
	if r == nil {
		return []LogLine{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]LogLine(nil), r.lines...)
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	if out == nil {
		return []LogLine{}
	}
	return out
}

// log.write 的服务端限速参数。
//
// **为什么必须限速**：日志是排查资产，不是可以让调用方随意覆盖的资源。
// 一个客户端（或者一个打转的客户端）不该能把引擎的日志缓冲刷掉 ——
// 那会让「出问题时唯一会被人翻的地方」变成一片客户端的噪音。
//
// 20 行 / 秒：客户端侧诊断的正常形态是「偶尔记一条」，不是逐帧打印。
// 窗口做成固定的一秒，是为了让逻辑简单到可以一眼看穿（没有补桶、没有令牌，
// 也就没有「限速器本身有 bug」这一层）。
const (
	clientLogBurst  = 20
	clientLogWindow = time.Second
)

// clientLogLimit 是 log.write 的固定窗口限速器。
type clientLogLimit struct {
	mu          sync.Mutex
	windowStart time.Time
	count       int
	// notified 记录「这个窗口里已经因为限速报过警」，用来只喊一次。
	notified bool
}

// allow 判断这一行能不能进缓冲；第一个被丢掉的会返回 firstDrop=true。
func (l *clientLogLimit) allow(now time.Time) (allowed bool, firstDrop bool) {
	if l == nil {
		return true, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windowStart.IsZero() || now.Sub(l.windowStart) >= clientLogWindow {
		l.windowStart = now
		l.count = 0
		l.notified = false
	}
	if l.count >= clientLogBurst {
		first := !l.notified
		l.notified = true
		return false, first
	}
	l.count++
	return true, false
}
