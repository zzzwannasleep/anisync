package bridge

import (
	"testing"
)

// t27：`plan.build` 的 `instance` 是**具名资源** —— 名字不存在时回 not_found。
//
// 为什么这是缺陷而不是「过滤条件本来就该匹配不到」：
//
//	limit 匹配不到 -> 空计划是对的（它就是个数量上限）
//	instance 打错   -> 也是空计划，而「这次没什么可做的」是一个**合法且重要的结论**
//
// 于是**一个合法的结论可以通过拼错一个名字到达**，「我打错了」与「确实没事」
// 从此无法区分。这正是这一路在猎的那类形状（静默回落到一个看起来正常的答案）。
//
// 同族的 instance.test / instance.subscriptions / subscription.* / probe.run
// 全都在实例名不存在时拒绝，plan.build 曾是唯一那个例外。

// TestPlanBuildUnknownInstanceIsNotFound 是这条修复的**主断言**：
// not_found + `detail` 逐字等于那个实例名（与 t23 的 13 个资源分支同形）。
func TestPlanBuildUnknownInstanceIsNotFound(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	const typo = "没有这个实例"
	env := call(t, core, "plan.build", map[string]any{"instance": typo}, "")
	if env.OK {
		// 修复前就是这个样子：一份空计划 + ok:true，与「真的没什么可做」一模一样。
		t.Fatalf("实例名打错时不该回一份空计划（那与「真的没什么可做」无法区分）：%+v",
			dataOf(t, env))
	}
	if env.Error.Code != CodeNotFound {
		t.Fatalf("应是 not_found，实际 %s（%s）", env.Error.Code, env.Error.Message)
	}
	if env.Error.Detail != typo {
		t.Errorf("detail 应逐字是那个实例名 %q，实际 %q", typo, env.Error.Detail)
	}
	// detail 不能是 op 名：§3 的两种含义靠它区分（见 t23）。
	if env.Error.Detail == "plan.build" {
		t.Error("资源分支的 detail 不该是 op 名 —— 客户端会把它读成版本偏斜")
	}
}

// TestPlanBuildKnownInstanceStillPlans：给了**存在**的实例名照常出计划。
//
// 这条也在钉「规整之后要能匹配上」：带首尾空白的实例名会被 requireInstance 规整，
// 若把原文继续往下传给 planner（那边是逐字比较），就会出现
// 「校验通过、计划却是空的」—— 又一个静默空结果。
func TestPlanBuildKnownInstanceStillPlans(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	for _, name := range []string{"主库", "  主库  "} {
		env := call(t, core, "plan.build", map[string]any{"instance": name}, "")
		if !env.OK {
			t.Fatalf("instance=%q 是存在的实例，应当照常出计划，实际 %+v", name, env.Error)
		}
		plan, _ := dataOf(t, env)["plan"].(map[string]any)
		if plan == nil {
			t.Fatalf("instance=%q 没回 plan", name)
		}
		// 计划里必须真的有这个实例的方案 —— 否则就是「校验过了但被静默过滤掉了」。
		schemes, _ := plan["schemes"].([]any)
		if len(schemes) == 0 {
			t.Errorf("instance=%q 的计划里一个方案都没有：名字过了校验却没被 planner 认出来", name)
		}
	}
}

// TestPlanBuildInstanceOmittedMeansAllInstances：省略 / 空串 = 「所有实例」，行为不变。
func TestPlanBuildInstanceOmittedMeansAllInstances(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	for _, payload := range []map[string]any{
		{},                  // 省略
		{"instance": ""},    // 空串
		{"instance": "   "}, // 只有空白（规整之后也是空）
	} {
		env := call(t, core, "plan.build", payload, "")
		if !env.OK {
			t.Fatalf("payload=%v 是「所有实例」的意思，不该报错，实际 %+v", payload, env.Error)
		}
		plan, _ := dataOf(t, env)["plan"].(map[string]any)
		if plan == nil {
			t.Fatalf("payload=%v 没回 plan", payload)
		}
		schemes, _ := plan["schemes"].([]any)
		if len(schemes) == 0 {
			t.Errorf("payload=%v 得到的计划里一个方案都没有 —— 空串被当成了「不存在的实例名」", payload)
		}
	}
}

// TestPlanBuildLimitIsNotANamedResource：`limit` 是数量上限，不是具名资源 —— 行为不变。
//
// 这条是上面那条修复的**边界**：不能因为「空结果很可疑」就把 limit 也拦掉。
func TestPlanBuildLimitIsNotANamedResource(t *testing.T) {
	server, _ := newRichInstance(t)
	core, _ := coreWithInstance(t, server.URL, nil)

	// limit=0 是「不限」（planner 的语义），必须照常成功。
	env := call(t, core, "plan.build", map[string]any{"limit": 0}, "")
	if !env.OK {
		t.Fatalf("limit=0 表示不限，不该报错，实际 %+v", env.Error)
	}
	if dataOf(t, env)["plan"] == nil {
		t.Fatal("limit=0 时没回 plan")
	}

	// limit 正常值时也要照常成功（它不是资源，匹配不到不算错）。
	env = call(t, core, "plan.build", map[string]any{"limit": 1}, "")
	if !env.OK {
		t.Fatalf("limit=1 不该报错，实际 %+v", env.Error)
	}
}
