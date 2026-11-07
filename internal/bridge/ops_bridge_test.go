package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/journal"
	"github.com/zzzwannasleep/anisync/internal/lease"
)

// ---------- 测试脚手架 ----------

// testCore 造一个绑在临时工作区上的桥接容器。
// testCore 造一个干净工作区。
//
// **先落一份默认配置，再 Init** —— 这一行的理由值得写清楚：
// `config.Load` 对「文件真的不存在」会花掉 fsutil.AccessRetryBudget（250ms）。
// 那 250ms 是**产品行为**，不是浪费：另一个进程正在原子替换 config.yaml 时，
// 「暂时读不到」不该被当成「不存在」而静默回落到默认配置（t15 的 B 项，
// 有专门的测试 Node）
//
// 但**测试夹具不该为它付 60 次**：bridge 包有 60 多个用例、每个都 Init 到一份
// 不存在的 config.yaml，整包因此从 3.5s 涨到 22.5s。
// 先落一份默认配置，走的就是「文件存在」那条快路径；而**内存里的配置与原来
// 逐字段相同**（Default() 写出去、再读回来，还是 Default()），
// 所以一条断言都不用动 —— 改的是夹具的初始化，不是被测行为。
//
// 唯一需要「真的没有配置」那条路径的用例（TestConfigSaveWritesZeroInstanceConfig）
// 自己把文件删掉再 Init，那 250ms 只由它一处付。
func testCore(t *testing.T) (*Core, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// 落一份**最小**配置（只写 listen），不是把整份 Default() 序列化出去。
	//
	// 为什么最小：整份 Default() 写出去会带上 `instances: []` 这类空集合，
	// 读回来是**非 nil 的空切片**，而「文件不存在」那条路径拿到的是 **nil** ——
	// 两者 JSON 不一样（`[]` vs `null`），于是 60 多个用例就跑在不同的输入上了。
	// 只写 listen 则其余字段谁也不碰，两条路径逐字段相同（见下面那条守卫测试）。
	seed := config.Default()
	if err := os.WriteFile(path, []byte("listen: "+seed.Listen+"\n"), 0o600); err != nil {
		t.Fatalf("落最小配置失败: %v", err)
	}

	core := New()
	t.Cleanup(core.Close)
	if _, err := core.Init(path, ""); err != nil {
		t.Fatalf("初始化工作区失败: %v", err)
	}
	return core, path
}

// TestFixtureSeedIsEquivalentToAMissingConfig 钉住 testCore 提速所依赖的那条前提。
//
// testCore 靠「先落一份默认配置」把 60 多次 250ms 的「文件真不存在」慢路径换成了快路径。
// 这个改法的**全部正当性**都在一句话上：**两种初始化得到的内存配置逐字段相同**。
// 如果哪天不成立了（比如 Default() 的字段与写出去再读回来之间出现了差异），
// 那 60 多个用例就会在**不同的输入**上跑，而它们全都还是绿的 ——
// 这种「绿色的漂移」才是真危险的东西，所以这里把它钉死。
func TestFixtureSeedIsEquivalentToAMissingConfig(t *testing.T) {
	dir := t.TempDir()

	// 一：文件真的不存在（父目录也不存在 —— 这就是「新工作区」）。
	missing := filepath.Join(dir, "新建", "config.yaml")
	fromMissing, err := config.Load(missing)
	if err != nil {
		t.Fatalf("不存在的配置应回默认配置，不该报错: %v", err)
	}

	// 二：testCore 的路子 —— 先落一份最小配置，再读回来。
	seeded := filepath.Join(dir, "config.yaml")
	seed := config.Default()
	if err := os.WriteFile(seeded, []byte("listen: "+seed.Listen+"\n"), 0o600); err != nil {
		t.Fatalf("落最小配置失败: %v", err)
	}
	fromFile, err := config.Load(seeded)
	if err != nil {
		t.Fatalf("读回落下的配置失败: %v", err)
	}

	// 逐字段比：JSON 往返覆盖所有导出字段（path 是未导出的，不参与）。
	wantJSON, err := json.Marshal(fromMissing)
	if err != nil {
		t.Fatal(err)
	}
	gotJSON, err := json.Marshal(fromFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Errorf("「文件不存在」与「落一份默认配置再读回」得到的配置不同 ——\n"+
			"testCore 因此会让 60 多个用例跑在不同的输入上。\n want(不存在)=%s\n got(落过盘)=%s",
			wantJSON, gotJSON)
	}

	// 校验结论也要一样（零实例 → 不合法）：config.get 的 warnings 靠它。
	if (fromMissing.Validate() == nil) != (fromFile.Validate() == nil) {
		t.Error("两种初始化的校验结论不同：几乎每条 config.get 断言都会跟着变")
	}
}

// mustConfig 取当前配置（读不出来就是测试自己的问题，直接 Fatal）。
func mustConfig(t *testing.T, core *Core) *config.Config {
	t.Helper()
	cfg, err := core.Config()
	if err != nil {
		t.Fatalf("读配置失败: %v", err)
	}
	return cfg
}

// call 走一次完整的 op 派发，返回解析后的信封。
func call(t *testing.T, core *Core, op string, payload any, callID string) Envelope {
	t.Helper()
	var raw []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("payload 序列化失败: %v", err)
		}
		raw = encoded
	}
	return decodeEnvelope(t, core.Handle(context.Background(), op, raw, callID))
}

func decodeEnvelope(t *testing.T, raw []byte) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("响应不是合法 JSON: %v\n%s", err, raw)
	}
	return env
}

func dataOf(t *testing.T, env Envelope) map[string]any {
	t.Helper()
	if !env.OK {
		t.Fatalf("期望成功，实际 error=%+v", env.Error)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("data 不是对象: %v", err)
	}
	return out
}

// writeRaw / readRaw 直接读写配置文件（测试里要造出特定的注释布局）。
func writeRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readRaw(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// writeConfig 把一份配置落到工作区里（走 Config.Save，与用户手上的文件同形）。
func writeConfig(t *testing.T, path string, mutate func(*config.Config)) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if mutate != nil {
		mutate(cfg)
	}
	cfg.SetPath(path)
	cfg.Normalize()
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return cfg
}

// assertSampleMatchesRepo 钉住内嵌样例与仓库根目录那份逐字节一致。
//
// 少这一步，「界面里看到的样例」与「发布物里的样例」就会各走各的 ——
// 而它们描述的是同一件事：配置该怎么写。
func assertSampleMatchesRepo(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "config.example.yaml"))
	if err != nil {
		t.Skipf("仓库根目录没有 config.example.yaml（打包产物里才会缺），跳过: %v", err)
	}
	if string(raw) != SampleYAML() {
		t.Error("internal/bridge/sample/config.example.yaml 与仓库根的 config.example.yaml 不一致")
	}
}

// appendJournalForTest 往运行日志里补一条带 NextAt 的记录。
func appendJournalForTest(t *testing.T) {
	t.Helper()
	entry := journal.Entry{
		At:       time.Now(),
		Mode:     "test",
		OK:       true,
		Writable: 3,
		Updated:  2,
		Added:    1,
		NextAt:   time.Now().Add(time.Hour),
		Note:     "测试记录",
	}
	if err := journal.Append(app.JournalPath(), entry, journal.DefaultMaxEntries); err != nil {
		t.Fatal(err)
	}
}

func sleepShort() { time.Sleep(5 * time.Millisecond) }

// ---------- 派发与错误 ----------

func TestUnknownOpIsNotFound(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "no.such_op", map[string]any{}, "")
	if env.OK {
		t.Fatal("未知 op 不该成功")
	}
	if env.Error.Code != CodeNotFound {
		t.Errorf("未知 op 的错误码应是 not_found，实际 %s（%s）", env.Error.Code, env.Error.Message)
	}
	if !strings.Contains(env.Error.Message, "no.such_op") {
		t.Errorf("错误信息里应点名那个 op，实际 %q", env.Error.Message)
	}
}

// 未知 op 与坏 payload 必须是**两个不同的码**，而且未知 op 的 detail 就是那个 op 名。
//
// 为什么较这个真：`not_found` 有两种含义 ——「这个 op 不存在」与
// 「这个名字的资源不存在」。客户端要能把它们分开，才能把「我的内核版本比节点新」
// 认出来（契约 §10 明确允许加 op，所以版本偏斜是**正常状态**，
// 必须可识别），而不是当成参数写错去改 payload。
func TestUnknownOpIsNotFoundNotBadRequest(t *testing.T) {
	core, _ := testCore(t)

	unknown := call(t, core, "core.no_such_op", map[string]any{}, "")
	if unknown.OK {
		t.Fatal("未知 op 不该成功")
	}
	if unknown.Error.Code != CodeNotFound {
		t.Errorf("未知 op 必须是 not_found（不是 %s）", unknown.Error.Code)
	}
	if unknown.Error.Detail != "core.no_such_op" {
		t.Errorf("未知 op 的 detail 必须恰好是那个 op 名，实际 %q", unknown.Error.Detail)
	}

	// payload 形状不对：同一个 op，名字是存在的，错的是参数。
	badPayload := call(t, core, "instance.test", map[string]any{"name": 42}, "")
	if badPayload.Error.Code != CodeBadRequest {
		t.Errorf("畸变 payload 必须是 bad_request（不是 %s）", badPayload.Error.Code)
	}
	if badPayload.Error.Code == unknown.Error.Code {
		t.Error("两种错误必须是不同的码，否则版本偏斜会被看成参数写错")
	}
}

func TestBadPayloadIsBadRequest(t *testing.T) {
	core, _ := testCore(t)
	// instance.test 需要字符串 name；给数组解不出来。
	raw := core.Handle(context.Background(), "instance.test", []byte(`{"name":[1,2]}`), "")
	env := decodeEnvelope(t, raw)
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Fatalf("类型不对的 payload 应是 bad_request，实际 %+v", env.Error)
	}
}

func TestDuplicateCallIDIsBadRequest(t *testing.T) {
	core, _ := testCore(t)
	if !core.calls.Add("dup", func() {}) {
		t.Fatal("第一次登记应该成功")
	}
	defer core.calls.Remove("dup")

	env := call(t, core, "core.version", map[string]any{}, "dup")
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Fatalf("同一个 callId 第二次在飞应是 bad_request，实际 %+v", env.Error)
	}
}

func TestCancelCallRegistry(t *testing.T) {
	core, _ := testCore(t)
	if core.Cancel("nope") {
		t.Error("不存在的 callId 应返回 false")
	}
	called := false
	if !core.calls.Add("live", func() { called = true }) {
		t.Fatal("登记失败")
	}
	if !core.Cancel("live") {
		t.Error("存在的 callId 应返回 true")
	}
	if !called {
		t.Error("Cancel 应该真的调到注册的取消函数")
	}
	core.calls.Remove("live")
	if core.calls.Len() != 0 {
		t.Errorf("摘掉之后不该还留着，实际 %d", core.calls.Len())
	}
}

// 任何 op 都不许让进程死：handler panic 必须变成一个 internal 错误 + 一条日志。
func TestOpPanicIsRecovered(t *testing.T) {
	core := New()
	defer core.Close()
	op := Op{Name: "panic.op", Group: "test", Handler: func(*Call) (any, error) {
		panic("炸了")
	}}
	c := &Call{Core: core, Op: &op, ctx: context.Background()}
	data, err := invoke(op, c)
	if err == nil {
		t.Fatal("panic 必须被兜住并转成错误")
	}
	if data != nil {
		t.Errorf("兜住的 panic 不该带数据，实际 %v", data)
	}
	if classified := Classify(err); classified.Code != CodeInternal {
		t.Errorf("兜住的 panic 应是 internal，实际 %s", classified.Code)
	}
	if len(core.RecentLogs(10)) == 0 {
		t.Error("兜住 panic 时必须留一条日志（否则没人知道它炸过）")
	}
}

// op 没实现时同样是 internal，而不是空指针崩溃。
func TestOpWithoutHandlerIsInternal(t *testing.T) {
	core := New()
	defer core.Close()
	op := Op{Name: "empty.op", Group: "test"}
	_, err := invoke(op, &Call{Core: core, Op: &op, ctx: context.Background()})
	if err == nil || Classify(err).Code != CodeInternal {
		t.Fatalf("没有 handler 的 op 应报 internal，实际 %v", err)
	}
}

func TestClassifyMapping(t *testing.T) {
	dir := t.TempDir()
	badCfg, err := config.Load(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	validateErr := badCfg.Validate() // 没有实例 -> 校验不过
	if validateErr == nil {
		t.Fatal("空配置不该通过校验（测试前提）")
	}

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"配置校验", InvalidConfig(validateErr), CodeInvalidConfig},
		{"取消", context.Canceled, CodeCanceled},
		{"租约被占", lease.ErrHeld, CodeLeaseHeld},
		{"租约丢了", lease.ErrLost, CodeLeaseHeld},
		{"实例不存在", app.ErrInstanceNotFound, CodeNotFound},
		{"文件不存在", os.ErrNotExist, CodeNotFound},
		{"忙", Busy("忙"), CodeBusy},
		{"不存在", NotFound("没有 %s", "x"), CodeNotFound},
		{"参数", BadRequest("字段不对"), CodeBadRequest},
	}
	for _, tc := range cases {
		if got := Classify(tc.err).Code; got != tc.want {
			t.Errorf("%s：Classify 应给 %s，实际 %s", tc.name, tc.want, got)
		}
	}
	if Classify(nil) != nil {
		t.Error("nil 应映射成 nil")
	}
	// 已经带码的错误不该被重新分类。
	if got := Classify(BadRequest("x")).Code; got != CodeBadRequest {
		t.Errorf("已带码的错误应原样返回，实际 %s", got)
	}
}

// 网络错误要能被认出来 —— 否则界面上「实例连不上」会显示成「程序坏了」。
func TestClassifyNetworkErrors(t *testing.T) {
	cases := []error{
		&url.Error{Op: "Post", URL: "http://x/api/about", Err: errors.New("boom")},
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
		io.EOF,
		errors.New("dial tcp 127.0.0.1:1: connect: connection refused"),
	}
	for _, err := range cases {
		if got := Classify(err).Code; got != CodeNetwork {
			t.Errorf("%v 应判成 network，实际 %s", err, got)
		}
	}
	// 真正看不懂的错误才是 internal。
	if got := Classify(errors.New("某个没人见过的错误")).Code; got != CodeInternal {
		t.Errorf("认不出的错误应判成 internal，实际 %s", got)
	}
}

// ---------- 事件与日志 ----------

// 投递永不阻塞：订阅者不读时也必须立刻返回（满了就丢）。
func TestEventBusNeverBlocks(t *testing.T) {
	bus := NewEventBus(1)
	_, stop := bus.Subscribe()
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			bus.Publish(NewEvent(EventLog, nil, nil, map[string]any{"text": "x"}))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("事件投递把业务 goroutine 卡住了；契约要求满了就丢")
	}
}

func TestEventShapeHasNullIDs(t *testing.T) {
	event := NewEvent(EventProgress, nil, nil, nil)
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	// 契约 §8：callId / jobId 每一帧都在，没有值就是 null。
	if !strings.Contains(text, `"callId":null`) || !strings.Contains(text, `"jobId":null`) {
		t.Errorf("事件里 callId / jobId 必须是 null 而不是消失：%s", text)
	}
	if !strings.Contains(text, `"data":{}`) {
		t.Errorf("没有数据时 data 应是空对象：%s", text)
	}
	if !strings.Contains(text, `"type":"progress"`) || !strings.Contains(text, `"ts":"`) {
		t.Errorf("事件必须有 type 与 ts：%s", text)
	}
}

func TestEventBusCloseEndsSubscribers(t *testing.T) {
	bus := NewEventBus(4)
	events, _ := bus.Subscribe()
	bus.Close()
	select {
	case _, ok := <-events:
		if ok {
			t.Error("关闭之后订阅通道应当关闭，不该再收到事件")
		}
	case <-time.After(time.Second):
		t.Error("关闭总线没有把订阅者放出去")
	}
}

func TestLogRingKeepsLastN(t *testing.T) {
	ring := NewLogRing(3)
	for i := 0; i < 5; i++ {
		ring.Add("info", string(rune('a'+i)))
	}
	lines := ring.Recent(0)
	if len(lines) != 3 {
		t.Fatalf("环形缓冲应保留 3 行，实际 %d", len(lines))
	}
	if lines[0].Text != "c" || lines[2].Text != "e" {
		t.Errorf("保留的应是最新的 3 行，实际 %+v", lines)
	}
	if got := ring.Recent(1); len(got) != 1 || got[0].Text != "e" {
		t.Errorf("Recent(1) 应给最后一行，实际 %+v", got)
	}
	if got := ring.Recent(-1); len(got) != 3 {
		t.Errorf("Recent(负数) 应给全部，实际 %d", len(got))
	}
}

func TestLogRecentOp(t *testing.T) {
	core, _ := testCore(t)
	core.Log("info", "第一行")
	core.Log("error", "第二行")
	data := dataOf(t, call(t, core, "log.recent", map[string]any{"limit": 1}, ""))
	lines, ok := data["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("log.recent 应回 1 行，实际 %v", data["lines"])
	}
	line := lines[0].(map[string]any)
	if line["text"] != "第二行" || line["level"] != "error" || line["ts"] == "" {
		t.Errorf("日志行字段不对: %v", line)
	}
}

// ---------- log.write（客户端写诊断日志） ----------

// 客户端写的行要能被 log.recent 读到，并且**带前缀**与引擎自己的行区分开。
func TestLogWriteGoesToRingWithPrefix(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "log.write", map[string]any{"level": "warn", "text": "事件解码失败 1 次"}, "")
	data := dataOf(t, env)
	if data["ok"] != true {
		t.Fatalf("写入应成功: %v", data)
	}
	if _, dropped := data["dropped"]; dropped {
		t.Errorf("没超限时不该带 dropped: %v", data)
	}

	lines := dataOf(t, call(t, core, "log.recent", map[string]any{}, ""))["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("缓冲里应有 1 行，实际 %v", lines)
	}
	line := lines[0].(map[string]any)
	if line["level"] != "warn" {
		t.Errorf("级别没传下去: %v", line)
	}
	if text, _ := line["text"].(string); !strings.HasPrefix(text, "[client] ") {
		t.Errorf("客户端行必须带 %q 前缀（否则排查时分不清是谁写的），实际 %q", "[client] ", text)
	}
	if !strings.Contains(line["text"].(string), "事件解码失败 1 次") {
		t.Errorf("内容没写进去: %v", line)
	}
}

func TestLogWriteRejectsBadLevel(t *testing.T) {
	core, _ := testCore(t)
	for _, level := range []string{"", "trace", "WARNING", "error "} {
		env := call(t, core, "log.write", map[string]any{"level": level, "text": "x"}, "")
		if level == "error " {
			// 首尾空白由服务端规整，不该因此被拒。
			if !env.OK {
				t.Errorf("%q 只是多了空白，应当被接受，实际 %+v", level, env.Error)
			}
			continue
		}
		if env.OK || env.Error.Code != CodeBadRequest {
			t.Errorf("level=%q 应是 bad_request，实际 %+v", level, env.Error)
		}
	}
}

// TestLogWriteLevelIsNormalisedNotCaseSensitive 把 ① 的实际行为钉死，供契约对齐。
//
// 实现是「**先规整、再判定**」：`strings.ToLower(strings.TrimSpace(level))`
// 之后查白名单。所以：
//
//	INFO / Warn / ERROr  -> 接受，并且**存进缓冲的是小写形式**
//	WARNING / trace / "" -> 拒绝（不在集合里，与大小写无关）
//
// 为什么选「规整后接受」而不是「严格小写」：level 是给人看的标签、不是标识符，
// 客户端把它写成大写是纯粹的拼写差异，为这个差异丢一条诊断信息不划算 ——
// 而诊断信息的特点正是「丢了就没人知道它存在过」。代价是 `WARNING` 这种
// 「拼对了大小写但名字不对」仍然被拒，那一条才是真的用错了。
//
// 契约 §7.12 之前只写了四个取值、没说大小写，这条测试就是回答那个问题的可执行证据。
func TestLogWriteLevelIsNormalisedNotCaseSensitive(t *testing.T) {
	core, _ := testCore(t)

	sent := []string{"info", "INFO", "Warn", "ERROR", "  debug  "}
	want := []string{"info", "info", "warn", "error", "debug"}
	for _, level := range sent {
		env := call(t, core, "log.write", map[string]any{"level": level, "text": "大小写测试"}, "")
		if !env.OK {
			t.Fatalf("level=%q 应被接受（实现先 ToLower + TrimSpace 再查白名单），实际 %+v",
				level, env.Error)
		}
	}

	lines := dataOf(t, call(t, core, "log.recent", map[string]any{}, ""))["lines"].([]any)
	if len(lines) != len(sent) {
		t.Fatalf("缓冲里应有 %d 行，实际 %d", len(sent), len(lines))
	}
	for i, raw := range lines {
		got, _ := raw.(map[string]any)["level"].(string)
		if got != want[i] {
			t.Errorf("第 %d 行：发送 %q 之后缓冲里的 level 应是 %q（规整成小写），实际 %q",
				i+1, sent[i], want[i], got)
		}
	}
}

// 超长的行**截断**而不是拒收：被截断的诊断仍然有用，被丢掉的永远不会再出现。
func TestLogWriteTruncatesLongText(t *testing.T) {
	core, _ := testCore(t)
	long := strings.Repeat("あ", maxClientLogText+500)
	if env := call(t, core, "log.write", map[string]any{"level": "info", "text": long}, ""); !env.OK {
		t.Fatalf("超长应被截断而不是拒收，实际 %+v", env.Error)
	}
	lines := dataOf(t, call(t, core, "log.recent", map[string]any{}, ""))["lines"].([]any)
	text, _ := lines[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "已截断") {
		t.Error("截断要留痕（读到一半的人得知道后面被切了）")
	}
	if runes := []rune(text); len(runes) > maxClientLogText+64 {
		t.Errorf("截断之后仍然太长：%d 个字符", len(runes))
	}
}

// 限速：超出的行被丢掉，并且**要能被调用方看见**（dropped）。
//
// 这不是洁癖：日志是排查资产，一个客户端不该能把引擎的日志缓冲刷掉。
func TestLogWriteIsRateLimited(t *testing.T) {
	core, _ := testCore(t)

	dropped := 0
	for i := 0; i < clientLogBurst+10; i++ {
		data := dataOf(t, call(t, core, "log.write", map[string]any{"level": "info", "text": "刷屏"}, ""))
		if data["ok"] != true {
			t.Fatalf("被限速不是错误（ok 仍为 true），实际 %v", data)
		}
		if n, ok := data["dropped"]; ok {
			if !numericEquals(n, 1) {
				t.Errorf("每丢掉一行应报 dropped:1，实际 %v", n)
			}
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatalf("写了 %d 行（超过每秒上限 %d）却一行都没被丢：限速没生效", clientLogBurst+10, clientLogBurst)
	}

	// 缓冲里最多只该有 burst 行客户端内容 + 一条「被限速」的告警。
	lines := dataOf(t, call(t, core, "log.recent", map[string]any{}, ""))["lines"].([]any)
	clientLines := 0
	for _, raw := range lines {
		if text, _ := raw.(map[string]any)["text"].(string); strings.HasPrefix(text, "[client] ") {
			clientLines++
		}
	}
	if clientLines > clientLogBurst {
		t.Errorf("限速之后缓冲里仍塞进了 %d 行客户端内容（上限 %d）", clientLines, clientLogBurst)
	}
}

// 客户端日志**只进内存环形缓冲**：不落盘、不进 runs.jsonl。
//
// 后者是「每轮同步的结果」，混进客户端行会把「最近 200 轮」淹掉 ——
// 而 status 正是靠它回答「它昨天到底跑没跑」。
func TestLogWriteDoesNotTouchJournal(t *testing.T) {
	core, path := testCore(t)
	writeConfig(t, path, nil)
	core.Init(path, "")

	for i := 0; i < 5; i++ {
		call(t, core, "log.write", map[string]any{"level": "info", "text": "客户端噪音"}, "")
	}
	data := dataOf(t, call(t, core, "status.get", map[string]any{"limit": 10}, ""))
	if runs, _ := data["runs"].([]any); len(runs) != 0 {
		t.Errorf("客户端日志不该进运行记录，实际 %v", data["runs"])
	}
	if journalData, _ := data["dataDir"].(string); journalData == "" {
		t.Fatal("status.get 应报出 dataDir")
	}
}

// ---------- 任务 ----------

func TestJobManagerKeepsLast20(t *testing.T) {
	mgr := NewJobManager(NewEventBus(8))
	for i := 0; i < 25; i++ {
		n := i
		mgr.Start("test", func(context.Context, *Job) (any, error) { return n, nil })
	}
	if list := mgr.List(); len(list) != maxJobsInMemory {
		t.Fatalf("内存里应只留 %d 个任务，实际 %d", maxJobsInMemory, len(list))
	}
}

func TestJobDoneAndCancel(t *testing.T) {
	mgr := NewJobManager(NewEventBus(64))
	events, stop := mgr.bus.Subscribe()
	defer stop()

	view := mgr.Start("test.ok", func(ctx context.Context, job *Job) (any, error) {
		job.SetMessage("做完了")
		return map[string]any{"n": 1}, nil
	})
	waitJob(t, mgr, view.ID, JobDone)

	got, ok := mgr.Get(view.ID)
	if !ok || got.State != JobDone || got.FinishedAt == nil {
		t.Fatalf("任务应已完成，实际 %+v", got)
	}
	if got.StartedAt == "" {
		t.Error("JobView 必须带 startedAt")
	}
	result, _ := got.Result.(map[string]any)
	if !numericEquals(result["n"], 1) {
		t.Errorf("结果没带回来: %v", got.Result)
	}

	// 事件流里必须能看到 job.start 与 job.done，且都带 jobId。
	// 注意不能只等「看到两个类型」就算了：progress 也在其中。
	seen := map[string]bool{}
	deadline := time.Now().Add(3 * time.Second)
	for !seen[EventJobDone] && time.Now().Before(deadline) {
		select {
		case event := <-events:
			if event.JobID == nil || *event.JobID != view.ID {
				continue
			}
			seen[event.Type] = true
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !seen[EventJobStart] || !seen[EventJobDone] {
		t.Errorf("应看到 job.start 与 job.done，实际 %v", seen)
	}

	// 取消一个已经结束的任务应返回 false。
	if mgr.Cancel(view.ID) {
		t.Error("已结束的任务不该还能取消")
	}
}

func TestJobFailureCarriesErrorCode(t *testing.T) {
	mgr := NewJobManager(NewEventBus(8))
	view := mgr.Start("test.fail", func(context.Context, *Job) (any, error) {
		return nil, InvalidConfig(errors.New("配置不合法"))
	})
	waitJob(t, mgr, view.ID, JobFailed)
	got, _ := mgr.Get(view.ID)
	if got.Error == nil || got.Error.Code != CodeInvalidConfig {
		t.Fatalf("失败的任务要带错误码，实际 %+v", got)
	}
	if got.Result != nil {
		t.Errorf("失败的任务不该有结果: %v", got.Result)
	}
}

func TestJobCancelStopsRun(t *testing.T) {
	mgr := NewJobManager(NewEventBus(8))
	started := make(chan struct{})
	view := mgr.Start("test.block", func(ctx context.Context, job *Job) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	if !mgr.Cancel(view.ID) {
		t.Fatal("取消在飞的任务应返回 true")
	}
	waitJob(t, mgr, view.ID, JobCanceled)
}

func waitJob(t *testing.T, mgr *JobManager, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if view, ok := mgr.Get(id); ok && view.State == want {
			return
		}
		sleepShort()
	}
	view, _ := mgr.Get(id)
	t.Fatalf("任务没有变成 %s，实际 %+v", want, view)
}

func TestJobGetAndCancelOps(t *testing.T) {
	core, _ := testCore(t)
	if data := dataOf(t, call(t, core, "job.list", map[string]any{}, "")); data["jobs"] == nil {
		t.Error("job.list 即使没有任务也要给 []，不能是 null")
	}
	env := call(t, core, "job.get", map[string]any{"id": "不存在"}, "")
	if env.OK || env.Error.Code != CodeNotFound {
		t.Fatalf("取不存在的任务应是 not_found，实际 %+v", env.Error)
	}
	if env := call(t, core, "job.cancel", map[string]any{"id": "不存在"}, ""); !env.OK {
		t.Error("取消不存在的任务不是错误，返回 ok:false 即可")
	}
}

// ---------- 视图 ----------

func TestMaskKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abcdefghijklmnop", "abcdef…op"},
		{"1234567890", "123456…90"},
		{"123456789", "*********"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := MaskKey(tc.in); got != tc.want {
			t.Errorf("MaskKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSubscriptionKeyRules(t *testing.T) {
	cases := []struct {
		name string
		ani  map[string]any
		want string
	}{
		{"有 id 用 id", map[string]any{"id": "1234", "bgmUrl": "https://bgm.tv/subject/1"}, "id:1234"},
		{"id 为 0 时退到 bgm", map[string]any{"id": "0", "bgmUrl": "https://bgm.tv/subject/975"}, "bgm:975"},
		{"bgmId 优先于标题", map[string]any{"bgmId": "975", "title": "航海王"}, "bgm:975"},
		{"都没有时用折过的标题", map[string]any{"title": "冰 之 城 墙"}, "title:冰之城墙"},
		{"RSS 里的 bgmId 也算", map[string]any{"url": "https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=x"}, "bgm:665390"},
		{"什么都没有", map[string]any{}, ""},
	}
	for _, tc := range cases {
		if got := SubscriptionKey(tc.ani); got != tc.want {
			t.Errorf("%s：SubscriptionKey = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// 实例里那几个字段可能是任意值：读不出数字时必须置空 + 说明原因，不许猜。
func TestSubscriptionViewReadsLooseFields(t *testing.T) {
	ani := map[string]any{
		"id":      "1",
		"title":   "某番",
		"season":  "第二季",
		"offset":  map[string]any{"nested": true},
		"enable":  true,
		"profile": "收藏",
		"standbyRssList": []any{
			"不是对象",
			map[string]any{"label": "ANi", "url": "https://x/rss", "offset": float64(-12)},
		},
	}
	view := subscriptionViewOf(ani)
	if view.Season != nil || view.Offset != nil {
		t.Errorf("非数字的 season / offset 必须置空，实际 %v / %v", view.Season, view.Offset)
	}
	if len(view.Issues) < 3 {
		t.Errorf("必须把原因写进 issues，实际 %v", view.Issues)
	}
	if len(view.Standby) != 1 || view.Standby[0].Offset == nil || *view.Standby[0].Offset != -12 {
		t.Errorf("副 RSS 只该收下那一条合法的，实际 %+v", view.Standby)
	}
	if view.Profile != "收藏" || !view.Enable {
		t.Errorf("profile / enable 没读出来: %+v", view)
	}
	if view.Raw == nil {
		t.Error("raw 必须原样保留（setAni 是全量覆盖，改完要整份交回去）")
	}
	if view.Main != nil {
		t.Errorf("没有 url 时 main 应是 null，不要凭空造一个，实际 %+v", view.Main)
	}
}

func TestLeaseViewIdle(t *testing.T) {
	raw, err := json.Marshal(leaseViewOf(lease.Info{}, false, leaseTTLSeconds))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"held":false}` {
		t.Errorf(`空闲租约的形状应是 {"held":false}，实际 %s`, raw)
	}
}

func TestLeaseViewHeld(t *testing.T) {
	info := lease.Info{PID: 1234, Host: "host-a", Note: "tray", Heartbeat: time.Now().Add(-12 * time.Second)}
	view := leaseViewOf(info, true, leaseTTLSeconds)
	if !view.Held || view.PID != 1234 || view.Note != "tray" || view.Owner != "host-a" {
		t.Errorf("租约视图不对: %+v", view)
	}
	if view.AgeSeconds < 11 || view.AgeSeconds > 30 {
		t.Errorf("ageSeconds 应在 12 秒左右，实际 %v", view.AgeSeconds)
	}
	if view.Expired {
		t.Error("12 秒远没到 TTL，不该判过期")
	}
	old := lease.Info{PID: 1, Heartbeat: time.Now().Add(-time.Hour)}
	if !leaseViewOf(old, true, leaseTTLSeconds).Expired {
		t.Error("超过 TTL 的租约必须判过期")
	}
}

// ---------- config ----------

func TestConfigGetOnInvalidConfigStillAnswers(t *testing.T) {
	core, _ := testCore(t)
	data := dataOf(t, call(t, core, "config.get", map[string]any{}, ""))
	warnings, _ := data["warnings"].([]any)
	if len(warnings) == 0 {
		t.Error("配置过不了校验时 config.get 仍要回答，但必须给 warnings（界面拿它显示红条）")
	}
	views, ok := data["views"].(map[string]any)
	if !ok {
		t.Fatal("config.get 必须带 views（instances / profiles）")
	}
	if views["instances"] == nil || views["profiles"] == nil {
		t.Errorf("views 里必须两张表都在: %v", views)
	}
	if data["ruleFiles"] == nil {
		t.Error("config.get 必须带 ruleFiles（空也要是 []）")
	}
}

func TestConfigSaveKeepsCommentsAndValidates(t *testing.T) {
	core, path := testCore(t)
	original := "# 顶部注释：不能被冲掉\nlisten: 127.0.0.1:51837\n" +
		"instances:\n  - name: 主库\n    baseUrl: https://ass.example.com/\n    apiKey: k\n"
	writeRaw(t, path, original)
	// 写完之后必须重新 init：core.Init 是在文件还不存在时读的，
	// 内存里那份仍是空配置 —— 拿空配置去保存当然会被校验拦下。
	if _, err := core.Init(path, ""); err != nil {
		t.Fatal(err)
	}

	cfg := mustConfig(t, core)
	cfg.Sync.IntervalMinutes = 30
	saveEnv := call(t, core, "config.save", map[string]any{"config": cfg}, "")
	if !saveEnv.OK {
		t.Fatalf("保存失败: %+v", saveEnv.Error)
	}
	raw := readRaw(t, path)
	if !strings.Contains(raw, "# 顶部注释：不能被冲掉") {
		t.Errorf("保存一次就把注释冲掉了:\n%s", raw)
	}
	if !strings.Contains(raw, "intervalMinutes: 30") {
		t.Errorf("改动没落盘:\n%s", raw)
	}

	// 一份过不了校验的配置：**仍然落盘**，但把「不合法」当数据回给界面。
	//
	// 这一条是 t13 ③ 改的（契约 §7.2）：以前这里期望的是 invalid_config + 拒写。
	// 现在期望「写下去 + valid:false + errors」，理由见 opConfigSave 的注释。
	bad := config.Default()
	bad.SetPath(path)
	bad.Instances = []config.Instance{{Name: "主库", BaseURL: "https://x/", APIKey: "k", Profile: "不存在的方案"}}
	bad.Profiles = []config.Profile{{Name: "别的方案"}}
	badEnv := call(t, core, "config.save", map[string]any{"config": bad}, "")
	if !badEnv.OK {
		t.Fatalf("校验失败也不该拒绝落盘，实际 %+v", badEnv.Error)
	}
	badData := dataOf(t, badEnv)
	if badData["valid"] != false {
		t.Errorf("挂错方案的配置必须报 valid:false，实际 %v", badData["valid"])
	}
	if errs, _ := badData["errors"].([]any); len(errs) == 0 {
		t.Error("valid:false 时必须给 errors（界面靠它显示红条）")
	}
	if _, ok := badData["warnings"]; !ok {
		t.Error("响应必须带 warnings（空也要是 []）")
	}
	// 磁盘上确实换成了刚保存的那一份 —— 用户点保存，文件就该是新的。
	if !strings.Contains(readRaw(t, path), "不存在的方案") {
		t.Errorf("保存之后磁盘上应是新内容:\n%s", readRaw(t, path))
	}
}

// ③ 的验收：**零实例**的配置也要能存下去，而且要把「不合法」当数据报回来。
//
// 这是首发体验上的一个具体卡点：新建工作区之后，用户可能只想先设代理 /
// 先改规则，还没加实例 —— 保存被拒等于连一个 config.yaml 都建不出来。
func TestConfigSaveWritesZeroInstanceConfig(t *testing.T) {
	core, path := testCore(t)
	// 这条测试要的正是「工作区里**还没有** config.yaml」那个状态：
	// testCore 现在会先落一份默认配置（t25 提速，见那里的注释），所以这里显式删掉再重载。
	// 整个包只有这一处付那 250ms 的「文件真不存在」预算。
	if err := os.Remove(path); err != nil {
		t.Fatalf("删掉夹具落的默认配置失败: %v", err)
	}
	if _, err := core.Init(path, ""); err != nil {
		t.Fatalf("在「没有配置」的状态下重新初始化失败: %v", err)
	}
	empty := config.Default()
	empty.SetPath(path)
	empty.Proxy = "http://127.0.0.1:7890"

	env := call(t, core, "config.save", map[string]any{"config": empty}, "")
	if !env.OK {
		t.Fatalf("零实例的配置必须能落盘，实际 %+v", env.Error)
	}
	data := dataOf(t, env)
	if data["valid"] != false {
		t.Errorf("零实例过不了校验，valid 应是 false，实际 %v", data["valid"])
	}
	errs, _ := data["errors"].([]any)
	if len(errs) == 0 {
		t.Fatal("valid:false 时必须给出 errors")
	}
	if msg, _ := errs[0].(map[string]any)["message"].(string); !strings.Contains(msg, "实例") {
		t.Errorf("错误信息应是那句「还没有配置任何 ANI-RSS 实例」，实际 %q", msg)
	}
	if data["path"] != path {
		t.Errorf("响应里的 path 应是落盘路径，实际 %v", data["path"])
	}

	// 文件真的写下去了吗 —— 直接看磁盘，再看 config.get 能不能读回来。
	if raw := readRaw(t, path); !strings.Contains(raw, "616") && !strings.Contains(raw, "proxy") {
		t.Fatalf("config.yaml 没有被创建/写入:\n%s", raw)
	}
	get := dataOf(t, call(t, core, "config.get", map[string]any{}, ""))
	cfg, _ := get["config"].(map[string]any)
	if cfg == nil {
		t.Fatal("config.get 没回 config")
	}
	instances, _ := cfg["instances"].([]any)
	if len(instances) != 0 {
		t.Errorf("读回来的应是零实例的那一份，实际 %v", cfg["instances"])
	}
}

func TestConfigSaveRequiresConfig(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "config.save", map[string]any{}, "")
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Fatalf("没给 config 应是 bad_request，实际 %+v", env.Error)
	}
}

func TestConfigValidateOp(t *testing.T) {
	core, path := testCore(t)
	writeConfig(t, path, func(cfg *config.Config) {
		cfg.Instances = []config.Instance{{Name: "主库", BaseURL: "https://ass.example.com/", APIKey: "k"}}
	})
	core.Init(path, "")

	env := call(t, core, "config.validate", map[string]any{"config": map[string]any{
		"instances": []any{map[string]any{"name": "x", "baseUrl": "ftp://x", "apiKey": "k"}},
	}}, "")
	data := dataOf(t, env)
	if data["ok"] != false {
		t.Errorf("baseUrl 不是 http(s) 时 ok 应为 false，实际 %v", data["ok"])
	}
	if errors, _ := data["errors"].([]any); len(errors) == 0 {
		t.Error("必须给出逐条错误")
	}

	good := call(t, core, "config.validate", map[string]any{}, "")
	if got := dataOf(t, good)["ok"]; got != true {
		t.Errorf("payload 没给 config 时校验当前配置；当前配置应是合法的，实际 %v", got)
	}
	// 校验是只读动作：不能把内存里那份改坏。
	if len(mustConfig(t, core).Instances) != 1 {
		t.Error("config.validate 不该动到当前配置")
	}
}

func TestConfigDefaultAndSample(t *testing.T) {
	core, _ := testCore(t)
	if env := call(t, core, "config.default", map[string]any{}, ""); !env.OK {
		t.Fatalf("config.default 失败: %+v", env.Error)
	}
	sample := dataOf(t, call(t, core, "config.sample", map[string]any{}, ""))
	text, _ := sample["yaml"].(string)
	if !strings.Contains(text, "listen:") {
		t.Errorf("config.sample 应给出 config.example.yaml 原文，实际 %q", firstLine(text))
	}
	assertSampleMatchesRepo(t)
}

func TestCoreVersionAndOps(t *testing.T) {
	core, _ := testCore(t)
	version := dataOf(t, call(t, core, "core.version", map[string]any{}, ""))
	for _, key := range []string{"version", "schema", "platform", "goVersion", "configPath", "dataDir"} {
		if version[key] == nil {
			t.Errorf("core.version 缺字段 %s", key)
		}
	}
	ops := dataOf(t, call(t, core, "core.ops", map[string]any{}, ""))
	if ops["schema"] != float64(app.Schema) {
		t.Errorf("core.ops 的 schema 应是 %d，实际 %v", app.Schema, ops["schema"])
	}
	list, _ := ops["ops"].([]any)
	if len(list) != len(Ops()) {
		t.Errorf("core.ops 应列出全部 %d 个 op，实际 %d", len(Ops()), len(list))
	}
	first := list[0].(map[string]any)
	for _, key := range []string{"name", "group", "summary", "async"} {
		if _, ok := first[key]; !ok {
			t.Errorf("core.ops 的条目缺字段 %s: %v", key, first)
		}
	}
}

// core.init 可重复调用：换工作区之后，配置路径与数据目录都必须跟着换。
func TestCoreInitSwitchesWorkspace(t *testing.T) {
	core, first := testCore(t)
	if got := dataOf(t, call(t, core, "core.init", map[string]any{}, ""))["configPath"]; got != first {
		t.Errorf("core.init 应回报生效的 configPath，实际 %v want %s", got, first)
	}
	secondDir := t.TempDir()
	second := filepath.Join(secondDir, "config.yaml")
	other := dataOf(t, call(t, core, "core.init", map[string]any{"configPath": second}, ""))
	if other["configPath"] != second {
		t.Errorf("第二次 core.init 应换到新工作区，实际 %v", other["configPath"])
	}
	if other["dataDir"] != filepath.Join(secondDir, "data") {
		t.Errorf("dataDir 省略时应取 dirname(configPath)/data，实际 %v", other["dataDir"])
	}
}

// status.get 承诺不发任何网络请求：实例地址是个死地址，它也必须秒回。
func TestStatusGetDoesNotTouchNetwork(t *testing.T) {
	core, path := testCore(t)
	writeConfig(t, path, func(cfg *config.Config) {
		cfg.Instances = []config.Instance{{Name: "死的实例", BaseURL: "http://127.0.0.1:1/", APIKey: "k"}}
	})
	core.Init(path, "")

	started := time.Now()
	data := dataOf(t, call(t, core, "status.get", map[string]any{"limit": 3}, ""))
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("status.get 不该走网络，耗时 %s", elapsed)
	}
	if data["configPath"] == nil || data["dataDir"] == nil {
		t.Errorf("status.get 必须报出 configPath / dataDir：%v", data)
	}
	instances, _ := data["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("应报 1 个实例，实际 %v", data["instances"])
	}
	scheme := instances[0].(map[string]any)
	if scheme["instance"] != "死的实例" {
		t.Errorf("方案视图不对: %v", scheme)
	}
	if lease, ok := data["lease"].(map[string]any); !ok || lease["held"] != false {
		t.Errorf("没有常驻进程时租约应是 {held:false}，实际 %v", data["lease"])
	}
	if runs, ok := data["runs"].([]any); !ok || len(runs) != 0 {
		t.Errorf("没有运行记录时 runs 应是 []，实际 %v", data["runs"])
	}
}

func TestJournalTailAndClear(t *testing.T) {
	core, path := testCore(t)
	writeConfig(t, path, nil)
	core.Init(path, "")

	data := dataOf(t, call(t, core, "journal.tail", map[string]any{"limit": 5}, ""))
	if data["path"] == nil {
		t.Error("journal.tail 必须报出日志路径")
	}
	if entries, ok := data["entries"].([]any); !ok || len(entries) != 0 {
		t.Errorf("没有记录时 entries 应是 []，实际 %v", data["entries"])
	}
	if env := call(t, core, "journal.clear", map[string]any{"confirm": true}, ""); !env.OK {
		t.Fatalf("带 confirm 的 journal.clear 应成功: %+v", env.Error)
	}
}

func TestJournalEntryShapeInStatus(t *testing.T) {
	core, path := testCore(t)
	writeConfig(t, path, nil)
	core.Init(path, "")
	appendJournalForTest(t)

	data := dataOf(t, call(t, core, "status.get", map[string]any{"limit": 5}, ""))
	runs, _ := data["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("应带出 1 轮记录，实际 %v", data["runs"])
	}
	entry := runs[0].(map[string]any)
	for _, key := range []string{"at", "ok"} {
		if _, ok := entry[key]; !ok {
			t.Errorf("运行记录缺字段 %s: %v", key, entry)
		}
	}
	if data["nextRunAt"] == nil {
		t.Error("日志里有 NextAt 时 status.get 应报出 nextRunAt")
	}
}

func TestLeaseTakeover(t *testing.T) {
	core, path := testCore(t)
	writeConfig(t, path, nil)
	core.Init(path, "")

	data := dataOf(t, call(t, core, "lease.takeover", map[string]any{"confirm": true, "force": true}, ""))
	if data["ok"] != true {
		t.Errorf("接管应成功: %v", data)
	}
	view, _ := data["lease"].(map[string]any)
	if view["held"] != true {
		t.Errorf("接管之后必须真的持有它，实际 %v", view)
	}
	if view["note"] != "takeover" {
		t.Errorf("租约备注应写明是接管，实际 %v", view["note"])
	}
	// 接管之后本进程就是持有者，再接管一次不该报 lease_held（那是在抢自己）。
	if again := call(t, core, "lease.takeover", map[string]any{"confirm": true, "force": true}, ""); !again.OK {
		t.Errorf("自己已持有租约时再接管不该失败: %+v", again.Error)
	}
}

// ---------- 危险动作闸门 ----------

func TestConfirmGateBlocksDangerousOps(t *testing.T) {
	for _, tc := range []struct {
		op      string
		payload map[string]any
	}{
		{"subscription.delete", map[string]any{"name": "x", "keys": []string{"id:1"}}},
		{"journal.clear", map[string]any{}},
		{"lease.takeover", map[string]any{"force": true}},
	} {
		t.Run(tc.op, func(t *testing.T) {
			core, _ := testCore(t)
			env := call(t, core, tc.op, tc.payload, "")
			if env.OK {
				t.Fatalf("%s 没有 confirm:true 时不该执行", tc.op)
			}
			if env.Error.Code != CodeBadRequest {
				t.Errorf("%s 应回 bad_request，实际 %s", tc.op, env.Error.Code)
			}
			if !strings.Contains(env.Error.Message, "confirm") {
				t.Errorf("错误信息应说清要 confirm，实际 %q", env.Error.Message)
			}
		})
	}
	// confirm:false 与「没写」等价。
	core, _ := testCore(t)
	env := call(t, core, "journal.clear", map[string]any{"confirm": false}, "")
	if env.OK || env.Error.Code != CodeBadRequest {
		t.Fatalf("confirm:false 必须被拦住，实际 %+v", env.Error)
	}
}

// ---------- 小工具 ----------

func firstLine(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}

// numericEquals 比较「可能是 int 也可能是 float64」的数字。
//
// 同一条结果在进程内是 int，走过一次 JSON 就成 float64；
// 断言里写死一种类型只会让测试变脆。
func numericEquals(value any, want int) bool {
	switch v := value.(type) {
	case int:
		return v == want
	case int64:
		return v == int64(want)
	case float64:
		return v == float64(want)
	}
	return false
}

func httpNotFoundServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
}
