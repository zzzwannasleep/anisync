package bridge

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zzzwannasleep/anisync/internal/config"
)

// ---------- 7.5 profile ----------

// profileUpsertPayload 是 profile.upsert 的 payload。
type profileUpsertPayload struct {
	Profile *config.Profile `json:"profile"`
}

func opProfileUpsert(c *Call) (any, error) {
	var payload profileUpsertPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if payload.Profile == nil {
		return nil, BadRequest("payload.profile 不能为空")
	}
	name := strings.TrimSpace(payload.Profile.Name)
	if name == "" {
		return nil, BadRequest("方案的 name 不能为空（实例要用它来挂载）")
	}

	next, err := c.mutateConfig(func(next *config.Config) error {
		incoming := *payload.Profile
		incoming.Name = name
		for i := range next.Profiles {
			if next.Profiles[i].Name == name {
				next.Profiles[i] = incoming
				return nil
			}
		}
		next.Profiles = append(next.Profiles, incoming)
		return nil
	})
	if err != nil {
		return nil, err
	}
	c.Log("info", "方案已保存："+name)
	return map[string]any{"config": next, "views": viewsPayload(next)}, nil
}

// profileDeletePayload 是 profile.delete 的 payload。
type profileDeletePayload struct {
	Name string `json:"name"`
}

func opProfileDelete(c *Call) (any, error) {
	var payload profileDeletePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, BadRequest("payload.name 不能为空")
	}

	type affected struct {
		Instance string `json:"instance"`
	}
	var affectedList []affected
	next, err := c.mutateConfig(func(next *config.Config) error {
		found := false
		kept := next.Profiles[:0]
		for _, profile := range next.Profiles {
			if profile.Name == name {
				found = true
				continue
			}
			kept = append(kept, profile)
		}
		next.Profiles = kept
		if !found {
			return NotFoundNamed(name, "没有这个方案: %s", name)
		}
		// 删掉方案之后必须把挂载点也清干净，否则下一次 Validate 会当场拦下来
		// ——「实例挂的方案不存在」。清干净并报出来，是这个 op 的语义：
		// 用户要看到「谁被影响了」，而不是删完收到一条看不懂的校验错误。
		for i := range next.Instances {
			if next.Instances[i].Profile == name {
				next.Instances[i].Profile = ""
				affectedList = append(affectedList, affected{Instance: next.Instances[i].Name})
			}
		}
		for i := range next.Targets {
			if next.Targets[i].Profile == name {
				next.Targets[i].Profile = ""
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if affectedList == nil {
		affectedList = []affected{}
	}
	c.Log("info", "方案已删除："+name)
	return map[string]any{"config": next, "views": viewsPayload(next), "affected": affectedList}, nil
}

// viewsPayload 是 config 派生表的统一形状。
func viewsPayload(cfg *config.Config) map[string]any {
	return map[string]any{
		"instances": instanceViews(cfg),
		"profiles":  profileViews(cfg),
	}
}

// profileRuleFilePayload 是 profile.ruleFile 的 payload。
//
// 字段名是 path 不是 file（契约 §7.5）：它和 RuleFileInfo 的 path 是同一个东西，
// 两个名字会让「配置里引用的那份」与「界面要读的那份」对不上。
type profileRuleFilePayload struct {
	Owner string `json:"owner"`
	Path  string `json:"path"`
}

// opProfileRuleFile 读一个外置规则文件的内容。
//
// **只读**：外置规则文件的内容仍然只能在文件里改（这是现有承诺，不许在 GUI 里破例）。
// 查看是刚需，编辑不是。
func opProfileRuleFile(c *Call) (any, error) {
	var payload profileRuleFilePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	owner := strings.TrimSpace(payload.Owner)
	file := strings.TrimSpace(payload.Path)
	if file == "" {
		return nil, BadRequest("payload.path 不能为空")
	}

	rules := cfg.Rules.PrimaryFiles
	denyRules := cfg.Rules.DenyFiles
	if owner != "" {
		profile, ok := cfg.ProfileByName(owner)
		if !ok {
			return nil, NotFoundNamed(owner, "没有这个方案: %s", owner)
		}
		rules = profile.Rules.PrimaryFiles
		denyRules = profile.Rules.DenyFiles
	}

	kind := "primary"
	for _, candidate := range denyRules {
		if sameFile(candidate, file) {
			kind = "deny"
		}
	}
	if kind == "primary" {
		matched := false
		for _, candidate := range rules {
			if sameFile(candidate, file) {
				matched = true
			}
		}
		if !matched {
			return nil, NotFoundNamed(file, "%s 没有被%s引用", file, ownerLabel(owner))
		}
	}

	resolved := resolveRuleFilePath(cfg, file)
	raw, readErr := os.ReadFile(resolved)
	if readErr != nil {
		// 「配置里引用了、但磁盘上没有」也是资源不存在：detail 用调用方给的那个路径
		// （逐字），message 里保留解析后的绝对路径供人去找 —— 两者各司其职。
		return nil, withResourceDetail(readErr, file)
	}

	// 响应字段集合与契约逐字相同：{owner, path, kind, rules, denyRules}。
	// 「有几条」由 len(rules) 回答，不再另给计数 —— 多一个字段就是多一份要维护的真相。
	data := map[string]any{
		"owner":     owner,
		"path":      file,
		"kind":      kind,
		"rules":     []map[string]any{},
		"denyRules": []map[string]any{},
	}
	if kind == "deny" {
		var list []config.DenyRule
		if err := yaml.Unmarshal(raw, &list); err != nil {
			return nil, InvalidConfig(err)
		}
		items := make([]map[string]any, 0, len(list))
		for _, rule := range list {
			items = append(items, map[string]any{"match": rule.Match, "why": rule.Why})
		}
		data["denyRules"] = items
		return data, nil
	}

	var list []config.Rule
	if err := yaml.Unmarshal(raw, &list); err != nil {
		return nil, InvalidConfig(err)
	}
	items := make([]map[string]any, 0, len(list))
	for _, rule := range list {
		items = append(items, map[string]any{
			"name":     rule.Name,
			"match":    rule.Match,
			"priority": rule.Priority,
			"scope":    rule.ScopeOr(),
			"in":       rule.InOr(),
		})
	}
	data["rules"] = items
	return data, nil
}

// sameFile 比较两个规则文件引用是不是同一个文件。
//
// 配置里写的是相对路径（raws.yaml），而调用方可能拿的是 RuleFileInfo 里的绝对路径，
// 所以两边都取 basename 比一次，再按原样比一次。
func sameFile(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.EqualFold(filepath.Base(a), filepath.Base(b))
}

// resolveRuleFilePath 把规则文件引用按主配置所在目录展开。
func resolveRuleFilePath(cfg *config.Config, file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(filepath.Dir(cfg.Path()), file)
}

// ownerLabel 让报错读起来像人话。
func ownerLabel(owner string) string {
	if owner == "" {
		return "顶层 rules"
	}
	return "方案 " + owner
}
