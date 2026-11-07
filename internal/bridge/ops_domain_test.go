package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
	"github.com/zzzwannasleep/anisync/internal/lease"
)

// fakeState 记录假实例收到过什么，用来断言「该写的写了、不该写的一条都没写」。
type fakeState struct {
	mu       sync.Mutex
	setAni   []map[string]any
	addAni   []map[string]any
	delAni   int
	reLists  int
	listAni  int
	rssToAni int
}

func (s *fakeState) countSetAni() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.setAni)
}

func (s *fakeState) lastSetAni() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.setAni) == 0 {
		return nil
	}
	return s.setAni[len(s.setAni)-1]
}

// newRichInstance 起一个带状态的假 ANI-RSS 实例。
//
// 它只说协议里真实存在的形状（code/message/data 信封、weekList 扁平化前的样子），
// 所以「面板上那条数据是怎么来的」能一路追到这里。
func newRichInstance(t *testing.T) (*httptest.Server, *fakeState) {
	t.Helper()
	state := &fakeState{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/about"):
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"version":"3.2.52"}}`))
		case strings.HasSuffix(r.URL.Path, "/api/listAni"):
			state.mu.Lock()
			state.listAni++
			state.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":{"total":1,"weekList":[
			  {"weekLabel":"四","items":[
			    {"id":"1234","title":"冰之城墙","bgmUrl":"https://bgm.tv/subject/665390",
			     "url":"https://old.example.com/rss.xml","subgroup":"旧组","season":1,"offset":0,
			     "enable":true,"standbyRssList":[]}
			  ]}
			]}}`))
		case strings.HasSuffix(r.URL.Path, "/api/setAni"):
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			state.mu.Lock()
			state.setAni = append(state.setAni, body)
			state.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
		case strings.HasSuffix(r.URL.Path, "/api/addAni"):
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			state.mu.Lock()
			state.addAni = append(state.addAni, body)
			state.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
		case strings.HasSuffix(r.URL.Path, "/api/deleteAni"):
			state.mu.Lock()
			state.delAni++
			state.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":null}`))
		case strings.HasSuffix(r.URL.Path, "/api/rssToAni"):
			state.mu.Lock()
			state.rssToAni++
			state.mu.Unlock()
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":
			  {"id":"9999","title":"新番","bgmUrl":"https://bgm.tv/subject/665390","enable":false,
			   "season":2,"offset":-12,"url":"https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=lh",
			   "standbyRssList":[]}}`))
		case strings.HasSuffix(r.URL.Path, "/api/aniBTGroup"):
			_, _ = w.Write([]byte(`{"code":200,"message":"ok","data":[
			  {"label":"猎户发布组","name":"猎户发布组","slug":"lh","rss":"https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=lh",
			   "bgmUrl":"https://bgm.tv/subject/665390","total":42,
			   "items":[{"title":"[猎户压制部] 冰之城墙 - 01 [简日内嵌]"}]}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, state
}

// coreWithInstance 造一个「一个实例指向假服务」的工作区。
func coreWithInstance(t *testing.T, baseURL string, mutate func(*config.Config)) (*Core, string) {
	t.Helper()
	core, path := testCore(t)
	writeConfig(t, path, func(cfg *config.Config) {
		cfg.Sources.Instance = "主库"
		cfg.Sources.Order = []string{"anibt"}
		cfg.Rules.MaxStandby = 1
		cfg.Rules.Primary = []config.Rule{{Name: "全都要", Match: `.+`, Priority: 100, Scope: "both", In: "any"}}
		cfg.Instances = []config.Instance{{Name: "主库", BaseURL: baseURL, APIKey: "abcdefghijklmnop"}}
		if mutate != nil {
			mutate(cfg)
		}
	})
	core.Init(path, "")
	return core, path
}

// ---------- instance ----------

func TestInstanceListAndTest(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	list := dataOf(t, call(t, core, "instance.list", map[string]any{}, ""))
	instances, _ := list["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("应报 1 个实例，实际 %v", list["instances"])
	}
	view := instances[0].(map[string]any)
	if view["apiKey"] != "abcdef…op" {
		t.Errorf("apiKey 必须遮蔽成前 6 后 2，实际 %v", view["apiKey"])
	}
	if view["hasApiKey"] != true || view["enabled"] != true {
		t.Errorf("实例状态字段不对: %v", view)
	}

	data := dataOf(t, call(t, core, "instance.test", map[string]any{"name": "主库"}, ""))
	if data["ok"] != true {
		t.Fatalf("连通性测试应成功: %v", data)
	}
	if numericEquals(data["subscriptionCount"], 0) {
		t.Error("应报出实例里的订阅条数")
	}
	if about, _ := data["about"].(map[string]any); about["version"] != "3.2.52" {
		t.Errorf("about 应原样带回来: %v", data["about"])
	}
	if data["error"] != nil {
		t.Errorf("成功时不该有 error 字段，实际 %v", data["error"])
	}
}

// 实例连不上时：instance.test 用 ok:false + error 报，而不是让整次调用失败。
func TestInstanceTestReportsFailureSoftly(t *testing.T) {
	dead := httpNotFoundServer(t)
	defer dead.Close()
	core, _ := coreWithInstance(t, dead.URL, nil)

	data := dataOf(t, call(t, core, "instance.test", map[string]any{"name": "主库"}, ""))
	if data["ok"] != false {
		t.Errorf("连不上时 ok 应是 false，实际 %v", data)
	}
	if text, _ := data["error"].(string); text == "" {
		t.Error("连不上时必须给出原因（界面要显示它）")
	}
}

// 实例名不存在是硬错误：不该被当成「连接失败」糊过去。
func TestInstanceOpsRejectUnknownName(t *testing.T) {
	core, _ := testCore(t)
	for _, op := range []string{"instance.test", "instance.subscriptions"} {
		env := call(t, core, op, map[string]any{"name": "不存在的实例"}, "")
		if env.OK || env.Error.Code != CodeNotFound {
			t.Errorf("%s 对不存在的实例应回 not_found，实际 %+v", op, env.Error)
		}
	}
	env := call(t, core, "instance.subscriptions", map[string]any{}, "")
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Errorf("缺少 name 应回 bad_request，实际 %+v", env.Error)
	}
}

func TestInstanceSubscriptionsOp(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	first := dataOf(t, call(t, core, "instance.subscriptions", map[string]any{"name": "主库"}, ""))
	items, _ := first["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("应回 1 条订阅，实际 %v", first["items"])
	}
	view := items[0].(map[string]any)
	if view["key"] != "id:1234" || view["title"] != "冰之城墙" {
		t.Errorf("订阅视图不对: %v", view)
	}
	if view["raw"] == nil {
		t.Error("raw 必须原样保留")
	}

	// refresh=false 允许吃一层很短的缓存：连读两次只该打上游一次。
	call(t, core, "instance.subscriptions", map[string]any{"name": "主库", "refresh": false}, "")
	call(t, core, "instance.subscriptions", map[string]any{"name": "主库", "refresh": false}, "")
	state.mu.Lock()
	after := state.listAni
	state.mu.Unlock()
	if after != 1 {
		t.Errorf("refresh=false 时应吃缓存，实际打了上游 %d 次", after)
	}
	// refresh=true 必须重新拉。
	call(t, core, "instance.subscriptions", map[string]any{"name": "主库", "refresh": true}, "")
	state.mu.Lock()
	afterRefresh := state.listAni
	state.mu.Unlock()
	if afterRefresh != 2 {
		t.Errorf("refresh=true 时应重新拉，实际共 %d 次", afterRefresh)
	}
}

// ---------- subscription ----------

func TestSubscriptionSetDryRunDoesNotWrite(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	env := call(t, core, "subscription.set", map[string]any{
		"name":   "主库",
		"dryRun": true,
		"items": []any{map[string]any{
			"key":   "id:1234",
			"patch": map[string]any{"season": 2, "offset": -12, "enable": false},
		}},
	}, "")
	data := dataOf(t, env)
	if numericEquals(data["updated"], 0) {
		t.Fatalf("dryRun 应报出会被改动的条数，实际 %v", data)
	}
	results, _ := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("应回 1 条结果，实际 %v", data["results"])
	}
	changed, _ := results[0].(map[string]any)["changed"].([]any)
	if len(changed) != 3 {
		t.Errorf("season / offset / enable 三个字段都该报出来，实际 %v", changed)
	}
	if state.countSetAni() != 0 {
		t.Error("dryRun 一条都不许写")
	}
}

func TestSubscriptionSetWritesAndReportsChanges(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	env := call(t, core, "subscription.set", map[string]any{
		"name": "主库",
		"items": []any{
			map[string]any{"key": "id:1234", "patch": map[string]any{"season": 2}},
			map[string]any{"key": "id:1234", "patch": map[string]any{"season": 1}}, // 与现值相同 -> 跳过
			map[string]any{"key": "id:不存在", "patch": map[string]any{"offset": 0}},
		},
	}, "")
	data := dataOf(t, env)
	if !numericEquals(data["updated"], 1) {
		t.Errorf("只该写 1 条，实际 %v", data["updated"])
	}
	if numericEquals(data["skipped"], 0) {
		t.Errorf("「没有变化」与「找不到那条」都要记进 skipped，实际 %v", data)
	}
	if state.countSetAni() != 1 {
		t.Errorf("实际写盘应只有 1 次，实际 %d", state.countSetAni())
	}
	written := state.lastSetAni()
	if !numericEquals(written["season"], 2) {
		t.Errorf("写回去的对象里 season 应是 2，实际 %v", written["season"])
	}
	// setAni 是全量覆盖：原对象里的字段必须整份带回去。
	if written["title"] != "冰之城墙" || written["url"] != "https://old.example.com/rss.xml" {
		t.Errorf("写回去的必须是完整对象，实际 %v", written)
	}
}

func TestSubscriptionSetPatchWithoutFieldsChangesNothing(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	data := dataOf(t, call(t, core, "subscription.set", map[string]any{
		"name":  "主库",
		"items": []any{map[string]any{"key": "id:1234", "patch": map[string]any{}}},
	}, ""))
	if !numericEquals(data["updated"], 0) || state.countSetAni() != 0 {
		t.Errorf("空补丁什么都不该做，实际 %v", data)
	}
}

func TestSubscriptionSetProfileClearing(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	raw := map[string]any{"id": "1234", "title": "冰之城墙", "profile": "收藏"}
	data := dataOf(t, call(t, core, "subscription.set", map[string]any{
		"name":  "主库",
		"items": []any{map[string]any{"key": "id:1234", "raw": raw, "patch": map[string]any{"profile": ""}}},
	}, ""))
	if !numericEquals(data["updated"], 1) {
		t.Fatalf("清空 profile 应算一次改动，实际 %v", data)
	}
	if _, exists := state.lastSetAni()["profile"]; exists {
		t.Error(`profile: "" 的语义是清除，不是写一个空串进去`)
	}
}

func TestSubscriptionSetRejectsTooMany(t *testing.T) {
	core, _ := testCore(t)
	items := make([]any, 0, maxSubscriptionBatch+1)
	for i := 0; i <= maxSubscriptionBatch; i++ {
		items = append(items, map[string]any{"key": "id:1", "patch": map[string]any{"offset": 0}})
	}
	env := call(t, core, "subscription.set", map[string]any{"name": "主库", "items": items}, "")
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Fatalf("超过 %d 条应被拒，实际 %+v", maxSubscriptionBatch, env.Error)
	}
}

func TestSubscriptionDeleteRequiresConfirmAndDeletes(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	blocked := call(t, core, "subscription.delete", map[string]any{"name": "主库", "keys": []string{"id:1234"}}, "")
	if blocked.OK || blocked.Error.Code != CodeBadRequest {
		t.Fatalf("没有 confirm 必须被拦下，实际 %+v", blocked.Error)
	}
	if state.mu.Lock(); state.delAni != 0 {
		state.mu.Unlock()
		t.Fatal("被拦下的一次不该发出删除请求")
	}
	state.mu.Unlock()

	data := dataOf(t, call(t, core, "subscription.delete", map[string]any{
		"name": "主库", "keys": []string{"id:1234", "id:不存在"}, "confirm": true,
	}, ""))
	if !numericEquals(data["deleted"], 1) {
		t.Errorf("应删掉 1 条，实际 %v", data["deleted"])
	}
	errors, _ := data["errors"].([]any)
	if len(errors) != 1 {
		t.Errorf("找不到的那条要记进 errors，实际 %v", data["errors"])
	}
	if state.delAni != 1 {
		t.Errorf("删除请求应只发 1 次，实际 %d", state.delAni)
	}
}

func TestSubscriptionAddAndResolve(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	resolved := dataOf(t, call(t, core, "subscription.resolve", map[string]any{
		"name": "主库", "rssUrl": "https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=lh",
	}, ""))
	if resolved["ani"] == nil || resolved["raw"] == nil {
		t.Fatalf("subscription.resolve 要同时给视图与原文，实际 %v", resolved)
	}

	added := dataOf(t, call(t, core, "subscription.add", map[string]any{
		"name": "主库", "bgmId": "665390", "title": "冰之城墙",
		"rssUrl": "https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=lh",
		"season": 2, "offset": -12, "enable": false,
	}, ""))
	if added["added"] != true {
		t.Fatalf("新增应成功: %v", added)
	}
	ani, _ := added["ani"].(map[string]any)
	if ani["key"] == "" {
		t.Errorf("新增之后必须回一条带 key 的订阅视图，实际 %v", ani)
	}
	if len(state.addAni) != 1 {
		t.Fatalf("应发出 1 次 addAni，实际 %d", len(state.addAni))
	}
	if body := state.addAni[0]; !numericEquals(body["season"], 2) {
		t.Errorf("season 要写进新增对象，实际 %v", body["season"])
	}
}

func TestSubscriptionAddRejectsEmptyPayload(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "subscription.add", map[string]any{"name": "主库"}, "")
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Fatalf("什么都没给时应回 bad_request，实际 %+v", env.Error)
	}
}

// ---------- plan / sync ----------

func TestPlanBuildProducesActions(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	data := dataOf(t, call(t, core, "plan.build", map[string]any{}, ""))
	plan, ok := data["plan"].(map[string]any)
	if !ok {
		t.Fatalf("plan.build 应回 {plan:{...}}，实际 %v", data)
	}
	actions, _ := plan["actions"].([]any)
	if len(actions) == 0 {
		t.Fatalf("假实例给的候选与现状不同，应产出至少一条动作: %v", plan)
	}
	action := actions[0].(map[string]any)
	if action["kind"] != "update" || action["instance"] != "主库" {
		t.Errorf("动作形状不对: %v", action)
	}
	if schemes, _ := plan["schemes"].([]any); len(schemes) != 1 {
		t.Errorf("计划里必须交代每个实例挂的方案，实际 %v", plan["schemes"])
	}
}

func TestPlanBuildRejectsInvalidConfig(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "plan.build", map[string]any{}, "")
	if env.OK || env.Error.Code != CodeInvalidConfig {
		t.Fatalf("配置不合法时应回 invalid_config，实际 %+v", env.Error)
	}
}

func TestPlanBuildEmitsProgress(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	events, stop := core.Events().Subscribe()
	defer stop()

	call(t, core, "plan.build", map[string]any{}, "")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case event := <-events:
			if event.Type == EventProgress {
				if event.Data["label"] == nil {
					t.Errorf("progress 必须带人话标签，实际 %v", event.Data)
				}
				return
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Error("plan.build 在飞行中必须发 progress 事件（界面靠它显示进度）")
}

func TestSyncRunIsAsyncAndWrites(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	env := call(t, core, "sync.run", map[string]any{}, "")
	data := dataOf(t, env)
	jobID, _ := data["jobId"].(string)
	if jobID == "" {
		t.Fatalf("sync.run 必须立刻返回 jobId，实际 %v", data)
	}
	waitJob(t, core.jobs, jobID, JobDone)

	view, ok := core.jobs.Get(jobID)
	if !ok {
		t.Fatal("取不到刚跑完的任务")
	}
	result, _ := view.Result.(map[string]any)
	if !numericEquals(result["updated"], 1) {
		t.Errorf("应写回 1 条更新，实际 %v", result)
	}
	if result["plan"] == nil {
		t.Error("结果里应带 plan（界面要展开看改了哪几条）")
	}
	if state.countSetAni() != 1 {
		t.Errorf("实例侧应收到 1 次 setAni，实际 %d", state.countSetAni())
	}
	// 写实例之前要拿租约，跑完之后交还（否则下一个进程永远抢不到）。
	if _, held := appCurrentLease(); held {
		t.Error("一轮同步跑完不该继续占着租约")
	}
}

func TestAsyncOpsReturnJobIDImmediately(t *testing.T) {
	for _, op := range []string{"sync.run", "plan.apply", "backup.restore", "discover.autoAdd"} {
		registered, ok := Lookup(op)
		if !ok {
			t.Fatalf("%s 没有注册", op)
		}
		if !registered.Async {
			t.Errorf("%s 必须是异步的", op)
		}
	}
}

// ---------- backup ----------

func TestBackupCreateListRestore(t *testing.T) {
	server, state := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	created := dataOf(t, call(t, core, "backup.create", map[string]any{}, ""))
	instances, _ := created["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("应备份 1 个实例，实际 %v", created)
	}
	summary := instances[0].(map[string]any)
	if !numericEquals(summary["count"], 1) || summary["file"] == nil {
		t.Errorf("实例摘要应带 name/count/file，实际 %v", summary)
	}
	dir, _ := created["dir"].(string)
	if dir == "" {
		t.Fatal("backup.create 必须回目录")
	}

	listed := dataOf(t, call(t, core, "backup.list", map[string]any{"dir": dir}, ""))
	backups, _ := listed["backups"].([]any)
	if len(backups) != 1 {
		t.Fatalf("应列出 1 份存档，实际 %v", listed["backups"])
	}
	row := backups[0].(map[string]any)
	if row["createdAt"] == "" || row["size"] == nil || row["instances"] == nil {
		t.Errorf("存档条目形状不对: %v", row)
	}
	file, _ := row["file"].(string)

	env := call(t, core, "backup.restore", map[string]any{"file": filepath.Join(dir, file)}, "")
	jobID, _ := dataOf(t, env)["jobId"].(string)
	if jobID == "" {
		t.Fatalf("backup.restore 必须立刻返回 jobId，实际 %v", env)
	}
	waitJob(t, core.jobs, jobID, JobDone)
	view, _ := core.jobs.Get(jobID)
	result, _ := view.Result.(map[string]any)
	if !numericEquals(result["restored"], 1) {
		t.Errorf("应还原 1 条，实际 %v", result)
	}
	if state.countSetAni() != 1 {
		t.Errorf("还原走的就是 setAni，应 1 次，实际 %d", state.countSetAni())
	}
}

func TestBackupRestoreMissingFile(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "backup.restore", map[string]any{"file": filepath.Join(t.TempDir(), "没有这个文件.json")}, "")
	jobID, _ := dataOf(t, env)["jobId"].(string)
	waitJob(t, core.jobs, jobID, JobFailed)
	view, _ := core.jobs.Get(jobID)
	if view.Error == nil || view.Error.Code != CodeNotFound {
		t.Errorf("存档不存在应报 not_found，实际 %+v", view.Error)
	}
	if env := call(t, core, "backup.restore", map[string]any{}, ""); env.OK || env.Error.Code != CodeBadRequest {
		t.Errorf("没给 file 应回 bad_request，实际 %+v", env.Error)
	}
}

// ---------- probe / bgm ----------

func TestProbeRunListsCandidates(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	data := dataOf(t, call(t, core, "probe.run", map[string]any{
		"site": "anibt", "bgmId": "665390", "title": "冰之城墙",
	}, ""))
	if data["site"] != "anibt" {
		t.Errorf("应回报探的哪个站点，实际 %v", data["site"])
	}
	candidates, _ := data["candidates"].([]any)
	if len(candidates) != 1 {
		t.Fatalf("应回 1 个候选，实际 %v", data["candidates"])
	}
	candidate := candidates[0].(map[string]any)
	if candidate["group"] != "猎户发布组" || candidate["rss"] == "" {
		t.Errorf("候选形状不对: %v", candidate)
	}
	if !numericEquals(candidate["rank"], 1) || candidate["matched"] != true {
		t.Errorf("候选应带 rank 与是否命中规则: %v", candidate)
	}
	if data["errors"] == nil {
		t.Error("errors 必须是 []，不能是 null")
	}
}

func TestBgmSearch(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":1,"list":[{
		  "id":665390,"name":"氷の城壁","name_cn":"冰之城墙","air_date":"2026-01-04",
		  "eps_count":12,"rank":1200,"summary":"  简介  ",
		  "images":{"common":"http://lain.bgm.tv/pic/cover/c/1.jpg"},
		  "rating":{"total":100,"score":7.5}}]}`))
	}))
	defer upstream.Close()
	old := bgm.API
	bgm.API = upstream.URL
	defer func() { bgm.API = old }()

	core, _ := testCore(t)
	data := dataOf(t, call(t, core, "bgm.search", map[string]any{"q": "冰之城墙", "limit": 5}, ""))
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("应回 1 条，实际 %v", data["items"])
	}
	item := items[0].(map[string]any)
	if item["id"] != "665390" || item["nameCN"] != "冰之城墙" || item["date"] != "2026-01-04" {
		t.Errorf("条目形状不对: %v", item)
	}
	if !strings.HasPrefix(item["image"].(string), "https://") {
		t.Errorf("封面应规范化成 https，实际 %v", item["image"])
	}
	if env := call(t, core, "bgm.search", map[string]any{"q": "  "}, ""); env.OK || env.Error.Code != CodeBadRequest {
		t.Errorf("空关键字应回 bad_request，实际 %+v", env.Error)
	}
}

// ---------- discover ----------

func TestDiscoverListUsesAppSeason(t *testing.T) {
	// 上游指向一个会失败的本地服务：这条测试要证明的是「discover.list 复用 app.Season」，
	// 而不是「Bangumi 今天活着吗」—— 所以绝不许打真网。
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	old := discoverAPI()
	setDiscoverAPI(upstream.URL)
	defer setDiscoverAPI(old)

	core, path := testCore(t)
	writeConfig(t, path, func(cfg *config.Config) {
		cfg.Discover.Sources = []string{config.DiscoverBgmCalendar}
		cfg.Instances = []config.Instance{{Name: "主库", BaseURL: "http://127.0.0.1:1/", APIKey: "k"}}
	})
	core.Init(path, "")

	env := call(t, core, "discover.list", map[string]any{}, "")
	if env.OK {
		data := dataOf(t, env)
		for _, key := range []string{"items", "total", "added", "filtered", "source", "warnings"} {
			if _, ok := data[key]; !ok {
				t.Errorf("discover.list 缺字段 %s", key)
			}
		}
		return
	}
	// 上游全挂时必须是错误，而不是一张「一部番都没有」的空列表。
	if env.Error.Message == "" {
		t.Error("失败时必须给原因")
	}
	if !strings.Contains(env.Error.Message, "发现来源") && env.Error.Code != CodeNetwork {
		t.Errorf("上游全挂时报错应是 network 或说清来源失败，实际 %s（%s）", env.Error.Code, env.Error.Message)
	}
}

// ---------- 测试内的小工具 ----------

// appCurrentLease 读当前租约（判断「一轮跑完有没有把租约交还」）。
func appCurrentLease() (lease.Info, bool) {
	return app.CurrentLease(app.LeasePath())
}

// discoverAPI / setDiscoverAPI 把 Bangumi 地址指到 httptest。
func discoverAPI() string { return discover.BangumiAPI }

func setDiscoverAPI(url string) { discover.BangumiAPI = url }
