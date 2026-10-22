// Package webui 提供本地设置页（内嵌静态资源，无前端构建步骤）。
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/zzzwannasleep/assm/internal/assapi"
	"github.com/zzzwannasleep/assm/internal/config"
	"github.com/zzzwannasleep/assm/internal/planner"
)

//go:embed assets/index.html
var assets embed.FS

// Server 是设置页服务。
type Server struct {
	mu  sync.Mutex
	cfg *config.Config
}

// New 创建服务。
func New(cfg *config.Config) *Server { return &Server{cfg: cfg} }

// ListenAndServe 启动服务。
func (s *Server) ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/plan", s.handlePlan)
	mux.HandleFunc("/api/sync", s.handleSync)
	mux.HandleFunc("/api/test", s.handleTest)
	mux.HandleFunc("/api/sources", s.handleSources)

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.mu.Lock()
		defer s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"path":   s.cfg.Path(),
			"config": s.cfg,
		})
	case http.MethodPut, http.MethodPost:
		var incoming config.Config
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "配置 JSON 解析失败: " + err.Error()})
			return
		}
		s.mu.Lock()
		path := s.cfg.Path()
		s.mu.Unlock()

		next := &incoming
		next.SetPath(path)
		next.Normalize()
		if err := next.Save(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		s.mu.Lock()
		s.cfg = next
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": next.Path()})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	plan, err := planner.Build(ctx, cfg, planner.Options{AddMissing: cfg.Sync.AddMissing})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	plan, err := planner.Build(ctx, cfg, planner.Options{AddMissing: cfg.Sync.AddMissing})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	updated, added, errs := planner.Apply(ctx, cfg, plan)
	writeJSON(w, http.StatusOK, map[string]any{
		"updated": updated,
		"added":   added,
		"errors":  errs,
		"plan":    plan,
	})
}

// handleTest 用请求里给的凭据测试连通性，不依赖已保存配置。
func (s *Server) handleTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BaseURL string `json:"baseUrl"`
		APIKey  string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.mu.Lock()
	proxy := s.cfg.Proxy
	s.mu.Unlock()

	client, err := assapi.New(config.NormalizeBaseURL(body.BaseURL), body.APIKey, proxy, 20*time.Second)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	about, err := client.About(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_, total, err := client.ListAni(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "连接成功但拉取订阅失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": fmt.Sprint(about["version"]),
		"total":   total,
	})
}

// handleSources 列出可用的站点适配器。
func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]string{
		{"id": "anibt", "name": "AniBT", "note": "官方 API，字幕组维度，主推"},
		{"id": "animegarden", "name": "AnimeGarden", "note": "API + feed.xml，字幕组维度"},
		{"id": "mikan", "name": "Mikan", "note": "标题搜索 + 详情页反查 bgm，字幕组维度"},
		{"id": "nyaa", "name": "Nyaa", "note": "标题搜索，按 [放流组] 聚合，type=other"},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
