package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/zzzwannasleep/assm/internal/config"
	"github.com/zzzwannasleep/assm/internal/source"
)

// cmdProbe 单站点探针：只跑一个来源，把候选和错误原样打出来，用来排查「为什么没匹配到」。
func cmdProbe(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径（只用其中的 proxy）")
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
	client, err := newProbeClient(cfg.Proxy, timeout)
	if err != nil {
		return fail(err)
	}

	query := source.Query{BgmID: *bgm, MikanID: *mikanID, Title: *title, JPTitle: *jp, NyaaQuery: *jp}
	fmt.Printf("probe %s  bgmId=%q mikanId=%q title=%q jp=%q\n", *site, query.BgmID, query.MikanID, query.Title, query.JPTitle)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	registry := source.NewRegistry(client, timeout, []string{*site})
	for _, result := range registry.Collect(ctx, query, false) {
		if result.Err != nil {
			fmt.Printf("✘ [%s] %v\n", result.Site, result.Err)
		}
		fmt.Printf("[%s] %d 个候选\n", result.Site, len(result.Candidates))
		for _, candidate := range result.Candidates {
			fmt.Printf("  - %-24s weight=%-4d %s\n", candidate.Label, candidate.Weight, candidate.URL)
		}
	}
	return 0
}
