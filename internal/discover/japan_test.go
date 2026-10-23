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

// 放送表本身不带地区与平台，只能靠条目详情的 platform + meta_tags 判断。
// 这张表就是判定规则本身：两个条件都要满足，缺一不可。
func TestIsJapaneseTV(t *testing.T) {
	cases := []struct {
		name string
		meta subjectMeta
		want bool
	}{
		{"日本 TV", subjectMeta{Platform: "TV", MetaTags: []string{"TV", "日本", "漫画改"}}, true},
		{"日本剧场版", subjectMeta{Platform: "剧场版", MetaTags: []string{"日本", "剧场版"}}, true},

		{"中国 WEB（大王饶命）", subjectMeta{Platform: "WEB", MetaTags: []string{"小说改", "中国", "WEB"}}, false},
		{"欧美 WEB（极恶老大）", subjectMeta{Platform: "WEB", MetaTags: []string{"原创", "欧美", "WEB"}}, false},
		{"美国 TV（极道寿司次郎）", subjectMeta{Platform: "TV", MetaTags: []string{"TV", "漫画改", "美国"}}, false},
		{"法国 TV（The Marsupilamis）", subjectMeta{Platform: "TV", MetaTags: []string{"TV", "法国"}}, false},
		{"日本 OVA", subjectMeta{Platform: "OVA", MetaTags: []string{"日本", "OVA"}}, false},
		{"日本 WEB（迷你动画剧场）", subjectMeta{Platform: "WEB", MetaTags: []string{"日本", "WEB"}}, false},
		{"TV 但没地区标签（屁屁侦探）", subjectMeta{Platform: "TV", MetaTags: []string{"TV"}}, false},
		{"什么都没有", subjectMeta{}, false},
	}
	for _, tc := range cases {
		if got := isJapaneseTV(tc.meta); got != tc.want {
			t.Errorf("%s: isJapaneseTV(%+v) = %v, want %v", tc.name, tc.meta, got, tc.want)
		}
	}
}

// filterJapaneseTV 要按 bgmId 逐条查详情再过滤，并且查不到的条目必须保留。
func TestFilterJapaneseTV(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimPrefix(r.URL.Path, "/v0/subjects/") {
		case "1":
			_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","日本"]}`))
		case "2":
			_, _ = w.Write([]byte(`{"platform":"WEB","meta_tags":["小说改","中国","WEB"]}`))
		case "3":
			_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","漫画改","美国"]}`))
		case "4":
			_, _ = w.Write([]byte(`{"platform":"OVA","meta_tags":["日本","OVA"]}`))
		case "5":
			_, _ = w.Write([]byte(`{"platform":"剧场版","meta_tags":["日本","剧场版"]}`))
		default:
			// 6 走这里：详情查不到
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()

	old := BangumiAPI
	BangumiAPI = upstream.URL
	defer func() { BangumiAPI = old }()

	client, err := httpx.New("", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store := cache.Open(filepath.Join(t.TempDir(), "cache.json"), time.Hour)

	in := []Item{
		{BgmID: "1", Title: "日本TV"},
		{BgmID: "2", Title: "国漫"},
		{BgmID: "3", Title: "美漫"},
		{BgmID: "4", Title: "OVA"},
		{BgmID: "5", Title: "剧场版"},
		{BgmID: "6", Title: "查不到详情的"},
	}
	out := filterJapaneseTV(context.Background(), client, store, in)

	got := make([]string, 0, len(out))
	for _, it := range out {
		got = append(got, it.BgmID)
	}
	// 1 和 5 是日本 TV/剧场版；6 查不到详情 —— 保留，一次网络抖动不该静默吞掉一部番。
	want := "1,5,6"
	if strings.Join(got, ",") != want {
		t.Errorf("过滤结果 = [%s], want [%s]", strings.Join(got, ","), want)
	}

	// 顺序必须保持原样，否则「按在看人数排序」会被打乱
	for i := 1; i < len(out); i++ {
		if out[i-1].Title == "" || out[i].Title == "" {
			t.Fatal("条目不该丢标题")
		}
	}

	// 第二遍：查成功过的条目必须走缓存，只有查不到的 6 会再试一次。
	// 失败的详情刻意不缓存 —— 一次网络抖动不该被记成「这部番永远查不到」。
	var secondPass []string
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondPass = append(secondPass, strings.TrimPrefix(r.URL.Path, "/v0/subjects/"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","日本"]}`))
	}))
	defer closed.Close()
	old2 := BangumiAPI
	BangumiAPI = closed.URL
	defer func() { BangumiAPI = old2 }()

	again := filterJapaneseTV(context.Background(), client, store, in)
	if len(again) != len(out) {
		t.Errorf("第二遍结果不一致：%d vs %d（条目详情没走缓存）", len(again), len(out))
	}
	if strings.Join(secondPass, ",") != "6" {
		t.Errorf("第二遍只该重试查不到的 6，实际请求了 [%s]", strings.Join(secondPass, ","))
	}
}

// 关掉 JapanOnly 时不该去查条目详情：那 110 次请求是白花的。
func TestBangumiCalendarSkipsEnrichmentWhenNotJapanOnly(t *testing.T) {
	var detailHits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v0/subjects/") {
			detailHits++
			_, _ = w.Write([]byte(`{"platform":"WEB","meta_tags":["中国"]}`))
			return
		}
		_, _ = w.Write([]byte(`[{"weekday":{"id":1},"items":[{"id":1,"name":"x","name_cn":"国漫"}]}]`))
	}))
	defer upstream.Close()

	old := BangumiAPI
	BangumiAPI = upstream.URL
	defer func() { BangumiAPI = old }()

	client, err := httpx.New("", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	items, stats, err := bangumiCalendar(context.Background(), client, Options{JapanOnly: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || stats.Filtered != 0 {
		t.Errorf("关掉过滤时应原样返回 1 条、丢弃 0 条，实际 %d 条 / 丢弃 %d", len(items), stats.Filtered)
	}
	if detailHits != 0 {
		t.Errorf("关掉 JapanOnly 还去查了 %d 次条目详情", detailHits)
	}
}

// 过滤条数必须跟着列表一起缓存：否则缓存命中时界面会显示「已滤掉 0 部」，
// 而列表明明短了一截 —— 冷热两条路径报出来的数字必须一致。
func TestFilteredCountSurvivesCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/v0/subjects/") {
			// 只有 1 是日本 TV，2 是国漫
			if strings.HasSuffix(r.URL.Path, "/1") {
				_, _ = w.Write([]byte(`{"platform":"TV","meta_tags":["TV","日本"]}`))
				return
			}
			_, _ = w.Write([]byte(`{"platform":"WEB","meta_tags":["中国","WEB"]}`))
			return
		}
		_, _ = w.Write([]byte(`[{"weekday":{"id":1},"items":[
		  {"id":1,"name":"a","name_cn":"日漫"},
		  {"id":2,"name":"b","name_cn":"国漫"}
		]}]`))
	}))
	defer upstream.Close()

	old := BangumiAPI
	BangumiAPI = upstream.URL
	defer func() { BangumiAPI = old }()

	t.Setenv("ANISYNC_PROXY", "")
	store := cache.Open(filepath.Join(t.TempDir(), "cache.json"), time.Hour)
	opts := Options{Sources: []string{"bangumi-calendar"}, JapanOnly: true, Store: store}

	client, err := httpx.New("", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	first, stats, err := fetchSource(context.Background(), client, opts, "bangumi-calendar")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || stats.Filtered != 1 {
		t.Fatalf("第一次应留 1 条、滤掉 1 条，实际 %d 条 / %d", len(first), stats.Filtered)
	}

	// 第二次必须走缓存，而且过滤条数不能丢
	second, stats2, err := fetchSource(context.Background(), client, opts, "bangumi-calendar")
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Errorf("缓存命中应返回 1 条，实际 %d", len(second))
	}
	if stats2.Filtered != stats.Filtered {
		t.Errorf("缓存命中后过滤条数从 %d 变成了 %d；界面会显示「已滤掉 0 部」", stats.Filtered, stats2.Filtered)
	}
}
