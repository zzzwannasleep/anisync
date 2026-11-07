package bridge

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// updateFixtures 是「重新生成契约夹具」的开关。
//
//	go test ./internal/bridge -run TestOpsSnapshot -update
//
// 不带 -update 时这两条测试是**校验**：夹具与注册表/实现不一致就红。
// 只写不校验的「生成器」不是门禁 —— 它只会在你忘记跑的时候安静地过期。
var updateFixtures = flag.Bool("update", false, "重新生成 app/test/fixtures 下的契约夹具")

// fixturesDir 是契约夹具的落点。
//
// 路径是**仓库根**下的 app/test/fixtures —— 那是 Flutter 侧（t3）读它的地方。
// 相对路径从 internal/bridge 出发，所以是 ../../app/test/fixtures。
func fixturesDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("解析仓库根失败: %v", err)
	}
	return filepath.Join(root, "app", "test", "fixtures")
}

// writeFixture 写入或校验一份夹具。
//
// 两侧都先过一遍 canonical：夹具要能在 Windows / macOS / Linux 上产出同一份内容，
// 否则「换台机器跑测试就红」会让这份门禁在一周内被所有人忽略。
func writeFixture(t *testing.T, path string, content []byte) {
	t.Helper()
	content = []byte(canonical(string(content)))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建夹具目录失败: %v", err)
	}
	if *updateFixtures {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("写入夹具失败: %v", err)
		}
		t.Logf("已更新夹具 %s（%d 字节）", path, len(content))
		return
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到夹具 %s（先跑：go test ./internal/bridge -run %s -update）: %v", path, t.Name(), err)
	}
	if canonical(string(existing)) != string(content) {
		t.Errorf("夹具 %s 与当前实现不一致。\n"+
			"契约是对拍用的：Go 侧改了 op 表或视图形状，Dart 侧必须同时改，\n"+
			"所以这里刻意红给你看。确认新形状没问题后跑：\n"+
			"  go test ./internal/bridge -run %s -update\n", path, t.Name())
	}
}

// platformPattern 匹配夹具里那个「跟平台走」的字面量。
var platformPattern = regexp.MustCompile(`"platform":\s*"[a-z0-9]+"`)

// canonical 把夹具里跟机器有关的部分换成占位符（幂等，两侧都过）。
//
// 两处：
//   - platform 是 GOOS，换台机器就不一样；
//   - Windows 的路径分隔符是反斜杠，同一个 <WORKSPACE> 在别的系统上是正斜杠。
//
// 这两处不定死，「夹具过期」的红就会永远亮着 —— 而永远亮着的门禁等于没有门禁。
func canonical(text string) string {
	text = platformPattern.ReplaceAllString(text, `"platform": "<GOOS>"`)
	text = strings.ReplaceAll(text, workspacePlaceholder+`\\`, workspacePlaceholder+"/")
	return text
}

// opsSnapshotOp 是 ops.json 里的一条 op。
type opsSnapshotOp struct {
	Name    string `json:"name"`
	Group   string `json:"group"`
	Summary string `json:"summary"`
	Async   bool   `json:"async"`
}

// opsSnapshot 是 ops.json 的整体形状。
type opsSnapshot struct {
	Schema int             `json:"schema"`
	Ops    []opsSnapshotOp `json:"ops"`
}

// TestOpsSnapshot 把 op 注册表 dump 成 app/test/fixtures/ops.json。
//
// 排序稳定（按名字）便于 diff：Dart 侧的契约测试拿它断言
// 「两边一个都不多、一个都不少」，所以这个文件是两个人的公共事实，
// 顺序抖动只会让 diff 全是噪音。
func TestOpsSnapshot(t *testing.T) {
	snapshot := opsSnapshot{Schema: 1, Ops: []opsSnapshotOp{}}
	for _, op := range Ops() {
		snapshot.Ops = append(snapshot.Ops, opsSnapshotOp{
			Name:    op.Name,
			Group:   op.Group,
			Summary: op.Summary,
			Async:   op.Async,
		})
	}
	sort.Slice(snapshot.Ops, func(i, j int) bool { return snapshot.Ops[i].Name < snapshot.Ops[j].Name })

	content, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("序列化注册表失败: %v", err)
	}
	content = append(content, '\n')
	writeFixture(t, filepath.Join(fixturesDir(t), "ops.json"), content)

	// 顺带把门禁里那几条「两边必须一致」的事实也钉在 Go 侧，
	// 免得 Dart 侧等一个永远不来的 op。
	for _, name := range []string{
		"core.init", "core.version", "core.ops",
		"config.get", "config.save", "config.validate", "config.default", "config.sample",
		"instance.list", "instance.test", "instance.subscriptions",
		"subscription.set", "subscription.add", "subscription.delete", "subscription.resolve",
		"profile.upsert", "profile.delete", "profile.ruleFile",
		"discover.list", "discover.apply", "discover.autoAdd",
		"plan.build", "plan.apply",
		"sync.run", "job.list", "job.get", "job.cancel",
		"status.get", "journal.tail", "journal.clear", "lease.takeover",
		"backup.create", "backup.list", "backup.restore",
		"probe.run", "bgm.search",
		"log.recent",
	} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("契约 §7 里的 op %s 没有实现", name)
		}
	}
}

// 异步集合的**权威来源是契约文档**（§7.8 那一列标了「异步」的行）——
// 见 contract_doc_test.go 的 TestAsyncOpsMatchContractDocument。
// 这里刻意不再写死一份名单：写死的那份只和注册表自己对，
// 文档把某个 op 改标成异步、或新增第 5 个异步 op，它都不会响。

// TestConfirmGateCoversDangerousOps 钉住危险动作闸门。
//
// 没有 confirm:true 就执行等于把「删订阅」做成了单击可完成的操作，
// 而它删的是实例里的东西 —— 二次确认不是 UI 的礼貌，是这一层的硬约束。
func TestConfirmGateCoversDangerousOps(t *testing.T) {
	for _, name := range []string{"subscription.delete", "journal.clear", "lease.takeover"} {
		op, ok := Lookup(name)
		if !ok {
			t.Fatalf("%s 没有注册", name)
		}
		if !op.Confirm {
			t.Errorf("%s 必须挂上 confirm 闸门", name)
		}
	}
}
