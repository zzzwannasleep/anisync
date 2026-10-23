package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/bgm"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/httpx"
)

// YucArchive 是 Yuc 公开归档的 raw 根地址。
//
// Yuc（yuc.wiki）本身是个网页，没有 API。OpenAniBT/Yuc 这个仓库每 6 小时把它的
// 季度页抓成结构化 JSON 提交进来，所以读 raw 就等于读一份现成的当季番剧表。
var YucArchive = "https://raw.githubusercontent.com/OpenAniBT/Yuc/main/data"

// yucPage 是 Yuc 季度页里我们用得到的部分。
//
// 只取 introductions（作品介绍卡片）和 movies：这两块是「这一季要播的番」。
// lineup 是放送时间表（同一部番会重复出现），previews/specials 是前瞻和特别篇，
// 都不是订阅目标。
type yucPage struct {
	Title         string     `json:"title"`
	Introductions []yucEntry `json:"introductions"`
	Movies        []yucEntry `json:"movies"`
}

// yucEntry 是一条作品条目。titles 里中英日都有，日文原名才是 Bangumi 认的那个。
type yucEntry struct {
	Titles struct {
		Chinese  yucText `json:"chinese"`
		Original yucText `json:"original"`
	} `json:"titles"`
	Cover struct {
		URL string `json:"url"`
	} `json:"cover"`
	Broadcast struct {
		Text string `json:"text"`
	} `json:"broadcast"`
}

type yucText struct {
	Text string `json:"text"`
}

// yucQuarter 返回给定时间所属季度的 Yuc 页面代码：YYYYMM，MM 取 01 / 04 / 07 / 10。
func yucQuarter(t time.Time) string {
	month := (int(t.Month())-1)/3*3 + 1
	return fmt.Sprintf("%d%02d", t.Year(), month)
}

// yucSubjects 拉 Yuc 的当季番剧表，再拿 Bangumi 补上 bgmId。
//
// 为什么绕这一圈：Bangumi 的放送表把国漫、美漫、OVA、网络短片全混在一起，
// 而且不带地区与平台，只能逐条查详情（110 次请求）才筛得干净。Yuc 的季度页
// 本身就是一份人工整理过的日本番剧表，拿它定「这一季有什么」，再让 Bangumi
// 只负责「这部番的 bgmId 是多少」，两边各干各擅长的事。
//
// 匹配用**日文原名**而不是 Yuc 的中文译名：中文译名是 Yuc 自己的写法，
// 和 Bangumi 上的中文名经常对不上（实测中文名命中率 46%，日文原名 88%）。
func yucSubjects(ctx context.Context, client *http.Client, opts Options) ([]Item, sourceStats, error) {
	code := yucQuarter(time.Now())
	page, err := fetchYucQuarter(ctx, client, code)
	if err != nil {
		return nil, sourceStats{}, err
	}
	entries := append(append([]yucEntry(nil), page.Introductions...), page.Movies...)
	if len(entries) == 0 {
		return nil, sourceStats{}, fmt.Errorf("Yuc %s 季页面里没有作品条目", code)
	}

	// 放送表当索引：全量，不做地区过滤（Yuc 已经筛过了）。
	calendar, err := calendarIndex(ctx, client, opts.Store)
	if err != nil {
		return nil, sourceStats{}, fmt.Errorf("拉 Bangumi 放送表当索引失败: %w", err)
	}
	byName := make(map[string]Item, len(calendar)*2)
	byID := make(map[string]Item, len(calendar))
	for _, item := range calendar {
		byID[item.BgmID] = item
		if key := foldTitle(item.JPTitle); key != "" {
			byName[key] = item
		}
		if key := foldTitle(item.Title); key != "" {
			byName[key] = item
		}
	}

	items := make([]Item, 0, len(entries))
	seen := map[string]bool{}
	var stats sourceStats
	for _, entry := range entries {
		jp := strings.TrimSpace(entry.Titles.Original.Text)
		cn := strings.TrimSpace(entry.Titles.Chinese.Text)

		item, ok := byName[foldTitle(jp)]
		if !ok {
			item, ok = byName[foldTitle(cn)]
		}
		if !ok {
			item, ok = resolveBySearch(ctx, client, byID, jp, cn)
		}
		if !ok {
			// 不当作错误：Yuc 里偶尔会有 Bangumi 还没建条目的新番。
			// 但要计数并报到界面上，否则用户只会发现列表比 Yuc 上少了几部。
			stats.Unmatched++
			continue
		}
		if seen[item.BgmID] {
			continue
		}
		seen[item.BgmID] = true
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, sourceStats{}, fmt.Errorf("Yuc %s 季的 %d 个条目一个都没能在 Bangumi 上认出来", code, len(entries))
	}
	sortByDoing(items)
	return items, stats, nil
}

// fetchYucQuarter 取一个季度的 Yuc 归档 JSON。
func fetchYucQuarter(ctx context.Context, client *http.Client, code string) (*yucPage, error) {
	url := YucArchive + "/pages/" + code + ".json"
	body, status, err := httpx.Get(ctx, client, url, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, fmt.Errorf("Yuc 归档里没有 %s 这一季（%s）", code, url)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Yuc 归档返回 HTTP %d", status)
	}
	var page yucPage
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("Yuc 季度页解析失败: %w", err)
	}
	return &page, nil
}

// calendarIndex 取放送表当名字索引，带缓存。
// 缓存键和 bangumi-calendar 那条来源分开：这条要的是**未过滤**的全量。
func calendarIndex(ctx context.Context, client *http.Client, store *cache.Store) ([]Item, error) {
	const key = "bgm:calendar:raw"
	if store != nil {
		var cached []Item
		if store.Get(key, &cached) {
			return cached, nil
		}
	}
	items, err := bangumiCalendarRaw(ctx, client)
	if err != nil {
		return nil, err
	}
	if store != nil {
		store.Put(key, items)
	}
	return items, nil
}

// resolveBySearch 拿日文原名（退到中文名）去 Bangumi 检索。
//
// 放送表索引里找不到的，多半是名字写法差异大（「フールナイト」对「愚者之夜」），
// 这时只能搜。搜到之后还要再比一次名字 —— 只信第一条会把「顶点武装」认成
// 「超兽武装之仁者无敌」。
func resolveBySearch(ctx context.Context, client *http.Client, byID map[string]Item, keywords ...string) (Item, bool) {
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		found, err := bgm.Search(ctx, client, keyword, 5)
		if err != nil {
			continue
		}
		want := foldTitle(keyword)

		// 在**全部**结果里挑最像的那个，而不是取第一个名字沾边的。
		// 沾边分三档（见 bestTitleRank）：续作搜索经常同时返回「X 第2期」和「X」，
		// 取第一个会把续作认成第一季。
		bestRank, bestIdx := -1, -1
		for i, subject := range found {
			if rank := bestTitleRank(want, foldTitle(subject.Name), foldTitle(subject.NameCN)); rank > bestRank {
				bestRank, bestIdx = rank, i
			}
		}
		if bestIdx < 0 || bestRank < 0 {
			continue
		}

		subject := found[bestIdx]
		// 认出来了。放送表里有同一部番的话，用它那份元数据 ——
		// 带在看人数和播出日期，搜索结果里没有。
		if item, ok := byID[subject.ID]; ok {
			return item, true
		}
		return Item{
			BgmID:   subject.ID,
			Title:   firstNonEmpty(subject.NameCN, subject.Name),
			JPTitle: strings.TrimSpace(subject.Name),
			Cover:   subject.Cover,
			Score:   subject.Score,
			AirAt:   parseTime(subject.AirDate),
		}, true
	}
	return Item{}, false
}

// bestTitleRank 给候选名字打分，用来在多个搜索结果里挑最像的那个。
//
//	2 = 完全相同
//	1 = 候选比目标更具体（候选包含目标），例如目标「X」、候选「X 第2期」
//	0 = 候选比目标更笼统（目标包含候选），例如目标「X 第2期」、候选「X」
//	-1 = 完全不沾边
//
// 0 也算命中，但要排在 1 后面：目标带「第2期」而候选只是「X」时，
// 包含关系确实成立，但它更可能是第一季那条，不该优先选。
func bestTitleRank(want string, candidates ...string) int {
	if want == "" {
		return -1
	}
	best := -1
	for _, c := range candidates {
		switch {
		case c == "":
			continue
		case c == want:
			return 2
		case strings.Contains(c, want):
			if best < 1 {
				best = 1
			}
		case strings.Contains(want, c):
			if best < 0 {
				best = 0
			}
		}
	}
	return best
}

// ---------- 标题归一化 ----------

// foldPunct 是标题里要去掉的标点。中英日三套都要覆盖：
// 同一个名字在不同来源里可能写成「魔法★探险家」「魔法・探险家」「魔法探险家」。
var foldPunct = strings.NewReplacer(
	" ", "", "\u3000", "", "\t", "",
	"!", "", "?", "", "！", "", "？", "",
	"~", "", "～", "", "〜", "",
	"・", "", "、", "", ",", "", "，", "",
	".", "", "。", "", "-", "", "—", "", "–", "", "‐", "",
	":", "", "：", "", ";", "", "；", "",
	"'", "", "\"", "", "“", "", "”", "", "‘", "", "’", "",
	"「", "", "」", "", "『", "", "』", "", "【", "", "】", "",
	"[", "", "]", "", "(", "", ")", "", "（", "", "）", "",
	"★", "", "☆", "",
)

// foldSeason 把季度的各种写法归一成「第N期」：
// Season 2 / 第2季 / 第2クール / 第2シーズン / Part 2 / 第2部分 说的是同一件事。
//
// 全部映射到「第N期」这一个形状是关键：Bangumi 常用「第2部分」，Yuc 常用「Part 2」，
// 两边各归一各的但落到同一个键上才比得起来。
var foldSeason = regexp.MustCompile(`(?:the)?season\s*(\d+)|第(\d+)(?:クール|シーズン|季|部分)|part\s*(\d+)`)

// romanFold 把罗马数字折成等价的 ASCII 字母：Ⅱ -> II、Ⅻ -> XII。
//
// 这一步不能省。Yuc 写「転生したら剣でしたⅡ」（U+2161），Bangumi 写
// 「転生したら剣でしたII」（两个 ASCII 的 I），同一个东西两种写法。
// 不折的话这两条落不到同一个键上，就会退化到检索，而检索很容易认成第一季 ——
// 实测踩过：用户已经订了「转生就是剑 第二季」，界面却又列出一条「转生就是剑」。
//
// 折成 ASCII 而不是直接折成阿拉伯数字，是为了和 NFKC 的行为对齐：
// NFKC("Ⅱ") == "II"，两边走同一条路才会落到同一个键上。
var romanFold = strings.NewReplacer(
	"Ⅰ", "I", "Ⅱ", "II", "Ⅲ", "III", "Ⅳ", "IV", "Ⅴ", "V", "Ⅵ", "VI",
	"Ⅶ", "VII", "Ⅷ", "VIII", "Ⅸ", "IX", "Ⅹ", "X", "Ⅺ", "XI", "Ⅻ", "XII",
	"ⅰ", "i", "ⅱ", "ii", "ⅲ", "iii", "ⅳ", "iv", "ⅴ", "v", "ⅵ", "vi",
	"ⅶ", "vii", "ⅷ", "viii", "ⅸ", "ix", "ⅹ", "x", "ⅺ", "xi", "ⅻ", "xii",
)

// foldTitle 把标题压成可比较的形式。
//
// 五步：全角折半角 -> 罗马数字折 ASCII -> 转小写 -> 去标点空白 -> 季度写法归一。
// 这样「魔法の姉妹ルルットリリィ 第2クール」和「魔法姉妹ルルットリリィ 第2期」
// 会落到同一个键上。
//
// 它只管**写法**差异（大小写、全角半角、罗马数字、标点、季度的说法），
// 不管语言差异：「佐々木とピーちゃん」和「佐佐木与文鸟小哔」压不到一起，
// 那种情况靠放送表索引里的中文名，或者检索兜底。
func foldTitle(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	// 全角 ASCII（Ａ１！）折回半角，省得同一串字符有两种写法
	var b strings.Builder
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		b.WriteRune(r)
	}
	out := romanFold.Replace(b.String())
	out = strings.ToLower(out)
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = foldPunct.Replace(out)
	out = foldSeason.ReplaceAllStringFunc(out, func(m string) string {
		groups := foldSeason.FindStringSubmatch(m)
		for i := 1; i < len(groups); i++ {
			if groups[i] != "" {
				return "第" + groups[i] + "期"
			}
		}
		return m
	})
	return out
}
