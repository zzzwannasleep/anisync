package discover

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/httpx"
)

// 季度代码必须是 YYYYMM 且 MM ∈ {01,04,07,10}，否则会去取一个不存在的页面。
func TestYucQuarter(t *testing.T) {
	cases := []struct {
		month time.Month
		want  string
	}{
		{time.January, "202601"}, {time.February, "202601"}, {time.March, "202601"},
		{time.April, "202604"}, {time.June, "202604"},
		{time.July, "202607"}, {time.September, "202607"},
		{time.October, "202610"}, {time.December, "202610"},
	}
	for _, tc := range cases {
		got := yucQuarter(time.Date(2026, tc.month, 15, 0, 0, 0, 0, time.UTC))
		if got != tc.want {
			t.Errorf("%v 应落在 %s，实际 %s", tc.month, tc.want, got)
		}
	}
}

// 归一化要能把同一部番的**写法差异**压到同一个键上。
// 这是整条 Yuc 链路的地基：压不到一起就等于认不出这部番。
//
// 注意它只管格式（大小写、全角半角、标点、季度的说法），**不管语言差异**。
// 「佐々木とピーちゃん」和「佐佐木与文鸟小哔」是两部番的两种语言的名字，
// 归一化本来就压不到一起 —— 那种情况靠放送表索引里的中文名，或者检索兜底。
func TestFoldTitle(t *testing.T) {
	same := [][2]string{
		{"魔法の姉妹ルルットリリィ 第2クール", "魔法の姉妹ルルットリリィ 第2期"},
		{"佐々木とピーちゃん Season2", "佐々木とピーちゃん 第2季"},
		{"STEEL BALL RUN", "steel ball run"},
		{"魔法★探险家", "魔法探险家"},
		{"転生貴族、鑑定スキルで成り上がる 第3期", "転生貴族鑑定スキルで成り上がる第3期"},
		{"Ａ Ｂ Ｃ", "abc"}, // 全角折半角
		{"進撃の巨人 Season 3 Part 2", "進撃の巨人 第3期 第2部分"},
		{"ダンジョン飯 第2シーズン", "ダンジョン飯 第2期"},
		// 罗马数字：Yuc 用 U+2161「Ⅱ」，Bangumi 用两个 ASCII 的「II」。
		// 不折的话这两条落不到同一个键上，会退化到检索并认成第一季（实测踩过）。
		{"転生したら剣でしたⅡ", "転生したら剣でしたII"},
		{"ダンジョン飯 Ⅳ", "ダンジョン飯 IV"},
		{"魔法少女リリカルなのはⅫ", "魔法少女リリカルなのはXII"},
	}
	for _, pair := range same {
		a, b := foldTitle(pair[0]), foldTitle(pair[1])
		if a != b {
			t.Errorf("归一化后应该相同：\n  %q -> %q\n  %q -> %q", pair[0], a, pair[1], b)
		}
	}

	// 不同的番不能被压到一起，否则会把订阅建到别的番上
	diff := [][2]string{
		{"氷の城壁 第2期", "氷の城壁 第3期"},
		{"ONE PIECE", "ONE PUNCH MAN"},
	}
	for _, pair := range diff {
		if foldTitle(pair[0]) == foldTitle(pair[1]) {
			t.Errorf("不该被压成同一个键：%q / %q", pair[0], pair[1])
		}
	}
	if foldTitle("   ") != "" {
		t.Error("空白标题应归一成空串，否则会拿它去建索引")
	}
}

// Yuc 那条路的完整链路：Yuc 给「这一季有哪些番」，Bangumi 放送表补 bgmId。
// 匹配走日文原名 —— 中文译名是各家自己的写法，对不上。
func TestYucSubjectsMatchesByJapaneseName(t *testing.T) {
	// 假的 Yuc 归档：一个季度页，三个条目
	yuc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "title": "2026年10月新番",
		  "introductions": [
		    {"titles":{"chinese":{"text":"顶点武装"},"original":{"text":"VERTEX FORCE"}}},
		    {"titles":{"chinese":{"text":"冰之城墙 第二季"},"original":{"text":"氷の城壁 第2期"}}},
		    {"titles":{"chinese":{"text":"这个BGM里没有"},"original":{"text":"NOT IN BANGUMI"}}}
		  ]
		}`))
	}))
	defer yuc.Close()

	// 假的放送表：name 是日文原名，name_cn 是中文名
	bgmSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
		  {"weekday":{"id":6},"items":[
		    {"id":636512,"name":"至高之力","name_cn":"顶点武装","air_date":"2026-10-03",
		     "images":{"common":"http://lain.bgm.tv/pic/cover/c/1.jpg"},
		     "rating":{"score":5.7},"collection":{"doing":1313}},
		    {"id":665390,"name":"氷の城壁 第2期","name_cn":"冰之城墙 第二季","air_date":"2026-10-04",
		     "images":{"common":"http://lain.bgm.tv/pic/cover/c/2.jpg"},
		     "rating":{"score":7.4},"collection":{"doing":2882}}
		  ]}
		]`))
	}))
	defer bgmSrv.Close()

	oldAPI, oldYuc := BangumiAPI, YucArchive
	BangumiAPI, YucArchive = bgmSrv.URL, yuc.URL
	defer func() { BangumiAPI, YucArchive = oldAPI, oldYuc }()

	client, err := httpx.New("", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store := cache.Open(filepath.Join(t.TempDir(), "cache.json"), time.Hour)

	items, stats, err := yucSubjects(context.Background(), client, Options{Store: store})
	if err != nil {
		t.Fatal(err)
	}

	// 「顶点武装」的日文原名是 VERTEX FORCE，而放送表里那一条 name 是「至高之力」、
	// name_cn 才是「顶点武装」。日文原名对不上，但中文名对得上 —— 两条路都得留。
	if len(items) != 2 {
		t.Fatalf("应认出 2 部（第 3 部 Bangumi 上没有），实际 %d：%+v", len(items), items)
	}
	if stats.Unmatched != 1 {
		t.Errorf("应有 1 部认不出来并被计数，实际 %d", stats.Unmatched)
	}

	byID := map[string]Item{}
	for _, it := range items {
		byID[it.BgmID] = it
	}
	if _, ok := byID["636512"]; !ok {
		t.Error("「顶点武装」应通过中文名匹配到 636512")
	}
	if _, ok := byID["665390"]; !ok {
		t.Error("「氷の城壁 第2期」应通过日文原名精确匹配到 665390")
	}

	// 元数据必须来自放送表那份，不是 Yuc 的 —— 封面要 lain.bgm.tv（在代理白名单里），
	// Yuc 的封面在 i0.hdslb.com 上，设置页的封面中转不认。
	vertex := byID["636512"]
	if !strings.HasPrefix(vertex.Cover, "https://lain.bgm.tv/") {
		t.Errorf("封面应取放送表那份并规范成 https，实际 %q", vertex.Cover)
	}
	if vertex.Doing != 1313 {
		t.Errorf("在看人数应来自放送表，实际 %d", vertex.Doing)
	}
	if vertex.Weekday != 6 {
		t.Errorf("放送星期应来自放送表，实际 %d", vertex.Weekday)
	}

	// 按在看人数倒序
	if items[0].BgmID != "665390" {
		t.Errorf("应按在看人数倒序（2882 > 1313），实际第一条是 %s", items[0].BgmID)
	}
}

// 多个搜索结果时要挑最像的那个，不能取第一个沾边的。
//
// 踩过的坑：搜索「転生したら剣でしたII」同时返回了「転生したら剣でしたII」和第一季
// 「転生したら剣でした」，取第一个沾边的会把续作认成第一季，于是用户明明订了第二季，
// 界面上却又冒出一条「转生就是剑」。
func TestBestTitleRank(t *testing.T) {
	cases := []struct {
		name string
		want string
		cand []string
		rank int
	}{
		{"完全相同", "x第2期", []string{"x第2期", "x"}, 2},
		{"候选更具体", "x", []string{"x第2期"}, 1},
		{"候选更笼统（第一季）", "x第2期", []string{"x"}, 0},
		{"完全不沾边", "x", []string{"y"}, -1},
		{"空目标", "", []string{"x"}, -1},
		{"中文名命中", "愚者之夜", []string{"フールナイト", "愚者之夜"}, 2},
		{"续作里挑更具体的那个", "転生したら剣でしたii", []string{"転生したら剣でした", "転生したら剣でしたii"}, 2},
	}
	for _, tc := range cases {
		if got := bestTitleRank(tc.want, tc.cand...); got != tc.rank {
			t.Errorf("%s: bestTitleRank(%q, %v) = %d, want %d", tc.name, tc.want, tc.cand, got, tc.rank)
		}
	}
}

// 归档里没有这一季时，报错要说清楚是哪一季、去哪个地址找，而不是一句 HTTP 404。
func TestYucMissingQuarterExplainsItself(t *testing.T) {
	yuc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer yuc.Close()

	old := YucArchive
	YucArchive = yuc.URL
	defer func() { YucArchive = old }()

	client, err := httpx.New("", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fetchYucQuarter(context.Background(), client, "209901")
	if err == nil {
		t.Fatal("应该报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, "209901") || !strings.Contains(msg, "pages/209901.json") {
		t.Errorf("错误信息应指出是哪一季、去哪找，实际：%s", msg)
	}
}
