package bridge

import "sync"

// opsOnce / opsCache 让注册表只建一次。
//
// 刻意用函数而不是包级变量字面量：费 op 的实现（opCoreOps）需要读这张表，
// 而包级变量初始化器之间不能成环 —— 那样 `go build` 会直接报
// "initialization cycle for ops"。函数体里的引用不参与初始化器依赖图。
var (
	opsOnce  sync.Once
	opsCache []Op
)

// opsTable 是操作注册表。**op 的名字与所属分组只在这一处声明。**
//
// 顺序即契约（docs/ops-api.md §7）里的分组顺序，也就是实现顺序。
// Dart 侧对着同一份表硬编码常量，两边靠 app/test/fixtures/ops.json 对拍：
// Go 侧加了 op 忘了告诉 Dart、Dart 写了个不存在的 op —— 两种漂移都在
// `flutter test` 这一步炸出来，而不是等到用户点了按钮才发现「没反应」。
func opsTable() []Op {
	return []Op{
		// ---------- 7.1 core ----------
		{Name: "core.init", Group: "core", Summary: "切换工作区：读配置、钉住 data 目录", Handler: opCoreInit},
		{Name: "core.version", Group: "core", Summary: "程序版本、契约版本、平台", Handler: opCoreVersion},
		{Name: "core.ops", Group: "core", Summary: "列出全部 op（名字 / 分组 / 简介 / 是否异步）", Handler: opCoreOps},

		// ---------- 7.2 config ----------
		{Name: "config.get", Group: "config", Summary: "读配置 + 界面要用的派生视图", Handler: opConfigGet},
		{Name: "config.save", Group: "config", Summary: "保存配置（保住注释与键序）", Handler: opConfigSave},
		{Name: "config.validate", Group: "config", Summary: "校验一份配置，返回逐条错误", Handler: opConfigValidate},
		{Name: "config.default", Group: "config", Summary: "默认配置（新建工作区用）", Handler: opConfigDefault},
		{Name: "config.sample", Group: "config", Summary: "配置样例 YAML 原文", Handler: opConfigSample},

		// ---------- 7.3 instance ----------
		{Name: "instance.list", Group: "instance", Summary: "实例列表（含生效方案）", Handler: opInstanceList},
		{Name: "instance.test", Group: "instance", Summary: "测试一个实例的连通性", Handler: opInstanceTest},
		{Name: "instance.subscriptions", Group: "instance", Summary: "读一个实例的订阅列表", Handler: opInstanceSubscriptions},

		// ---------- 7.4 subscription ----------
		{Name: "subscription.set", Group: "subscription", Summary: "改订阅的季度 / 偏移 / 开关 / 方案", Handler: opSubscriptionSet},
		{Name: "subscription.add", Group: "subscription", Summary: "新增一条订阅", Handler: opSubscriptionAdd},
		{Name: "subscription.delete", Group: "subscription", Summary: "删除订阅（危险动作）", Confirm: true, Handler: opSubscriptionDelete},
		{Name: "subscription.resolve", Group: "subscription", Summary: "让实例把一个 RSS 解析成订阅草稿", Handler: opSubscriptionResolve},

		// ---------- 7.5 profile ----------
		{Name: "profile.upsert", Group: "profile", Summary: "新增 / 覆盖一套订阅方案", Handler: opProfileUpsert},
		{Name: "profile.delete", Group: "profile", Summary: "删除一套订阅方案", Handler: opProfileDelete},
		{Name: "profile.ruleFile", Group: "profile", Summary: "读一个外置规则文件的内容（只读）", Handler: opProfileRuleFile},

		// ---------- 7.6 discover ----------
		{Name: "discover.list", Group: "discover", Summary: "本季新番：有哪些还没加", Handler: opDiscoverList},
		{Name: "discover.apply", Group: "discover", Summary: "把选中的番剧写进 targets", Handler: opDiscoverApply},
		{Name: "discover.autoAdd", Group: "discover", Summary: "自动发现并新增订阅", Async: true, Handler: opDiscoverAutoAdd},

		// ---------- 7.7 plan ----------
		{Name: "plan.build", Group: "plan", Summary: "生成打点计划（只读）", Handler: opPlanBuild},
		{Name: "plan.apply", Group: "plan", Summary: "执行计划", Async: true, Handler: opPlanApply},

		// ---------- 7.8 sync / job ----------
		{Name: "sync.run", Group: "sync", Summary: "跑一轮完整同步（抢租约 + 写实例 + 落日志）", Async: true, Handler: opSyncRun},
		{Name: "job.list", Group: "job", Summary: "最近的任务", Handler: opJobList},
		{Name: "job.get", Group: "job", Summary: "取一个任务", Handler: opJobGet},
		{Name: "job.cancel", Group: "job", Summary: "取消一个任务", Handler: opJobCancel},

		// ---------- 7.9 status / journal / lease ----------
		{Name: "status.get", Group: "status", Summary: "实例与方案、租约、最近几轮（不发网络请求）", Handler: opStatusGet},
		{Name: "journal.tail", Group: "journal", Summary: "读最近几轮运行记录", Handler: opJournalTail},
		{Name: "journal.clear", Group: "journal", Summary: "清空运行记录（危险动作）", Confirm: true, Handler: opJournalClear},
		{Name: "lease.takeover", Group: "lease", Summary: "接管单飞租约（危险动作）", Confirm: true, Handler: opLeaseTakeover},

		// ---------- 7.10 backup ----------
		{Name: "backup.create", Group: "backup", Summary: "导出订阅存档", Handler: opBackupCreate},
		{Name: "backup.list", Group: "backup", Summary: "列出存档", Handler: opBackupList},
		{Name: "backup.restore", Group: "backup", Summary: "用存档还原订阅", Async: true, Handler: opBackupRestore},

		// ---------- 7.11 probe / bgm ----------
		{Name: "probe.run", Group: "probe", Summary: "单站点探针：看候选与命中规则", Handler: opProbeRun},
		{Name: "bgm.search", Group: "bgm", Summary: "按关键字搜 Bangumi 条目", Handler: opBgmSearch},

		// ---------- 7.12 log / events ----------
		{Name: "log.recent", Group: "log", Summary: "最近 500 行日志（事件流断了兜底）", Handler: opLogRecent},
		{Name: "log.write", Group: "log", Summary: "客户端写一行诊断日志（只进内存缓冲，有限速）", Handler: opLogWrite},
	}
}
