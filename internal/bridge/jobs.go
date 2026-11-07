package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// job 状态。取值由 docs/ops-api.md §7.8 固定。
const (
	JobRunning  = "running"
	JobDone     = "done"
	JobFailed   = "failed"
	JobCanceled = "canceled"
)

// JobView 是一个异步任务的状态。
type JobView struct {
	ID         string        `json:"id"`
	Kind       string        `json:"kind"`
	State      string        `json:"state"`
	StartedAt  string        `json:"startedAt"`
	FinishedAt *string       `json:"finishedAt"`
	Message    string        `json:"message"`
	Error      *ErrorPayload `json:"error"`
	Result     any           `json:"result"`
}

// maxJobsInMemory 是内存里保留的 job 数。
//
// 20 个：够覆盖「刚点过的那几次」以及它们在界面上的收尾，
// 又不至于让一个跑了一整天的进程攒出几百个没人再看的结果对象
// —— 每个 result 里都可能挂着一份完整计划。
const maxJobsInMemory = 20

// Job 是异步 op 的执行句柄。
type Job struct {
	ID   string
	Kind string

	mgr *JobManager

	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	message   string
	state     string
	startedAt string
	finished  *string
	result    any
	err       error
}

// SetMessage 更新「正在干什么」。它会随 job.get 与 job.done 一起送出去，
// 同时推一帧 progress 事件 —— 界面上的进度条靠它说话。
func (j *Job) SetMessage(message string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	if j.state != JobRunning {
		j.mu.Unlock()
		return
	}
	j.message = message
	id := j.ID
	j.mu.Unlock()
	if j.mgr != nil {
		jobID := id
		j.mgr.emit(NewEvent(EventProgress, nil, &jobID, map[string]any{"label": message}))
	}
}

// Context 返回这个 job 的上下文（Cancel 会让它结束）。
func (j *Job) Context() context.Context {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.ctx
}

// JobManager 管异步任务：最近 20 个留在内存里。
type JobManager struct {
	bus *EventBus

	mu    sync.Mutex
	order []string
	jobs  map[string]*Job
	seq   int64
}

// NewJobManager 建一个任务管理器。
func NewJobManager(bus *EventBus) *JobManager {
	return &JobManager{bus: bus, jobs: map[string]*Job{}}
}

// Start 启动一个异步任务并立刻返回它的视图。
//
// run 在独立 goroutine 里跑；ctx 由 job.cancel / CancelAll 取消。
func (m *JobManager) Start(kind string, run func(ctx context.Context, job *Job) (any, error)) JobView {
	if m == nil {
		return JobView{}
	}
	m.mu.Lock()
	m.seq++
	id := fmt.Sprintf("%s-%d-%d", kind, time.Now().UnixMilli(), m.seq)
	ctx, cancel := context.WithCancel(context.Background())
	startedAt := time.Now().UTC().Format(time.RFC3339)
	job := &Job{
		ID:        id,
		Kind:      kind,
		ctx:       ctx,
		cancel:    cancel,
		mgr:       m,
		state:     JobRunning,
		startedAt: startedAt,
	}
	m.jobs[id] = job
	m.order = append(m.order, id)
	for len(m.order) > maxJobsInMemory {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.jobs, oldest)
	}
	m.mu.Unlock()

	jobID := id
	m.emit(NewEvent(EventJobStart, nil, &jobID, map[string]any{"kind": kind}))

	go func() {
		var (
			result any
			err    error
		)
		func() {
			defer func() {
				if r := recover(); r != nil {
					// 异步任务同样不许带走进程：兜住的 panic 变成一个 failed job。
					err = Errorf(CodeInternal, "任务内部错误（已兜住）: %v", r)
				}
			}()
			result, err = run(ctx, job)
		}()

		finished := time.Now().UTC().Format(time.RFC3339)
		state := JobDone
		switch {
		case err != nil && ctx.Err() != nil:
			state = JobCanceled
		case err != nil:
			state = JobFailed
		}

		job.mu.Lock()
		job.state = state
		job.result = result
		job.err = err
		job.finished = &finished
		message := job.message
		job.mu.Unlock()

		jobID := id
		switch state {
		case JobDone:
			m.emit(NewEvent(EventJobDone, nil, &jobID, map[string]any{"result": result}))
		case JobCanceled:
			payload := Classify(context.Canceled)
			m.emit(NewEvent(EventJobFailed, nil, &jobID, map[string]any{
				"error": map[string]any{"code": payload.Code, "message": payload.Message},
			}))
		default:
			payload := Classify(err)
			m.emit(NewEvent(EventJobFailed, nil, &jobID, map[string]any{
				"error": map[string]any{"code": payload.Code, "message": payload.Message},
			}))
		}

		// 收尾一条 log：任务结束这件事必须能在 log.recent 里查到，
		// 否则「事件流断过」的客户端只能看到一个永远 running 的 job。
		level := "info"
		text := fmt.Sprintf("[%s] 完成", kind)
		if message != "" {
			text = fmt.Sprintf("[%s] %s", kind, message)
		}
		if state != JobDone {
			level = "error"
			text = fmt.Sprintf("[%s] %s", kind, Classify(err).Message)
		}
		m.emit(NewEvent(EventLog, nil, &jobID, map[string]any{"level": level, "text": text}))
	}()

	return JobView{
		ID:        id,
		Kind:      kind,
		State:     JobRunning,
		StartedAt: startedAt,
	}
}

// Get 取一个任务的视图。
func (m *JobManager) Get(id string) (JobView, bool) {
	if m == nil {
		return JobView{}, false
	}
	m.mu.Lock()
	job, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return JobView{}, false
	}
	return job.view(), true
}

// List 按启动时间倒序返回最近的任务。
func (m *JobManager) List() []JobView {
	if m == nil {
		return []JobView{}
	}
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	jobs := make([]*Job, 0, len(ids))
	for _, id := range ids {
		if job, ok := m.jobs[id]; ok {
			jobs = append(jobs, job)
		}
	}
	m.mu.Unlock()

	out := make([]JobView, 0, len(jobs))
	for i := len(jobs) - 1; i >= 0; i-- {
		out = append(out, jobs[i].view())
	}
	return out
}

// Cancel 取消一个任务；返回 false 表示没有这个 id 或它已经结束。
func (m *JobManager) Cancel(id string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	job, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return false
	}
	job.mu.Lock()
	finished := job.state != JobRunning
	cancel := job.cancel
	job.mu.Unlock()
	if finished {
		return false
	}
	cancel()
	return true
}

// CancelAll 取消所有在飞的任务（容器关闭时用）。
func (m *JobManager) CancelAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	jobs := make([]*Job, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	for _, job := range jobs {
		job.mu.Lock()
		running := job.state == JobRunning
		cancel := job.cancel
		job.mu.Unlock()
		if running {
			cancel()
		}
	}
}

// emit 推一帧事件；没有总线时静默丢弃。
func (m *JobManager) emit(ev Event) {
	if m == nil || m.bus == nil {
		return
	}
	m.bus.Publish(ev)
}

// view 把内部记录渲染成契约形状。
func (j *Job) view() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()

	raw := JobView{
		ID:         j.ID,
		Kind:       j.Kind,
		State:      j.state,
		StartedAt:  j.startedAt,
		FinishedAt: j.finished,
		Message:    j.message,
		Result:     nil,
	}
	if j.state == JobDone {
		raw.Result = j.result
	}
	if j.err != nil {
		payload := Classify(j.err)
		raw.Error = &ErrorPayload{Code: payload.Code, Message: payload.Message, Detail: payload.Detail}
	}
	return raw
}

// CallRegistry 是 callId → context.CancelFunc 的注册表。
type CallRegistry struct {
	mu    sync.Mutex
	calls map[string]context.CancelFunc
}

// NewCallRegistry 建一份注册表。
func NewCallRegistry() *CallRegistry { return &CallRegistry{calls: map[string]context.CancelFunc{}} }

// Add 登记一次在飞调用；同一个 callId 重复登记返回 false。
func (r *CallRegistry) Add(callID string, cancel context.CancelFunc) bool {
	if r == nil || callID == "" {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.calls[callID]; exists {
		return false
	}
	r.calls[callID] = cancel
	return true
}

// Remove 摘掉一次调用。
func (r *CallRegistry) Remove(callID string) {
	if r == nil || callID == "" {
		return
	}
	r.mu.Lock()
	delete(r.calls, callID)
	r.mu.Unlock()
}

// Cancel 取消一次调用；返回 false 表示没有这个 callId。
func (r *CallRegistry) Cancel(callID string) bool {
	if r == nil || callID == "" {
		return false
	}
	r.mu.Lock()
	cancel, ok := r.calls[callID]
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// Len 返回在飞调用数（测试用）。
func (r *CallRegistry) Len() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}
