package bridge

import (
	"fmt"
	"strconv"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/planner"
)

// maxSubscriptionBatch 是一次 subscription.set 最多应付多少条。
//
// 200 条：够覆盖一个正常规模的收藏库，同时挡住「拿这个 op 当批量写入器」
// 的用法 —— 那种用法的失败模式是写一半断了，而 setAni 是全量覆盖。
const maxSubscriptionBatch = 200

// ---------- 7.4 subscription ----------

// subscriptionPatch 是 subscription.set 的字段补丁。
//
// 全用指针：**没写的字段一律不动**，这是契约里写死的一条。
// 用值类型的话「没写」和「写成 0」长得一模一样，
// 一次调用就会把全部订阅的季度洗成 0。
type subscriptionPatch struct {
	Season        *int    `json:"season"`
	Offset        *int    `json:"offset"`
	StandbyOffset *int    `json:"standbyOffset"`
	Enable        *bool   `json:"enable"`
	Profile       *string `json:"profile"`
}

// subscriptionSetItem 是一条待改的订阅。
type subscriptionSetItem struct {
	Key   string             `json:"key"`
	Raw   map[string]any     `json:"raw"`
	Patch *subscriptionPatch `json:"patch"`
}

// subscriptionSetPayload 是 subscription.set 的 payload。
type subscriptionSetPayload struct {
	Name   string                `json:"name"`
	Items  []subscriptionSetItem `json:"items"`
	DryRun bool                  `json:"dryRun"`
}

func opSubscriptionSet(c *Call) (any, error) {
	var payload subscriptionSetPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	// 先判 payload 自己的合法性，再去找实例：一批 201 条无论实例名对不对都是坏请求，
	// 而「实例名打错」不该把它盖成 not_found。
	if len(payload.Items) > maxSubscriptionBatch {
		return nil, BadRequest("一次最多改 %d 条，收到 %d 条", maxSubscriptionBatch, len(payload.Items))
	}
	name, err := requireInstance(cfg, payload.Name)
	if err != nil {
		return nil, err
	}

	var current []assapi.Ani
	if !payload.DryRun || anyItemNeedsLookup(payload.Items) {
		items, _, listErr := c.Core.listAni(c.Ctx(), cfg, name, false)
		if listErr != nil {
			return nil, Classify(listErr)
		}
		current = items
	}

	results := make([]map[string]any, 0, len(payload.Items))
	type pendingItem struct {
		index int
		ani   assapi.Ani
	}
	var pending []pendingItem
	updated, skipped := 0, 0
	for _, item := range payload.Items {
		raw := item.Raw
		if raw == nil {
			// raw 缺省时按 key 重新读一次 listAni 定位：UI 拿到的就是最新的一份。
			located, ok := locateAni(current, item.Key)
			if !ok {
				results = append(results, map[string]any{
					"key":   item.Key,
					"error": "找不到这条订阅（key 过期？）",
				})
				skipped++
				continue
			}
			raw = located
		}
		next := map[string]any{}
		for key, value := range raw {
			next[key] = value
		}
		changes := applyPatch(next, item.Patch)
		entry := map[string]any{"key": item.Key, "changed": changes}
		results = append(results, entry)
		if len(changes) == 0 {
			skipped++
			continue
		}
		if payload.DryRun {
			// dryRun 只算 results，不调 setAni：界面拿它做「保存前先看一眼」。
			updated++
			continue
		}
		pending = append(pending, pendingItem{index: len(results) - 1, ani: next})
	}

	if !payload.DryRun && len(pending) > 0 {
		writeErr := c.Core.write(func() error {
			for _, item := range pending {
				if err := app.SetAni(c.Ctx(), cfg, name, item.ani); err != nil {
					// 单条失败不该让整批白写：已经写进去的那些是真的写进去了。
					// 但要说清楚是哪一条 —— 界面按 key 高亮。
					results[item.index]["error"] = err.Error()
					skipped++
					continue
				}
				updated++
			}
			return nil
		})
		if writeErr != nil {
			return nil, writeErr
		}
		c.Core.mu.Lock()
		delete(c.Core.list, name)
		c.Core.mu.Unlock()
	}
	c.Log("info", fmt.Sprintf("subscription.set：实例 %s，改 %d 条，跳过 %d 条", name, updated, skipped))
	return map[string]any{"updated": updated, "skipped": skipped, "results": results}, nil
}

// anyItemNeedsLookup 判断这批改动里是否有必须回查 listAni 的（raw 缺省的）。
func anyItemNeedsLookup(items []subscriptionSetItem) bool {
	for _, item := range items {
		if item.Raw == nil {
			return true
		}
	}
	return false
}

// applyPatch 把补丁写进一份 ana 对象，返回字段级差异。
func applyPatch(ani map[string]any, patch *subscriptionPatch) []planner.Change {
	if patch == nil {
		return nil
	}
	var changes []planner.Change
	if patch.Season != nil {
		if from := intText(ani["season"]); from != strconv.Itoa(*patch.Season) {
			changes = append(changes, planner.Change{Field: "season", From: from, To: strconv.Itoa(*patch.Season)})
		}
		ani["season"] = *patch.Season
	}
	if patch.Offset != nil {
		if from := intText(ani["offset"]); from != strconv.Itoa(*patch.Offset) {
			changes = append(changes, planner.Change{Field: "offset", From: from, To: strconv.Itoa(*patch.Offset)})
		}
		ani["offset"] = *patch.Offset
	}
	if patch.StandbyOffset != nil {
		entries, _ := ani["standbyRssList"].([]any)
		for i, item := range entries {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if from := intText(m["offset"]); from != strconv.Itoa(*patch.StandbyOffset) {
				changes = append(changes, planner.Change{
					Field: fmt.Sprintf("standbyRssList[%d].offset", i),
					From:  from,
					To:    strconv.Itoa(*patch.StandbyOffset),
				})
			}
			m["offset"] = *patch.StandbyOffset
		}
	}
	if patch.Enable != nil {
		if current, _ := ani["enable"].(bool); current != *patch.Enable {
			changes = append(changes, planner.Change{
				Field: "enable",
				From:  strconv.FormatBool(current),
				To:    strconv.FormatBool(*patch.Enable),
			})
		}
		ani["enable"] = *patch.Enable
	}
	if patch.Profile != nil {
		current := stringFieldOf(ani, "profile")
		if current != *patch.Profile {
			changes = append(changes, planner.Change{Field: "profile", From: current, To: *patch.Profile})
		}
		if *patch.Profile == "" {
			// profile: "" 表示**清除**该订阅的覆盖，不是写一个空串进去。
			delete(ani, "profile")
		} else {
			ani["profile"] = *patch.Profile
		}
	}
	return changes
}

// intText 把一个可能是数字的字段渲染成可比对的文本。
func intText(raw any) string {
	if value, ok := intValue(raw); ok {
		return strconv.Itoa(value)
	}
	return ""
}

// locateAni 按稳定 key 在列表里定位一条订阅。
func locateAni(items []assapi.Ani, key string) (assapi.Ani, bool) {
	for _, ani := range items {
		if SubscriptionKey(ani) == key {
			return ani, true
		}
	}
	return nil, false
}

// subscriptionAddPayload 是 subscription.add 的 payload。
type subscriptionAddPayload struct {
	Name          string  `json:"name"`
	BgmID         string  `json:"bgmId"`
	Title         string  `json:"title"`
	RSSURL        string  `json:"rssUrl"`
	Season        *int    `json:"season"`
	Offset        *int    `json:"offset"`
	StandbyOffset *int    `json:"standbyOffset"`
	Enable        *bool   `json:"enable"`
	Profile       *string `json:"profile"`
	Override      *bool   `json:"override"`
}

func opSubscriptionAdd(c *Call) (any, error) {
	var payload subscriptionAddPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	bgmID := config.BgmIDFromText(payload.BgmID)
	if bgmID == "" {
		bgmID = firstNonEmpty(payload.BgmID)
	}
	rssURL := firstNonEmpty(payload.RSSURL)
	title := firstNonEmpty(payload.Title)
	// 先判 payload，再找实例：顺序反了会把「什么都没给」报成「找不到实例」。
	if rssURL == "" && bgmID == "" && title == "" {
		return nil, BadRequest("rssUrl / bgmId / title 至少要有一个，否则无从解析这条订阅")
	}
	name, err := requireInstance(cfg, payload.Name)
	if err != nil {
		return nil, err
	}

	enable := false
	if payload.Enable != nil {
		enable = *payload.Enable
	}

	var draft assapi.Ani
	if rssURL != "" {
		body := map[string]any{
			"url":            rssURL,
			"bgmUrl":         bgmURLOfID(bgmID),
			"enable":         enable,
			"standbyRssList": []any{},
		}
		parsed, parseErr := app.RssToAni(c.Ctx(), cfg, name, body)
		if parseErr != nil {
			return nil, Classify(parseErr)
		}
		draft = parsed
	}
	if draft == nil {
		// 没有 RSS 地址时只能构造一份最小草稿：实例侧仍然会按 bgmUrl 认番剧。
		draft = assapi.Ani{
			"title":  title,
			"bgmUrl": bgmURLOfID(bgmID),
		}
	}
	draft["enable"] = enable
	if payload.Season != nil {
		draft["season"] = *payload.Season
	}
	if payload.Offset != nil {
		draft["offset"] = *payload.Offset
	}
	if payload.StandbyOffset != nil {
		if entries, ok := draft["standbyRssList"].([]any); ok {
			for _, item := range entries {
				if m, ok := item.(map[string]any); ok {
					m["offset"] = *payload.StandbyOffset
				}
			}
		}
	}
	if payload.Profile != nil && *payload.Profile != "" {
		draft["profile"] = *payload.Profile
	}

	override := payload.Override != nil && *payload.Override
	writeErr := c.Core.write(func() error {
		return app.AddAni(c.Ctx(), cfg, name, draft, override)
	})
	if writeErr != nil {
		return nil, Classify(writeErr)
	}
	c.Core.mu.Lock()
	delete(c.Core.list, name)
	c.Core.mu.Unlock()

	// 新增之后回读一次：实例可能补了 id / 标题 / 副 RSS 列表，
	// 界面要拿真实的那一份继续编辑（setAni 是全量覆盖）。
	if items, _, listErr := c.Core.listAni(c.Ctx(), cfg, name, true); listErr == nil {
		if found, ok := locateAddedAni(items, bgmID, title); ok {
			c.Log("info", "已新增订阅："+SubscriptionTitle(found))
			return map[string]any{"added": true, "ani": subscriptionViewOf(found)}, nil
		}
	}
	c.Log("info", "已新增订阅（实例未回读，返回草稿）："+title)
	return map[string]any{"added": true, "ani": subscriptionViewOf(draft)}, nil
}

// locateAddedAni 在回读的列表里找刚新增的那一条。
func locateAddedAni(items []assapi.Ani, bgmID, title string) (assapi.Ani, bool) {
	if bgmID != "" {
		for _, ani := range items {
			if BgmIDOf(ani) == bgmID {
				return ani, true
			}
		}
	}
	folded := app.FoldTitle(title)
	if folded != "" {
		for _, ani := range items {
			if app.FoldTitle(SubscriptionTitle(ani)) == folded {
				return ani, true
			}
		}
	}
	return nil, false
}

// subscriptionDeletePayload 是 subscription.delete 的 payload。
type subscriptionDeletePayload struct {
	Name        string   `json:"name"`
	Keys        []string `json:"keys"`
	DeleteFiles bool     `json:"deleteFiles"`
	Confirm     bool     `json:"confirm"`
}

func opSubscriptionDelete(c *Call) (any, error) {
	var payload subscriptionDeletePayload
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
	if len(payload.Keys) == 0 {
		return nil, BadRequest("payload.keys 不能为空")
	}
	if len(payload.Keys) > maxSubscriptionBatch {
		return nil, BadRequest("一次最多删 %d 条，收到 %d 条", maxSubscriptionBatch, len(payload.Keys))
	}

	items, _, listErr := c.Core.listAni(c.Ctx(), cfg, name, true)
	if listErr != nil {
		return nil, Classify(listErr)
	}
	var ids []string
	errors := []string{}
	for _, key := range payload.Keys {
		ani, ok := locateAni(items, key)
		if !ok {
			errors = append(errors, fmt.Sprintf("%s：找不到这条订阅", key))
			continue
		}
		id := stringFieldOf(ani, "id")
		if id == "" {
			errors = append(errors, fmt.Sprintf("%s：这条订阅没有 id，删不掉", key))
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return map[string]any{"deleted": 0, "errors": errors}, nil
	}
	writeErr := c.Core.write(func() error {
		return app.DeleteAni(c.Ctx(), cfg, name, ids, payload.DeleteFiles)
	})
	if writeErr != nil {
		return nil, Classify(writeErr)
	}
	c.Core.mu.Lock()
	delete(c.Core.list, name)
	c.Core.mu.Unlock()
	c.Log("info", fmt.Sprintf("已删除 %d 条订阅（实例 %s）", len(ids), name))
	return map[string]any{"deleted": len(ids), "errors": errors}, nil
}

// subscriptionResolvePayload 是 subscription.resolve 的 payload。
type subscriptionResolvePayload struct {
	Name   string `json:"name"`
	RSSURL string `json:"rssUrl"`
}

func opSubscriptionResolve(c *Call) (any, error) {
	var payload subscriptionResolvePayload
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
	rssURL := firstNonEmpty(payload.RSSURL)
	if rssURL == "" {
		return nil, BadRequest("payload.rssUrl 不能为空")
	}
	draft, err := app.RssToAni(c.Ctx(), cfg, name, map[string]any{"url": rssURL})
	if err != nil {
		return nil, Classify(err)
	}
	// 两份都给：ani 是界面直接用的视图，raw 是解析原文 ——
	// 界面把这个草稿原样交给 subscription.add 时不必再拼一次字段。
	return map[string]any{"ani": subscriptionViewOf(draft), "raw": draft}, nil
}

// bgmURLOfID 把 bgmId 拼成实例能认的番剧链接。
func bgmURLOfID(id string) string {
	id = config.BgmIDFromText(id)
	if id == "" {
		return ""
	}
	return "https://bgm.tv/subject/" + id
}
