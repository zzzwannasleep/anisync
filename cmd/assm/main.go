// Command assm 是多 ANI-RSS 实例的主副 RSS 自动打点工具。
//
// 同一个二进制既是 CLI（Linux/Windows/macOS），也是托盘程序（Windows 原生托盘），
// 还内置了一个本地 Web 设置页（默认 http://127.0.0.1:7788）。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zzzwannasleep/assm/internal/assapi"
	"github.com/zzzwannasleep/assm/internal/config"
	"github.com/zzzwannasleep/assm/internal/httpx"
	"github.com/zzzwannasleep/assm/internal/planner"
	"github.com/zzzwannasleep/assm/internal/tray"
	"github.com/zzzwannasleep/assm/internal/webui"
)

var version = "0.1.0"

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
	case "probe":
		os.Exit(cmdProbe(os.Args[2:]))
	case "init":
		os.Exit(cmdInit(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Printf("assm %s\n", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Print(`assm —— 多 ANI-RSS 实例的主副 RSS 自动打点

用法:
  assm init                    生成默认配置文件
  assm check                   校验配置并测试每个实例连通性
  assm plan  [--json]          只生成打点计划（不写任何东西）
  assm sync  [--dry-run] [-y]  生成并执行计划
  assm serve [--listen addr]   启动本地 Web 设置页
  assm tray                    常驻托盘（Windows 原生托盘；其它平台打印提示）

通用参数:
  --config <path>   配置文件路径（默认 ` + config.DefaultPath() + `）
  --instance <name> 只处理指定实例

环境变量:
  ASSM_PROXY        代理地址，例如 http://127.0.0.1:7890
  配置里的 apiKey 支持 ${ENV_NAME} 写法
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
	fmt.Printf("已生成配置: %s\n接下来编辑它，填入实例和 apiKey，然后跑 `assm check`。\n", cfg.Path())
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
	fmt.Printf("✔ 配置校验通过（%d 个实例，站点顺序 %s）\n", len(cfg.Instances), strings.Join(cfg.Sources.Order, " > "))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	failed := 0
	for _, instance := range cfg.Instances {
		client, err := assapi.New(instance.BaseURL, instance.APIKey, cfg.Proxy, 20*time.Second)
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

func cmdPlan(args []string) int {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	instance := fs.String("instance", "", "只处理指定实例")
	asJSON := fs.Bool("json", false, "输出 JSON")
	addMissing := fs.Bool("add-missing", false, "包含需要新增的订阅")
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
	plan, err := planner.Build(ctx, cfg, planner.Options{OnlyInstance: *instance, AddMissing: *addMissing, Limit: *limit})
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
	dryRun := fs.Bool("dry-run", false, "只预览不写入")
	yes := fs.Bool("y", false, "跳过确认")
	addMissing := fs.Bool("add-missing", false, "包含需要新增的订阅")
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
		Limit:        *limit,
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
	if *dryRun || cfg.Sync.DryRun {
		fmt.Printf("\n[dry-run] 有 %d 条变更未执行。\n", writable)
		return 0
	}
	if !*yes {
		fmt.Printf("\n将写入 %d 条变更到 ANI-RSS。加 -y 直接执行，或加 --dry-run 只预览。\n", writable)
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
	fmt.Printf("assm %s 设置页: http://%s\n配置文件: %s\n", version, addr, cfg.Path())
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
		plan, err := planner.Build(syncCtx, cfg, planner.Options{AddMissing: cfg.Sync.AddMissing})
		if err != nil {
			return "", err
		}
		if countWritable(plan) == 0 {
			return "没有需要写入的变更", nil
		}
		updated, added, errs := planner.Apply(syncCtx, cfg, plan)
		return fmt.Sprintf("更新 %d 条，新增 %d 条，失败 %d 条", updated, added, len(errs)), nil
	}

	// 定时同步：sync.intervalMinutes > 0 时生效
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
						fmt.Fprintf(os.Stderr, "[定时同步] 失败: %v\n", err)
						continue
					}
					fmt.Printf("[定时同步] %s\n", message)
				}
			}
		}()
		fmt.Printf("已开启定时同步：每 %d 分钟一次\n", cfg.Sync.IntervalMinutes)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
		tray.Quit()
	}()

	return tray.Run(tray.Options{
		Title:      "assm",
		ConfigPath: cfg.Path(),
		Settings:   "http://" + cfg.Listen,
		Sync:       syncOnce,
	})
}

// ---------- 输出 ----------

func printPlan(plan *planner.Plan, elapsed time.Duration) {
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
			fmt.Printf("● [%s] %s\n", action.Instance, action.Title)
			for _, change := range action.Changes {
				fmt.Printf("    %-15s %s\n", change.Field+":", truncate(change.From, 70))
				fmt.Printf("    %-15s %s\n", "→", truncate(change.To, 70))
			}
		case "add":
			fmt.Printf("+ [%s] %s（新增）\n", action.Instance, action.Title)
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

// newProbeClient 给 probe 命令用的 HTTP 客户端。
func newProbeClient(proxy string, timeout time.Duration) (*http.Client, error) {
	return httpx.New(proxy, timeout)
}
