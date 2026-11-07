package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/bridge"
)

// 节点模式的路由。与设置页共用同一个监听端口。
//
// 为什么是这三个路径：docs/ops-api.md §2 把它们写死了 —— 同一组 op
// 既走 FFI 又走 HTTP，Dart 侧的 NodeBackend 只认这三条。
const (
	nodeCallPath   = "/api/v1/call"
	nodeEventsPath = "/api/v1/events"
	nodeHealthPath = "/api/v1/health"
)

// tokenHeader 是节点鉴权的头。
const tokenHeader = "X-AniSync-Token"

// nodeRequest 是 POST /api/v1/call 的请求体。
type nodeRequest struct {
	Op string `json:"op"`
	// Payload 原样透传：契约说省略等价于 {}，所以这里不做任何默认值注入。
	Payload json.RawMessage `json:"payload"`
	// CallID 可选：给了它就能用同一个 id 取消（与 FFI 的 AniSyncCancel 同义）。
	CallID string `json:"callId"`
}

// handleNodeCall 执行一次 op。
//
// 响应体**就是**那封信封（{"ok":true,"data":{...}} / {"ok":false,"error":{...}}），
// 不再包一层 HTTP 状态码：HTTP 层只负责「请求到达了没有」，
// 「这个 op 成功没有」由信封回答 —— 这样 FFI 与 HTTP 两条路的行为逐字一致。
func (s *Server) handleNodeCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 POST"})
		return
	}
	if !s.nodeAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "token 不匹配"})
		return
	}
	core := s.nodeCore()
	if core == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "节点未启用"})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "读取请求体失败: " + err.Error()})
		return
	}
	var req nodeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if strings.TrimSpace(req.Op) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 op"})
		return
	}

	envelope := core.Handle(r.Context(), req.Op, req.Payload, req.CallID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(envelope)
}

// handleNodeEvents 是 SSE 事件流：每行一个 data: <事件 JSON>。
func (s *Server) handleNodeEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 GET"})
		return
	}
	if !s.nodeAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "token 不匹配"})
		return
	}
	core := s.nodeCore()
	if core == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "节点未启用"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "这个连接不支持流式响应"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, stop := core.Events().Subscribe()
	defer stop()

	// 心跳：代理与手机网络会把「长时间没有字节」的连接掐掉。
	// 用注释行（以 ":" 开头）保活，它不会被 SSE 解析器当成事件。
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			raw, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := w.Write(append(append([]byte("data: "), raw...), '\n', '\n')); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleNodeHealth 是不带鉴权的存活探针 —— 客户端要能先问「对面是不是 AniSync」，
// 再决定要不要带 token 说话。
func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 GET"})
		return
	}
	if s.nodeCore() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "节点未启用"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"data": map[string]any{
			"version":       app.Version,
			"schema":        app.Schema,
			"tokenRequired": s.NodeToken() != "",
		},
	})
}

// nodeAuthorized 检查鉴权。
//
// 设了 token 时**所有**请求都必须带它，包括 SSE —— 能读事件流就等于能看见
// 「谁在什么时候改了哪些订阅」，那同样是隐私。
func (s *Server) nodeAuthorized(r *http.Request) bool {
	token := s.NodeToken()
	if token == "" {
		return true
	}
	return strings.TrimSpace(r.Header.Get(tokenHeader)) == token
}

// nodeCore 取节点容器。
func (s *Server) nodeCore() *bridge.Core {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.core
}
