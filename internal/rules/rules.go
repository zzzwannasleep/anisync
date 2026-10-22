// Package rules 实现「正则 + 优先级」的字幕组/放流组打分。
//
// 语义：
//   - deny 命中 -> 直接剔除；
//   - primary 规则按配置顺序匹配，第一条命中的决定优先级（数字越小越优先）；
//   - 没命中任何规则的候选排在所有命中规则的候选之后，同优先级时按站点优先级、再按组名排序。
package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/zzzwannasleep/assm/internal/source"
)

// UnmatchedPriority 是未命中规则候选的基准优先级。
const UnmatchedPriority = 1000

type compiledRule struct {
	name     string
	re       *regexp.Regexp
	priority int
	site     string
}

// Engine 是编译好的规则引擎。
type Engine struct {
	primary    []compiledRule
	deny       []compiledRule
	maxStandby int
}

// Rule 是输入规则（与 config.Rule 对齐，避免循环依赖）。
type Rule struct {
	Name     string
	Match    string
	Priority int
	Site     string
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
		engine.maxStandby = 3
	}
	for _, rule := range primary {
		if strings.TrimSpace(rule.Match) == "" {
			continue
		}
		re, err := regexp.Compile(rule.Match)
		if err != nil {
			return nil, fmt.Errorf("规则 %q 正则不合法: %w", rule.Name, err)
		}
		engine.primary = append(engine.primary, compiledRule{
			name:     rule.Name,
			re:       re,
			priority: rule.Priority,
			site:     strings.ToLower(strings.TrimSpace(rule.Site)),
		})
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

// Match 返回命中的规则名与优先级；未命中时 ok=false。
func (e *Engine) Match(candidate source.Candidate) (name string, priority int, ok bool) {
	text := candidate.Label
	if text == "" {
		text = candidate.Subgroup
	}
	for _, rule := range e.primary {
		if rule.site != "" && !strings.EqualFold(rule.site, candidate.Site) {
			continue
		}
		if rule.re.MatchString(text) {
			return rule.name, rule.priority, true
		}
	}
	return "", UnmatchedPriority, false
}

// Denied 判断是否被排除。
func (e *Engine) Denied(candidate source.Candidate) (bool, string) {
	text := candidate.Label + " " + candidate.Subgroup + " " + candidate.URL
	for _, rule := range e.deny {
		if rule.re.MatchString(text) {
			return true, rule.name
		}
	}
	return false, ""
}

// Rank 过滤 + 排序 + 回填优先级，返回排好序的候选。
// 排序键：规则优先级 -> 站点热度（发布/资源数，降序）-> 站点顺序 -> 组名。
// 排序后按「组名」去重：同一个字幕组在多个站点的镜像只保留最优的那条，
// 这样副 RSS 列表里是不同字幕组，而不是同一个组的多个镜像。
func (e *Engine) Rank(candidates []source.Candidate) []source.Candidate {
	out := make([]source.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if denied, _ := e.Denied(candidate); denied {
			continue
		}
		name, priority, ok := e.Match(candidate)
		candidate.Priority = priority
		if ok {
			candidate.RuleName = name
		}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		if out[i].SiteOrder != out[j].SiteOrder {
			return out[i].SiteOrder < out[j].SiteOrder
		}
		return out[i].Label < out[j].Label
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

var labelNoise = regexp.MustCompile(`[\s\-_·・,，。.()（）\[\]【】]+`)

func normalizeLabel(label string) string {
	return strings.ToLower(labelNoise.ReplaceAllString(label, ""))
}
