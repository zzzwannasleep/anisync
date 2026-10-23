package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/source"
)

// cmdProbe 单站点探针：只跑一个来源，把候选和错误原样打出来，用来排查「为什么没匹配到」。
func cmdProbe(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径（用其中的 proxy 与站点数据来源实例）")
	site := fs.String("site", "mikan", "站点: anibt / animegarden / mikan / nyaa")
	bgm := fs.String("bgm", "", "bgm.tv 条目 id")
	mikanID := fs.String("mikan", "", "mikan bangumi id")
	title := fs.String("title", "", "中文标题")
	jp := fs.String("jp", "", "日文标题")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	timeout := time.Duration(cfg.Sources.TimeoutSeconds) * time.Second

	httpClient, err := newProbeClient(cfg.ResolvedProxy(), timeout)
	if err != nil {
		return fail(err)
	}

	var siteClient *assapi.Client
	if name := cfg.SiteInstance(); name != "" {
		instance, ok := cfg.InstanceByName(name)
		if !ok {
			return fail(fmt.Errorf("找不到站点数据来源实例 %s", name))
		}
		resolved := instance.Resolve()
		siteClient, err = assapi.New(resolved.BaseURL, resolved.APIKey, cfg.ResolvedProxy(), timeout)
		if err != nil {
			return fail(err)
		}
	}

	query := source.Query{BgmID: *bgm, MikanID: *mikanID, Title: *title, JPTitle: *jp, NyaaQuery: *jp}
	fmt.Printf("probe %s  bgmId=%q mikanId=%q title=%q jp=%q\n", *site, query.BgmID, query.MikanID, query.Title, query.JPTitle)

	ctx, cancel := context.WithTimeout(context.Background(), timeout*4)
	defer cancel()

	registry := source.NewRegistry(siteClient, httpClient, timeout, []string{*site}, nil)
	for _, result := range registry.Collect(ctx, query, false) {
		if result.Err != nil {
			fmt.Printf("✘ [%s] %v\n", result.Site, result.Err)
		}
		fmt.Printf("[%s] %d 个候选\n", result.Site, len(result.Candidates))
		for _, candidate := range result.Candidates {
			fmt.Printf("  - %-24s 热度=%-4d 标题样本=%-3d %s\n",
				candidate.Label, candidate.Weight, len(candidate.Titles), candidate.URL)
			// 组名这一侧的全部可匹配名字：显示名 + 标题方括号里抠出来的别名。
			// 规则里写 in: group 时，写这里面任何一个都能命中这个候选。
			if len(candidate.GroupAliases) > 0 {
				fmt.Printf("      组名可写: %s\n", strings.Join(candidate.GroupAliases, " / "))
			}
		}
	}
	return 0
}
