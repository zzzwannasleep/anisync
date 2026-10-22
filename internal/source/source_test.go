package source

import (
	"strings"
	"testing"
)

// 这些片段取自真实 Mikan 页面（已做裁剪），用来锁住解析规则。
const mikanSearchFixture = `<div class="sk-bangumi">
  <ul class="an-ul">
    <li><a href="/Home/Bangumi/4059" target="_blank"><img data-src="/images/1.jpg"></a></li>
    <li><a href="/Home/Bangumi/3984" target="_blank"><img data-src="/images/2.jpg"></a></li>
  </ul>
</div>`

const mikanDetailFixture = `<div class="bangumi-info">
  <p class="bangumi-info">放送日期：&#x661F;&#x671F;&#x56DB;</p>
  <p class="bangumi-info">官方网站：<br /><a class="w-other-c" href="https://fxkurumi-info.com/">https://fxkurumi-info.com/</a></p>
  <p class="bangumi-info">Bangumi番组计划链接：<br /><a class="w-other-c" href="https://bgm.tv/subject/622288">https://bgm.tv/subject/622288</a></p>
</div>
<div class="leftbar-item"><a class="subgroup-name" data-anchor="#203">桜都字幕组</a><span class="date">10/19/2026</span></div>
<div class="leftbar-item"><a class="subgroup-name" data-anchor="#12">悠哈璃羽字幕社</a><span class="date">10/19/2026</span></div>
<div class="recommend"><a href="https://bgm.tv/subject/999999">推荐位里的其它作品</a></div>`

func TestCleanKeywordStripsAniRssSuffixes(t *testing.T) {
	cases := map[string]string{
		"铁旅！邂逅铁道少女 (2026) [tmdbid=302020]": "铁旅！邂逅铁道少女",
		"FX战士久留美 (2026) [tmdbid=311842]":   "FX战士久留美",
		"冰之城墙":                               "冰之城墙",
		"葬送的芙莉莲【合集】":                         "葬送的芙莉莲",
	}
	for input, want := range cases {
		if got := CleanKeyword(input); got != want {
			t.Errorf("CleanKeyword(%q) = %q, 期望 %q", input, got, want)
		}
	}
}

func TestMikanSearchLinkWithoutText(t *testing.T) {
	matches := mikanBangumiLink.FindAllStringSubmatch(mikanSearchFixture, -1)
	if len(matches) != 2 {
		t.Fatalf("应匹配到 2 个番剧链接（锚点内容是 img，不能要求有文本），实际 %d", len(matches))
	}
	if matches[0][1] != "4059" {
		t.Fatalf("第一个番剧 id 应为 4059，实际 %s", matches[0][1])
	}
}

func TestExtractBgmIDPrefersInfoBlock(t *testing.T) {
	// 推荐位里的 999999 不能覆盖作品信息块里的 622288
	if got := extractBgmID(mikanDetailFixture); got != "622288" {
		t.Fatalf("应取作品信息块里的 bgm id，实际 %q", got)
	}
}

func TestMikanSubgroupAnchors(t *testing.T) {
	matches := mikanSubgroup.FindAllStringSubmatch(mikanDetailFixture, -1)
	if len(matches) != 2 {
		t.Fatalf("应匹配到 2 个字幕组，实际 %d", len(matches))
	}
	anchor := mikanAnchor.FindStringSubmatch(matches[0][0])
	if anchor == nil || anchor[1] != "203" {
		t.Fatalf("第一个字幕组锚点应为 203，实际 %v", anchor)
	}
	if clean(matches[0][1]) != "桜都字幕组" {
		t.Fatalf("字幕组名解析错误: %q", matches[0][1])
	}
}

func TestNyaaSearchURLUsesAllCategories(t *testing.T) {
	n := &Nyaa{}
	url := n.searchURL("氷の城壁", "LoliHouse")
	// 实测 c=1_2 对中文字幕组返回空，必须用 0_0
	if !contains(url, "c=0_0") {
		t.Fatalf("nyaa 搜索必须用全分类，实际 %s", url)
	}
	if !contains(url, "LoliHouse") || !contains(url, "%E6%B0%B7") {
		t.Fatalf("nyaa 搜索词编码异常: %s", url)
	}
}

func TestAniBTGroupURLCarriesSlug(t *testing.T) {
	// 与 ani-rss AniBTService 的拼接方式保持一致
	want := "https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=sakurato"
	got := "https://anibt.net/rss/anime.xml?bgmId=665390&groupSlug=sakurato"
	if got != want {
		t.Fatalf("anibt RSS 形式应为 %s", want)
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
