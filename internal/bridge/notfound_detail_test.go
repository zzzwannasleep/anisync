package bridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// t23：`not_found` 的**资源分支**必须把资源名放进 `detail`（契约 §3 的判别表）。
//
// 为什么这件事不能省：客户端要靠 `detail` 区分两种 not_found ——
//
//	detail == op 名   -> 「这个 op 不认识」= 我的内核比节点新（版本偏斜）
//	detail == 资源名  -> 「op 认识，但它要的东西没有」= 去显示这个名字
//
// 而**解析 `message` 是契约明文禁止的**（它是会被翻译和改写的人话）。
// 资源分支不填 detail，UI 那条「把资源名显示出来」的要求在现有契约下就无法满足。
//
// 这张表是**逐个分支**列出来的（不是只测 verifier 报的那两个）：他明确声明
// 「没有断言资源分支普遍如此」，所以这里自己把全部资源分支过一遍。

// resourceBranch 是一个「按名字取资源」的分支。
type resourceBranch struct {
	name    string         // 分支的可读名字（失败信息里用）
	op      string         // 调哪个 op
	payload map[string]any // 传进去的东西（名字字段就是那个资源名）
	want    string         // 期望的 detail（= 传进去的资源名）
	async   bool           // 异步 op：错误在 job 里，不在信封里
	// setup 是可选的准备工作（工作区路径给进来）。用来造「配置里引用了、
	// 但磁盘上已经没有」这类只有先存在才能失败的场景。
	setup func(t *testing.T, path string)
	// afterInit 在配置被载入**之后**跑：用来删掉一个刚被引用过的文件
	// （「文件必须先在，Load 才过得去；载入之后再让它消失」）。
	afterInit func(t *testing.T, path string)
}

// resourceBranches 覆盖 t23 时的全部资源分支。
//
// 分组依据是**资源种类**：
//   - 实例名：6 个 op 都走 requireInstance 这一个入口（所以改一处全覆盖）
//   - 方案名：profile.delete、profile.ruleFile 的 owner
//   - 规则文件路径：profile.ruleFile（文件没被任何方案引用 / 引用了但磁盘上没有）
//   - job id：job.get
//   - 存档文件：backup.restore（异步，错误经 job 出来）
//   - 探针的实例名：probe.run（配了名字但配置里没有）
func resourceBranches() []resourceBranch {
	const missingInstance = "没有这个实例"
	return []resourceBranch{
		{name: "instance.test 的实例名", op: "instance.test", payload: map[string]any{"name": missingInstance}, want: missingInstance},
		{name: "instance.subscriptions 的实例名", op: "instance.subscriptions", payload: map[string]any{"name": missingInstance}, want: missingInstance},
		{name: "subscription.set 的实例名", op: "subscription.set", payload: map[string]any{"name": missingInstance, "items": []any{}}, want: missingInstance},
		{name: "subscription.add 的实例名", op: "subscription.add", payload: map[string]any{"name": missingInstance, "bgmId": "1"}, want: missingInstance},
		{name: "subscription.delete 的实例名", op: "subscription.delete", payload: map[string]any{"name": missingInstance, "keys": []any{"x"}, "confirm": true}, want: missingInstance},
		{name: "subscription.resolve 的实例名", op: "subscription.resolve", payload: map[string]any{"name": missingInstance, "rssUrl": "https://x/"}, want: missingInstance},
		{name: "profile.delete 的方案名", op: "profile.delete", payload: map[string]any{"name": "没有这个方案"}, want: "没有这个方案"},
		{name: "profile.ruleFile 的 owner 方案名", op: "profile.ruleFile", payload: map[string]any{"owner": "没有这个方案", "path": "raws.yaml"}, want: "没有这个方案"},
		{name: "profile.ruleFile 的规则文件（没被引用）", op: "profile.ruleFile", payload: map[string]any{"path": "没有这个文件.yaml"}, want: "没有这个文件.yaml"},
		{name: "job.get 的 job id", op: "job.get", payload: map[string]any{"id": "zzz"}, want: "zzz"},
		{name: "backup.restore 的存档文件", op: "backup.restore", payload: map[string]any{"file": "没有这个存档.zip"}, want: "没有这个存档.zip", async: true},
		{name: "probe.run 的实例名", op: "probe.run", payload: map[string]any{"site": "mikan", "title": "某番", "instance": missingInstance}, want: missingInstance},
		{
			name: "profile.ruleFile 的规则文件（配置里引用了、磁盘上没了）",
			op:   "profile.ruleFile", payload: map[string]any{"path": "gone.yaml"}, want: "gone.yaml",
			setup: func(t *testing.T, path string) {
				// 先让这个规则文件真的存在：配置里写一个不存在的文件是**加载阶段**的硬错误
				// （LoadRuleFiles 会当场拦下），那样连工作区都建不起来，
				// 也就跑不到 profile.ruleFile 这一步。
				writeRaw(t, filepath.Join(filepath.Dir(path), "gone.yaml"),
					"- name: 简日\n  match: 简日\n  priority: 10\n  scope: main\n  in: title\n")
				writeConfig(t, path, func(cfg *config.Config) {
					cfg.Rules.PrimaryFiles = []string{"gone.yaml"}
				})
			},
			afterInit: func(t *testing.T, path string) {
				// 载入之后再删掉它 —— 复刻「配置文件还在指向一个已经被删掉的文件」，
				// 这是真实会发生的（用户手动删了规则文件、或者从别处拷来一份配置）。
				if err := os.Remove(filepath.Join(filepath.Dir(path), "gone.yaml")); err != nil {
					t.Fatalf("删规则文件失败: %v", err)
				}
			},
		},
	}
}

// TestNotFoundResourceBranchesCarryTheName 逐分支断言 code == not_found && detail == 名字。
func TestNotFoundResourceBranchesCarryTheName(t *testing.T) {
	for _, br := range resourceBranches() {
		t.Run(br.name, func(t *testing.T) {
			core, path := testCore(t) // 一份干净工作区：所有资源都不存在
			if br.setup != nil {
				br.setup(t, path)
				if _, err := core.Init(path, ""); err != nil {
					t.Fatalf("准备工作区之后重新载入失败: %v", err)
				}
			}
			if br.afterInit != nil {
				br.afterInit(t, path)
			}
			env := call(t, core, br.op, br.payload, "")

			if br.async {
				assertJobNotFoundDetail(t, core, &env, br)
				return
			}
			if env.OK {
				t.Fatalf("%s：这个资源不存在，不该成功", br.op)
			}
			if env.Error.Code != CodeNotFound {
				t.Fatalf("%s：应是 not_found，实际 %s（%s）", br.op, env.Error.Code, env.Error.Message)
			}
			if env.Error.Detail != br.want {
				t.Errorf("%s：detail 应是那个资源名 %q（逐字），实际 %q\n"+
					"（UI 要显示「哪个资源没有」，而它不许解析 message —— 契约 §3）",
					br.op, br.want, env.Error.Detail)
			}
		})
	}
}

// assertJobNotFoundDetail 处理异步 op：not_found 出现在 job 的 error 里。
func assertJobNotFoundDetail(t *testing.T, core *Core, env *Envelope, br resourceBranch) {
	t.Helper()
	if !env.OK {
		t.Fatalf("%s 是异步 op，信封里应回 jobId，实际 %+v", br.op, env.Error)
	}
	jobID, _ := dataOf(t, *env)["jobId"].(string)
	if jobID == "" {
		t.Fatalf("%s 没回 jobId：%v", br.op, dataOf(t, *env))
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		job := dataOf(t, call(t, core, "job.get", map[string]any{"id": jobID}, ""))["job"].(map[string]any)
		state, _ := job["state"].(string)
		if state == "running" {
			if time.Now().After(deadline) {
				t.Fatalf("%s 的 job 一直没结束", br.op)
			}
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if state != "failed" {
			t.Fatalf("%s 的 job 应失败，实际 state=%s（%v）", br.op, state, job)
		}
		errPayload, ok := job["error"].(map[string]any)
		if !ok {
			t.Fatalf("%s 的 job 失败但没带 error 对象：%v", br.op, job)
		}
		if errPayload["code"] != CodeNotFound {
			t.Fatalf("%s 的 job 错误应是 not_found，实际 %v（%v）", br.op, errPayload["code"], errPayload)
		}
		if errPayload["detail"] != br.want {
			t.Errorf("%s：job 错误里的 detail 应是那个资源名 %q（逐字），实际 %v",
				br.op, br.want, errPayload["detail"])
		}
		return
	}
}

// TestNotFoundResourceDetailIsNeverAnOpName 钉住 t23 的约束 1：
// **资源分支的 detail 绝不能等于任何合法 op 名**。
//
// 这条防的是「手一滑把自己 sc 的 op 名填进资源分支」——那会让 §3 的判别规则
// 直接读反：客户端会把「资源不存在」当成「你的内核比节点新」，
// 然后把 op 名当成版本号去显示。
func TestNotFoundResourceDetailIsNeverAnOpName(t *testing.T) {
	opNames := map[string]bool{}
	for _, op := range Ops() {
		opNames[op.Name] = true
	}

	for _, br := range resourceBranches() {
		if opNames[br.want] {
			t.Errorf("这条测试自己就错了：%q 既是资源名又是一个合法 op 名，"+
				"判别规则在那时会读反 —— 换一个资源名当测试输入", br.want)
		}
	}

	// 真跑一遍，确认**实际返回的** detail 也不是 op 名（不是只检查表里的常量）。
	for _, br := range resourceBranches() {
		t.Run(br.name, func(t *testing.T) {
			core, path := testCore(t)
			if br.setup != nil {
				br.setup(t, path)
				if _, err := core.Init(path, ""); err != nil {
					t.Fatalf("准备工作区之后重新载入失败: %v", err)
				}
			}
			if br.afterInit != nil {
				br.afterInit(t, path)
			}
			env := call(t, core, br.op, br.payload, "")
			var detail string
			if br.async {
				if !env.OK {
					return // 信封就错了，另一条测试已经在报
				}
				jobID, _ := dataOf(t, env)["jobId"].(string)
				deadline := time.Now().Add(3 * time.Second)
				for time.Now().Before(deadline) {
					job := dataOf(t, call(t, core, "job.get", map[string]any{"id": jobID}, ""))["job"].(map[string]any)
					if state, _ := job["state"].(string); state != "running" {
						if errPayload, ok := job["error"].(map[string]any); ok {
							detail, _ = errPayload["detail"].(string)
						}
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
			} else if env.Error != nil {
				detail = env.Error.Detail
			}
			if detail == "" {
				t.Fatalf("%s 没有给出 detail，资源名会只剩在 message 里（UI 不许解析 message）", br.op)
			}
			if opNames[detail] {
				t.Errorf("%s 的 detail 是 %q —— 恰好是一个合法 op 名！"+
					"客户端会把「资源不存在」读成「版本偏斜」（契约 §3）", br.op, detail)
			}
		})
	}
}

// TestUnknownOpDetailIsStillTheOpName 是上面两条的**对照**：
// op 分支的 detail 是调用方**发过来的那个名字**，两种含义靠「等于不等于自己发的名字」区分。
func TestUnknownOpDetailIsStillTheOpName(t *testing.T) {
	core, _ := testCore(t)
	env := call(t, core, "core.no_such_op", map[string]any{}, "")
	if env.OK || env.Error.Code != CodeNotFound {
		t.Fatalf("未知 op 应是 not_found，实际 %+v", env.Error)
	}
	if env.Error.Detail != "core.no_such_op" {
		t.Fatalf("op 分支的 detail 必须逐字等于调用方发来的 op 名，实际 %q", env.Error.Detail)
	}

	// 判别的真正规则：**客户端拿 detail 与「自己刚发出去的那个 op 名」比**——
	// 相等 ⇒ 这个 op 不存在（版本偏斜）；不相等 ⇒ op 认识，是资源没有。
	//
	// 注意别写成「detail 是不是一个合法 op 名」：未知 op 的 detail 恰恰**不是**
	// 注册表里的名字（否则那个 op 就存在了）。这个区别决定了判别规则怎么写，
	// 也决定了资源分支的 detail 绝不能去填一个 op 名。
	validOp := map[string]bool{}
	for _, op := range Ops() {
		validOp[op.Name] = true
	}
	if validOp[env.Error.Detail] {
		t.Fatal("未知 op 的 detail 不该出现在注册表里（那意味着它其实是个已知 op）")
	}

	// 资源分支与 op 分支必须能被同一套规则分开：资源分支的 detail 不等于它所调的 op 名。
	for _, br := range resourceBranches() {
		core, path := testCore(t)
		if br.setup != nil {
			br.setup(t, path)
			if _, err := core.Init(path, ""); err != nil {
				t.Fatalf("准备工作区之后重新载入失败: %v", err)
			}
		}
		if br.afterInit != nil {
			br.afterInit(t, path)
		}
		env := call(t, core, br.op, br.payload, "")
		if br.async || env.Error == nil {
			continue
		}
		if env.Error.Detail == br.op {
			t.Errorf("%s：资源分支的 detail 竟然等于 op 名 %q —— 客户端会把它读成版本偏斜",
				br.op, br.op)
		}
	}
}
