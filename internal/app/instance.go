package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/rules"
	"github.com/zzzwannasleep/anisync/internal/source"
)

// InstanceClientTimeout 是面向实例的默认超时。
const InstanceClientTimeout = 30 * time.Second

// ErrInstanceNotFound 表示按名字找不到实例。桥接层靠它把错误码判成 not_found
// —— 否则「实例名打错了」会和「程序坏了」一起显示成同一条内部错误。
var ErrInstanceNotFound = errors.New("找不到实例")

// FindInstance 按名字取实例，找不到时报错信息统一。
func FindInstance(cfg *config.Config, name string) (config.Instance, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return config.Instance{}, fmt.Errorf("实例名不能为空")
	}
	inst, ok := cfg.InstanceByName(name)
	if !ok {
		return config.Instance{}, fmt.Errorf("%w %s", ErrInstanceNotFound, name)
	}
	return inst, nil
}

// Client 按实例名建一个客户端（自动展开 ${ENV} 并套用代理）。
func Client(cfg *config.Config, name string) (*assapi.Client, error) {
	inst, err := FindInstance(cfg, name)
	if err != nil {
		return nil, err
	}
	return ClientFor(cfg, inst)
}

// ClientFor 按实例结构建一个客户端。
func ClientFor(cfg *config.Config, inst config.Instance) (*assapi.Client, error) {
	return ClientWithTimeout(cfg, inst, InstanceClientTimeout)
}

// ClientWithTimeout 按实例结构建一个客户端，并指定超时。
//
// 校验（check 命令）用 20 秒：它只想知道「连得上吗」，
// 不该让人对着一个 30 秒不动的光标等。同步路径用默认那 30 秒。
func ClientWithTimeout(cfg *config.Config, inst config.Instance, timeout time.Duration) (*assapi.Client, error) {
	resolved := inst.Resolve()
	return assapi.New(config.NormalizeBaseURL(resolved.BaseURL), resolved.APIKey, cfg.ResolvedProxy(), timeout)
}

// About 读实例的 about（连通性测试）。
func About(ctx context.Context, cfg *config.Config, name string) (map[string]any, error) {
	client, err := Client(cfg, name)
	if err != nil {
		return nil, err
	}
	return client.About(ctx)
}

// ListAni 拉取某个实例的全部订阅。
func ListAni(ctx context.Context, cfg *config.Config, name string) ([]assapi.Ani, int, error) {
	client, err := Client(cfg, name)
	if err != nil {
		return nil, 0, err
	}
	return ListAniWithRetry(ctx, client)
}

// SetAni 修改一条订阅（全量覆盖，必须传完整对象）。
func SetAni(ctx context.Context, cfg *config.Config, name string, ani assapi.Ani) error {
	client, err := Client(cfg, name)
	if err != nil {
		return err
	}
	return client.SetAni(ctx, ani)
}

// AddAni 新增一条订阅；override=true 时覆盖同名订阅。
func AddAni(ctx context.Context, cfg *config.Config, name string, ani assapi.Ani, override bool) error {
	client, err := Client(cfg, name)
	if err != nil {
		return err
	}
	return client.AddAni(ctx, ani, override)
}

// DeleteAni 删除订阅；deleteFiles=false 时只删订阅与种子缓存。
func DeleteAni(ctx context.Context, cfg *config.Config, name string, ids []string, deleteFiles bool) error {
	client, err := Client(cfg, name)
	if err != nil {
		return err
	}
	return client.DeleteAni(ctx, ids, deleteFiles)
}

// RssToAni 让实例把 RSS 解析成订阅草稿。
func RssToAni(ctx context.Context, cfg *config.Config, name string, body map[string]any) (assapi.Ani, error) {
	client, err := Client(cfg, name)
	if err != nil {
		return nil, err
	}
	return client.RssToAni(ctx, body)
}

// ListAniWithRetry 读一次实例订阅，传输层错误重试一次。
//
// listAni 的响应有 1.8MB，经本地代理时偶发 EOF（实测遇到过一次，同一秒直接 curl 是好的）。
// 这类错误重试一次基本就好。但 HTTP 层错误不重试 —— 401/403/500 不会因为再问一遍就变好，
// 只会让界面白等一轮。
func ListAniWithRetry(ctx context.Context, client *assapi.Client) ([]assapi.Ani, int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(300 * time.Millisecond):
			}
		}
		list, count, err := client.ListAni(ctx)
		if err == nil {
			return list, count, nil
		}
		var apiErr *assapi.APIError
		if errors.As(err, &apiErr) {
			return nil, 0, err
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

// SubscribedBgmIDs 读各启用实例里已有的订阅，取出它们的 bgmId。
//
// 返回的第三个值是「一个实例都没读到」时的原因。只要有一个实例读成功就不算失败：
// 多实例场景下某个实例临时连不上，不该让整张列表退化成「全部未添加」。
func SubscribedBgmIDs(ctx context.Context, cfg *config.Config) (map[string]bool, int, error) {
	ids := map[string]bool{}
	total := 0
	reached := 0
	var firstErr error

	for _, instance := range cfg.Instances {
		if !instance.IsEnabled() {
			continue
		}
		client, err := ClientFor(cfg, instance)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("实例 %s：%w", instance.Name, err)
			}
			continue
		}
		list, count, err := ListAniWithRetry(ctx, client)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("实例 %s：%w", instance.Name, err)
			}
			continue
		}
		reached++
		total += count
		for _, ani := range list {
			if id := BgmIDOfAni(ani); id != "" {
				ids[strings.ToLower(id)] = true
			}
		}
	}
	if reached == 0 && firstErr != nil {
		return ids, 0, firstErr
	}
	return ids, total, nil
}

// bgmIDParam 从 RSS 地址的查询参数里取 bgmId。
//
// AniBT 的订阅地址就是这个形状：https://anibt.net/rss/anime.xml?bgmId=975&groupSlug=xxx。
// source.BgmIDFromURL 只认 /subject/NNN 那种路径，认不出这里的 bgmId，所以得单独匹配。
var bgmIDParam = regexp.MustCompile(`(?i)[?&]bgmId=(\d+)`)

// BgmIDOfAni 从一条订阅里取 bgmId。
// bgmUrl 是实例自己填的番剧链接（形如 https://bgm.tv/subject/975），最可靠；
// 取不到再退到订阅的 RSS 地址上找，那里带的是 ?bgmId= 查询参数。
func BgmIDOfAni(ani assapi.Ani) string {
	for _, key := range []string{"bgmUrl", "url"} {
		raw, ok := ani[key].(string)
		if !ok || raw == "" {
			continue
		}
		if id := source.BgmIDFromURL(raw); id != "" {
			return id
		}
		if m := bgmIDParam.FindStringSubmatch(raw); m != nil {
			return m[1]
		}
	}
	return ""
}

// FoldTitle 把标题压成可比较的形式：去掉全部空白、统一小写。
//
// 只用于「这条 target 是不是已经覆盖了这部番」，不做模糊匹配 ——
// 模糊匹配是 planner 的活，这里误判的代价是用户看到错的「已添加」状态。
func FoldTitle(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), ""))
}

// SchemeEngine 按方案名编译规则引擎；profile 为空时取顶层 rules。
//
// probe 要它：候选的「命中哪条规则」只有在排过一遍之后才有值，
// 而排序用的必须是这个实例真正生效的那套规则。
func SchemeEngine(cfg *config.Config, profile string) (*rules.Engine, error) {
	scheme := cfg.SchemeForProfile(profile)
	primary := make([]rules.Rule, 0, len(scheme.Primary))
	for _, rule := range scheme.Primary {
		primary = append(primary, rules.Rule{
			Name:     rule.Name,
			Match:    rule.Match,
			Priority: rule.Priority,
			Site:     rule.Site,
			Scope:    rule.ScopeOr(),
			In:       rule.InOr(),
		})
	}
	deny := make([]rules.DenyRule, 0, len(scheme.Deny))
	for _, rule := range scheme.Deny {
		deny = append(deny, rules.DenyRule{Match: rule.Match, Why: rule.Why})
	}
	maxStandby := scheme.MaxStandby
	if maxStandby <= 0 {
		maxStandby = 1
	}
	return rules.New(primary, deny, maxStandby)
}
