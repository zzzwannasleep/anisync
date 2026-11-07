package bridge

import (
	"fmt"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/journal"
	"github.com/zzzwannasleep/anisync/internal/lease"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

// RuleFileView 描述一个外置规则文件（只读展示）。
//
// 它**就是** config.RuleFileInfo，不另造一套字段名（契约 §6.0）：
// `{path, kind, count}` 只有这一种形状，**不许输出超集**。
//
// 为什么超集是错的：契约一旦允许两种读法，就等于没有契约 ——
// Dart 侧只能猜哪个字段权威，而对拍测试再也抓不出漂移
// （多出来的字段永远「合法」）。字段集合必须与文档逐字相同。
//
// owner 不在每一行里，它是外层对象：SchemeView 的持有者是同一对象里的 instance，
// ProfileView 的是 name，Config.fileRules 的是「顶层 rules」（没有名字）。
type RuleFileView = config.RuleFileInfo

// ruleFileViews 把规则文件摘要渲染成契约形状。
//
// 同一份文件被两套方案各引用一次时会出现两条 —— 那是两个持有者各自的加载结果，
// 不是重复数据，**不要去重**。
func ruleFileViews(files []config.RuleFileInfo) []RuleFileView {
	out := make([]RuleFileView, 0, len(files))
	out = append(out, files...)
	return out
}

// SchemeView 是一个实例实际生效的方案（字段与 planner.SchemeSummary 对齐）。
type SchemeView struct {
	Instance   string                `json:"instance"`
	Profile    string                `json:"profile,omitempty"`
	MaxStandby int                   `json:"maxStandby"`
	SiteOrder  []string              `json:"siteOrder"`
	RuleCount  int                   `json:"ruleCount"`
	DenyCount  int                   `json:"denyCount"`
	RuleFiles  []config.RuleFileInfo `json:"ruleFiles"`
}

// schemeViewOf 渲染一个实例的方案视图。
func schemeViewOf(summary planner.SchemeSummary) SchemeView {
	return SchemeView{
		Instance:   summary.Instance,
		Profile:    summary.Profile,
		MaxStandby: summary.MaxStandby,
		SiteOrder:  nonNilStrings(summary.SiteOrder),
		RuleCount:  summary.RuleCount,
		DenyCount:  summary.DenyCount,
		RuleFiles:  ruleFileViews(summary.RuleFiles),
	}
}

// InstanceView 是一个实例的界面视图。apiKey 一律遮蔽：它不会经过任何一层明文。
type InstanceView struct {
	Name         string     `json:"name"`
	BaseURL      string     `json:"baseUrl"`
	APIKey       string     `json:"apiKey"`
	HasAPIKey    bool       `json:"hasApiKey"`
	Enabled      bool       `json:"enabled"`
	Profile      string     `json:"profile"`
	ProfileLabel string     `json:"profileLabel"`
	Scheme       SchemeView `json:"scheme"`
}

// instanceViewOf 渲染一个实例。
func instanceViewOf(cfg *config.Config, instance config.Instance) InstanceView {
	scheme := cfg.SchemeFor(instance.Name)
	summary := planner.SchemeSummary{
		Instance:   instance.Name,
		Profile:    scheme.Name,
		MaxStandby: app.MaxStandbyOf(scheme),
		SiteOrder:  scheme.SourceOrder,
		RuleCount:  scheme.EffectivePrimary(),
		DenyCount:  scheme.EffectiveDeny(),
		RuleFiles:  scheme.RuleFiles,
	}
	return InstanceView{
		Name:         instance.Name,
		BaseURL:      instance.BaseURL,
		APIKey:       MaskKey(instance.APIKey),
		HasAPIKey:    strings.TrimSpace(instance.APIKey) != "",
		Enabled:      instance.IsEnabled(),
		Profile:      instance.Profile,
		ProfileLabel: app.ProfileLabel(scheme),
		Scheme:       schemeViewOf(summary),
	}
}

// MaskKey 遮蔽密钥：前 6 后 2，长度不足 10 时全遮蔽。
//
// 界面上要能认出「是不是我刚才填的那把」，但不能把整把钥匙画在屏幕上 ——
// 截图、录屏、投屏都会把它带出去。
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	runes := []rune(key)
	if len(runes) == 0 {
		return ""
	}
	if len(runes) < 10 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:6]) + "…" + string(runes[len(runes)-2:])
}

// ProfileRulesView 是一套方案的规则部分。
type ProfileRulesView struct {
	MaxStandby   int               `json:"maxStandby"`
	Primary      []config.Rule     `json:"primary"`
	Deny         []config.DenyRule `json:"deny"`
	PrimaryFiles []string          `json:"primaryFiles,omitempty"`
	DenyFiles    []string          `json:"denyFiles,omitempty"`
}

// SourcesView 是一套方案的站点来源。
type SourcesView struct {
	Order []string `json:"order"`
}

// ProfileView 是一套订阅方案。
type ProfileView struct {
	Name      string                `json:"name"`
	Rules     ProfileRulesView      `json:"rules"`
	Sources   *SourcesView          `json:"sources"`
	RuleFiles []config.RuleFileInfo `json:"ruleFiles"`
}

// profileViewOf 渲染一套方案。
func profileViewOf(cfg *config.Config, profile config.Profile) ProfileView {
	view := ProfileView{
		Name: profile.Name,
		Rules: ProfileRulesView{
			MaxStandby:   profile.Rules.MaxStandby,
			Primary:      nonNilRules(profile.Rules.Primary),
			Deny:         nonNilDeny(profile.Rules.Deny),
			PrimaryFiles: profile.Rules.PrimaryFiles,
			DenyFiles:    profile.Rules.DenyFiles,
		},
		RuleFiles: ruleFileViews(profile.FileRules),
	}
	if profile.Sources != nil {
		order := profile.Sources.Order
		if len(order) == 0 {
			order = cfg.Sources.Order
		}
		view.Sources = &SourcesView{Order: nonNilStrings(order)}
	}
	return view
}

// RSSItem 是一条 RSS（主 RSS 或副 RSS）。
type RSSItem struct {
	URL    string `json:"url"`
	Group  string `json:"group,omitempty"`
	Title  string `json:"title,omitempty"`
	Offset *int   `json:"offset,omitempty"`
}

// SubscriptionView 是一条订阅。
//
// raw 必须原样保留：setAni 是全量覆盖，界面改完一个字段要把整份对象交回去。
type SubscriptionView struct {
	Key           string         `json:"key"`
	ID            string         `json:"id,omitempty"`
	Title         string         `json:"title,omitempty"`
	BgmID         string         `json:"bgmId,omitempty"`
	BgmURL        string         `json:"bgmUrl,omitempty"`
	Image         string         `json:"image,omitempty"`
	Season        *int           `json:"season"`
	Offset        *int           `json:"offset"`
	StandbyOffset *int           `json:"standbyOffset"`
	Enable        bool           `json:"enable"`
	Profile       string         `json:"profile,omitempty"`
	Main          *RSSItem       `json:"main"`
	Standby       []RSSItem      `json:"standby"`
	Issues        []string       `json:"issues"`
	Raw           map[string]any `json:"raw"`
}

// SubscriptionKey 算出一条订阅的稳定标识。
//
// 规则按序（契约 §6.3）：
//  1. id 非空且非 "0" → "id:<id>"
//  2. bgmId 非空 → "bgm:<bgmId>"
//  3. 否则 → "title:<去掉全部空白并转小写后的标题>"
//
// UI 只认 key，不拿 title 当身份：标题会随字幕组改名、随季号变化，
// 用它做标识会让「同一条订阅」在两次刷新之间变成两条。
func SubscriptionKey(ani assapi.Ani) string {
	if id := stringFieldOf(ani, "id"); id != "" && id != "0" {
		return "id:" + id
	}
	if bgm := BgmIDOf(ani); bgm != "" {
		return "bgm:" + bgm
	}
	if title := app.FoldTitle(SubscriptionTitle(ani)); title != "" {
		return "title:" + title
	}
	return ""
}

// BgmIDOf 取一条订阅的 bgmId：先看实例自己的字段，再从 bgmUrl / RSS 地址里认。
func BgmIDOf(ani assapi.Ani) string {
	if bgm := stringFieldOf(ani, "bgmId"); bgm != "" {
		return bgm
	}
	return app.BgmIDOfAni(ani)
}

// SubscriptionTitle 取一条订阅的可读标题。
func SubscriptionTitle(ani assapi.Ani) string {
	for _, key := range []string{"title", "mikanTitle", "name"} {
		if value := stringFieldOf(ani, key); value != "" {
			return value
		}
	}
	return ""
}

// subscriptionViewOf 渲染一条订阅。
func subscriptionViewOf(ani assapi.Ani) SubscriptionView {
	issues := []string{}
	view := SubscriptionView{
		Key:           SubscriptionKey(ani),
		ID:            stringFieldOf(ani, "id"),
		Title:         SubscriptionTitle(ani),
		BgmID:         BgmIDOf(ani),
		BgmURL:        stringFieldOf(ani, "bgmUrl"),
		Image:         firstNonEmpty(stringFieldOf(ani, "image"), stringFieldOf(ani, "cover")),
		Season:        intFieldOrIssue(ani, "season", &issues, "season"),
		Offset:        intFieldOrIssue(ani, "offset", &issues, "offset"),
		StandbyOffset: intFieldOrIssue(ani, "standbyRssListOffset", &issues, "standbyOffset"),
		Enable:        boolFieldOf(ani, "enable"),
		Profile:       stringFieldOf(ani, "profile"),
		Standby:       []RSSItem{},
		Issues:        issues,
		Raw:           ani,
	}
	// standbyOffset 在实例对象里的位置并不统一：有的写在订阅上，有的写在每个副 RSS 上。
	// 订阅级没读到就退到第一条副 RSS 的 offset —— 那正是 ANI-RSS 用来推断集偏移的地方。
	if view.StandbyOffset == nil {
		if raw, ok := ani["standbyRssList"].([]any); ok && len(raw) > 0 {
			if m, ok := raw[0].(map[string]any); ok {
				if value, ok := intValue(m["offset"]); ok {
					view.StandbyOffset = &value
				}
			}
		}
	}
	if url := stringFieldOf(ani, "url"); url != "" {
		view.Main = &RSSItem{
			URL:   url,
			Group: stringFieldOf(ani, "subgroup"),
			Title: view.Title,
		}
	}
	if raw, ok := ani["standbyRssList"].([]any); ok {
		for i, item := range raw {
			m, ok := item.(map[string]any)
			if !ok {
				view.Issues = append(view.Issues, fmt.Sprintf("副 RSS 第 %d 条不是对象，已跳过", i+1))
				continue
			}
			entry := RSSItem{
				URL:   stringFieldOf(m, "url"),
				Group: firstNonEmpty(stringFieldOf(m, "label"), stringFieldOf(m, "subgroup")),
				Title: stringFieldOf(m, "title"),
			}
			if value, ok := intValue(m["offset"]); ok {
				entry.Offset = &value
			}
			view.Standby = append(view.Standby, entry)
		}
	}
	return view
}

// LeaseView 是单飞租约的视图；空闲时只有 {"held":false}。
type LeaseView struct {
	Held       bool    `json:"held"`
	PID        int     `json:"pid,omitempty"`
	Note       string  `json:"note,omitempty"`
	Owner      string  `json:"owner,omitempty"`
	AgeSeconds float64 `json:"ageSeconds,omitempty"`
	Expired    bool    `json:"expired,omitempty"`
}

// leaseViewOf 渲染租约。
func leaseViewOf(info lease.Info, held bool, ttlSeconds float64) LeaseView {
	if !held {
		return LeaseView{Held: false}
	}
	age := info.Age(time.Now())
	return LeaseView{
		Held:       true,
		PID:        info.PID,
		Note:       info.Note,
		Owner:      info.Host,
		AgeSeconds: age.Seconds(),
		Expired:    age.Seconds() >= ttlSeconds,
	}
}

// RunEntry 就是 journal.Entry 原样（json tag 已有）。
type RunEntry = journal.Entry

// intFieldOrIssue 读一个整数字段；读不出数字时置空并把原因记进 issues。
//
// 刻意不猜：实例里这几个字段可能是对象、字符串、null（不同版本、不同来源的
// 订阅对象都出现过）。猜一个 0 出来，界面上就会出现「季度 0」这种没人写过的东西。
func intFieldOrIssue(ani assapi.Ani, key string, issues *[]string, label string) *int {
	raw, exists := ani[key]
	if !exists || raw == nil {
		return nil
	}
	if value, ok := intValue(raw); ok {
		return &value
	}
	if issues != nil {
		*issues = append(*issues, fmt.Sprintf("%s 不是数字（原值：%v），已置空", label, raw))
	}
	return nil
}

// intValue 把 JSON / Go 里的数字收敛成 int。
func intValue(raw any) (int, bool) {
	switch value := raw.(type) {
	case float64:
		return int(value), true
	case int:
		return value, true
	case int64:
		return int(value), true
	}
	return 0, false
}

// stringFieldOf 取一个字符串字段。
func stringFieldOf(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if value, ok := m[key].(string); ok {
		return value
	}
	return ""
}

// boolFieldOf 取一个布尔字段。
func boolFieldOf(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	if value, ok := m[key].(bool); ok {
		return value
	}
	return false
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// nonNilStrings 保证 JSON 里是 [] 而不是 null。
func nonNilStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// nonNilRules / nonNilDeny 同上。
func nonNilRules(in []config.Rule) []config.Rule {
	if in == nil {
		return []config.Rule{}
	}
	return in
}

func nonNilDeny(in []config.DenyRule) []config.DenyRule {
	if in == nil {
		return []config.DenyRule{}
	}
	return in
}
