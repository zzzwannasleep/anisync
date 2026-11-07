package app

import (
	"time"

	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/journal"
	"github.com/zzzwannasleep/anisync/internal/lease"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

// InstanceStatus 是 status 命令与 status.get 共用的实例视图（不含网络请求）。
type InstanceStatus struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	// Profile 是实际生效的方案名；空 = 顶层 rules。
	// 刻意不塞「（默认，顶层 rules）」这种展示文案：README 说 --json 是给脚本用的，
	// 脚本要判「回落」应该看 requested/missing，而不是匹配一句中文。
	Profile      string                `json:"profile"`
	ProfileLabel string                `json:"profileLabel"`
	Requested    string                `json:"requested,omitempty"`
	Missing      bool                  `json:"missing,omitempty"`
	MaxStandby   int                   `json:"maxStandby"`
	SiteOrder    []string              `json:"siteOrder"`
	RuleCount    int                   `json:"ruleCount"`
	DenyCount    int                   `json:"denyCount"`
	RuleFiles    []config.RuleFileInfo `json:"ruleFiles,omitempty"`
	Enabled      bool                  `json:"enabled"`
}

// ProfileLabel 把生效方案渲染成一句人话。
//
// 回落的写法是刻意啰嗦的：实例挂的方案名写错时，只显示「（默认，顶层 rules）」
// 会让人以为配置本来就该这样，而真相是「配置里写的那个名字不存在」。
func ProfileLabel(scheme config.Scheme) string {
	switch {
	case scheme.Missing:
		return "回落：顶层 rules —— 配置里写的「" + scheme.Requested + "」不存在"
	case scheme.Name == "":
		return "（默认，顶层 rules）"
	default:
		return scheme.Name
	}
}

// MaxStandbyOf 取一条订阅生效的副 RSS 上限。
func MaxStandbyOf(scheme config.Scheme) int {
	if scheme.MaxStandby > 0 {
		return scheme.MaxStandby
	}
	return 1
}

// InstanceStatuses 汇总「实例 + 方案」这张表。全程不发网络请求。
func InstanceStatuses(cfg *config.Config) []InstanceStatus {
	out := make([]InstanceStatus, 0, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		scheme := cfg.SchemeFor(instance.Name)
		out = append(out, InstanceStatus{
			Name:         instance.Name,
			BaseURL:      instance.BaseURL,
			Profile:      scheme.Name,
			ProfileLabel: ProfileLabel(scheme),
			Requested:    scheme.Requested,
			Missing:      scheme.Missing,
			MaxStandby:   MaxStandbyOf(scheme),
			SiteOrder:    scheme.SourceOrder,
			// 报「实际会参与匹配」的条数：rules.New 会跳过 match 为空的规则，
			// 报总条数会让人以为配了 3 条、其实只有 1 条生效。
			RuleCount: scheme.EffectivePrimary(),
			DenyCount: scheme.EffectiveDeny(),
			RuleFiles: scheme.RuleFiles,
			Enabled:   instance.IsEnabled(),
		})
	}
	return out
}

// SchemeSummaries 汇总每个启用实例实际生效的方案。不发网络请求。
//
// 形状与 planner.Build 里那份一致：status.get 承诺不发任何网络请求，
// 但它报出来的方案表必须和 plan 顶部那张表对得上，否则「看状态」与「看计划」会打架。
func SchemeSummaries(cfg *config.Config) []planner.SchemeSummary {
	out := make([]planner.SchemeSummary, 0, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		if !instance.IsEnabled() {
			continue
		}
		scheme := cfg.SchemeFor(instance.Name)
		out = append(out, planner.SchemeSummary{
			Instance:   instance.Name,
			Profile:    scheme.Name,
			MaxStandby: MaxStandbyOf(scheme),
			SiteOrder:  scheme.SourceOrder,
			RuleCount:  scheme.EffectivePrimary(),
			DenyCount:  scheme.EffectiveDeny(),
			RuleFiles:  scheme.RuleFiles,
		})
	}
	return out
}

// CurrentLease 读出当前租约持有者；没人持有时第二个返回值为 false。
func CurrentLease(path string) (lease.Info, bool) { return lease.Holder(path) }

// TailJournal 读最近 n 轮运行记录。
func TailJournal(limit int) ([]journal.Entry, error) { return journal.Tail(JournalPath(), limit) }

// NextRunAt 从运行日志里推断下一次运行时间。
//
// 取最后一条带 NextAt 的记录：它正是调度循环写给「我还活着」用的。
// 没有任何记录时返回 false —— 猜一个时间出来只会让人以为后台在跑。
func NextRunAt(entries []journal.Entry) (time.Time, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].NextAt.IsZero() {
			return entries[i].NextAt, true
		}
	}
	return time.Time{}, false
}
