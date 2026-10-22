// Command AniSync 是多 ANI-RSS 实例的主副 RSS 自动打点工具。
//
// 同一个二进制既是 CLI（Linux / Windows / macOS），也是托盘程序（Windows 通知区域、macOS 菜单栏），
// 还内置一个本地 Web 设置页。所有运行期文件都放在程序同目录，删掉文件夹即卸载干净。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/discover"
	"github.com/zzzwannasleep/anisync/internal/httpx"
	"github.com/zzzwannasleep/anisync/internal/planner"
	"github.com/zzzwannasleep/anisync/internal/tray"
	"github.com/zzzwannasleep/anisync/internal/webui"
	"github.com/zzzwannasleep/anisync/internal/workdir"
)

// version 由构建脚本通过 -ldflags "-X main.version=..." 注入。
var version = "0.1.0"

const appName = "AniSync"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "plan":
		os.Exit(cmdPlan(os.Args[2:]))
	case "sync":
		os.Exit(cmdSync(os.Args[2:]))
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	case "tray":
		os.Exit(cmdTray(os.Args[2:]))
	case "check":
		os.Exit(cmdCheck(os.Args[2:]))
	case "discover":
		os.Exit(cmdDiscover(os.Args[2:]))
	case "backup":
		os.Exit(cmdBackup(os.Args[2:]))
	case "restore":
		os.Exit(cmdRestore(os.Args[2:]))
	case "probe":
		os.Exit(cmdProbe(os.Args[2:]))
	case "init":
		os.Exit(cmdInit(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("%s %s\n", appName, version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(appName + ` —— 多 ANI-RSS 实例的主副 RSS 自动打点

用法:
  ` + appName + ` init                       生成默认配置文件
  ` + appName + ` check                      校验配置并测试每个实例连通性
  ` + appName + ` discover                   自动扫一遍现在有什么番剧在放（只读，不写任何东西）
  ` + appName + ` backup                     把每个实例当前的订阅导出成存档
  ` + appName + ` restore [--prune] [-y] <存档>  用存档把订阅改回去
  ` + appName + ` plan   [--json]            只生成打点计划（不写任何东西）
  ` + appName + ` sync   [--preview] [-y]    生成并执行计划
  ` + appName + ` serve  [--listen addr]     启动本地 Web 设置页
  ` + appName + ` tray                       常驻托盘 + 后台按配置间隔自动爬取
  ` + appName + ` probe  --site mikan ...    单站点探针，排查为什么没匹配到

自动发现（不用打开 ANI-RSS 的 WebUI 加订阅）:
  配置里写 discover.enabled: true，或者给 plan / sync 加 --discover。
  发现出来的番剧会被当成自动生成的 targets：实例里没有的就新建，默认不启用，
  看一眼计划确认没问题，再把 discover.enable 改成 true 放手。

通用参数:
  --config <path>     配置文件路径（默认 ` + config.DefaultPath() + `）
  --instance <name>   只处理指定实例

环境变量:
  ANISYNC_PROXY       代理地址，例如 http://127.0.0.1:7890
  配置里的 apiKey 支持 ${ENV_NAME} 写法

运行期文件（都放在程序同目录，不往别处写）:
  config.yaml         配置
  data/cache.json     站点数据缓存
  data/backup/        订阅存档（backup 命令生成）
`)
}

// ---------- 命令实现 ----------

func cmdInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	if _, err := os.Stat(cfg.Path()); err == nil {
		fmt.Printf("配置文件已存在，未覆盖: %s\n", cfg.Path())
		return 0
	}
	if err := cfg.Save(); err != nil {
		return fail(err)
	}
	fmt.Printf("已生成配置: %s\n接下来编辑它，填入实例和 apiKey，然后跑 `%s check`。\n", cfg.Path(), appName)
	return 0
}

func cmdCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("配置文件: %s\n", cfg.Path())
	if err := cfg.Validate(); err != nil {
		fmt.Printf("✘ 配置校验失败: %v\n", err)
		return 1
	}
	fmt.Printf("✔ 配置校验通过（%d 个实例，站点顺序 %s，站点数据取自 %s）\n",
		len(cfg.Instances), strings.Join(cfg.Sources.Order, " > "), cfg.SiteInstance())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	failed := 0
	for _, instance := range cfg.Instances {
		resolved := instance.Resolve()
		client, err := assapi.New(resolved.BaseURL, resolved.APIKey, cfg.ResolvedProxy(), 20*time.Second)
		if err != nil {
			fmt.Printf("✘ [%s] %v\n", instance.Name, err)
			failed++
			continue
		}
		data, err := client.About(ctx)
		if err != nil {
			fmt.Printf("✘ [%s] %v\n", instance.Name, err)
			failed++
			continue
		}
		items, total, err := client.ListAni(ctx)
		if err != nil {
			fmt.Printf("✘ [%s] 连接成功但拉取订阅失败: %v\n", instance.Name, err)
			failed++
			continue
		}
		fmt.Printf("✔ [%s] %s v%v，订阅 %d 条（拉取 %d）\n", instance.Name, instance.BaseURL, data["version"], total, len(items))
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// cmdDiscover 只做发现，不碰任何实例：先把「现在有什么番剧在放」列出来。
//
// 这是开箱即用的第一步——用户先看见扫到了什么，再决定要不要写进去。
func cmdDiscover(args []string) int {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	sourceFlag := fs.String("source", "", "只扫指定来源，逗号分隔：animegarden / bangumi-calendar / bangumi-collections")
	bgmUser := fs.String("bgm-user", "", "Bangumi 用户名或 uid（bangumi-collections 用）")
	days := fs.Int("days", 0, "首播日期在最近多少天内，0 用配置值")
	asJSON := fs.Bool("json", false, "输出 JSON")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	// 只校验发现这一段：没配实例也要能先看看现在有什么番剧在放。
	if err := cfg.ValidateDiscover(); err != nil {
		return fail(err)
	}

	store := cache.Open(cachePath(), time.Duration(cfg.Sources.CacheMinutes)*time.Minute)
	defer func() { _ = store.Flush() }()

	opts := discover.Options{
		Sources: cfg.Discover.SourceList(),
		BgmUser: cfg.Discover.BgmUser,
		Days:    cfg.Discover.WindowDays(),
		Proxy:   cfg.ResolvedProxy(),
		Timeout: time.Duration(cfg.Sources.TimeoutSeconds) * time.Second,
		Store:   store,
		Exclude: cfg.Discover.Exclude,
	}
	if *sourceFlag != "" {
		opts.Sources = strings.Split(*sourceFlag, ",")
	}
	if *bgmUser != "" {
		opts.BgmUser = *bgmUser
	}
	if *days > 0 {
		opts.Days = *days
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	start := time.Now()
	result, err := discover.Discover(ctx, opts)
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		out, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(out))
		return 0
	}

	fmt.Printf("发现来源：%s（首播 %d 天内，耗时 %s）\n",
		strings.Join(result.Sources, " > "), opts.Days, time.Since(start).Round(time.Millisecond))
	for _, warning := range result.Warnings {
		fmt.Printf("  ! %s\n", warning)
	}
	fmt.Printf("\n共发现 %d 部番剧：\n\n", len(result.Items))
	for i, item := range result.Items {
		jp := ""
		if item.JPTitle != "" && item.JPTitle != item.Title {
			jp = "  " + item.JPTitle
		}
		air := "          "
		if !item.AirAt.IsZero() {
			air = item.AirAt.Format("2006-01-02")
		}
		fmt.Printf("%3d. %s  [%s] %s%s\n", i+1, air, item.BgmID, truncate(item.Title, 46), jp)
	}
	fmt.Printf("\n以上只是「在放什么」。跑 `%s plan --discover` 看哪些是实例里还没有的。\n", appName)
	return 0
}

func cmdPlan(args []string) int {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	instance := fs.String("instance", "", "只处理指定实例")
	asJSON := fs.Bool("json", false, "输出 JSON")
	addMissing := fs.Bool("add-missing", false, "包含需要新增的订阅")
	discoverFlag := fs.Bool("discover", false, "自动扫一遍现在有什么番剧在放，把实例里没有的补进来")
	limit := fs.Int("limit", 0, "每个实例最多处理多少条订阅（调试用）")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	plan, err := planner.Build(ctx, cfg, planner.Options{
		OnlyInstance: *instance,
		AddMissing:   *addMissing,
		Discover:     *discoverFlag || cfg.Discover.Enabled,
		MaxNew:       cfg.Discover.MaxNew(),
		Limit:        *limit,
		CachePath:    cachePath(),
	})
	if err != nil {
		return fail(err)
	}

	if *asJSON {
		out, _ := json.MarshalIndent(plan, "", "  ")
		fmt.Println(string(out))
		return 0
	}
	printPlan(plan, time.Since(start))
	return 0
}

func cmdSync(args []string) int {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	instance := fs.String("instance", "", "只处理指定实例")
	preview := fs.Bool("preview", false, "只预览，不写入")
	yes := fs.Bool("y", false, "跳过确认")
	addMissing := fs.Bool("add-missing", false, "包含需要新增的订阅")
	discoverFlag := fs.Bool("discover", false, "自动扫一遍现在有什么番剧在放，把实例里没有的补进来")
	limit := fs.Int("limit", 0, "每个实例最多处理多少条订阅（调试用）")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	start := time.Now()
	plan, err := planner.Build(ctx, cfg, planner.Options{
		OnlyInstance: *instance,
		AddMissing:   *addMissing || cfg.Sync.AddMissing,
		Discover:     *discoverFlag || cfg.Discover.Enabled,
		MaxNew:       cfg.Discover.MaxNew(),
		Limit:        *limit,
		CachePath:    cachePath(),
	})
	if err != nil {
		return fail(err)
	}
	printPlan(plan, time.Since(start))

	writable := countWritable(plan)
	if writable == 0 {
		fmt.Println("\n没有需要写入的变更。")
		return 0
	}
	if *preview || cfg.Sync.Preview {
		fmt.Printf("\n预览：%d 条变更未写入。去掉 --preview 并加 -y 即可执行。\n", writable)
		return 0
	}
	if !*yes {
		fmt.Printf("\n将写入 %d 条变更到 ANI-RSS。加 -y 直接执行，或加 --preview 只预览。\n", writable)
		return 1
	}

	updated, added, errs := planner.Apply(ctx, cfg, plan)
	fmt.Printf("\n完成：更新 %d 条，新增 %d 条，失败 %d 条。\n", updated, added, len(errs))
	for _, e := range errs {
		fmt.Printf("  ✘ %s\n", e)
	}
	if len(errs) > 0 {
		return 1
	}
	return 0
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	listen := fs.String("listen", "", "监听地址")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	addr := *listen
	if addr == "" {
		addr = cfg.Listen
	}
	server := webui.New(cfg)
	fmt.Printf("%s %s 设置页: http://%s\n配置文件: %s\n", appName, version, addr, cfg.Path())
	if err := server.ListenAndServe(addr); err != nil {
		return fail(err)
	}
	return 0
}

func cmdTray(args []string) int {
	fs := flag.NewFlagSet("tray", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 托盘和 Web 设置页共用一个进程：点「打开设置」直接开浏览器
	server := webui.New(cfg)
	go func() {
		if err := server.ListenAndServe(cfg.Listen); err != nil {
			fmt.Fprintf(os.Stderr, "设置页启动失败: %v\n", err)
		}
	}()

	syncOnce := func() (string, error) {
		syncCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		plan, err := planner.Build(syncCtx, cfg, planner.Options{
			AddMissing: cfg.Sync.AddMissing,
			Discover:   cfg.Discover.Enabled,
			MaxNew:     cfg.Discover.MaxNew(),
			CachePath:  cachePath(),
		})
		if err != nil {
			return "", err
		}
		if countWritable(plan) == 0 {
			return "没有需要写入的变更", nil
		}
		updated, added, errs := planner.Apply(syncCtx, cfg, plan)
		return fmt.Sprintf("更新 %d 条，新增 %d 条，失败 %d 条", updated, added, len(errs)), nil
	}

	// 后台自动爬取：sync.intervalMinutes > 0 时生效，单位分钟
	if cfg.Sync.IntervalMinutes > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.Sync.IntervalMinutes) * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					message, err := syncOnce()
					if err != nil {
						fmt.Fprintf(os.Stderr, "[自动同步] 失败: %v\n", err)
						continue
					}
					fmt.Printf("[自动同步] %s\n", message)
				}
			}
		}()
		fmt.Printf("已开启后台自动同步：每 %d 分钟爬取一次\n", cfg.Sync.IntervalMinutes)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
		tray.Quit()
	}()

	return tray.Run(tray.Options{
		Title:      appName,
		ConfigPath: cfg.Path(),
		Settings:   "http://" + cfg.Listen,
		Sync:       syncOnce,
	})
}

// ---------- 输出 ----------

func printPlan(plan *planner.Plan, elapsed time.Duration) {
	fmt.Printf("站点数据取自实例：%s\n", plan.SiteInstance)
	fmt.Println("┌─ 站点爬取 ─────────────────────────────────────────")
	for _, site := range plan.Sites {
		status := fmt.Sprintf("%d 个候选", site.Candidates)
		if site.Failures > 0 {
			status += fmt.Sprintf("，%d/%d 次失败", site.Failures, site.Requests)
			if site.Error != "" {
				status += "：" + truncate(site.Error, 60)
			}
		}
		fmt.Printf("│ %-12s %s\n", site.Site, status)
	}
	fmt.Println("└────────────────────────────────────────────────────")

	if len(plan.Discovered) > 0 || len(plan.DiscoverNotes) > 0 {
		fmt.Printf("自动发现：扫到 %d 部在放的番剧", len(plan.Discovered))
		if len(plan.DiscoverNotes) > 0 {
			fmt.Printf("，%d 个来源降级", len(plan.DiscoverNotes))
		}
		fmt.Println()
		for _, note := range plan.DiscoverNotes {
			fmt.Printf("  ! %s\n", note)
		}
	}

	updates, adds, noops := 0, 0, 0
	for _, action := range plan.Actions {
		switch action.Kind {
		case "update":
			updates++
		case "add":
			adds++
		default:
			noops++
		}
	}
	fmt.Printf("\n计划：更新 %d 条 / 新增 %d 条 / 无需变更 %d 条（耗时 %s）\n\n",
		updates, adds, noops, elapsed.Round(time.Millisecond))

	for _, action := range plan.Actions {
		switch action.Kind {
		case "update":
			fmt.Printf("● [%s] %s%s\n", action.Instance, action.Title, targetNote(action))
			for _, change := range action.Changes {
				fmt.Printf("    %-15s %s\n", change.Field+":", truncate(change.From, 70))
				fmt.Printf("    %-15s %s\n", "→", truncate(change.To, 70))
			}
		case "add":
			fmt.Printf("+ [%s] %s（新增）%s\n", action.Instance, action.Title, targetNote(action))
			for _, change := range action.Changes {
				fmt.Printf("    %-15s %s\n", change.Field+":", truncate(change.To, 70))
			}
		}
	}
	if len(plan.Skipped) > 0 {
		fmt.Printf("\n跳过 %d 条：\n", len(plan.Skipped))
		for _, skip := range plan.Skipped {
			fmt.Printf("  - [%s] %s：%s\n", skip.Instance, skip.Title, skip.Reason)
		}
	}
}

// targetNote 说明这条订阅的季度与偏移是按哪个番剧规定的。
func targetNote(action planner.Action) string {
	if action.Target == "" {
		return ""
	}
	return "（按番剧 " + action.Target + " 规定季度与偏移）"
}

func countWritable(plan *planner.Plan) int {
	count := 0
	for _, action := range plan.Actions {
		if action.Kind == "update" || action.Kind == "add" {
			count++
		}
	}
	return count
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "✘ %v\n", err)
	return 1
}

// cachePath 返回站点数据缓存文件路径（程序同目录的 data/ 下）。
func cachePath() string {
	return filepath.Join(workdir.DataDir(), "cache.json")
}

// newProbeClient 给 probe 命令用的 HTTP 客户端。
func newProbeClient(proxy string, timeout time.Duration) (*http.Client, error) {
	return httpx.New(proxy, timeout)
}
