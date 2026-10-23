// Package config 负责 AniSync 的配置读写。
// 配置文件是唯一的真相来源：CLI、托盘、Web 设置页读写同一份 YAML。
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zzzwannasleep/anisync/internal/workdir"
)

// 规则作用域：决定这条规则参与挑选主 RSS 还是副 RSS。
const (
	ScopeMain    = "main"
	ScopeStandby = "standby"
	ScopeBoth    = "both"
)

// 规则匹配位置：决定 match 正则作用在哪个字符串上。
//
// 分轴的理由和 ANI-RSS 一样 —— 组名是一个身份，字幕类型才是标题里的文字。
// 两轴混在一起时，短组名会被作品名误伤（Ani ni Tsukeru Kusuri wa Nai 里的 Ani），
// 只能靠 ^ani\b|\[ani\] 这种位置锚定去绕。
const (
	// InAny 是默认值，与分轴之前的行为逐字一致，老配置不受影响。
	InAny = "any"
	// InGroup 只看组名：显示名 + 种子标题里印出来的别名。
	InGroup = "group"
	// InTitle 只看资源标题：简日 / 繁日 / 内嵌 / 内封 / 1080p 这类版本信息。
	InTitle = "title"
)

// DefaultListen 是本地设置页的默认监听地址。
// 用五位数高位端口（49152–65535），避开常见服务端口，也不撞系统动态端口分配区。
const DefaultListen = "127.0.0.1:51837"

// Instance 是一个 ANI-RSS 实例。
type Instance struct {
	Name    string `yaml:"name" json:"name"`
	BaseURL string `yaml:"baseUrl" json:"baseUrl"`
	APIKey  string `yaml:"apiKey" json:"apiKey"`
	Enabled *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// IsEnabled 默认启用。
func (i Instance) IsEnabled() bool { return i.Enabled == nil || *i.Enabled }

// Rule 是一条匹配规则：正则 + 优先级（数字越小越优先）。
//
// Match 默认同时作用于字幕组名与最近的资源标题；想只排其中一侧就写 In。
type Rule struct {
	Name     string `yaml:"name" json:"name"`
	Match    string `yaml:"match" json:"match"`
	Priority int    `yaml:"priority" json:"priority"`
	// Site 限定只在该来源生效，空表示不限。
	Site string `yaml:"site,omitempty" json:"site,omitempty"`
	// Scope 决定这条规则给主 RSS 排序还是给副 RSS 排序：main / standby / both。
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
	// In 决定 Match 作用在哪个字符串上：any（默认）/ group / title。
	//
	// 排字幕组用 group：只看组名，不会被作品名里的同名字符串误伤。
	// 排字幕类型用 title：只看资源标题。
	In string `yaml:"in,omitempty" json:"in,omitempty"`
}

// InOr 返回规整后的匹配位置，缺省为 any（与分轴之前的行为一致）。
func (r Rule) InOr() string {
	switch strings.ToLower(strings.TrimSpace(r.In)) {
	case InGroup:
		return InGroup
	case InTitle:
		return InTitle
	default:
		return InAny
	}
}

// ScopeOr 返回规整后的作用域，缺省为主 RSS。
func (r Rule) ScopeOr() string {
	switch strings.ToLower(strings.TrimSpace(r.Scope)) {
	case ScopeStandby:
		return ScopeStandby
	case ScopeBoth:
		return ScopeBoth
	default:
		return ScopeMain
	}
}

// DenyRule 命中即剔除该候选。
type DenyRule struct {
	Match string `yaml:"match" json:"match"`
	Why   string `yaml:"why,omitempty" json:"why,omitempty"`
}

// Rules 是全局匹配规则。
type Rules struct {
	Primary []Rule     `yaml:"primary" json:"primary"`
	Deny    []DenyRule `yaml:"deny" json:"deny"`
	// PrimaryFiles / DenyFiles 是外置规则文件：白名单（放流组排名）与黑名单各放一个文件，
	// 免得把它们写死在主配置里 —— 想换一套口味就换一份文件，主配置不用动。
	//
	// 相对路径按主配置所在目录解析；文件里是裸列表，和 primary / deny 同构；
	// 按声明顺序追加在内联条目之后。文件读不出来会直接报错，不会静默跳过。
	PrimaryFiles []string `yaml:"primaryFiles,omitempty" json:"primaryFiles,omitempty"`
	DenyFiles    []string `yaml:"denyFiles,omitempty" json:"denyFiles,omitempty"`
	// MaxStandby 是每条订阅保留几条副 RSS。
	// 默认 1：多数人只要一个备胎；想「所有版本都收」的自己调大，
	// 并在规则里把想要的组都写成 scope: standby。
	MaxStandby int `yaml:"maxStandby" json:"maxStandby"`
}

// RuleFileInfo 描述一个外置规则文件的加载结果，只用于展示。
type RuleFileInfo struct {
	Path  string `json:"path"`
	Kind  string `json:"kind"` // primary / deny
	Count int    `json:"count"`
}

// DefaultSourceOrder 是默认的站点优先级。
//
// 只留两家，各有各的不可替代性：
//
//	anibt  走实例的 aniBTGroup 接口，字幕组维度、API 齐全、有热度权重
//	nyaa   由 AniSync 自己按关键字搜索，资源覆盖最广，补 anibt 的盲区
//
// animegarden / mikan 仍然能写进配置并且照常工作（老配置不会失效），
// 只是不再进默认值：三家都走实例的同类接口，多出来的两家换不回额外的覆盖面。
var DefaultSourceOrder = []string{"anibt", "nyaa"}

// Sources 描述站点数据来源。
type Sources struct {
	// Instance 指定借用哪个实例的站点数据（mikanGroup / aniBTGroup / animeGardenGroup）；
	// 留空时用第一个启用的实例。
	//
	// 刻意不加 omitempty：空值也要写出来，否则保存一次这个键就没了，
	// 挂在它上面的注释（「留空 = 第一个启用的实例」）也跟着一起消失。
	Instance         string   `yaml:"instance" json:"instance"`
	Order            []string `yaml:"order" json:"order"`
	StopOnFirstMatch bool     `yaml:"stopOnFirstMatch" json:"stopOnFirstMatch"`
	TimeoutSeconds   int      `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	// CacheMinutes 是站点数据的缓存时长，0 表示不缓存。
	CacheMinutes int `yaml:"cacheMinutes" json:"cacheMinutes"`
}

// Target 是一条番剧级配置，两种用途共用一份字段：
//
//  1. 规定季度与剧集偏移（和 ANI-RSS 的 Ani.season / Ani.offset 一致）：
//     命中该番剧的所有订阅都按这里的值写入，实例里已有的订阅同样生效。
//     季度按番剧给一个值，集偏移按 RSS 分开给：Offset 管主 RSS，StandbyOffset 管副 RSS。
//  2. 实例里没有这个番剧时自动新增（需要 addMissing / --add-missing）。
//
// 匹配方式：先按 BgmID 精确匹配，再按 Title 对订阅标题做包含匹配。
type Target struct {
	Title string `yaml:"title" json:"title"`
	// JPTitle 是日文原名。Nyaa 这类站点只认日文/罗马音，中文名搜不到东西，
	// 自动发现出来的条目会把 Bangumi 的原名填在这里。
	JPTitle   string   `yaml:"jpTitle,omitempty" json:"jpTitle,omitempty"`
	BgmID     string   `yaml:"bgmId" json:"bgmId"`
	MikanID   string   `yaml:"mikanId,omitempty" json:"mikanId,omitempty"`
	Instances []string `yaml:"instances,omitempty" json:"instances,omitempty"`
	// Enable 新订阅是否启用；nil 时按 false 处理（不启用就不会触发下载）。
	// 只影响新增，不会去改动已有订阅的开关。
	Enable *bool `yaml:"enable,omitempty" json:"enable,omitempty"`
	// 季度与剧集偏移；留空表示沿用 sync 里的全局值，全局也留空就不碰订阅里已有的值。
	Season        *int `yaml:"season,omitempty" json:"season,omitempty"`
	Offset        *int `yaml:"offset,omitempty" json:"offset,omitempty"`
	StandbyOffset *int `yaml:"standbyOffset,omitempty" json:"standbyOffset,omitempty"`
}

// Sync 是同步行为配置。
type Sync struct {
	// IntervalMinutes 是后台自动爬取的间隔，单位分钟；0 表示不自动。
	IntervalMinutes int  `yaml:"intervalMinutes" json:"intervalMinutes"`
	Preview         bool `yaml:"preview" json:"preview"`
	AddMissing      bool `yaml:"addMissing" json:"addMissing"`

	// 季度与剧集偏移的全局兜底值，语义与 ANI-RSS 的 Ani.season / Ani.offset 一致。
	//
	// 正常用法是按番剧写在 targets 里（季度按番剧给，主副 RSS 的偏移分开给）；
	// 这里的全局值只在番剧没命中 targets、又想统一指定时才用。
	//
	// 用指针是为了区分「没配置」与「配置成 0」：
	//   - 留空（不写这一行）-> 保持订阅里已有的值，AniSync 不碰；
	//   - 写了（哪怕是 0）  -> 按写的值覆盖。
	//
	// ANI-RSS 会按每个 RSS 的实际集数自动推断 offset，所以默认必须是「不碰」，
	// 否则一次同步就会把全部订阅的偏移洗成同一个值。
	Season *int `yaml:"season,omitempty" json:"season,omitempty"`
	// Offset 是主 RSS 的剧集偏移。
	Offset *int `yaml:"offset,omitempty" json:"offset,omitempty"`
	// StandbyOffset 是副 RSS 的剧集偏移，与主 RSS 分开。
	StandbyOffset *int `yaml:"standbyOffset,omitempty" json:"standbyOffset,omitempty"`
}

// Discover 描述「自动发现该订阅什么」：AniSync 不只在实例已有的订阅上打点，
// 还能自己去扫一遍「现在有什么番剧在放」，把实例里还没有的补进去。
//
// 默认来源是 yuc（Yuc 的当季番剧表）：
//
//	yuc                 Yuc 季度页给「这一季有哪些番」，Bangumi 只负责补 bgmId
//	bangumi-calendar    Bangumi 每日放送表；自带 bgmId，但混着国漫美漫 OVA，要过滤
//	animegarden         只列出真的有字幕组在做的番剧
//	bangumi-collections 扫某个 Bangumi 用户的「在看」，需要 bgmUser
//
// 发现出来的条目会被当成自动生成的 targets：季度与偏移留空，交给 ANI-RSS 自己推断。
type Discover struct {
	// Enabled 打开自动发现；命令行 --discover 等价于此。
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Sources 是发现来源，按顺序取并集；留空用 DefaultDiscoverSources。
	Sources []string `yaml:"sources,omitempty" json:"sources,omitempty"`
	// BgmUser 是 Bangumi 用户名或 uid，只有 bangumi-collections 需要。
	BgmUser string `yaml:"bgmUser,omitempty" json:"bgmUser,omitempty"`
	// Days 是「多久内更新过才算在放」的窗口，0 用默认 45 天。
	Days int `yaml:"days,omitempty" json:"days,omitempty"`
	// Limit 是单次最多新增多少条，防止第一次跑就把几百部番剧灌进实例；0 用默认 50。
	Limit int `yaml:"limit,omitempty" json:"limit,omitempty"`
	// Enable 决定发现出来的新订阅是否直接启用。
	// 留空 = false：先只新增不启用，看一眼计划确认没问题，再改成 true 放手。
	Enable *bool `yaml:"enable,omitempty" json:"enable,omitempty"`
	// Exclude 是标题正则黑名单，命中就不发现。用来挡掉国漫、日剧、合集这些不想要的。
	Exclude []string `yaml:"exclude,omitempty" json:"exclude,omitempty"`
	// JapanOnly 只列日本 TV 番剧与剧场版，默认开。
	//
	// 放送表按星期列出所有在放的东西，本身不带地区与平台，里面混着国漫、美漫、
	// OVA、网络短片和幼儿节目。打开时按 Bangumi 条目的地区标签（日本）与
	// 平台（TV / 剧场版）过滤掉那些。
	//
	// 用指针是为了区分「没配」与「配成 false」：没配按 true 处理。
	// 关掉就回到「放送表给什么就列什么」。
	JapanOnly *bool `yaml:"japanOnly,omitempty" json:"japanOnly,omitempty"`
}

// 自动发现来源标识。
const (
	DiscoverYuc            = "yuc"
	DiscoverAnimeGarden    = "animegarden"
	DiscoverBgmCalendar    = "bangumi-calendar"
	DiscoverBgmCollections = "bangumi-collections"
)

// DiscoverSourceNames 是全部合法的发现来源。
var DiscoverSourceNames = []string{DiscoverYuc, DiscoverAnimeGarden, DiscoverBgmCalendar, DiscoverBgmCollections}

// DefaultDiscoverSources 是自动发现的默认来源：Yuc 的当季番剧表。
//
// 为什么是 Yuc 而不是 Bangumi 放送表：放送表把国漫、美漫、OVA、网络短片全混在一起，
// 而且不带地区与平台，要筛干净只能逐条查详情（110 次请求）。Yuc 的季度页本身就是
// 一份人工整理过的日本番剧表，拿它定「这一季有什么」，再让 Bangumi 只负责补 bgmId。
//
// 匹配走日文原名：中文译名是各家自己的写法，实测命中率 46%，日文原名 88%。
var DefaultDiscoverSources = []string{DiscoverYuc}

// DefaultDiscoverDays 是自动发现的时间窗口。
//
// 按「首播日期在最近多少天内」算。AnimeGarden 的主题列表覆盖约一年，
// 180 天刚好留住当季 + 上一季；再窄就会把上一季还没播完的番剧漏掉。
const DefaultDiscoverDays = 180

// DefaultDiscoverLimit 是单次自动新增的条数上限。
const DefaultDiscoverLimit = 50

// DiscoverEnabled 返回发现出来的新订阅是否启用。
func (d Discover) DiscoverEnabled() bool { return d.Enable != nil && *d.Enable }

// JapanOnlyEnabled 返回是否只列日本 TV 番剧与剧场版；没配时按开处理。
func (d Discover) JapanOnlyEnabled() bool { return d.JapanOnly == nil || *d.JapanOnly }

// SourceList 返回规整后的发现来源。
func (d Discover) SourceList() []string {
	if len(d.Sources) == 0 {
		return append([]string(nil), DefaultDiscoverSources...)
	}
	out := make([]string, 0, len(d.Sources))
	for _, name := range d.Sources {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		return append([]string(nil), DefaultDiscoverSources...)
	}
	return out
}

// WindowDays 返回生效的时间窗口。
func (d Discover) WindowDays() int {
	if d.Days > 0 {
		return d.Days
	}
	return DefaultDiscoverDays
}

// MaxNew 返回生效的单次新增上限。
func (d Discover) MaxNew() int {
	if d.Limit > 0 {
		return d.Limit
	}
	return DefaultDiscoverLimit
}

// Config 是 AniSync 的完整配置。
type Config struct {
	Listen string `yaml:"listen" json:"listen"`
	// Proxy 是出站请求用的代理地址，例如 http://127.0.0.1:7890；
	// 留空时用环境变量 ANISYNC_PROXY，其次 HTTPS_PROXY / HTTP_PROXY。
	//
	// 它作用于所有出站请求：站点数据、Bangumi 搜索、封面图片。
	//
	// 刻意不加 omitempty：空值也要写出来，否则保存一次这个键就没了，
	// 挂在它上面的注释（「留空 = 用环境变量」）也跟着一起消失。
	Proxy     string     `yaml:"proxy" json:"proxy"`
	Sources   Sources    `yaml:"sources" json:"sources"`
	Rules     Rules      `yaml:"rules" json:"rules"`
	Instances []Instance `yaml:"instances" json:"instances"`
	Targets   []Target   `yaml:"targets" json:"targets"`
	Discover  Discover   `yaml:"discover,omitempty" json:"discover,omitempty"`
	Sync      Sync       `yaml:"sync" json:"sync"`

	path string
	// filePrimary / fileDeny 是外置规则文件读出来的条目。
	//
	// 刻意不参与序列化：保存时只写回内联的那部分，否则一次保存就会把外置内容
	// 抄进主配置，下次加载变成两份（一份内联、一份外置）。
	filePrimary []Rule
	fileDeny    []DenyRule
	// FileRules 是外置规则的来源摘要，只读展示用。yaml:"-" 保证它永远不落盘。
	FileRules []RuleFileInfo `yaml:"-" json:"fileRules,omitempty"`
	// rawNode 是配置文件的原始 YAML 节点树，保存时用来把注释合并回去。
	// 不参与序列化，也不参与 JSON：它只是「上次读到的文件长什么样」。
	rawNode *yaml.Node
}

// AllPrimary 返回生效的匹配规则：内联的在前，外置文件按声明顺序追加。
func (c *Config) AllPrimary() []Rule {
	out := make([]Rule, 0, len(c.Rules.Primary)+len(c.filePrimary))
	out = append(out, c.Rules.Primary...)
	return append(out, c.filePrimary...)
}

// AllDeny 返回生效的黑名单：内联的在前，外置文件按声明顺序追加。
func (c *Config) AllDeny() []DenyRule {
	out := make([]DenyRule, 0, len(c.Rules.Deny)+len(c.fileDeny))
	out = append(out, c.Rules.Deny...)
	return append(out, c.fileDeny...)
}

// Path 返回配置文件路径。
func (c *Config) Path() string { return c.path }

// SetPath 设置配置文件路径（Web 设置页保存时用）。
func (c *Config) SetPath(path string) { c.path = path }

// Default 返回带默认值的配置。
func Default() *Config {
	return &Config{
		Listen: DefaultListen,
		Sources: Sources{
			Order:          append([]string(nil), DefaultSourceOrder...),
			TimeoutSeconds: 25,
			CacheMinutes:   720,
		},
		Rules: Rules{MaxStandby: 1},
		Discover: Discover{
			Sources: append([]string(nil), DefaultDiscoverSources...),
			Days:    DefaultDiscoverDays,
			Limit:   DefaultDiscoverLimit,
		},
	}
}

// DefaultPath 返回默认配置路径：程序同目录下的 config.yaml。
func DefaultPath() string { return workdir.ConfigPath() }

// Load 读取配置；文件不存在时返回默认配置（path 仍指向该位置）。
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	cfg := Default()
	cfg.path = path

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	cfg.path = path
	// 留住原文件的节点树：保存时把新值合并上去，注释与键序才不会被冲掉。
	cfg.rawNode = parseLayout(raw)
	if err := cfg.LoadRuleFiles(); err != nil {
		return nil, err
	}
	cfg.Normalize()
	return cfg, nil
}

// ReloadLayout 重新读一遍配置文件，只取回节点树（注释布局），不动结构体字段。
//
// Web 设置页保存时构造的是一份全新 Config（来自 JSON），必须先把原文件的节点树
// 接过来，否则一次保存就会把 config.yaml 里的注释全冲掉。
//
// 原文件不存在或读不动时返回 nil：没有布局可继承而已，不该因此挡住保存。
func (c *Config) ReloadLayout() error {
	c.rawNode = nil
	if c.path == "" {
		return nil
	}
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return nil
	}
	c.rawNode = parseLayout(raw)
	return nil
}

// LoadRuleFiles 读取 rules.primaryFiles / rules.denyFiles 指向的外置规则文件。
//
// 单独暴露成方法是为了 Web 设置页：保存时它构造的是一份全新的 Config，
// 不重新读一遍的话，用户在设置页点一次保存就会让外置白名单当场失效。
//
// 相对路径按主配置所在目录解析。文件读不出来、或者格式不对，都会直接报错 ——
// 静默跳过会让人以为规则生效了，其实一条都没加载。
func (c *Config) LoadRuleFiles() error {
	dir := filepath.Dir(c.path)
	c.filePrimary = nil
	c.fileDeny = nil
	c.FileRules = nil

	for _, name := range c.Rules.PrimaryFiles {
		path := resolveRulePath(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取规则文件失败（rules.primaryFiles 里的 %q）: %w", name, err)
		}
		var list []Rule
		if err := yaml.Unmarshal(raw, &list); err != nil {
			return fmt.Errorf("解析规则文件 %s 失败（内容应为与 rules.primary 同构的列表）: %w", path, err)
		}
		c.filePrimary = append(c.filePrimary, list...)
		c.FileRules = append(c.FileRules, RuleFileInfo{Path: path, Kind: "primary", Count: len(list)})
	}

	for _, name := range c.Rules.DenyFiles {
		path := resolveRulePath(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取黑名单文件失败（rules.denyFiles 里的 %q）: %w", name, err)
		}
		var list []DenyRule
		if err := yaml.Unmarshal(raw, &list); err != nil {
			return fmt.Errorf("解析黑名单文件 %s 失败（内容应为与 rules.deny 同构的列表）: %w", path, err)
		}
		c.fileDeny = append(c.fileDeny, list...)
		c.FileRules = append(c.FileRules, RuleFileInfo{Path: path, Kind: "deny", Count: len(list)})
	}
	return nil
}

// resolveRulePath 把规则文件路径按主配置所在目录展开。
func resolveRulePath(dir, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dir, name)
}

// envPattern 支持 ${VAR} 与 $VAR，便于把密钥放在环境变量里。
var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// bgmSubjectPattern 从 bgm.tv 链接里取条目 id。
var bgmSubjectPattern = regexp.MustCompile(`(?:bgm\.tv|bangumi\.tv|chii\.in)/subject/(\d+)`)

// BgmIDFromText 从「bgm 链接 / 纯数字 id」里取出条目 id，取不到返回空串。
//
// 加番剧的常规做法是提前把 bgm 链接贴进来，而不是先建订阅再回头引用它，
// 所以 title 与 bgmId 两栏都接受链接：贴链接比手抄数字不容易错。
func BgmIDFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if m := bgmSubjectPattern.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	if strings.Trim(text, "0123456789") == "" {
		return text
	}
	return ""
}

// ExpandEnv 展开 ${VAR} / $VAR；未定义的变量原样保留。
func ExpandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(m, "${"), "}")
		name = strings.TrimPrefix(name, "$")
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return m
	})
}

// Resolve 返回展开环境变量后的实例信息，供真正发请求时使用。
//
// 配置结构里始终保留 ${VAR} 原文：Web 设置页把配置读出来又存回去，
// 如果在 Load 时就把密钥展开进内存，一次保存就会把明文写进 config.yaml。
func (i Instance) Resolve() Instance {
	out := i
	out.BaseURL = NormalizeBaseURL(ExpandEnv(i.BaseURL))
	out.APIKey = ExpandEnv(i.APIKey)
	return out
}

// ResolvedProxy 返回生效的代理：配置优先，其次环境变量 ANISYNC_PROXY。
func (c *Config) ResolvedProxy() string {
	if strings.TrimSpace(c.Proxy) != "" {
		return c.Proxy
	}
	return os.Getenv("ANISYNC_PROXY")
}

// Normalize 补齐默认值并规整 URL。
func (c *Config) Normalize() {
	d := Default()
	if !validListen(c.Listen) {
		c.Listen = d.Listen
	}
	if len(c.Sources.Order) == 0 {
		c.Sources.Order = d.Sources.Order
	}
	if c.Sources.TimeoutSeconds <= 0 {
		c.Sources.TimeoutSeconds = d.Sources.TimeoutSeconds
	}
	if c.Sources.CacheMinutes < 0 {
		c.Sources.CacheMinutes = 0
	}
	if c.Rules.MaxStandby <= 0 {
		c.Rules.MaxStandby = d.Rules.MaxStandby
	}
	for i := range c.Rules.Primary {
		c.Rules.Primary[i].Scope = c.Rules.Primary[i].ScopeOr()
		// in 只规整显式写了的取值：默认值留空，免得保存时给每条规则都补一个 in: any。
		if strings.TrimSpace(c.Rules.Primary[i].In) != "" {
			c.Rules.Primary[i].In = c.Rules.Primary[i].InOr()
		}
	}
	// 外置文件里的规则同样要规整 scope / in，否则文件里写的取值会失效。
	for i := range c.filePrimary {
		c.filePrimary[i].Scope = c.filePrimary[i].ScopeOr()
		if strings.TrimSpace(c.filePrimary[i].In) != "" {
			c.filePrimary[i].In = c.filePrimary[i].InOr()
		}
	}
	for i := range c.Instances {
		c.Instances[i].BaseURL = NormalizeBaseURL(c.Instances[i].BaseURL)
		if c.Instances[i].Name == "" {
			c.Instances[i].Name = c.Instances[i].BaseURL
		}
	}
	for i := range c.Targets {
		target := &c.Targets[i]
		target.Title = strings.TrimSpace(target.Title)
		target.JPTitle = strings.TrimSpace(target.JPTitle)
		target.BgmID = strings.TrimSpace(target.BgmID)
		target.MikanID = strings.TrimSpace(target.MikanID)
		// 允许把 bgm 链接贴在任意一栏：贴链接比手抄数字不容易错。
		if id := BgmIDFromText(target.BgmID); id != "" {
			target.BgmID = id
		}
		if target.BgmID == "" {
			if id := BgmIDFromText(target.Title); id != "" {
				target.BgmID = id
				target.Title = ""
			}
		}
	}
	c.Discover.Sources = c.Discover.SourceList()
	c.Discover.BgmUser = strings.TrimSpace(c.Discover.BgmUser)
	c.Discover.Days = c.Discover.WindowDays()
	c.Discover.Limit = c.Discover.MaxNew()
}

// validListen 校验监听地址：必须是 host:port，且端口合法。
func validListen(addr string) bool {
	_, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || port == "" {
		return false
	}
	return strings.Trim(port, "0123456789") == "" && port != "0"
}

// SiteInstance 返回借用站点数据的实例名：优先用显式配置，否则取第一个启用的实例。
func (c *Config) SiteInstance() string {
	if name := strings.TrimSpace(c.Sources.Instance); name != "" {
		return name
	}
	for _, instance := range c.Instances {
		if instance.IsEnabled() {
			return instance.Name
		}
	}
	return ""
}

// NormalizeBaseURL 与 ANI-RSS 的 normalizeBaseUrl 保持一致：只保留协议+主机+代理子路径，末尾补 /。
func NormalizeBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" {
		return value
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return value
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u.String()
}

// Validate 校验配置是否可用于真实同步。
func (c *Config) Validate() error {
	if len(c.Instances) == 0 {
		return errors.New("还没有配置任何 ANI-RSS 实例")
	}
	seen := map[string]bool{}
	for i, inst := range c.Instances {
		// 按展开后的值校验：baseUrl / apiKey 都可以写成 ${ENV} 占位符。
		resolved := inst.Resolve()
		if resolved.BaseURL == "" {
			return fmt.Errorf("第 %d 个实例缺少 baseUrl", i+1)
		}
		if !strings.HasPrefix(resolved.BaseURL, "http://") && !strings.HasPrefix(resolved.BaseURL, "https://") {
			return fmt.Errorf("实例 %s 的 baseUrl 必须以 http:// 或 https:// 开头", inst.Name)
		}
		if resolved.APIKey == "" {
			return fmt.Errorf("实例 %s 缺少 apiKey（ANI-RSS → 设置 → 登录 → API Key）", inst.Name)
		}
		if seen[inst.Name] {
			return fmt.Errorf("实例名重复: %s", inst.Name)
		}
		seen[inst.Name] = true
	}
	if name := strings.TrimSpace(c.Sources.Instance); name != "" {
		if _, ok := c.InstanceByName(name); !ok {
			return fmt.Errorf("sources.instance 指向的实例不存在: %s", name)
		}
	}
	for _, r := range c.AllPrimary() {
		if r.Match == "" {
			continue
		}
		if _, err := regexp.Compile(r.Match); err != nil {
			return fmt.Errorf("规则 %q 的正则不合法: %w", r.Name, err)
		}
		switch strings.ToLower(strings.TrimSpace(r.Scope)) {
		case "", ScopeMain, ScopeStandby, ScopeBoth:
		default:
			return fmt.Errorf("规则 %q 的 scope 只能是 main / standby / both，实际 %q", r.Name, r.Scope)
		}
		switch strings.ToLower(strings.TrimSpace(r.In)) {
		case "", InAny, InGroup, InTitle:
		default:
			return fmt.Errorf("规则 %q 的 in 只能是 any / group / title，实际 %q", r.Name, r.In)
		}
	}
	for _, r := range c.AllDeny() {
		if _, err := regexp.Compile(r.Match); err != nil {
			return fmt.Errorf("排除规则 %q 的正则不合法: %w", r.Match, err)
		}
	}
	for i, target := range c.Targets {
		if target.Title == "" && target.BgmID == "" {
			return fmt.Errorf("第 %d 条 targets 既没有 title 也没有 bgmId，匹配不到任何番剧"+
				"（bgmId 可以直接贴 https://bgm.tv/subject/123 这样的链接）", i+1)
		}
	}
	return c.ValidateDiscover()
}

// ValidateDiscover 只校验自动发现这一段。
//
// 单独拆出来是为了让 `AniSync discover` 在「还没配实例」时也能跑：
// 用户下载下来的第一件事就是想看看现在有什么番剧在放，不该先被 apiKey 拦住。
func (c *Config) ValidateDiscover() error {
	for _, name := range c.Discover.SourceList() {
		if !validDiscoverSource(name) {
			return fmt.Errorf("discover.sources 里的 %q 不是合法来源，可选：%s",
				name, strings.Join(DiscoverSourceNames, " / "))
		}
	}
	if c.Discover.BgmUser == "" && usesDiscoverSource(c, DiscoverBgmCollections) {
		return errors.New("discover.sources 用了 bangumi-collections，必须同时填 discover.bgmUser（Bangumi 用户名或 uid）")
	}
	for _, pattern := range c.Discover.Exclude {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("discover.exclude 的正则不合法 %q: %w", pattern, err)
		}
	}
	return nil
}

// validDiscoverSource 判断发现来源是否合法。
func validDiscoverSource(name string) bool {
	for _, known := range DiscoverSourceNames {
		if name == known {
			return true
		}
	}
	return false
}

// usesDiscoverSource 判断某个来源是否在生效列表里。
func usesDiscoverSource(c *Config, name string) bool {
	for _, item := range c.Discover.SourceList() {
		if item == name {
			return true
		}
	}
	return false
}

// fileHeader 只在「没有原文件可以继承」时补上，避免和用户自己的头部注释重复。
const fileHeader = "# AniSync 配置：多 ANI-RSS 实例 + 主副 RSS 自动打点\n" +
	"# apiKey 支持 ${ENV_VAR} 写法，避免明文落盘。\n"

// Save 写回配置文件（Unix 下收紧权限，因为里面有 API Key）。
//
// 走的是「合并」而不是「重写」：新值合并到上次读到的节点树上，
// 注释、键序、引号风格都保留。样例配置的注释就是文档，不能保存一次就没了。
func (c *Config) Save() error {
	if c.path == "" {
		c.path = DefaultPath()
	}
	c.Normalize()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	out, err := c.encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(c.path, out, 0o600); err != nil {
		return err
	}
	_ = os.Chmod(c.path, 0o600)
	// 保存完自己就是新的「原文件」。不更新的话，连着保存两次时第二次就没注释可合并了。
	c.rawNode = parseLayout(out)
	return nil
}

// encode 生成要写盘的 YAML 文本。
func (c *Config) encode() ([]byte, error) {
	fresh, err := c.freshNode()
	if err != nil {
		return nil, err
	}
	if c.rawNode != nil {
		return encodeNode(mergeNode(c.rawNode, fresh))
	}
	body, err := encodeNode(fresh)
	if err != nil {
		return nil, err
	}
	return append([]byte(fileHeader), body...), nil
}

// freshNode 把当前结构体转成一份全新的节点树（没有注释）。
func (c *Config) freshNode() (*yaml.Node, error) {
	raw, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}
	node := parseLayout(raw)
	if node == nil {
		return nil, fmt.Errorf("序列化配置失败")
	}
	return node, nil
}

// InstanceByName 按名字取实例。
func (c *Config) InstanceByName(name string) (Instance, bool) {
	for _, inst := range c.Instances {
		if inst.Name == name {
			return inst, true
		}
	}
	return Instance{}, false
}
