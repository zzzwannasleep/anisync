package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig 在临时目录里落一份配置，返回路径。
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写临时配置失败: %v", err)
	}
	return path
}

// writeRuleFile 在配置旁边落一份外置规则文件。
func writeRuleFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatalf("写规则文件 %s 失败: %v", name, err)
	}
}

// TestSchemeForUsesInstanceProfile 是这次改动最核心的那条：
// 两个实例挂不同方案，就必须各自拿到不同的规则、不同的站点顺序、不同的副 RSS 上限。
func TestSchemeForUsesInstanceProfile(t *testing.T) {
	path := writeConfig(t, `
rules:
  primary:
    - {name: 顶层规则, match: '^top$', priority: 10}
  maxStandby: 3
sources:
  order: [anibt, nyaa]
profiles:
  - name: 追番
    rules:
      primary:
        - {name: 只要中文字幕组, match: '(?i)lolihouse', priority: 5, in: group}
      maxStandby: 1
    sources:
      order: [anibt]
  - name: 收藏
    rules:
      primary:
        - {name: 全收, match: '.+', priority: 900}
      maxStandby: 12
    sources:
      order: [anibt, animegarden, mikan, nyaa]
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
    profile: 追番
  - name: 收藏库
    baseUrl: https://b.example.com/
    apiKey: k2
    profile: 收藏
  - name: 裸库
    baseUrl: https://c.example.com/
    apiKey: k3
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	chase := cfg.SchemeFor("主库")
	if chase.Name != "追番" {
		t.Fatalf("主库的方案名应该是 追番，实际 %q", chase.Name)
	}
	if len(chase.Primary) != 1 || chase.Primary[0].Match != `(?i)lolihouse` {
		t.Fatalf("主库应该只拿到方案里那一条规则，实际 %+v", chase.Primary)
	}
	if chase.MaxStandby != 1 {
		t.Fatalf("主库副 RSS 上限应该是方案里的 1，实际 %d", chase.MaxStandby)
	}
	if strings.Join(chase.SourceOrder, ">") != "anibt" {
		t.Fatalf("主库站点顺序应该是方案里的 anibt，实际 %v", chase.SourceOrder)
	}

	collect := cfg.SchemeFor("收藏库")
	if collect.MaxStandby != 12 {
		t.Fatalf("收藏库副 RSS 上限应该是 12，实际 %d", collect.MaxStandby)
	}
	if strings.Join(collect.SourceOrder, ">") != "anibt>animegarden>mikan>nyaa" {
		t.Fatalf("收藏库站点顺序不对: %v", collect.SourceOrder)
	}

	// 两个实例的方案不能互相污染：这是分方案唯一的意义。
	if chase.Primary[0].Match == collect.Primary[0].Match {
		t.Fatalf("两个方案拿到了同一条规则，方案没生效")
	}
}

// TestSchemeForFallsBackToTopLevelRules 保证「不写 profile」= 与改动之前完全一致。
func TestSchemeForFallsBackToTopLevelRules(t *testing.T) {
	path := writeConfig(t, `
rules:
  primary:
    - {name: 顶层规则, match: '^top$', priority: 10}
  deny:
    - {match: 合集, why: 不要合集}
  maxStandby: 3
sources:
  order: [anibt, nyaa]
profiles:
  - name: 追番
    rules:
      primary:
        - {name: 方案规则, match: '(?i)lolihouse', priority: 5}
instances:
  - name: 裸库
    baseUrl: https://c.example.com/
    apiKey: k3
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	scheme := cfg.SchemeFor("裸库")
	if scheme.Name != "" {
		t.Fatalf("不写 profile 的实例方案名应为空，实际 %q", scheme.Name)
	}
	if len(scheme.Primary) != 1 || scheme.Primary[0].Name != "顶层规则" {
		t.Fatalf("应该拿到顶层规则，实际 %+v", scheme.Primary)
	}
	if len(scheme.Deny) != 1 {
		t.Fatalf("应该拿到顶层黑名单，实际 %+v", scheme.Deny)
	}
	if scheme.MaxStandby != 3 {
		t.Fatalf("副 RSS 上限应该继承顶层 3，实际 %d", scheme.MaxStandby)
	}
	if strings.Join(scheme.SourceOrder, ">") != "anibt>nyaa" {
		t.Fatalf("站点顺序应该继承顶层，实际 %v", scheme.SourceOrder)
	}
}

// TestSchemeInheritsUnsetFields 证明方案只需要写「和默认不一样的地方」。
func TestSchemeInheritsUnsetFields(t *testing.T) {
	path := writeConfig(t, `
rules:
  primary:
    - {name: 顶层规则, match: '^top$', priority: 10}
  maxStandby: 7
sources:
  order: [anibt, nyaa]
profiles:
  - name: 只写规则
    rules:
      primary:
        - {name: 方案规则, match: '(?i)nyamoe', priority: 5}
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
    profile: 只写规则
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	scheme := cfg.SchemeFor("主库")
	if scheme.MaxStandby != 7 {
		t.Fatalf("方案没写 maxStandby 时应该继承顶层 7，实际 %d", scheme.MaxStandby)
	}
	if strings.Join(scheme.SourceOrder, ">") != "anibt>nyaa" {
		t.Fatalf("方案没写 sources 时应该继承顶层顺序，实际 %v", scheme.SourceOrder)
	}
	// 顶层规则不该被带进方案：方案是一套完整的规则表，不是「在顶层之上加几条」。
	if len(scheme.Primary) != 1 {
		t.Fatalf("方案只应该有自己的 1 条规则，实际 %d 条：%+v", len(scheme.Primary), scheme.Primary)
	}
}

// TestProfileRuleFilesAreLoadedSeparately 证明两套方案能各引各的规则文件，且互不串味。
func TestProfileRuleFilesAreLoadedSeparately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeRuleFile(t, dir, "chase.yaml", `
- {name: 追番白名单, match: '(?i)lolihouse', priority: 10, in: group}
`)
	writeRuleFile(t, dir, "collect.yaml", `
- {name: 收藏白名单, match: '.+', priority: 900, scope: both}
`)
	writeRuleFile(t, dir, "deny.yaml", `
- {match: '合集|全集', why: 不要合集}
`)
	body := `
rules:
  primaryFiles: [chase.yaml]
  denyFiles: [deny.yaml]
  maxStandby: 1
profiles:
  - name: 收藏
    rules:
      primaryFiles: [collect.yaml]
      denyFiles: [deny.yaml]
      maxStandby: 12
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
  - name: 收藏库
    baseUrl: https://b.example.com/
    apiKey: k2
    profile: 收藏
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	def := cfg.SchemeFor("主库")
	if len(def.Primary) != 1 || def.Primary[0].Name != "追番白名单" {
		t.Fatalf("默认方案应该只加载 chase.yaml，实际 %+v", def.Primary)
	}
	if len(def.Deny) != 1 {
		t.Fatalf("默认方案黑名单应该加载到 1 条，实际 %d 条", len(def.Deny))
	}
	// 外置文件不应该被抄进内联列表：抄进去之后下次保存就会变成两份。
	if len(def.Primary) != 1 || len(cfg.Rules.Primary) != 0 {
		t.Fatalf("外置规则不该被内联，内联 %d 条、生效 %d 条", len(cfg.Rules.Primary), len(def.Primary))
	}

	col := cfg.SchemeFor("收藏库")
	if len(col.Primary) != 1 || col.Primary[0].Name != "收藏白名单" {
		t.Fatalf("收藏方案应该只加载 collect.yaml，实际 %+v", col.Primary)
	}
	if len(col.RuleFiles) != 2 {
		t.Fatalf("收藏方案应该报告 2 个外置文件（1 白 1 黑），实际 %d 个：%+v", len(col.RuleFiles), col.RuleFiles)
	}
}

// TestMissingProfileRuleFileIsLoudError 证明方案里的规则文件读不出来会当场报错。
//
// 这条很重要：静默跳过会让人以为方案生效了，其实一条规则都没加载，
// 于是「所有候选都未命中」——看起来像规则写错了，其实是文件没读到。
func TestMissingProfileRuleFileIsLoudError(t *testing.T) {
	path := writeConfig(t, `
profiles:
  - name: 收藏
    rules:
      primaryFiles: [no-such-file.yaml]
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
    profile: 收藏
`)
	_, err := Load(path)
	if err == nil {
		t.Fatalf("方案引用了不存在的规则文件，应该报错")
	}
	// 报错要说清是哪个方案引用的：现在有多套方案，只报文件名分不清是谁。
	if !strings.Contains(err.Error(), "收藏") {
		t.Fatalf("报错里应该点出是哪个方案，实际: %v", err)
	}
}

// TestValidateRejectsUnknownInstanceProfile 证明挂错方案名会被当场拦下。
func TestValidateRejectsUnknownInstanceProfile(t *testing.T) {
	cfg := Default()
	cfg.Instances = []Instance{{Name: "主库", BaseURL: "https://a.example.com/", APIKey: "k"}}
	cfg.Profiles = []Profile{{Name: "追番"}}
	cfg.Instances[0].Profile = "收藏"
	err := cfg.Validate()
	if err == nil {
		t.Fatalf("实例挂了不存在的方案，应该报错")
	}
	// 报错要给出可选值，否则用户只能去翻文件。
	if !strings.Contains(err.Error(), "追番") {
		t.Fatalf("报错里应该列出可选方案名，实际: %v", err)
	}
}

// TestValidateRejectsBadProfileNames 方案名是挂载点，空的和重的都不行。
func TestValidateRejectsBadProfileNames(t *testing.T) {
	base := func() *Config {
		cfg := Default()
		cfg.Instances = []Instance{{Name: "主库", BaseURL: "https://a.example.com/", APIKey: "k"}}
		return cfg
	}

	empty := base()
	empty.Profiles = []Profile{{Name: "  "}}
	empty.Normalize()
	if err := empty.Validate(); err == nil {
		t.Fatalf("空方案名应该报错")
	}

	dup := base()
	dup.Profiles = []Profile{{Name: "追番"}, {Name: "追番"}}
	if err := dup.Validate(); err == nil {
		t.Fatalf("重复方案名应该报错")
	}
}

// TestValidateRejectsUnknownSiteName 是一条「静默失效」的补丁。
//
// source.NewRegistry 对认不出的站点名是丢弃；改站点顺序时打错一个字母，
// 现象是「这个站点就是不返回候选」，看上去像上游挂了。所以在校验阶段拦。
func TestValidateRejectsUnknownSiteName(t *testing.T) {
	cases := map[string]func(*Config){
		"顶层打错": func(cfg *Config) { cfg.Sources.Order = []string{"anibt", "nyaa2"} },
		"方案里打错": func(cfg *Config) {
			cfg.Profiles = []Profile{{Name: "追番", Sources: &ProfileSources{Order: []string{"animegardenn"}}}}
			cfg.Instances[0].Profile = "追番"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			cfg.Instances = []Instance{{Name: "主库", BaseURL: "https://a.example.com/", APIKey: "k"}}
			mutate(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("认不出的站点名应该报错")
			}
			if !strings.Contains(err.Error(), "anibt") {
				t.Fatalf("报错里应该列出合法站点，实际: %v", err)
			}
		})
	}
}

// TestSaveRoundTripsProfiles 证明设置页保存那条路径不会把方案写丢。
//
// Web 设置页的做法是「构造一份全新的 Config 再 Save」，所以这里模拟同一条路径：
// 保存后重新加载，方案、实例挂载、方案自己的站点顺序都必须还在。
func TestSaveRoundTripsProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeRuleFile(t, dir, "collect.yaml", `
- {name: 收藏白名单, match: '.+', priority: 900, scope: both}
`)
	body := `
rules:
  primary:
    - {name: 顶层规则, match: '^top$', priority: 10}
  maxStandby: 2
sources:
  order: [anibt, nyaa]
profiles:
  - name: 收藏
    rules:
      primaryFiles: [collect.yaml]
      maxStandby: 12
    sources:
      order: [anibt, animegarden, mikan, nyaa]
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
  - name: 收藏库
    baseUrl: https://b.example.com/
    apiKey: k2
    profile: 收藏
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读保存后的配置失败: %v", err)
	}
	text := string(saved)
	if !strings.Contains(text, "profiles:") {
		t.Fatalf("保存后的配置里没有 profiles 段:\n%s", text)
	}
	// 外置规则文件不能被抄进主配置：抄进去之后下次加载就变成两份。
	if strings.Contains(text, "收藏白名单") {
		t.Fatalf("方案引用的外置规则被内联进了主配置:\n%s", text)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	scheme := again.SchemeFor("收藏库")
	if scheme.Name != "收藏" {
		t.Fatalf("重新加载后方案名丢了，实际 %q", scheme.Name)
	}
	if scheme.MaxStandby != 12 {
		t.Fatalf("重新加载后副 RSS 上限丢了，实际 %d", scheme.MaxStandby)
	}
	if len(scheme.SourceOrder) != 4 {
		t.Fatalf("重新加载后方案自己的站点顺序丢了，实际 %v", scheme.SourceOrder)
	}
	if len(scheme.Primary) != 1 || scheme.Primary[0].Name != "收藏白名单" {
		t.Fatalf("重新加载后方案的规则文件没重新读上，实际 %+v", scheme.Primary)
	}
}

// TestSaveAddsProfilesKeyToOldFile 守的是升级路径：老配置里根本没有 profiles 这个键。
//
// Save 走的是「新值合并到上次读到的节点树」（mergeNode），注释与键序才保得住。
// 那么一个**原先不存在**的顶层键能不能被合并进去？如果 mergeNode 只更新已有键，
// 用户在设置页加了方案、保存成功、文件里却什么都没有 —— 而且没有任何报错。
// 这是分方案功能对老用户唯一真正要紧的一条路径。
func TestSaveAddsProfilesKeyToOldFile(t *testing.T) {
	path := writeConfig(t, `
# 老配置：只有一层顶层规则，没有 profiles
listen: 127.0.0.1:51837
rules:
  primary:
    - {name: 顶层规则, match: '^top$', priority: 10}
  maxStandby: 2
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载老配置失败: %v", err)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("前置条件：老配置不该有方案")
	}

	// 模拟设置页：加一套方案并挂到实例上，然后保存。
	cfg.Profiles = []Profile{{
		Name: "追番",
		Rules: Rules{
			Primary:    []Rule{{Name: "只要 LoliHouse", Match: `(?i)lolihouse`, Priority: 1, In: InGroup}},
			MaxStandby: 1,
		},
		Sources: &ProfileSources{Order: []string{"anibt"}},
	}}
	cfg.Instances[0].Profile = "追番"
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读保存后的文件失败: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "profiles:") {
		t.Fatalf("保存后文件里没有 profiles 键（顶层新键被合并逻辑吞掉了）:\n%s", text)
	}
	// 注释得留住：注释就是这个项目的文档。
	if !strings.Contains(text, "# 老配置：只有一层顶层规则，没有 profiles") {
		t.Fatalf("保存后原有的注释丢了:\n%s", text)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("重新加载失败: %v", err)
	}
	scheme := again.SchemeFor("主库")
	if scheme.Name != "追番" {
		t.Fatalf("重新加载后实例没挂上方案，实际 %q", scheme.Name)
	}
	if len(scheme.SourceOrder) != 1 || scheme.SourceOrder[0] != "anibt" {
		t.Fatalf("重新加载后方案自己的站点顺序丢了，实际 %v", scheme.SourceOrder)
	}
	if err := again.Validate(); err != nil {
		t.Fatalf("保存后的配置应该能通过校验: %v", err)
	}
}

// TestSaveDropsRemovedProfiles 是对称的一条：把方案删干净，文件里也不该留残渣。
func TestSaveDropsRemovedProfiles(t *testing.T) {
	path := writeConfig(t, `
rules:
  primary:
    - {name: 顶层规则, match: '^top$', priority: 10}
profiles:
  - name: 追番
    rules:
      primary:
        - {name: 方案规则, match: '(?i)lolihouse', priority: 1}
instances:
  - name: 主库
    baseUrl: https://a.example.com/
    apiKey: k1
    profile: 追番
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	cfg.Profiles = nil
	cfg.Instances[0].Profile = ""
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件失败: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, "追番") {
		t.Fatalf("方案删掉之后文件里还留着它的痕迹:\n%s", text)
	}
	if strings.Contains(text, "profile:") {
		t.Fatalf("实例上的 profile 清空之后不该再落盘:\n%s", text)
	}
}

func TestNormalizeTrimsProfileNameAndDropsEmptySources(t *testing.T) {
	cfg := Default()
	cfg.Profiles = []Profile{
		{Name: "  追番  ", Sources: &ProfileSources{Order: []string{"  ", "", " Nyaa "}}},
		{Name: "空的", Sources: &ProfileSources{Order: []string{" ", ""}}},
	}
	cfg.Normalize()

	if cfg.Profiles[0].Name != "追番" {
		t.Fatalf("方案名应该去掉首尾空白，实际 %q", cfg.Profiles[0].Name)
	}
	if got := cfg.Profiles[0].Sources; got == nil || len(got.Order) != 1 || got.Order[0] != "nyaa" {
		t.Fatalf("站点名应该小写、去空白项后保留 nyaa，实际 %+v", got)
	}
	// 一项都不剩的 sources 整段丢掉：留着一个空对象，保存后的配置里会多出
	// 一段什么也没说的 `sources: {}`。
	if got := cfg.Profiles[1].Sources; got != nil {
		t.Fatalf("清成空列表的 sources 应该被整段丢掉，实际 %+v", got)
	}

	// 内部带空格的站点名（"ani bt"）不会被悄悄修好 —— 那是拼错，交给 Validate 报错。
	bad := Default()
	bad.Profiles = []Profile{{Name: "拼错", Sources: &ProfileSources{Order: []string{"ani bt"}}}}
	bad.Instances = []Instance{{Name: "主库", BaseURL: "https://a.example.com/", APIKey: "k", Profile: "拼错"}}
	bad.Normalize()
	if err := bad.Validate(); err == nil {
		t.Fatalf("站点名拼错（含空格）应该在校验阶段报错，而不是被当成合法站点传下去")
	}
}
