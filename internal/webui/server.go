// Package webui 提供本地设置页（内嵌静态资源，无前端构建步骤）。
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/bridge"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/httpx"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

//go:embed assets/index.html assets/*.svg
var assets embed.FS

// SyncResult 与 SyncFunc 是 internal/app 里那两个的别名。
//
// 别名而不是另立一套：设置页的「立即同步」和后台定时那一轮必须**是同一段代码**，
// 而类型别名让这条约束在编译期成立 —— 再造一个同形结构体，
// 就等于给自己留了一条「悄悄只改一边」的路。
type (
	SyncResult = app.SyncResult
	SyncFunc   = app.SyncFunc
)

// Server 是设置页服务。
type Server struct {
	mu  sync.Mutex
	cfg *config.Config
	// syncFn 为空时（`AniSync serve` 这种纯设置页模式）走 app 自己那条同步链路。
	syncFn SyncFunc
	// core 与 token 非空时同时提供 HTTP 节点（POST /api/v1/call 等）。
	core  *bridge.Core
	token string
}

// New 创建服务。
func New(cfg *config.Config) *Server { return &Server{cfg: cfg} }

// SetSyncFunc 注入同步入口；常驻进程启动时调用。
func (s *Server) SetSyncFunc(fn SyncFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncFn = fn
}

// SetNode 打开 HTTP 节点模式：同一组 op 通过 /api/v1/call 暴露。
//
// token 为空时只允许绑回环地址 —— 这个 API 能改写订阅，
// 裸奔在网络上等于把实例交出去。绑定规则在 ListenAndServe 里强制。
func (s *Server) SetNode(core *bridge.Core, token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.core = core
	s.token = strings.TrimSpace(token)
}

// NodeToken 返回当前节点鉴权 token（可能为空）。
func (s *Server) NodeToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.token
}

// ListenAndServe 启动服务。
//
// 绑定规则（不许放宽）：
//   - 默认仍然只绑 127.0.0.1:51837；
//   - 要绑非回环地址（0.0.0.0、局域网 IP）时**必须**同时给 token，
//     否则直接拒绝启动并打印原因；
//   - 没有 token 的节点**永远**不监听非回环地址。
func (s *Server) ListenAndServe(addr string) error {
	if err := s.checkBinding(addr); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/plan", s.handlePlan)
	mux.HandleFunc("/api/sync", s.handleSync)
	mux.HandleFunc("/api/test", s.handleTest)
	mux.HandleFunc("/api/sources", s.handleSources)
	mux.HandleFunc("/api/season", s.handleSeason)
	mux.HandleFunc("/api/bgm/search", s.handleBgmSearch)
	mux.HandleFunc("/api/bgm/cover", s.handleBgmCover)
	mux.HandleFunc("/favicon.svg", s.handleFavicon)
	// 节点路由：只有注入了 Core 才挂。纯设置页模式不暴露 /api/v1/*。
	if s.core != nil {
		mux.HandleFunc(nodeCallPath, s.handleNodeCall)
		mux.HandleFunc(nodeEventsPath, s.handleNodeEvents)
		mux.HandleFunc(nodeHealthPath, s.handleNodeHealth)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return server.ListenAndServe()
}

// checkBinding 在真正监听之前拦一次：这条规则必须由代码守住，
// 而不是靠文档提醒用户「记得加 token」。
func (s *Server) checkBinding(addr string) error {
	if s.core == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if isLoopbackHost(host) {
		return nil
	}
	if s.NodeToken() == "" {
		return fmt.Errorf("拒绝监听 %s：这个地址不是回环地址，而节点没有设置 token。"+
			"HTTP 节点能改写订阅，裸奔在网络上等于把 ANI-RSS 实例交出去；"+
			"请加 --token <随机串>，或把地址改回 127.0.0.1", addr)
	}
	return nil
}

// isLoopbackHost 判断监听地址是不是只对本机可见。
func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		// ":51837" 表示所有网卡 —— 那**不是**回环。
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
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
		// 外置规则文件必须重新读一遍：incoming 是全新对象，不读的话
		// 用户在设置页点一次保存，白名单与黑名单就会当场失效。
		//
		// 这一条**仍然是硬错误、仍然不落盘**（契约 §7.2 的那条线）：
		// 校验类问题只是「配置不完整」，而规则文件读不出来是「配置会静默变形」——
		// 放行会让后续 plan / sync 少一套规则地跑，而配置文件看上去毫无问题。
		if err := next.LoadRuleFiles(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		// 注释布局也要从原文件接过来，否则保存一次就把 config.yaml 的注释全冲掉。
		if err := next.ReloadLayout(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		next.Normalize()

		// 校验只记录、不拦截 —— 与 bridge 的 config.save 同一种脾气（契约 §7.2）。
		//
		// 以前这里是「校验没过就 400，什么都不写」。改成总是落盘的理由：
		// **增量编辑是交互式界面的基本用法** —— 新建工作区之后用户可能只想先设代理、
		// 先改规则，还没加实例；这时保存被拒，等于连一个 config.yaml 都建不出来。
		// 校验该拦在真的会碰实例的地方（/api/plan、/api/sync）。
		validateErr := next.Validate()

		if err := next.Save(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		s.mu.Lock()
		s.cfg = next
		s.mu.Unlock()

		// 响应形状与 bridge 的 config.save 对齐（多带一个 ok，见下面的注释）。
		payload := map[string]any{
			"ok":       true,
			"path":     next.Path(),
			"valid":    validateErr == nil,
			"errors":   []map[string]any{},
			"warnings": []string{},
		}
		if validateErr != nil {
			// 与 bridge 侧同形：field 留空（config.Validate 只给一条人话消息，不报字段），
			// 与其猜一个字段名（猜错会把红条挂在错的输入框上），不如把原文交出去。
			payload["errors"] = []map[string]any{{"message": validateErr.Error()}}
		}
		writeJSON(w, http.StatusOK, payload)
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

	plan, err := planner.Build(ctx, cfg, planner.Options{AddMissing: cfg.Sync.AddMissing, CachePath: app.CachePath()})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg := s.cfg
	injected := s.syncFn
	s.mu.Unlock()

	// 常驻进程里必须走注入的那一条：它和后台定时任务共用写锁与租约。
	if injected != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
		defer cancel()
		result, err := injected(ctx)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"updated": result.Updated,
				"added":   result.Added,
				"errors":  append(result.Errors, err.Error()),
				"plan":    result.Plan,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"updated": result.Updated,
			"added":   result.Added,
			"errors":  result.Errors,
			"plan":    result.Plan,
		})
		return
	}

	if err := cfg.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()

	// 走 app 这条唯一实现：与托盘、桥接层同一条链路（含 discover 与 MaxNew）。
	outcome, plan, err := app.BuildAndApply(ctx, cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"updated": outcome.Updated,
		"added":   outcome.Added,
		"errors":  outcome.Errors,
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
	proxy := s.cfg.ResolvedProxy()
	s.mu.Unlock()

	// 设置页里填的也可能是 ${ENV} 占位符，真正发请求时才展开。
	client, err := assapi.New(config.NormalizeBaseURL(config.ExpandEnv(body.BaseURL)), config.ExpandEnv(body.APIKey), proxy, 20*time.Second)
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
//
// 只报默认的两家：anibt 走实例接口、nyaa 自己搜，覆盖面互补。
// animegarden / mikan 仍然能写进配置生效，但设置页不再提供——
// 它们和 anibt 是同一类实例接口，多出来的两家换不回额外覆盖面。
func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]string{
		{"id": "anibt", "name": "AniBT", "note": "字幕组维度，走实例的 aniBTGroup 接口"},
		{"id": "nyaa", "name": "Nyaa", "note": "关键字搜索型 RSS，按标题里的 [放流组] 聚合"},
	})
}

// handleSeason 返回「本季新番」：Bangumi 放送表里当前在放送的日本 TV 番剧与剧场版，
// 并标出哪些实例里已经订过。设置页拿它渲染成一屏可勾选的卡片。
//
// 业务部分整份在 internal/app（`app.Season`）：同一份判定还要给桥接层的
// discover.list 用，两个入口各写一份的话，「界面上说没加、计划里说已存在」
// 这种分歧迟早会出现，而且只在两部番上出现。
func (s *Server) handleSeason(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 GET"})
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	// 设置页的「只要日漫」开关可以按当前勾选状态预览，不必先保存配置 ——
	// 切换过滤条件只是想看看列表长什么样，逼用户先落盘一次没有道理。
	var japanOnly *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("japanOnly")); raw != "" {
		value := raw != "0" && !strings.EqualFold(raw, "false")
		japanOnly = &value
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	result, err := app.Season(ctx, cfg, app.SeasonOptions{JapanOnly: japanOnly})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result.Payload())
}

// subscribedBgmIDs / listAniWithRetry / bgmIDOfAni / foldTitle 已经**搬去**
// internal/app（app.SubscribedBgmIDs / app.ListAniWithRetry / app.BgmIDOfAni /
// app.FoldTitle）。这里刻意不留一层转发函数：留了就等于给「设置页那份判定」
// 与「桥接层那份判定」留了分叉的余地，而它们必须逐字一致。

// handleBgmSearch 按关键字搜 Bangumi 动画条目，供设置页「搜一部番剧、从卡片里挑」用。
func (s *Server) handleBgmSearch(w http.ResponseWriter, r *http.Request) {
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if keyword == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请输入搜索关键字"})
		return
	}
	limit := bgm.DefaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "limit 必须是数字"})
			return
		}
		limit = bgm.ClampLimit(n)
	}

	s.mu.Lock()
	cfg := s.cfg
	s.mu.Unlock()

	// 复用站点数据那份磁盘缓存：同一个关键字反复敲不该反复打 Bangumi。
	store := cache.Open(app.CachePath(), time.Duration(cfg.Sources.CacheMinutes)*time.Minute)
	defer func() { _ = store.Flush() }()

	key := bgmSearchCacheKey(keyword)
	var hit []bgm.Subject
	if store.Get(key, &hit) {
		writeJSON(w, http.StatusOK, map[string]any{"items": head(hit, limit), "cached": true})
		return
	}

	client, err := httpx.New(cfg.ResolvedProxy(), 20*time.Second)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Bangumi 搜索失败: " + err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// 一律按上限取满再切片：缓存键里就不必带 limit，一个关键字只占一条缓存。
	items, err := bgm.Search(ctx, client, keyword, bgm.MaxLimit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "Bangumi 搜索失败: " + err.Error()})
		return
	}
	store.Put(key, items)
	writeJSON(w, http.StatusOK, map[string]any{"items": head(items, limit), "cached": false})
}

// bgmSearchCacheKey 是搜索结果的缓存键。
//
// 刻意不含 limit：limit 只是「这次要几条」，写进键里会让同一个关键字在
// limit=8 和 limit=20 时各打一次 Bangumi。取满上限再切片就没有这个问题。
func bgmSearchCacheKey(keyword string) string { return "bgm:search:" + keyword }

// maxCoverBytes 是封面代理单次最多回传的字节数。
// 封面撑死几百 KB，8 MiB 足够，同时挡住「拿这个接口当下载器」的用法。
const maxCoverBytes = 8 << 20

// coverHostAllow 是封面代理允许访问的主机白名单（小写，精确匹配）。
//
// 这个接口是刻意留出来的 SSRF 面：用户浏览器直连不了 lain.bgm.tv，
// 只能由本机服务代取。因此这里必须精确匹配主机名，绝不能用后缀判断 ——
// evil-lain.bgm.tv.attacker.com 这类域名用 HasSuffix 是拦不住的。
//
// 声明成变量是为了测试能临时放行 httptest.Server 的 127.0.0.1。
var coverHostAllow = map[string]bool{
	"lain.bgm.tv":    true,
	"bgm.tv":         true,
	"www.bgm.tv":     true,
	"bangumi.tv":     true,
	"www.bangumi.tv": true,
	"chii.in":        true,
}

// coverPortAllow 是封面代理允许的端口：缺省、80、443。
//
// 白名单只认主机名，所以 https://lain.bgm.tv:8080/... 也能过主机检查。
// 那仍然只连到 lain.bgm.tv，危害有限，但「允许任意端口」没有理由，
// 顺手收紧到 HTTP 该有的两个。同样声明成变量，测试才能放行 httptest 的随机端口。
var coverPortAllow = map[string]bool{"": true, "80": true, "443": true}

// coverAllowed 判断一个地址是否落在封面代理的白名单里。
func coverAllowed(u *url.URL) bool {
	return coverHostAllow[strings.ToLower(u.Hostname())] && coverPortAllow[u.Port()]
}

// handleBgmCover 代取封面图片。
func (s *Server) handleBgmCover(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse(strings.TrimSpace(r.URL.Query().Get("u")))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不支持的图片地址"})
		return
	}
	if !coverAllowed(target) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "不支持的图片地址"})
		return
	}

	s.mu.Lock()
	proxy := s.cfg.ResolvedProxy()
	s.mu.Unlock()

	client, err := httpx.New(proxy, 20*time.Second)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "获取封面失败: " + err.Error()})
		return
	}
	// 重定向也要重新过一遍白名单：上游哪天挂了个跳转，
	// 不能让它成为绕过上面那次主机检查的跳板。
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("重定向次数过多")
		}
		if !coverAllowed(req.URL) {
			return fmt.Errorf("重定向到了不允许的地址 %s", req.URL.Host)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "获取封面失败: " + err.Error()})
		return
	}
	// 这里就是「浏览器取图」的场景，普通浏览器 UA 即可（限流那条规矩只针对 API）。
	req.Header.Set("User-Agent", httpx.UserAgent)

	res, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "获取封面失败: " + err.Error()})
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": fmt.Sprintf("获取封面失败: 上游返回 HTTP %d", res.StatusCode)})
		return
	}
	contentType := strings.TrimSpace(res.Header.Get("Content-Type"))
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "获取封面失败: 上游返回的不是图片（" + contentType + "）"})
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=2592000")
	w.WriteHeader(http.StatusOK)
	// 限长读取：既防超大响应把内存吃光，也防拿这个接口当通用下载器。
	_, _ = io.Copy(w, io.LimitReader(res.Body, maxCoverBytes))
}

// 内嵌的应用图标。设置页里那个品牌标是内联 SVG，不走这里；
// 这个路由只服务浏览器标签页的 favicon。
const (
	faviconLight = "assets/icon-light.svg"
	faviconDark  = "assets/icon-dark.svg"
)

// handleFavicon 按主题返回应用图标；认不出的 theme 一律给浅色。
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	name := faviconLight
	if strings.TrimSpace(r.URL.Query().Get("theme")) == "dark" {
		name = faviconDark
	}
	icon, err := assets.ReadFile(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(icon)
}

// nonNilSubjects 换成 head：既切片又保证 JSON 里是 [] 而不是 null，
// 前端拿到就能直接 .map()，不用再判空。
func head(items []bgm.Subject, n int) []bgm.Subject {
	if n < 0 {
		n = 0
	}
	if n > len(items) {
		n = len(items)
	}
	out := make([]bgm.Subject, n)
	copy(out, items[:n])
	return out
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
