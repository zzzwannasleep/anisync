package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intPtr(v int) *int { return &v }

func validConfig() *Config {
	cfg := Default()
	cfg.Instances = []Instance{{Name: "主库", BaseURL: "https://ass.example.com/", APIKey: "test-key"}}
	return cfg
}

// 番剧级配置靠 title / bgmId 匹配，两个都空就永远命中不了，属于写错了。
func TestValidateTargetNeedsTitleOrBgmID(t *testing.T) {
	cfg := validConfig()
	cfg.Targets = []Target{{Season: intPtr(2), Offset: intPtr(-12)}}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "targets") {
		t.Fatalf("既没有 title 也没有 bgmId 的番剧配置应校验失败，实际 %v", err)
	}
}

// 和 ASS 一样：季度按番剧给，主副 RSS 的集偏移分开给。
func TestValidateAcceptsTargetWithSeasonAndOffsets(t *testing.T) {
	cfg := validConfig()
	cfg.Targets = []Target{{
		Title:         "冰之城墙",
		Season:        intPtr(2),
		Offset:        intPtr(-12),
		StandbyOffset: intPtr(-12),
	}}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("按番剧规定季度与偏移应校验通过，实际 %v", err)
	}
}

func TestNormalizeTrimsTargetFields(t *testing.T) {
	cfg := validConfig()
	cfg.Targets = []Target{{Title: "  冰之城墙 ", BgmID: " 665390 "}}

	cfg.Normalize()
	if cfg.Targets[0].Title != "冰之城墙" || cfg.Targets[0].BgmID != "665390" {
		t.Fatalf("targets 的 title / bgmId 应去掉首尾空白，实际 %+v", cfg.Targets[0])
	}
}

// 回归：Web 设置页会把配置读出来再存回去。如果 Load 时就把 ${ENV} 展开进内存，
// 一次保存就会把密钥明文写进 config.yaml。
func TestLoadKeepsEnvPlaceholderOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := "listen: 127.0.0.1:51837\n" +
		"instances:\n" +
		"    - name: 主库\n" +
		"      baseUrl: https://ass.example.com/\n" +
		"      apiKey: ${ANISYNC_TEST_KEY}\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANISYNC_TEST_KEY", "secret-value")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Instances[0].APIKey != "${ANISYNC_TEST_KEY}" {
		t.Fatalf("内存里应保留 ${ENV} 原文，实际 %q", cfg.Instances[0].APIKey)
	}
	if got := cfg.Instances[0].Resolve().APIKey; got != "secret-value" {
		t.Fatalf("真正发请求时应展开成密钥，实际 %q", got)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "secret-value") {
		t.Fatalf("保存后密钥被明文写进配置文件:\n%s", out)
	}
	if !strings.Contains(string(out), "${ANISYNC_TEST_KEY}") {
		t.Fatalf("保存后应保留 ${ENV} 占位符:\n%s", out)
	}
}

func TestValidateResolvesEnvPlaceholders(t *testing.T) {
	t.Setenv("ANISYNC_TEST_HOST", "https://ass.example.com/")
	t.Setenv("ANISYNC_TEST_KEY", "secret-value")

	cfg := &Config{Listen: DefaultListen, Instances: []Instance{{
		Name:    "主库",
		BaseURL: "${ANISYNC_TEST_HOST}",
		APIKey:  "${ANISYNC_TEST_KEY}",
	}}}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("baseUrl / apiKey 写成占位符也应校验通过，实际 %v", err)
	}
}

// 加番剧的常规做法是提前把 bgm 链接贴进来，而不是先建订阅再回头引用它。
// 所以 bgmId 与 title 两栏都得能吃链接，用户贴哪儿都能用。
func TestBgmIDFromText(t *testing.T) {
	cases := map[string]string{
		"https://bgm.tv/subject/665390":          "665390",
		"http://bgm.tv/subject/665390/":          "665390",
		"https://bangumi.tv/subject/665390":      "665390",
		"https://chii.in/subject/665390":         "665390",
		"665390":                                 "665390",
		"  665390  ":                             "665390",
		"冰之城墙":                                   "",
		"https://bgm.tv/subject/":                "",
		"https://example.com/subject/665390":     "",
		"":                                       "",
		"https://bgm.tv/subject/665390?from=xyz": "665390",
	}
	for input, want := range cases {
		if got := BgmIDFromText(input); got != want {
			t.Errorf("BgmIDFromText(%q) = %q，期望 %q", input, got, want)
		}
	}
}

func TestNormalizePullsBgmLinkIntoBgmID(t *testing.T) {
	// 链接贴在 bgmId 栏
	cfg := validConfig()
	cfg.Targets = []Target{{BgmID: "https://bgm.tv/subject/665390", Season: intPtr(2)}}
	cfg.Normalize()
	if cfg.Targets[0].BgmID != "665390" {
		t.Fatalf("bgmId 栏里的链接应被解析成 id，实际 %q", cfg.Targets[0].BgmID)
	}

	// 链接贴在 title 栏：解析出 id 后 title 要清空，否则会退化成标题包含匹配
	cfg = validConfig()
	cfg.Targets = []Target{{Title: "https://bgm.tv/subject/665390", Season: intPtr(2)}}
	cfg.Normalize()
	if cfg.Targets[0].BgmID != "665390" || cfg.Targets[0].Title != "" {
		t.Fatalf("title 栏里的链接应被解析成 bgmId 并清空 title，实际 %+v", cfg.Targets[0])
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("只贴了链接的 targets 应能通过校验，实际 %v", err)
	}

	// 普通标题不受影响
	cfg = validConfig()
	cfg.Targets = []Target{{Title: "冰之城墙"}}
	cfg.Normalize()
	if cfg.Targets[0].Title != "冰之城墙" || cfg.Targets[0].BgmID != "" {
		t.Fatalf("普通标题不应被改动，实际 %+v", cfg.Targets[0])
	}
}

// 白名单与黑名单可以放在单独的文件里，主配置只留引用。
func TestRuleFilesAreLoaded(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "standby.yaml"), `
# 白名单：放流组排名
- {name: LoliHouse, match: '(?i)lolihouse', priority: 10, scope: standby}
- {name: Nix-Raws,  match: '(?i)nix-raws',  priority: 20, scope: standby}
`)
	writeFile(t, filepath.Join(dir, "deny.yaml"), `
- {match: '(?i)kirara', why: 搬运刷榜}
`)
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
listen: 127.0.0.1:51837
rules:
  primary:
    - {name: 简日内嵌, match: '(?i)简日', priority: 10, scope: main}
  primaryFiles: [standby.yaml]
  denyFiles: [deny.yaml]
  maxStandby: 1
instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: 'k'}
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if got := len(cfg.AllPrimary()); got != 3 {
		t.Fatalf("生效规则应为 1 条内联 + 2 条外置 = 3，实际 %d", got)
	}
	if got := len(cfg.AllDeny()); got != 1 {
		t.Fatalf("生效黑名单应为 1 条，实际 %d", got)
	}
	if len(cfg.FileRules) != 2 {
		t.Fatalf("应记录 2 个外置文件来源，实际 %+v", cfg.FileRules)
	}
	// 外置规则同样要规整 scope，否则文件里写 standby 会失效
	if got := cfg.AllPrimary()[1].ScopeOr(); got != ScopeStandby {
		t.Fatalf("外置规则的 scope 应被规整为 standby，实际 %q", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("含外置规则的配置应校验通过: %v", err)
	}
}

// 关键回归：保存时只写回内联的那部分，不能把外置内容抄进主配置。
// 抄进去的话下次加载会变成「一份内联 + 一份外置」，规则数翻倍。
func TestSaveDoesNotInlineRuleFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "standby.yaml"), `
- {name: LoliHouse, match: '(?i)lolihouse', priority: 10, scope: standby}
`)
	writeFile(t, filepath.Join(dir, "deny.yaml"), `
- {match: '(?i)kirara', why: 搬运刷榜}
`)
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
listen: 127.0.0.1:51837
rules:
  primaryFiles: [standby.yaml]
  denyFiles: [deny.yaml]
instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: 'k'}
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	out, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读回配置失败: %v", err)
	}
	text := string(out)
	if strings.Contains(text, "lolihouse") || strings.Contains(text, "kirara") {
		t.Fatalf("外置规则被抄进主配置了，下次加载会变成两份:\n%s", text)
	}
	if !strings.Contains(text, "primaryFiles") || !strings.Contains(text, "denyFiles") {
		t.Fatalf("保存后文件引用应该还在:\n%s", text)
	}

	// 再加载一次，规则数不能翻倍
	again, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("二次加载失败: %v", err)
	}
	if got := len(again.AllPrimary()); got != 1 {
		t.Fatalf("二次加载后规则数应仍为 1，实际 %d", got)
	}
	if got := len(again.AllDeny()); got != 1 {
		t.Fatalf("二次加载后黑名单应仍为 1，实际 %d", got)
	}
}

// 文件写错路径要当场报错，不能静默跳过让人以为规则生效了。
func TestMissingRuleFileIsLoudError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
rules:
  primaryFiles: [nope.yaml]
instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: 'k'}
`)
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("规则文件不存在时必须报错")
	} else if !strings.Contains(err.Error(), "nope.yaml") {
		t.Fatalf("报错要点出是哪个文件，实际 %v", err)
	}
}

// 外置文件格式不对（这里是映射而不是列表）也要报错。
func TestBadRuleFileFormatIsLoudError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "deny.yaml"), "rules:\n  - {match: x}\n")
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `
rules:
  denyFiles: [deny.yaml]
instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: 'k'}
`)
	if _, err := Load(cfgPath); err == nil {
		t.Fatal("黑名单文件格式不对时必须报错")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试文件 %s 失败: %v", path, err)
	}
}

// 样例配置的注释就是文档。保存时必须把注释留住，只换值。
func TestSavePreservesComments(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `# AniSync 配置：手写的头部注释
# 第二行也要留住。
listen: 127.0.0.1:51837

rules:
  maxStandby: 3   # 保留几条副 RSS
  primary:
    # ---- 第一段：语言类型 ----
    - name: 简日内嵌
      match: '(?i)简日'
      priority: 10
      scope: main

# 实例：填自己的地址与密钥
instances:
  - name: 主库
    baseUrl: https://ass.example.com/
    apiKey: k
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	// 改两个值：一个标量、一个嵌套在列表里的字段
	cfg.Rules.MaxStandby = 1
	cfg.Rules.Primary[0].Priority = 42
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	out, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	text := string(out)
	for _, want := range []string{
		"# AniSync 配置：手写的头部注释",
		"# 第二行也要留住。",
		"# 保留几条副 RSS",
		"# ---- 第一段：语言类型 ----",
		"# 实例：填自己的地址与密钥",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("注释丢了: %q\n--- 实际内容 ---\n%s", want, text)
		}
	}
	if !strings.Contains(text, "maxStandby: 1") {
		t.Errorf("标量改动没写进去:\n%s", text)
	}
	if !strings.Contains(text, "priority: 42") {
		t.Errorf("列表里的字段改动没写进去:\n%s", text)
	}
	if strings.Contains(text, "maxStandby: 3") {
		t.Errorf("旧值还在:\n%s", text)
	}
}

// 连续保存两次，第二次也要有注释可合并（保存后自己就是新的「原文件」）。
func TestSaveTwiceKeepsComments(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `# 头部注释
listen: 127.0.0.1:51837
# 实例说明
instances:
  - name: 主库
    baseUrl: https://ass.example.com/
    apiKey: k
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	for round := 1; round <= 2; round++ {
		if err := cfg.Save(); err != nil {
			t.Fatalf("第 %d 次保存失败: %v", round, err)
		}
		out, _ := os.ReadFile(cfgPath)
		if !strings.Contains(string(out), "# 头部注释") || !strings.Contains(string(out), "# 实例说明") {
			t.Fatalf("第 %d 次保存后注释丢了:\n%s", round, out)
		}
	}
}

// 列表元素按身份键配对，不按下标：删掉中间一条时，后面的注释不能错位到别的条目上。
func TestSaveKeepsCommentsWhenRuleDeleted(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `rules:
  primary:
    # 这是甲组的注释
    - name: 甲组
      match: a
      priority: 10
    # 这是乙组的注释
    - name: 乙组
      match: b
      priority: 20
    # 这是丙组的注释
    - name: 丙组
      match: c
      priority: 30
instances:
  - name: 主库
    baseUrl: https://ass.example.com/
    apiKey: k
`)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	// 删掉中间的乙组
	cfg.Rules.Primary = []Rule{cfg.Rules.Primary[0], cfg.Rules.Primary[2]}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	out, _ := os.ReadFile(cfgPath)
	text := string(out)
	if strings.Contains(text, "乙组") || strings.Contains(text, "这是乙组的注释") {
		t.Errorf("删掉的规则还在:\n%s", text)
	}
	// 关键：丙组要带着自己的注释，不能继承乙组的
	if !strings.Contains(text, "这是丙组的注释") {
		t.Errorf("丙组的注释丢了:\n%s", text)
	}
	jia := strings.Index(text, "甲组")
	yiComment := strings.Index(text, "这是乙组的注释")
	bing := strings.Index(text, "丙组")
	if jia < 0 || bing < 0 {
		t.Fatalf("剩下的规则不全:\n%s", text)
	}
	if yiComment >= 0 {
		t.Errorf("已删除条目的注释仍留在文件里:\n%s", text)
	}
	// 丙组的注释必须在丙组之前，而不是跑到甲组头上
	if idx := strings.Index(text, "这是丙组的注释"); idx > bing {
		t.Errorf("丙组的注释位置不对:\n%s", text)
	}
}

// Web 设置页保存走的是「全新 Config + ReloadLayout」，注释同样要留住。
func TestReloadLayoutKeepsCommentsForFreshConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `# 设置页保存也不该冲掉我
listen: 127.0.0.1:51837
rules:
  maxStandby: 3
  primary:
    # 规则段说明
    - name: 简日
      match: '(?i)简日'
      priority: 10
instances:
  - name: 主库
    baseUrl: https://ass.example.com/
    apiKey: k
`)
	loaded, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}

	// 模拟 PUT：一份全新对象，只带 SetPath + ReloadLayout
	fresh := &Config{Listen: loaded.Listen, Rules: loaded.Rules, Instances: loaded.Instances}
	fresh.SetPath(cfgPath)
	if err := fresh.ReloadLayout(); err != nil {
		t.Fatalf("ReloadLayout 失败: %v", err)
	}
	fresh.Rules.MaxStandby = 2
	if err := fresh.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	out, _ := os.ReadFile(cfgPath)
	text := string(out)
	if !strings.Contains(text, "# 设置页保存也不该冲掉我") || !strings.Contains(text, "# 规则段说明") {
		t.Fatalf("全新 Config 走 ReloadLayout 后注释还是丢了:\n%s", text)
	}
	if !strings.Contains(text, "maxStandby: 2") {
		t.Fatalf("改动没写进去:\n%s", text)
	}
}

// 首次生成（磁盘上还没有文件）时补默认头部，且不重复。
func TestSaveAddsHeaderOnlyWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := Default()
	cfg.SetPath(cfgPath)
	cfg.Instances = []Instance{{Name: "主库", BaseURL: "https://ass.example.com/", APIKey: "k"}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	out, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(out), "AniSync 配置") {
		t.Fatalf("首次生成应有默认头部:\n%s", out)
	}
	// 再存一次，头部不能变成两遍
	if err := cfg.Save(); err != nil {
		t.Fatalf("第二次保存失败: %v", err)
	}
	out2, _ := os.ReadFile(cfgPath)
	if n := strings.Count(string(out2), "AniSync 配置"); n != 1 {
		t.Fatalf("默认头部重复了 %d 次:\n%s", n, out2)
	}
}

// 文件最上面那块「和下面的键之间隔了空行」的头部注释，yaml.v3 会挂在文档节点上，
// 不在任何键上。这一块恰恰是样例说明自己怎么用的地方，丢了最心疼。
func TestSaveKeepsHeaderBlockSeparatedByBlankLine(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `# 样例 01：中文优先
#
# 怎么用：
#   1. 复制成 config.yaml
#   2. 改 instances

# 这一块是 listen 的注释
listen: 127.0.0.1:51837
instances:
  - name: 主库
    baseUrl: https://ass.example.com/
    apiKey: k
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	out, _ := os.ReadFile(cfgPath)
	text := string(out)
	for _, want := range []string{"# 样例 01：中文优先", "# 怎么用：", "#   2. 改 instances", "# 这一块是 listen 的注释"} {
		if !strings.Contains(text, want) {
			t.Errorf("头部注释丢了: %q\n--- 实际内容 ---\n%s", want, text)
		}
	}
}

// 样例里的规则写成一行 `{name: x, match: y}`。保存时不能把它展开成五行，
// 否则 23 条规则的文件会立刻膨胀一倍，读起来也散架。
func TestSaveKeepsFlowStyle(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `rules:
  primary:
    - {name: 简日内嵌, match: '(?i)简日', priority: 10, scope: main}
    - {name: 桜都字幕组, match: '(?i)桜都', priority: 40, scope: main}
  deny:
    - {match: '合集', why: 不要合集}
instances:
  - {name: 主库, baseUrl: 'https://ass.example.com/', apiKey: k}
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	out, _ := os.ReadFile(cfgPath)
	text := string(out)
	if !strings.Contains(text, "{name: 简日内嵌, match: '(?i)简日', priority: 10, scope: main}") {
		t.Errorf("规则的一行写法被展开了:\n%s", text)
	}
	if !strings.Contains(text, "{name: 主库, baseUrl: 'https://ass.example.com/', apiKey: k}") {
		t.Errorf("实例的一行写法被展开了:\n%s", text)
	}
	// 被展开的话会出现单独的 `- name: xxx` 行（原文件里没有这种写法）
	for _, expanded := range []string{"- name: 简日内嵌", "- name: 主库"} {
		if strings.Contains(text, expanded) {
			t.Errorf("一行写法被展开成多行了: %q\n%s", expanded, text)
		}
	}
	// 存出来的东西必须还能读回来，而且规则一条不少
	again, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("保存后的文件读不回来了: %v\n%s", err, text)
	}
	if len(again.Rules.Primary) != 2 || len(again.Rules.Deny) != 1 {
		t.Fatalf("读回来的规则数不对: primary=%d deny=%d", len(again.Rules.Primary), len(again.Rules.Deny))
	}
	if again.Rules.Primary[0].Name != "简日内嵌" || again.Rules.Primary[0].Match != `(?i)简日` {
		t.Fatalf("读回来的规则内容不对: %+v", again.Rules.Primary[0])
	}
}

// 空值键（targets: [] / instance: ""）不能被 omitempty 吞掉：
// 键一没，挂在它上面的说明注释也跟着消失，而那段注释正是教用户怎么写的。
func TestSaveKeepsEmptyButDocumentedKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	writeFile(t, cfgPath, `sources:
  # 留空 = 第一个启用的实例
  instance: ""
  order: [anibt]
# 按番剧规定季度与偏移：
# 一般只写季度，集偏移得看是哪个组才写。
targets: []
#  - bgmId: "https://bgm.tv/subject/665390"
#    season: 2
instances:
  - name: 主库
    baseUrl: https://ass.example.com/
    apiKey: k
`)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	out, _ := os.ReadFile(cfgPath)
	text := string(out)
	if !strings.Contains(text, "instance:") {
		t.Errorf("空值的 instance 键被吞掉了:\n%s", text)
	}
	if !strings.Contains(text, "# 留空 = 第一个启用的实例") {
		t.Errorf("instance 的注释丢了:\n%s", text)
	}
	if !strings.Contains(text, "targets:") {
		t.Errorf("空的 targets 键被吞掉了:\n%s", text)
	}
	for _, want := range []string{"# 按番剧规定季度与偏移：", "# 一般只写季度，集偏移得看是哪个组才写。", `#  - bgmId: "https://bgm.tv/subject/665390"`} {
		if !strings.Contains(text, want) {
			t.Errorf("targets 的说明注释丢了: %q\n--- 实际内容 ---\n%s", want, text)
		}
	}
}
