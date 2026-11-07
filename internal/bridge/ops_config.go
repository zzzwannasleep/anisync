package bridge

import (
	_ "embed"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// ---------- 7.1 core ----------

// coreInitPayload 是 core.init 的 payload。
type coreInitPayload struct {
	ConfigPath string `json:"configPath"`
	DataDir    string `json:"dataDir"`
}

func opCoreInit(c *Call) (any, error) {
	var payload coreInitPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	if _, err := c.Core.Init(payload.ConfigPath, payload.DataDir); err != nil {
		return nil, Classify(err)
	}
	c.Log("info", "工作区已切换到 "+app.ConfigPath())
	return coreInfo(), nil
}

func opCoreVersion(c *Call) (any, error) { return coreInfo(), nil }

// coreInfo 是 core.init / core.version 的 data；两者只差 configPath / dataDir。
func coreInfo() map[string]any {
	return map[string]any{
		"version":    app.Version,
		"schema":     app.Schema,
		"platform":   app.Platform(),
		"configPath": app.ConfigPath(),
		"dataDir":    app.DataDir(),
		"goVersion":  app.GoVersion(),
	}
}

func opCoreOps(c *Call) (any, error) {
	all := Ops()
	items := make([]map[string]any, 0, len(all))
	for _, op := range all {
		items = append(items, map[string]any{
			"name":    op.Name,
			"group":   op.Group,
			"summary": op.Summary,
			"async":   op.Async,
		})
	}
	return map[string]any{"schema": app.Schema, "ops": items}, nil
}

// ---------- 7.2 config ----------

func opConfigGet(c *Call) (any, error) {
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	return configPayload(cfg), nil
}

// configPayload 是 config.get 的 data，也是 config.save / profile.* 回给界面的那份。
//
// views 是**只读派生**：一次调用就把界面要用的表算好，省掉三次往返。
// config 是**原样可回传的**：config.get → 改 → config.save，
// 中间不许有 Go 侧不知道的额外字段。
func configPayload(cfg *config.Config) map[string]any {
	return map[string]any{
		"path":      cfg.Path(),
		"config":    cfg,
		"ruleFiles": ruleFileViews(cfg.FileRules),
		"views": map[string]any{
			"instances": instanceViews(cfg),
			"profiles":  profileViews(cfg),
		},
		"warnings": configWarnings(cfg),
	}
}

// configWarnings 报出「配置读得出来但过不了校验」。
//
// config.get 不该因为配置写错就拒绝回答：界面正是拿它来显示「错在哪」的。
// 但也不能装作没事 —— 那条红条就靠这里给出的原文。
func configWarnings(cfg *config.Config) []string {
	warnings := []string{}
	if err := cfg.Validate(); err != nil {
		warnings = append(warnings, "配置校验未通过："+err.Error())
	}
	return warnings
}

func instanceViews(cfg *config.Config) []InstanceView {
	out := make([]InstanceView, 0, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		out = append(out, instanceViewOf(cfg, instance))
	}
	return out
}

func profileViews(cfg *config.Config) []ProfileView {
	out := make([]ProfileView, 0, len(cfg.Profiles))
	for _, profile := range cfg.Profiles {
		out = append(out, profileViewOf(cfg, profile))
	}
	return out
}

// configSavePayload 是 config.save 的 payload。
type configSavePayload struct {
	Config *config.Config `json:"config"`
}

// opConfigSave 保存配置：**落盘 + 把校验结果当数据返回，不拒绝**。
//
// 为什么校验失败也要落盘（契约 §7.2）：
//   - 与既有行为一致：网页设置页的 PUT /api/config 走的是同一个 `Save()`，
//     本来就不拦，GUI 比它更严会造成「同一个引擎两种脾气」；
//   - 「失败要响」不等于「不许写」：校验该在 check / plan / sync 上拦
//     （那些真的会碰实例），不该在保存上拦；
//   - 增量编辑是 GUI 的基本用法：新建工作区之后用户可能只想先设代理，
//     这时保存被拒，等于**连一个 config.yaml 都建不出来**。
//
// 唯一仍然是硬错误的是「外置规则文件读不出来」：那不是校验，
// 而是「这份配置的规则加载不了」，放行会让后续 plan / sync 静默少一套规则。
func opConfigSave(c *Call) (any, error) {
	var payload configSavePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	if payload.Config == nil {
		return nil, BadRequest("payload.config 不能为空")
	}
	if c.Core == nil {
		return nil, InternalMsg("桥接层未初始化")
	}
	next := payload.Config
	path := c.Core.ConfigPath()
	next.SetPath(path)
	// 外置规则文件必须重新读一遍：incoming 是全新对象，不读的话
	// 用户点一次保存，白名单与黑名单就会当场失效。
	if err := next.LoadRuleFiles(); err != nil {
		return nil, InvalidConfig(err)
	}
	// 注释布局也要从原文件接过来，否则保存一次就把 config.yaml 的注释全冲掉。
	if err := next.ReloadLayout(); err != nil {
		return nil, Internal(err)
	}
	next.Normalize()

	// 校验结果只记录，不拦截 —— 它随响应一起回给界面，由界面显著地显示。
	validateErr := next.Validate()

	var saveErr error
	writeErr := c.Core.write(func() error {
		saveErr = next.Save()
		return saveErr
	})
	if writeErr != nil {
		return nil, writeErr
	}
	if saveErr != nil {
		return nil, Internal(saveErr)
	}
	c.Core.SetConfig(next)

	data := map[string]any{
		"path":     next.Path(),
		"valid":    validateErr == nil,
		"errors":   []map[string]any{},
		"warnings": []string{},
	}
	if validateErr != nil {
		// field 留空：config.Validate 只给一条人话消息，不报字段。
		// 与其猜一个字段名（猜错会把红条挂在错的输入框上），不如把原文交出去。
		data["errors"] = []map[string]any{{"message": validateErr.Error()}}
		c.Log("warn", "配置已保存，但校验没过："+validateErr.Error())
	} else {
		c.Log("info", "配置已保存："+next.Path())
	}
	return data, nil
}

// configValidatePayload 是 config.validate 的 payload。
type configValidatePayload struct {
	Config *config.Config `json:"config"`
}

func opConfigValidate(c *Call) (any, error) {
	var payload configValidatePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	target := payload.Config
	if target == nil {
		cfg, err := currentConfig(c)
		if err != nil {
			return nil, err
		}
		target = cfg
	}
	// 复制一份再规整：校验是只读动作，不许把调用方的对象改掉。
	candidate := *target
	candidate.Normalize()

	data := map[string]any{
		"ok":       true,
		"errors":   []map[string]any{},
		"warnings": []string{},
	}
	if err := candidate.Validate(); err != nil {
		data["ok"] = false
		data["errors"] = []map[string]any{{"message": err.Error()}}
	}
	return data, nil
}

func opConfigDefault(c *Call) (any, error) {
	cfg := config.Default()
	cfg.Normalize()
	return map[string]any{"config": cfg}, nil
}

// sampleYAML 是 config.example.yaml 的内嵌副本。
//
// 内嵌而不是运行时找文件：移动端的 APK 目录里没有这个文件，
// 而「看一眼样例配置」在手机上是刚需（配置写坏了要用它对照）。
// 与仓库根目录那份的一致性由 ops_test.go 钉住，不靠人记。
//
//go:embed sample/config.example.yaml
var sampleYAML []byte

// SampleYAML 返回样例配置的原文。
//
// 刻意只认内嵌那一份：磁盘上「旁边刚好有个同名文件」会让同一台机器上的
// 两次调用给出不同结果，而 UI 拿它当只读参考 —— 不该有这种漂移。
func SampleYAML() string { return string(sampleYAML) }

func opConfigSample(c *Call) (any, error) {
	return map[string]any{"yaml": SampleYAML()}, nil
}
