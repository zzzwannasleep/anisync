package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// useServer 把接口根地址临时指向测试服务器。
func useServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	oldAG, oldBgm := AnimeGardenAPI, BangumiAPI
	AnimeGardenAPI, BangumiAPI = server.URL, server.URL
	t.Cleanup(func() { AnimeGardenAPI, BangumiAPI = oldAG, oldBgm })
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("写响应失败: %v", err)
	}
}

// 只有日文标题带假名；中文标题也是汉字，光看「有没有汉字」分不出来。
func TestPickJPTitleUsesKana(t *testing.T) {
	got := pickJPTitle([]string{"Cyberpunk: Edgerunners 2", "サイバーパンク: エッジランナーズ２", "赛博朋克：边缘行者 2"})
	if got != "サイバーパンク: エッジランナーズ２" {
		t.Fatalf("应挑出带假名的日文原名，实际 %q", got)
	}
	if got := pickJPTitle([]string{"Fool Night", "愚者之夜"}); got != "" {
		t.Fatalf("没有假名时不应瞎猜，实际 %q", got)
	}
	if got := pickJPTitle(nil); got != "" {
		t.Fatalf("空列表应返回空串，实际 %q", got)
	}
}

// activedAt 是首播时间，不是更新时间：太老的番剧要挡掉，但未来的新番要留着。
func TestAnimeGardenSubjectsSkipsArchivedAndOld(t *testing.T) {
	old := time.Now().AddDate(0, 0, -400).Format(time.RFC3339)
	future := time.Now().AddDate(0, 0, 30).Format(time.RFC3339)
	fresh := time.Now().AddDate(0, 0, -10).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/subjects" {
			t.Errorf("意外的请求路径: %s", r.URL.Path)
		}
		writeJSON(t, w, map[string]any{
			"status": "OK",
			"subjects": []map[string]any{
				{"id": 1, "name": "在放的", "activedAt": fresh, "isArchived": false,
					"search": map[string]any{"include": []string{"Airing Now", "放送中のアニメ"}}},
				{"id": 2, "name": "还没开播的", "activedAt": future, "isArchived": false},
				{"id": 3, "name": "很老的", "activedAt": old, "isArchived": false},
				{"id": 4, "name": "已归档的", "activedAt": fresh, "isArchived": true},
			},
		})
	}))
	defer server.Close()
	useServer(t, server)

	items, err := animeGardenSubjects(context.Background(), server.Client(), config.DefaultDiscoverDays)
	if err != nil {
		t.Fatalf("拉取失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("应只留下「在放的」和「还没开播的」，实际 %d 条: %+v", len(items), items)
	}
	// 按首播时间倒序：未来的排在最前
	if items[0].BgmID != "2" || items[1].BgmID != "1" {
		t.Fatalf("应按首播时间倒序，实际 %s, %s", items[0].BgmID, items[1].BgmID)
	}
	if items[1].JPTitle != "放送中のアニメ" {
		t.Fatalf("应带上日文原名，实际 %q", items[1].JPTitle)
	}
	if items[1].Source != "" {
		t.Fatalf("来源由 Discover 统一填，单来源函数不该自己填，实际 %q", items[1].Source)
	}
}

// 同一个番剧被两个来源都扫到时只留一条，来源顺序即优先级；黑名单在合并后生效。
func TestDiscoverDedupesAndAppliesExclude(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/subjects":
			writeJSON(t, w, map[string]any{
				"status": "OK",
				"subjects": []map[string]any{
					{"id": 100, "name": "两边都有的", "activedAt": time.Now().Format(time.RFC3339)},
					{"id": 200, "name": "国漫不要的", "activedAt": time.Now().Format(time.RFC3339)},
				},
			})
		case r.URL.Path == "/calendar":
			writeJSON(t, w, []map[string]any{
				{"weekday": map[string]any{"id": 1}, "items": []map[string]any{
					{"id": 100, "name": "両方にある", "name_cn": "两边都有的"},
					{"id": 300, "name": "只有放送表有的", "name_cn": "只有放送表有的"},
				}},
			})
		default:
			t.Errorf("意外的请求路径: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	useServer(t, server)

	result, err := Discover(context.Background(), Options{
		Sources: []string{config.DiscoverAnimeGarden, config.DiscoverBgmCalendar},
		Days:    config.DefaultDiscoverDays,
		Exclude: []string{`国漫`},
	})
	if err != nil {
		t.Fatalf("发现失败: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("应剩「两边都有的」与「只有放送表有的」两条，实际 %d 条: %+v", len(result.Items), result.Items)
	}
	// 100 被两个来源扫到：保留先出现的 animegarden，而不是 calendar
	if result.Items[0].BgmID != "100" || result.Items[0].Source != config.DiscoverAnimeGarden {
		t.Fatalf("重复项应保留高优先级来源，实际 %+v", result.Items[0])
	}
	if result.Items[1].BgmID != "300" || result.Items[1].Source != config.DiscoverBgmCalendar {
		t.Fatalf("第二个来源独有的条目应保留，实际 %+v", result.Items[1])
	}
}

// 单个来源挂掉只降级、不中断：另一个来源照样能用。
func TestDiscoverDegradesWhenOneSourceFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/subjects" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		writeJSON(t, w, []map[string]any{
			{"weekday": map[string]any{"id": 1}, "items": []map[string]any{{"id": 7, "name": "还行", "name_cn": "还行"}}},
		})
	}))
	defer server.Close()
	useServer(t, server)

	result, err := Discover(context.Background(), Options{
		Sources: []string{config.DiscoverAnimeGarden, config.DiscoverBgmCalendar},
		Days:    config.DefaultDiscoverDays,
	})
	if err != nil {
		t.Fatalf("单个来源失败不应让整次发现失败: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("应保留可用来源的结果，实际 %+v", result.Items)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], config.DiscoverAnimeGarden) {
		t.Fatalf("应记录一条降级警告并指明来源，实际 %+v", result.Warnings)
	}
}

// 全部来源都失败才算真失败，不能静默返回空列表让人以为「没有新番」。
func TestDiscoverFailsWhenAllSourcesFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	useServer(t, server)

	if _, err := Discover(context.Background(), Options{Sources: []string{config.DiscoverAnimeGarden}}); err == nil {
		t.Fatal("所有来源都失败时必须返回错误")
	}
}

// 未知来源要报错，不能当成「没有结果」悄悄放过。
func TestDiscoverRejectsUnknownSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	useServer(t, server)

	_, err := Discover(context.Background(), Options{Sources: []string{"dmhy"}})
	if err == nil || !strings.Contains(err.Error(), "dmhy") {
		t.Fatalf("未知来源应报错并点出名字，实际 %v", err)
	}
}

// Bangumi 收藏要翻页，否则追番多的用户只能同步到前 50 部。
func TestBangumiCollectionsPaginates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		if got := r.URL.Query().Get("type"); got != "3" {
			t.Errorf("应只取「在看」，实际 type=%s", got)
		}
		if got := r.URL.Query().Get("subject_type"); got != "2" {
			t.Errorf("应只取动画，实际 subject_type=%s", got)
		}
		var rows []map[string]any
		switch offset {
		case "0":
			for i := 0; i < collectionsPageSize; i++ {
				rows = append(rows, map[string]any{
					"subject": map[string]any{"id": 1000 + i, "name": fmt.Sprintf("作品%d", i), "name_cn": fmt.Sprintf("作品%d", i)},
				})
			}
		case "50":
			rows = append(rows, map[string]any{
				"subject": map[string]any{"id": 9999, "name": "最後の作品", "name_cn": "最后一部"},
			})
		default:
			t.Errorf("翻到了不该翻的页: offset=%s", offset)
		}
		writeJSON(t, w, map[string]any{"total": collectionsPageSize + 1, "limit": collectionsPageSize, "offset": offset, "data": rows})
	}))
	defer server.Close()
	useServer(t, server)

	items, err := bangumiCollections(context.Background(), server.Client(), "someone")
	if err != nil {
		t.Fatalf("拉取收藏失败: %v", err)
	}
	if len(items) != collectionsPageSize+1 {
		t.Fatalf("应翻完两页拿到 %d 条，实际 %d 条", collectionsPageSize+1, len(items))
	}
	last := items[len(items)-1]
	if last.BgmID != "9999" || last.Title != "最后一部" || last.JPTitle != "最後の作品" {
		t.Fatalf("最后一条应带中文名与日文原名，实际 %+v", last)
	}
}

func TestBangumiCollectionsRequiresUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	useServer(t, server)

	if _, err := bangumiCollections(context.Background(), server.Client(), "  "); err == nil {
		t.Fatal("没填用户名时必须报错")
	}
}

// 发现结果转成 target 时，季度与偏移必须留空：留给 ANI-RSS 自己按集数推断，
// 一旦填了 0 就会把所有自动新增的番剧偏移洗成同一个值。
func TestItemTargetLeavesOffsetsUnset(t *testing.T) {
	item := Item{BgmID: "665390", Title: "冰之城墙", JPTitle: "氷の城壁"}
	target := item.Target(true)

	if target.Season != nil || target.Offset != nil || target.StandbyOffset != nil {
		t.Fatalf("自动发现的条目不应写季度与偏移，实际 %+v", target)
	}
	if target.Enable == nil || !*target.Enable {
		t.Fatal("enable=true 时新增的订阅应直接启用")
	}
	if off := item.Target(false); off.Enable == nil || *off.Enable {
		t.Fatal("enable=false 时新增的订阅应保持停用")
	}
	if target.Title != "冰之城墙" || target.JPTitle != "氷の城壁" || target.BgmID != "665390" {
		t.Fatalf("字段搬运不对: %+v", target)
	}
}

// 缓存命中时不应再打上游请求。
func TestDiscoverUsesCache(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		writeJSON(t, w, map[string]any{
			"status":   "OK",
			"subjects": []map[string]any{{"id": 1, "name": "缓存住的", "activedAt": time.Now().Format(time.RFC3339)}},
		})
	}))
	defer server.Close()
	useServer(t, server)

	store := cache.Open(t.TempDir()+"/cache.json", time.Hour)
	opts := Options{Sources: []string{config.DiscoverAnimeGarden}, Days: config.DefaultDiscoverDays, Store: store}

	for round := 0; round < 2; round++ {
		if _, err := Discover(context.Background(), opts); err != nil {
			t.Fatalf("第 %d 轮发现失败: %v", round+1, err)
		}
	}
	if hits != 1 {
		t.Fatalf("第二次应命中缓存，实际请求了 %d 次", hits)
	}
}
