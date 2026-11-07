package bridge

import (
	"context"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// ---------- 7.3 instance ----------

func opInstanceList(c *Call) (any, error) {
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	return map[string]any{"instances": instanceViews(cfg)}, nil
}

// instanceTestPayload 是 instance.test 的 payload。
type instanceTestPayload struct {
	Name string `json:"name"`
}

func opInstanceTest(c *Call) (any, error) {
	var payload instanceTestPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	name, err := requireInstance(cfg, payload.Name)
	if err != nil {
		return nil, err
	}

	ctx := c.Ctx()
	started := time.Now()
	data := map[string]any{
		"name":              name,
		"ok":                false,
		"latencyMs":         0,
		"about":             map[string]any{},
		"subscriptionCount": 0,
	}
	about, err := app.About(ctx, cfg, name)
	if err != nil {
		data["latencyMs"] = time.Since(started).Milliseconds()
		data["error"] = err.Error()
		return data, nil
	}
	_, total, err := app.ListAni(ctx, cfg, name)
	if err != nil {
		data["latencyMs"] = time.Since(started).Milliseconds()
		data["error"] = "连接成功但拉取订阅失败: " + err.Error()
		return data, nil
	}
	data["ok"] = true
	data["latencyMs"] = time.Since(started).Milliseconds()
	data["about"] = about
	data["subscriptionCount"] = total
	c.Log("info", "实例连通性正常: "+name)
	return data, nil
}

// instanceSubscriptionsPayload 是 instance.subscriptions 的 payload。
type instanceSubscriptionsPayload struct {
	Name string `json:"name"`
	// Refresh 为 false 时允许吃一层很短的缓存（60 秒）。
	Refresh *bool `json:"refresh"`
}

func opInstanceSubscriptions(c *Call) (any, error) {
	var payload instanceSubscriptionsPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	name, err := requireInstance(cfg, payload.Name)
	if err != nil {
		return nil, err
	}
	refresh := payload.Refresh == nil || *payload.Refresh

	items, total, err := c.Core.listAni(c.Ctx(), cfg, name, refresh)
	data := map[string]any{
		"name":  name,
		"total": total,
		"items": []SubscriptionView{},
	}
	if err != nil {
		// 读不到时把原因放进 data.error：界面拿它渲染「实例连不上」，
		// 而列表本身仍然是合法的空列表（契约 §7.3 的形状）。
		data["error"] = err.Error()
		data["total"] = 0
		return data, nil
	}
	views := make([]SubscriptionView, 0, len(items))
	for _, ani := range items {
		views = append(views, subscriptionViewOf(ani))
	}
	data["items"] = views
	return data, nil
}

// requireInstance 校验实例名非空且存在。
//
// 这是**所有「按实例名取资源」的 op 的唯一入口**（instance.test / instance.subscriptions /
// subscription.set / add / delete / resolve 都走这里），所以 not_found 的 `detail`
// 也只需要在这里补一次：`detail` = 那个实例名（契约 §3 的判别表）。
func requireInstance(cfg *config.Config, name string) (string, error) {
	name = firstNonEmpty(name)
	if name == "" {
		return "", BadRequest("payload.name 不能为空")
	}
	if _, err := app.FindInstance(cfg, name); err != nil {
		// app.FindInstance 只会说「找不到实例」，是哪个名字只有这里知道。
		return "", withResourceDetail(err, name)
	}
	return name, nil
}

// instanceExists 判断实例名是否在配置里（不发网络请求）。
func instanceExists(cfg *config.Config, name string) bool {
	_, ok := cfg.InstanceByName(name)
	return ok
}

// ctxOf 取调用上下文（给不接受 nil 的领域函数用）。
func ctxOf(c *Call) context.Context {
	if c == nil || c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}
