// Package rules 实现「正则 + 优先级」的打分，主 RSS 与副 RSS 可以各用一套规则。
//
// 语义：
//   - deny 命中 -> 直接剔除；
//   - 规则按声明顺序匹配，第一条命中的决定优先级（数字越小越优先）；
//   - scope 决定这条规则参与挑主 RSS（main）、副 RSS（standby），还是两边都参与（both）；
//   - in 决定 match 作用在哪个字符串上：group（只看组名）、title（只看资源标题）、
//     any（两边一起看，默认值，和以前的行为完全一样）。
//     分轴的理由和 ANI-RSS 一样：组名是一个身份，字幕类型才是标题里的文字。
//     混在一起匹配时，短组名会被作品名误伤（Ani ni Tsukeru Kusuri wa Nai 里的 Ani），
//     只能靠 ^ani\b|\[ani\] 这种位置锚定去绕；分成两轴之后组名规则写 ^ani$ 就够。
//   - 没命中任何规则的候选排在命中规则的候选之后，同优先级时按热度、站点顺序、名称兜底；
//   - 副 RSS 例外：只有被 standby 规则点过名的组才进副 RSS，没点过名的一律不要。
//
// 典型用法：主 RSS 用字幕组规则（in: group），副 RSS 用放流组规则（scope: standby）；
// 想按版本分档就再写几条 in: title 的规则，priority 给得更小。
package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zzzwannasleep/anisync/internal/source"
)

// UnmatchedPriority 是未命中规则候选的基准优先级。
const UnmatchedPriority = 1000

// 规则作用域取值。
const (
	ScopeMain    = "main"
	ScopeStandby = "standby"
	ScopeBoth    = "both"
)

// 规则匹配位置的取值。
const (
	// InAny 是默认值：组名与资源标题拼在一起匹配，与分轴之前的行为逐字一致。
	InAny = "any"
	// InGroup 只看组名：显示名 + 种子标题里印出来的别名（见 source.Candidate.GroupAliases）。
	InGroup = "group"
	// InTitle 只看资源标题：简日 / 繁日 / 内嵌 / 内封 / 1080p 这类版本信息。
	InTitle = "title"
)

// NormalizeIn 把 in 规整成合法取值，认不出来的一律当作 any。
func NormalizeIn(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case InGroup:
		return InGroup
	case InTitle:
		return InTitle
	default:
		return InAny
	}
}

type compiledRule struct {
	name     string
	re       *regexp.Regexp
	priority int
	site     string
	in       string
}

// Engine 是编译好的规则引擎。
type Engine struct {
	main       []compiledRule
	standby    []compiledRule
	deny       []compiledRule
	maxStandby int
}

// Rule 是输入规则（与 config.Rule 对齐，避免循环依赖）。
type Rule struct {
	Name     string
	Match    string
	Priority int
	Site     string
	Scope    string
	// In 决定 Match 作用在哪个字符串上：any（默认）/ group / title。
	In string
}

// DenyRule 是排除规则。
type DenyRule struct {
	Match string
	Why   string
}

// New 编译规则。
func New(primary []Rule, deny []DenyRule, maxStandby int) (*Engine, error) {
	engine := &Engine{maxStandby: maxStandby}
	if engine.maxStandby <= 0 {
		// 副 RSS 默认只要一条：多数人只要一个备胎，想全收的自己把这个数字调大。
		engine.maxStandby = 1
	}
	for _, rule := range primary {
		if strings.TrimSpace(rule.Match) == "" {
			continue
		}
		re, err := regexp.Compile(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("规则 %q 正则不合法: %w", rule.Name, err)
		}
		compiled := compiledRule{
			name:     rule.Name,
			re:       re,
			priority: rule.Priority,
			site:     strings.ToLower(strings.TrimSpace(rule.Site)),
			in:       NormalizeIn(rule.In),
		}
		switch strings.ToLower(strings.TrimSpace(rule.Scope)) {
		case ScopeStandby:
			engine.standby = append(engine.standby, compiled)
		case ScopeBoth:
			engine.main = append(engine.main, compiled)
			engine.standby = append(engine.standby, compiled)
		default:
			engine.main = append(engine.main, compiled)
		}
	}
	for _, rule := range deny {
		if strings.TrimSpace(rule.Match) == "" {
			continue
		}
		re, err := regexp.Compile(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("排除规则 %q 正则不合法: %w", rule.Match, err)
		}
		engine.deny = append(engine.deny, compiledRule{name: rule.Why, re: re})
	}
	return engine, nil
}

// MaxStandby 返回副 RSS 上限。
func (e *Engine) MaxStandby() int { return e.maxStandby }

// Match 返回主 RSS 作用域下命中的规则名与优先级；未命中时 ok=false。
func (e *Engine) Match(candidate source.Candidate) (name string, priority int, ok bool) {
	return matchIn(e.main, candidate)
}

// StandbyMatch 返回副 RSS 作用域下命中的规则名与优先级。
func (e *Engine) StandbyMatch(candidate source.Candidate) (name string, priority int, ok bool) {
	return matchIn(e.standby, candidate)
}

// matchIn 按声明顺序找第一条命中的规则。
//
// 三种 in 各自惰性求值：配置里通常只有一种 in，另外两种文本根本不会拼出来。
func matchIn(list []compiledRule, candidate source.Candidate) (string, int, bool) {
	var (
		anyText    string
		titleText  string
		groupNames []string
		hasAny     bool
		hasTitle   bool
		hasGroup   bool
	)
	for _, rule := range list {
		if rule.site != "" && !strings.EqualFold(rule.site, candidate.Site) {
			continue
		}
		switch rule.in {
		case InGroup:
			// 逐个名字试，而不是拼成一个字符串再试：这样 ^ani$ 只认叫 ANi 的那个组，
			// 不会被 Kirara Fantasia 里的 ani 蹭到。
			if !hasGroup {
				groupNames, hasGroup = groupNamesOf(candidate), true
			}
			for _, name := range groupNames {
				if rule.re.MatchString(name) {
					return rule.name, rule.priority, true
				}
			}
		case InTitle:
			if !hasTitle {
				titleText, hasTitle = titleTextOf(candidate), true
			}
			if rule.re.MatchString(titleText) {
				return rule.name, rule.priority, true
			}
		default:
			if !hasAny {
				anyText, hasAny = matchText(candidate), true
			}
			if rule.re.MatchString(anyText) {
				return rule.name, rule.priority, true
			}
		}
	}
	return "", UnmatchedPriority, false
}

// groupNamesOf 是 in: group 的匹配对象：显示名 + 种子标题里印出来的别名。
//
// 返回的是一个一个名字，而不是拼好的字符串 —— 规则逐个名字试，
// 于是 ^ani$ 这种精确锚定真的能只认叫 ANi 的那个组。
//
// 与 ANI-RSS 一致，组名是一个身份而不是标题里的一段文字：
// 实例把组名存成订阅的 subgroup / standby label，我们拿到的就是候选的 Label；
// 再加上从标题方括号里抠出来的别名，显示名和种子名写哪个都能命中。
func groupNamesOf(candidate source.Candidate) []string {
	names := make([]string, 0, len(candidate.GroupAliases)+2)
	seen := make(map[string]bool, len(candidate.GroupAliases)+2)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(candidate.Label)
	add(candidate.Subgroup)
	for _, alias := range candidate.GroupAliases {
		add(alias)
	}
	return names
}

// titleTextOf 是 in: title 的匹配对象：只放资源标题，不放组名。
func titleTextOf(candidate source.Candidate) string {
	return strings.Join(candidate.Titles, " ")
}

// matchText 是 in: any（默认）的匹配对象：字幕组名 + 最近的资源标题。
//
// 保留「两轴混在一起」的原样是为了不改变已有配置的行为 ——
// 新写的规则该用 in: group / in: title 把两轴分开。
func matchText(candidate source.Candidate) string {
	if len(candidate.Titles) == 0 {
		if candidate.Label != "" {
			return candidate.Label
		}
		return candidate.Subgroup
	}
	var b strings.Builder
	b.WriteString(candidate.Label)
	b.WriteString(" ")
	b.WriteString(candidate.Subgroup)
	for _, title := range candidate.Titles {
		b.WriteString(" ")
		b.WriteString(title)
	}
	return b.String()
}

// Denied 判断是否被排除。
func (e *Engine) Denied(candidate source.Candidate) (bool, string) {
	text := matchText(candidate) + " " + candidate.URL
	for _, rule := range e.deny {
		if rule.re.MatchString(text) {
			return true, rule.name
		}
	}
	return false, ""
}

// Rank 过滤 + 按主 RSS 规则排序 + 回填两套优先级，返回排好序的候选。
// 排序键：主 RSS 规则优先级 -> 热度（发布条数，降序）-> 站点顺序 -> 组名。
// 排序后按「组名」去重：同一个字幕组在多个站点的镜像只保留最优的那条，
// 这样副 RSS 列表里是不同字幕组，而不是同一个组的多个镜像。
func (e *Engine) Rank(candidates []source.Candidate) []source.Candidate {
	out := make([]source.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if denied, _ := e.Denied(candidate); denied {
			continue
		}
		if name, priority, ok := e.Match(candidate); ok {
			candidate.Priority = priority
			candidate.RuleName = name
		} else {
			candidate.Priority = UnmatchedPriority
		}
		if name, priority, ok := e.StandbyMatch(candidate); ok {
			candidate.StandbyPriority = priority
			candidate.StandbyRuleName = name
		} else {
			candidate.StandbyPriority = UnmatchedPriority
		}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return less(out[i], out[j])
	})

	seen := map[string]bool{}
	deduped := make([]source.Candidate, 0, len(out))
	for _, candidate := range out {
		key := normalizeLabel(candidate.Label)
		if key == "" {
			key = candidate.URL
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, candidate)
	}
	return deduped
}

// Standby 从主 RSS 排序结果里挑副 RSS。
//
// 传入的是已经去重、且主 RSS 已取走首项的候选。
//
// 只保留命中过 standby 规则的候选：**没在规则里点过名的组，一律不进副 RSS**。
// 这里故意不做「没命中就按发布条数兜底」——真实数据里那样会被搬运组刷满：
// Kirara Fantasia 2833 条、生肉/不明字幕、20190912 这类实例侧的伪组名，
// 全靠热度挤进副 RSS，副 RSS 也就失去意义了。
//
// 因此「主 RSS 是字幕组、副 RSS 是放流组」只需要给放流组规则写上 scope: standby；
// 想要几条副 RSS，就在规则里点几个组的名，再用 maxStandby 截断。
func (e *Engine) Standby(rest []source.Candidate) []source.Candidate {
	out := make([]source.Candidate, 0, len(rest))
	for _, candidate := range rest {
		// StandbyRuleName 只有命中规则时才会被填上，比拿优先级跟 UnmatchedPriority 比更准
		// （用户完全可以给规则写个 1000 的优先级）。
		if candidate.StandbyRuleName == "" {
			continue
		}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StandbyPriority != out[j].StandbyPriority {
			return out[i].StandbyPriority < out[j].StandbyPriority
		}
		return less(out[i], out[j])
	})
	if len(out) > e.maxStandby {
		out = out[:e.maxStandby]
	}
	return out
}

// less 是同优先级下的兜底排序：热度降序 -> 站点顺序 -> 名称。
func less(a, b source.Candidate) bool {
	if a.Weight != b.Weight {
		return a.Weight > b.Weight
	}
	if a.SiteOrder != b.SiteOrder {
		return a.SiteOrder < b.SiteOrder
	}
	return a.Label < b.Label
}

var labelNoise = regexp.MustCompile(`[\s\-_·・,，。.()（）\[\]【】]+`)

func normalizeLabel(label string) string {
	return strings.ToLower(labelNoise.ReplaceAllString(label, ""))
}
