package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
)

// 设置页只驱动一条发现来源（Bangumi 放送表），列表整份来自 /api/season。
//
// 这条测试钉的是一个**已经删掉的东西**：以前设置页里写死一份来源标签，id 必须和
// config.DiscoverSourceNames 对齐。现在来源不再由用户在界面上勾选，那套对齐契约也就不存在了——
// 留着它只会让人以为界面还能选来源。改成钉住新的契约：
// 页面必须去问 /api/season，而不是自己拼一份番剧列表。
func TestWebUIDrivesSeasonListFromAPI(t *testing.T) {
	page := readIndex(t)
	if !strings.Contains(page, "/api/season") {
		t.Error("设置页没有请求 /api/season；本季新番的列表应该整份来自服务端，前端不该自己拼")
	}
	if strings.Contains(page, "discoverSources") {
		t.Error("设置页里还留着 discoverSources：来源选择已经从界面移除，残留会让它看起来还能配")
	}
	// 默认来源必须是 Yuc 的当季番剧表。理由：Bangumi 放送表把国漫、美漫、OVA
	// 全混在一起且不带地区与平台，要筛干净只能逐条查详情；Yuc 本身就是人工整理过的
	// 日本番剧表，拿它定「这一季有什么」，Bangumi 只负责补 bgmId。
	if len(config.DefaultDiscoverSources) != 1 || config.DefaultDiscoverSources[0] != config.DiscoverYuc {
		t.Errorf("默认发现来源应只有 yuc（人工整理的日本番剧表），实际 %v", config.DefaultDiscoverSources)
	}
}

// 本季新番相关的控件必须有可访问名，否则键盘和读屏用户用不了。
func TestWebUIDiscoverInputsAreLabelled(t *testing.T) {
	page := readIndex(t)
	for _, id := range []string{
		"discoverEnabled", "discoverEnable", "discoverLimit", "discoverExclude",
		"seasonAll", "seasonAdd", "seasonGrid", "seasonCount",
	} {
		if !strings.Contains(page, `id="`+id+`"`) {
			t.Errorf("设置页里缺少控件 %s", id)
		}
	}
	// 每个控件后面都应该跟着 aria-label；粗略但够用的检查。
	for _, label := range []string{
		`id="discoverEnabled" aria-label="同步时自动添加本季新番"`,
		`id="discoverEnable" aria-label="新加的订阅直接启用"`,
		`id="discoverLimit" min="1" max="500" placeholder="50" aria-label="自动添加单次上限"`,
		`id="discoverExclude" rows="3"`,
		`id="seasonAll" aria-label="全选尚未添加的番剧"`,
		`id="seasonAdd" onclick="addSeasonPicked()" aria-label="添加勾选的番剧"`,
		`id="seasonGrid" aria-busy="false" role="group" aria-label="本季新番"`,
	} {
		if !strings.Contains(page, label) {
			t.Errorf("设置页里这段结构变了，检查可访问名是否还在: %s", label)
		}
	}
}

// 侧栏收起时，里面每个图标必须排在同一列上。
//
// 这条测试盯的是一个真实踩过的坑：收起态把标签压成 0 宽，但 flex-basis、
// margin-left:auto 和容器的 gap 三样都还在占位，于是同一个列表里跑出两套对齐，
// 有角标的项图标靠左、没角标的项图标居中 —— 侧栏一收就排成两列。
// 品牌标记（32px）和状态点（6px）也栽在同一个 gap 上。
//
// CSS 字符串断言只能证明「规则还在」，证明不了「量出来对齐」；真正的验收是
// 浏览器里量所有 .nav-item svg 的 getBoundingClientRect().x 必须只有一个值。
func TestWebUIRailKeepsIconsAligned(t *testing.T) {
	page := readIndex(t)
	for _, rule := range []string{
		// 三样都要归零，缺一样就会错位
		`flex:0 0 0px;width:0;margin:0;overflow:hidden`,
		`:root[data-rail="1"] .nav-item{justify-content:center;gap:0;padding:9px 0}`,
		`:root[data-rail="1"] .brand{flex:none;justify-content:center;gap:0;padding:0}`,
		`:root[data-rail="1"] .row-s{gap:0}`,
	} {
		if !strings.Contains(page, rule) {
			t.Errorf("收起态的对齐规则不见了: %s", rule)
		}
	}
}

// 面板标题上不再有 01/02 之类的序号。
func TestWebUIPanelsHaveNoOrdinalBadge(t *testing.T) {
	if page := readIndex(t); strings.Contains(page, `class="idx"`) {
		t.Error("面板标题里还有 class=\"idx\" 序号；这东西没有信息量，已经决定去掉")
	}
}

// 规则「限定站点」下拉必须能显示界面上已不再提供的站点。
//
// 站点列表从四家收到两家之后，老配置里 site: mikan 的规则会让 <select> 回落到
// 第一项「不限站点」：显示是错的，而且用户碰一下下拉，onchange 就把 cfg 里的
// site 写成空串 —— 「只在 mikan 生效」静默变成「所有站点生效」，改变主 RSS 的挑选结果。
// 修法是给这种值补一个选项。这条测试钉住那个兜底存在。
func TestWebUIRendersLegacyRuleSite(t *testing.T) {
	page := readIndex(t)
	if !strings.Contains(page, "legacySiteOption(") {
		t.Error("规则站点下拉缺少 legacySiteOption 兜底：老配置里的 animegarden / mikan 会被清成空值")
	}
	if !strings.Contains(page, "配置里有，界面不再提供") {
		t.Error("兜底选项没有标明它来自配置文件，用户会以为界面支持这个站点")
	}
}

// 设置页支持深色/浅色双模式：默认跟随系统，右上角可以手动切三档。
// 这几块缺一个，主题功能都会静默失效（比如少了浅色色板 -> 切了没反应），所以钉住。
func TestWebUIHasThemeSupport(t *testing.T) {
	page := readIndex(t)

	// 1. 浅色色板。深色是 :root 默认值，不需要单独一块。
	if !strings.Contains(page, `:root[data-theme="light"]{`) {
		t.Error(`缺少浅色色板 :root[data-theme="light"]`)
	}
	// 2. 三档按钮都要在，且带可访问名。
	for _, mode := range []string{"auto", "light", "dark"} {
		if !strings.Contains(page, `data-theme-mode="`+mode+`"`) {
			t.Errorf("右上角缺少 %q 档的主题按钮", mode)
		}
	}
	if !strings.Contains(page, `class="theme-seg" role="group" aria-label="主题"`) {
		t.Error("主题切换控件缺少 role=group 与 aria-label")
	}
	// 3. accent 必须拆成「填充」和「文字」两个变量。
	//    浅色下填充要亮、文字要暗，合成一个变量必有一边看不清。
	for _, v := range []string{"--accent:", "--accent-hover:", "--accent-bright:", "--accent-ink:"} {
		if !strings.Contains(page, v) {
			t.Errorf("缺少变量 %s（accent 的填充/悬停/文字三个角色必须分开）", v)
		}
	}
	// 4. 主题必须在首次绘制之前定好，否则跟随系统的深色用户会先闪一帧亮色。
	//    所以设置主题的内联脚本要出现在 <body> 之前。
	script := strings.Index(page, "anisync-theme")
	body := strings.Index(page, "<body")
	if script < 0 {
		t.Fatal("找不到设置主题的代码（localStorage 键 anisync-theme）")
	}
	if body < 0 {
		t.Fatal("找不到 <body>")
	}
	if script > body {
		t.Error("设置主题的脚本在 <body> 之后，深色用户会先看到一帧亮色")
	}
	// 5. auto 档靠系统偏好解析，必须真的读这个媒体查询。
	if !strings.Contains(page, "prefers-color-scheme: dark") {
		t.Error("找不到 prefers-color-scheme 查询，auto 档没法跟随系统")
	}
	if !strings.Contains(page, "localStorage.setItem") {
		t.Error("用户选的档位没有被记住")
	}
}

func readIndex(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("读取设置页失败: %v", err)
	}
	return string(raw)
}

// newTestServer 造一个不带代理、不写磁盘缓存的 Server，让下面的网络测试可重复。
func newTestServer(t *testing.T) *Server {
	t.Helper()
	// 环境里若设了 ANISYNC_PROXY，会盖到所有出站请求上；测试要的是直连。
	t.Setenv("ANISYNC_PROXY", "")
	cfg := config.Default()
	cfg.Proxy = ""
	return New(cfg)
}

// 封面代理是刻意留出来的 SSRF 面：只认白名单主机，且必须精确匹配。
// 这里同时钉住「一个出站请求都不许发」—— 拦截必须发生在拨号之前。
func TestBgmCoverRejectsForeignHost(t *testing.T) {
	// 陷阱服务器：一旦被访问，就说明白名单没拦住，直接判失败。
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("不该发起任何出站请求，实际请求了 %s", r.URL.String())
	}))
	defer trap.Close()

	srv := newTestServer(t)

	cases := []struct {
		name string
		raw  string
	}{
		{"后缀伪装成 lain.bgm.tv 的域名", "https://evil-lain.bgm.tv.attacker.com/x.jpg"},
		{"本机回环地址", "http://127.0.0.1:9/x.jpg"},
		{"陷阱服务器（回环）", trap.URL + "/x.jpg"},
		{"非 http(s) 协议", "file:///etc/passwd"},
		{"白名单之外的普通域名", "https://example.com/x.jpg"},
		{"空地址", ""},
		{"白名单主机但端口不对", "https://lain.bgm.tv:8080/x.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/bgm/cover?u="+url.QueryEscape(tc.raw), nil)
			rec := httptest.NewRecorder()
			srv.handleBgmCover(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("应回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "不支持的图片地址") {
				t.Errorf("错误信息不对: %s", rec.Body.String())
			}
		})
	}
}

// 放行之后必须真的把图片转发出去 —— 只测「拦住」的话，把处理函数写死成 400 也能过。
func TestBgmCoverForwardsImage(t *testing.T) {
	payload := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR-fake-cover-bytes")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cover.png" {
			t.Errorf("请求路径不对: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()

	// 白名单是精确匹配主机名（不含端口），所以临时放行 httptest 用的 127.0.0.1。
	// 端口也在白名单里（缺省 / 80 / 443），所以连随机端口一起放行。
	hostPort := strings.TrimPrefix(upstream.URL, "http://")
	host, port, _ := strings.Cut(hostPort, ":")
	coverHostAllow[host] = true
	coverPortAllow[port] = true
	t.Cleanup(func() { delete(coverHostAllow, host); delete(coverPortAllow, port) })

	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/bgm/cover?u="+url.QueryEscape(upstream.URL+"/cover.png"), nil)
	rec := httptest.NewRecorder()
	srv.handleBgmCover(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("应回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type 应透传上游的 image/png，实际 %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=2592000" {
		t.Errorf("Cache-Control 应为 public, max-age=2592000，实际 %q", cc)
	}
	if !bytes.Equal(rec.Body.Bytes(), payload) {
		t.Errorf("图片字节没有原样转发: %q", rec.Body.Bytes())
	}
}

// favicon 深浅两版都要能取到，且认不出的 theme 落回浅色。
func TestFaviconServesSVG(t *testing.T) {
	srv := newTestServer(t)

	fetch := func(query string) (int, string, []byte) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleFavicon(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg"+query, nil))
		return rec.Code, rec.Header().Get("Content-Type"), rec.Body.Bytes()
	}

	check := func(label string, code int, contentType string, body []byte) {
		t.Helper()
		if code != http.StatusOK {
			t.Fatalf("%s 应回 200，实际 %d", label, code)
		}
		if contentType != "image/svg+xml" {
			t.Errorf("%s 的 Content-Type 应为 image/svg+xml，实际 %q", label, contentType)
		}
		if !bytes.HasPrefix(body, []byte("<svg")) && !bytes.HasPrefix(body, []byte("<?xml")) {
			t.Errorf("%s 返回的不是 SVG: %.40q", label, body)
		}
	}

	lightCode, lightType, light := fetch("")
	check("/favicon.svg", lightCode, lightType, light)

	darkCode, darkType, dark := fetch("?theme=dark")
	check("/favicon.svg?theme=dark", darkCode, darkType, dark)

	if bytes.Equal(light, dark) {
		t.Error("浅色与深色图标的内容应不一样")
	}

	if _, _, unknown := fetch("?theme=neon"); !bytes.Equal(unknown, light) {
		t.Error("认不出的 theme 应落回浅色图标")
	}
}

// 搜索关键字为空要就地回 400，且不碰缓存、不发请求。
func TestBgmSearchRejectsEmptyQuery(t *testing.T) {
	srv := newTestServer(t)
	for _, query := range []string{"", "?q=", "?q=%20%20", "?limit=5"} {
		rec := httptest.NewRecorder()
		srv.handleBgmSearch(rec, httptest.NewRequest(http.MethodGet, "/api/bgm/search"+query, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q 应回 400，实际 %d，body=%s", query, rec.Code, rec.Body.String())
			continue
		}
		if !strings.Contains(rec.Body.String(), "请输入搜索关键字") {
			t.Errorf("%q 的错误信息不对: %s", query, rec.Body.String())
		}
	}
}

// limit 不进缓存键：同一个关键字换 limit 不该再打一次 Bangumi。
// 顺带钉住 head 的切片与非 nil 语义 —— 前端拿到就 .map()，null 会当场炸。
func TestBgmSearchCacheKeyIgnoresLimit(t *testing.T) {
	key := bgmSearchCacheKey("冰之城墙")
	if key != bgmSearchCacheKey("冰之城墙") {
		t.Error("同一个关键字两次应得到同一个缓存键")
	}
	if strings.Contains(key, "limit") || strings.HasSuffix(key, ":8") {
		t.Errorf("缓存键里不该带 limit: %q", key)
	}

	items := []bgm.Subject{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	if got := head(items, 2); len(got) != 2 || got[0].ID != "1" || got[1].ID != "2" {
		t.Errorf("head(items, 2) 应取前两条，实际 %+v", got)
	}
	if got := head(items, 99); len(got) != 3 {
		t.Errorf("head 的 n 超过长度应全给，实际 %d 条", len(got))
	}
	if got := head(nil, 5); got == nil || len(got) != 0 {
		t.Errorf("head(nil, 5) 应是空切片而不是 nil，实际 %#v", got)
	}

	// 必须是副本：调用方改到切片元素，不能污染缓存里那一份
	got := head(items, 1)
	got[0].ID = "改过了"
	if items[0].ID != "1" {
		t.Error("head 应返回副本，改它不该动到原切片")
	}
}

// handleSeason 把「本季新番」和实例现状合起来：列表来自放送表，added 由 targets 里的 bgmId 决定。
//
// 关键是**全程不按标题匹配**。用标题匹配的话，同一部番的中文名和日文名对不上，
// 已经加过的番会被再列一遍，用户点一次就多一份重复订阅。
func TestSeasonRouteMarksAlreadyAddedByBgmID(t *testing.T) {
	// 放送表的顺序刻意是「在看人数少的在前」，用来验证服务端确实重排过。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendar" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
		  {"weekday":{"id":6},"items":[
		    {"id":554779,"name":"生徒会にも穴はある！","name_cn":"学生会也有洞！","air_date":"2026-10-03",
		     "summary":"  有洞  ","images":{"common":"http://lain.bgm.tv/pic/cover/c/17/c7/554779.jpg"},
		     "rating":{"score":7.5},"collection":{"doing":13229}},
		    {"id":975,"name":"ONE PIECE","name_cn":"航海王","air_date":"1999-10-20",
		     "images":{"large":"http://lain.bgm.tv/pic/cover/l/92/97/975.jpg"},
		     "rating":{"score":8.3},"collection":{"doing":16361}}
		  ]}
		]`))
	}))
	defer upstream.Close()

	old := discover.BangumiAPI
	discover.BangumiAPI = upstream.URL
	defer func() { discover.BangumiAPI = old }()

	// handleSeason 会把放送表写进磁盘缓存，缓存路径由工作目录决定。
	// 不切目录的话会在 internal/webui/ 下留一个 data/cache.json —— 仓库根锚定的
	// /data/ 忽略规则拦不住它，测试跑完就多一个未跟踪文件。
	t.Chdir(t.TempDir())
	t.Setenv("ANISYNC_PROXY", "")

	cfg := config.Default()
	cfg.Proxy = ""
	// 这条用例测的是放送表那条路，把来源钉死；默认来源是 yuc，
	// 不钉的话它会去请求真的 raw.githubusercontent.com。
	cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
	// 已经加过航海王。注意这里只给 bgmId，标题故意写成别的写法：
	// added 判定必须靠 bgmId，靠标题就会漏。
	cfg.Targets = []config.Target{{Title: "海贼王", BgmID: "975"}}
	srv := New(cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/season", nil)
	rec := httptest.NewRecorder()
	srv.handleSeason(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("应回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Total int `json:"total"`
		Added int `json:"added"`
		Items []struct {
			BgmID string  `json:"bgmId"`
			Title string  `json:"title"`
			Cover string  `json:"cover"`
			Doing int     `json:"doing"`
			Score float64 `json:"score"`
			Added bool    `json:"added"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, rec.Body.String())
	}
	if got.Total != 2 || len(got.Items) != 2 {
		t.Fatalf("应返回 2 条，实际 total=%d len=%d", got.Total, len(got.Items))
	}
	if got.Added != 1 {
		t.Errorf("应只有 1 条标记为已添加，实际 %d", got.Added)
	}
	// 按在看人数倒序：975（16361）必须排在 554779（13229）前面，与上游给的顺序相反。
	if got.Items[0].BgmID != "975" || got.Items[1].BgmID != "554779" {
		t.Errorf("应按在看人数倒序，实际 %s, %s", got.Items[0].BgmID, got.Items[1].BgmID)
	}
	if !got.Items[0].Added {
		t.Error("975 在 targets 里（标题写法不同但 bgmId 相同），应标记为已添加")
	}
	if got.Items[1].Added {
		t.Error("554779 不在 targets 里，不该标记为已添加")
	}
	// 封面必须规范成 https：浏览器直连 lain.bgm.tv 取不到图，中转端点也按 https 白名单校验。
	if !strings.HasPrefix(got.Items[1].Cover, "https://") {
		t.Errorf("封面应规范成 https，实际 %q", got.Items[1].Cover)
	}
	// 没有 common 时回落到 large，不能因为缺一个尺寸就没封面。
	if !strings.HasPrefix(got.Items[0].Cover, "https://lain.bgm.tv/pic/cover/l/") {
		t.Errorf("缺 common 时应回落到 large，实际 %q", got.Items[0].Cover)
	}
}
