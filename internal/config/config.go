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
// Match 会同时作用于字幕组名与最近的资源标题，因此既能排字幕组，
// 也能排「简日内嵌 / 繁日内嵌 / 简繁日内封」这类字幕类型。
type Rule struct {
	Name     string `yaml:"name" json:"name"`
	Match    string `yaml:"match" json:"match"`
	Priority int    `yaml:"priority" json:"priority"`
	// Site 限定只在该来源生效，空表示不限。
	Site string `yaml:"site,omitempty" json:"site,omitempty"`
	// Scope 决定这条规则给主 RSS 排序还是给副 RSS 排序：main / standby / both。
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
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
	Primary    []Rule     `yaml:"primary" json:"primary"`
	Deny       []DenyRule `yaml:"deny" json:"deny"`
	MaxStandby int        `yaml:"maxStandby" json:"maxStandby"`
}

// Sources 描述站点数据来源。
type Sources struct {
	// Instance 指定借用哪个实例的站点数据（mikanGroup / aniBTGroup / animeGardenGroup）；
	// 留空时用第一个启用的实例。
	Instance         string   `yaml:"instance,omitempty" json:"instance,omitempty"`
	Order            []string `yaml:"order" json:"order"`
	StopOnFirstMatch bool     `yaml:"stopOnFirstMatch" json:"stopOnFirstMatch"`
	TimeoutSeconds   int      `yaml:"timeoutSeconds" json:"timeoutSeconds"`
	// CacheMinutes 是站点数据的缓存时长，0 表示不缓存。
	CacheMinutes int `yaml:"cacheMinutes" json:"cacheMinutes"`
}

// Target 是希望自动添加的番剧（实例里没有就 addAni）。
type Target struct {
	Title     string   `yaml:"title" json:"title"`
	BgmID     string   `yaml:"bgmId" json:"bgmId"`
	MikanID   string   `yaml:"mikanId,omitempty" json:"mikanId,omitempty"`
	Instances []string `yaml:"instances,omitempty" json:"instances,omitempty"`
	// Enable 新订阅是否启用；nil 时按 false 处理（不启用就不会触发下载）。
	Enable *bool `yaml:"enable,omitempty" json:"enable,omitempty"`
}

// Sync 是同步行为配置。
type Sync struct {
	// IntervalMinutes 是后台自动爬取的间隔，单位分钟；0 表示不自动。
	IntervalMinutes int  `yaml:"intervalMinutes" json:"intervalMinutes"`
	Preview         bool `yaml:"preview" json:"preview"`
	AddMissing      bool `yaml:"addMissing" json:"addMissing"`
}

// Config 是 AniSync 的完整配置。
type Config struct {
	Listen    string     `yaml:"listen" json:"listen"`
	Proxy     string     `yaml:"proxy,omitempty" json:"proxy,omitempty"`
	Sources   Sources    `yaml:"sources" json:"sources"`
	Rules     Rules      `yaml:"rules" json:"rules"`
	Instances []Instance `yaml:"instances" json:"instances"`
	Targets   []Target   `yaml:"targets,omitempty" json:"targets,omitempty"`
	Sync      Sync       `yaml:"sync" json:"sync"`

	path string
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
			Order:          []string{"anibt", "animegarden", "mikan", "nyaa"},
			TimeoutSeconds: 25,
			CacheMinutes:   720,
		},
		Rules: Rules{MaxStandby: 3},
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
	cfg.expandEnv()
	cfg.Normalize()
	return cfg, nil
}

// envPattern 支持 ${VAR} 与 $VAR，便于把密钥放在环境变量里。
var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func (c *Config) expandEnv() {
	expand := func(s string) string {
		return envPattern.ReplaceAllStringFunc(s, func(m string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(m, "${"), "}")
			name = strings.TrimPrefix(name, "$")
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
			return m
		})
	}
	for i := range c.Instances {
		c.Instances[i].APIKey = expand(c.Instances[i].APIKey)
		c.Instances[i].BaseURL = expand(c.Instances[i].BaseURL)
	}
	if c.Proxy == "" {
		c.Proxy = os.Getenv("ANISYNC_PROXY")
	}
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
	}
	for i := range c.Instances {
		c.Instances[i].BaseURL = NormalizeBaseURL(c.Instances[i].BaseURL)
		if c.Instances[i].Name == "" {
			c.Instances[i].Name = c.Instances[i].BaseURL
		}
	}
	for i := range c.Targets {
		c.Targets[i].BgmID = strings.TrimSpace(c.Targets[i].BgmID)
		c.Targets[i].MikanID = strings.TrimSpace(c.Targets[i].MikanID)
	}
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
		if inst.BaseURL == "" {
			return fmt.Errorf("第 %d 个实例缺少 baseUrl", i+1)
		}
		if !strings.HasPrefix(inst.BaseURL, "http://") && !strings.HasPrefix(inst.BaseURL, "https://") {
			return fmt.Errorf("实例 %s 的 baseUrl 必须以 http:// 或 https:// 开头", inst.Name)
		}
		if inst.APIKey == "" {
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
	for _, r := range c.Rules.Primary {
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
	}
	for _, r := range c.Rules.Deny {
		if _, err := regexp.Compile(r.Match); err != nil {
			return fmt.Errorf("排除规则 %q 的正则不合法: %w", r.Match, err)
		}
	}
	return nil
}

// Save 写回配置文件（Unix 下收紧权限，因为里面有 API Key）。
func (c *Config) Save() error {
	if c.path == "" {
		c.path = DefaultPath()
	}
	c.Normalize()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	header := "# AniSync 配置：多 ANI-RSS 实例 + 主副 RSS 自动打点\n" +
		"# apiKey 支持 ${ENV_VAR} 写法，避免明文落盘。\n"
	if err := os.WriteFile(c.path, append([]byte(header), out...), 0o600); err != nil {
		return err
	}
	_ = os.Chmod(c.path, 0o600)
	return nil
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
