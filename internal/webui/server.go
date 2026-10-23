// Package webui 提供本地设置页（内嵌静态资源，无前端构建步骤）。
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
	"github.com/zzzwannasleep/anisync/internal/httpx"
	"github.com/zzzwannasleep/anisync/internal/planner"
	"github.com/zzzwannasleep/anisync/internal/source"
	"github.com/zzzwannasleep/anisync/internal/workdir"
)

//go:embed assets/index.html assets/*.svg
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
	mux.HandleFunc("/api/season", s.handleSeason)
	mux.HandleFunc("/api/bgm/search", s.handleBgmSearch)
	mux.HandleFunc("/api/bgm/cover", s.handleBgmCover)
	mux.HandleFunc("/favicon.svg", s.handleFavicon)

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
		// 外置规则文件必须重新读一遍：incoming 是全新对象，不读的话
		// 用户在设置页点一次保存，白名单与黑名单就会当场失效。
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
		if err := next.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "配置校验失败: " + err.Error()})
			return
		}
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

	plan, err := planner.Build(ctx, cfg, planner.Options{AddMissing: cfg.Sync.AddMissing, CachePath: cachePath()})
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

	plan, err := planner.Build(ctx, cfg, planner.Options{AddMissing: cfg.Sync.AddMissing, CachePath: cachePath()})
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

// seasonItem 是一条本季新番。Added 表示实例的 targets 里已经有它。
type seasonItem struct {
	discover.Item
	Added bool `json:"added"`
}

// handleSeason 返回「本季新番」：Bangumi 放送表里当前在放送的日本 TV 番剧与剧场版，
// 并标出哪些实例里已经订过。设置页拿它渲染成一屏可勾选的卡片。
//
// 这条链路上没有模糊匹配：放送表的每一项都自带 bgmId，加进 targets 就是原样搬运。
func (s *Server) handleSeason(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 GET"})
		return
	}
	s.mu.Lock()
	cfg := s.cfg
	proxy := cfg.ResolvedProxy()
	timeout := time.Duration(cfg.Sources.TimeoutSeconds) * time.Second
	ttl := time.Duration(cfg.Sources.CacheMinutes) * time.Minute
	exclude := append([]string(nil), cfg.Discover.Exclude...)
	japanOnly := cfg.Discover.JapanOnlyEnabled()
	// 设置页的「只要日漫」开关可以按当前勾选状态预览，不必先保存配置 ——
	// 切换过滤条件只是想看看列表长什么样，逼用户先落盘一次没有道理。
	if raw := strings.TrimSpace(r.URL.Query().Get("japanOnly")); raw != "" {
		japanOnly = raw != "0" && !strings.EqualFold(raw, "false")
	}
	// 面板只跑「当季番剧表」型的来源。bangumi-collections 是用户自己的追番列表，
	// 不是一季的番剧表，混进来会让面板变成两种东西的并集。
	sources := make([]string, 0, len(cfg.Discover.SourceList()))
	for _, name := range cfg.Discover.SourceList() {
		if name != config.DiscoverBgmCollections {
			sources = append(sources, name)
		}
	}
	if len(sources) == 0 {
		sources = append([]string(nil), config.DefaultDiscoverSources...)
	}

	// 手写 target 的标题也要算「已添加」：配置里允许只写 title 不写 bgmId，
	// planner 照样能按标题包含匹配命中。只认 bgmId 的话，这种番在界面上会被当成
	// 没加过，用户就会给同一部番再加一条带 bgmId 的 —— 两份配置描述同一个番剧。
	existingTitles := make(map[string]bool, len(cfg.Targets))
	for _, target := range cfg.Targets {
		if title := foldTitle(target.Title); title != "" {
			existingTitles[title] = true
		}
	}
	s.mu.Unlock()

	if timeout <= 0 {
		timeout = 25 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// 「已添加」必须问实例，不能只看配置里的 targets。
	//
	// targets 是用户手写的番剧级配置（规定季度与偏移），订阅是实例里真实存在的东西，
	// 两者本来就不是一回事：用户在 ANI-RSS 里手动加的订阅根本不会出现在 targets 里。
	// 之前只看 targets，结果实例里明明订了 71 部，界面上显示一部都没加。
	existingIDs, subscribed, instanceErr := subscribedBgmIDs(ctx, cfg)
	for _, target := range cfg.Targets {
		if id := strings.ToLower(strings.TrimSpace(target.BgmID)); id != "" {
			existingIDs[id] = true
		}
	}

	// 缓存要显式 Flush 才会落盘（cache.Put 只改内存）。这里必须开成变量再 defer，
	// 写成 cache.Open(...) 内联传参会得到一个「每次请求新建、请求结束即丢弃」的
	// 内存缓存 —— 看着像有缓存，实际上每次都重新打上游。
	// main.go / planner.go / handleBgmSearch 都是这个模式，别在这里破例。
	store := cache.Open(cachePath(), ttl)
	defer func() { _ = store.Flush() }()

	result, err := discover.Discover(ctx, discover.Options{
		Sources:   sources,
		JapanOnly: japanOnly,
		Proxy:     proxy,
		Timeout:   timeout,
		Store:     store,
		// 黑名单必须一起生效：界面上写着「命中就不列出来」，不传的话用户能用「全选」
		// 把黑名单里的番一次性灌进 targets。自动添加那条路（planner）本来就传它。
		Exclude: exclude,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	items := make([]seasonItem, 0, len(result.Items))
	added := 0
	for _, item := range result.Items {
		isAdded := existingIDs[strings.ToLower(item.BgmID)] ||
			existingTitles[foldTitle(item.Title)] ||
			existingTitles[foldTitle(item.JPTitle)]
		if isAdded {
			added++
		}
		items = append(items, seasonItem{Item: item, Added: isAdded})
	}
	payload := map[string]any{
		"items":    items,
		"total":    len(items),
		"added":    added,
		"filtered": result.Filtered,
		"source":   strings.Join(result.Sources, ","),
		"warnings": result.Warnings,
	}
	// Yuc 里有、但 Bangumi 上还没建条目的番，认不出来就进不了列表。
	// 报出来，否则用户只会发现列表比 Yuc 上少了几部。
	if result.Unmatched > 0 {
		payload["unmatched"] = result.Unmatched
	}
	// 读实例失败时要说清楚，否则用户看到「一部都没加」会以为是判定逻辑坏了。
	if instanceErr != nil {
		payload["instanceError"] = instanceErr.Error()
	} else {
		payload["subscribed"] = subscribed
	}
	writeJSON(w, http.StatusOK, payload)
}

// subscribedBgmIDs 读各启用实例里已有的订阅，取出它们的 bgmId。
//
// 返回的第三个值是「一个实例都没读到」时的原因。只要有一个实例读成功就不算失败：
// 多实例场景下某个实例临时连不上，不该让整张列表退化成「全部未添加」。
func subscribedBgmIDs(ctx context.Context, cfg *config.Config) (map[string]bool, int, error) {
	ids := map[string]bool{}
	total := 0
	reached := 0
	var firstErr error

	for _, instance := range cfg.Instances {
		if !instance.IsEnabled() {
			continue
		}
		resolved := instance.Resolve()
		client, err := assapi.New(config.NormalizeBaseURL(resolved.BaseURL), resolved.APIKey, cfg.ResolvedProxy(), 30*time.Second)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("实例 %s：%w", instance.Name, err)
			}
			continue
		}
		list, count, err := listAniWithRetry(ctx, client)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("实例 %s：%w", instance.Name, err)
			}
			continue
		}
		reached++
		total += count
		for _, ani := range list {
			if id := bgmIDOfAni(ani); id != "" {
				ids[strings.ToLower(id)] = true
			}
		}
	}
	if reached == 0 && firstErr != nil {
		return ids, 0, firstErr
	}
	return ids, total, nil
}

// listAniWithRetry 读一次实例订阅，传输层错误重试一次。
//
// listAni 的响应有 1.8MB，经本地代理时偶发 EOF（实测遇到过一次，同一秒直接 curl 是好的）。
// 这类错误重试一次基本就好。但 HTTP 层错误不重试 —— 401/403/500 不会因为再问一遍就变好，
// 只会让界面白等一轮。
func listAniWithRetry(ctx context.Context, client *assapi.Client) ([]assapi.Ani, int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		list, count, err := client.ListAni(ctx)
		if err == nil {
			return list, count, nil
		}
		var apiErr *assapi.APIError
		if errors.As(err, &apiErr) {
			return nil, 0, err
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

// bgmIDParam 从 RSS 地址的查询参数里取 bgmId。
//
// AniBT 的订阅地址就是这个形状：https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=xxx。
// source.BgmIDFromURL 只认 /subject/NNN 那种路径，认不出这里的 bgmId，所以得单独匹配。
var bgmIDParam = regexp.MustCompile(`(?i)[?&]bgmId=(\d+)`)

// bgmIDOfAni 从一条订阅里取 bgmId。
// bgmUrl 是实例自己填的番剧链接（形如 https://bgm.tv/subject/975），最可靠；
// 取不到再退到订阅的 RSS 地址上找，那里带的是 ?bgmId= 查询参数。
func bgmIDOfAni(ani assapi.Ani) string {
	for _, key := range []string{"bgmUrl", "url"} {
		raw, ok := ani[key].(string)
		if !ok || raw == "" {
			continue
		}
		if id := source.BgmIDFromURL(raw); id != "" {
			return id
		}
		if m := bgmIDParam.FindStringSubmatch(raw); m != nil {
			return m[1]
		}
	}
	return ""
}

// foldTitle 把标题压成可比较的形式：去掉全部空白、统一小写。
//
// 只用于「这条 target 是不是已经覆盖了这部番」，不做模糊匹配 ——
// 模糊匹配是 planner 的活，这里误判的代价是用户看到错的「已添加」状态。
func foldTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

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
	store := cache.Open(cachePath(), time.Duration(cfg.Sources.CacheMinutes)*time.Minute)
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

// cachePath 返回站点数据缓存文件路径。
func cachePath() string { return filepath.Join(workdir.DataDir(), "cache.json") }

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
