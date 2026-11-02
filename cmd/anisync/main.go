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
	"sync"
	"syscall"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/cache"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/daemon"
	"github.com/zzzwannasleep/anisync/internal/discover"
	"github.com/zzzwannasleep/anisync/internal/httpx"
	"github.com/zzzwannasleep/anisync/internal/journal"
	"github.com/zzzwannasleep/anisync/internal/lease"
	"github.com/zzzwannasleep/anisync/internal/planner"
	"github.com/zzzwannasleep/anisync/internal/tray"
	"github.com/zzzwannasleep/anisync/internal/webui"
	"github.com/zzzwannasleep/anisync/internal/workdir"
)

// version 由构建脚本通过 -ldflags "-X main.version=..." 注入。
var version = "0.1.0"

const appName = "AniSync"

// 租约参数。
//
// TTL 必须明显大于一轮的最长耗时（同步上下文给的是 15 分钟），
// 否则「正在认真跑的那一轮」会被别人判成已死并接管，两个进程同时写。
// 续约周期压到分钟级，则进程被强杀后最多等一个 TTL 就能被别人接管。
const (
	leaseTTL        = 10 * time.Minute
	leaseRenewEvery = 2 * time.Minute
)

// syncTimeout 是一轮同步的时间上限。
const syncTimeout = 15 * time.Minute

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
	case "status":
		os.Exit(cmdStatus(os.Args[2:]))
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
  ` + appName + ` tray   [--force] [--sync-now]  常驻托盘 + 后台定时同步 + 设置页
  ` + appName + ` status [--json]            看上次同步结果、实例挂的方案、租约状态
  ` + appName + ` probe  --site mikan ...    单站点探针，排查为什么没匹配到

多套订阅方案（不同实例跑不同口味）:
  在 config.yaml 里写 profiles，每个实例用 profile: <名字> 挂一套。
  两个实例挂不同方案，主副 RSS 就各按各的规则挑；不写 profile 的实例
  仍然用顶层 rules / sources，老配置一个字节都不用改。
  用 ` + appName + ` status 可以确认每个实例到底挂在哪套方案上。

后台定时同步（无人值守）:
  sync.intervalMinutes 非 0 时，` + appName + ` tray 会按间隔自动跑。
  每轮都重新读一遍 config.yaml 与外置规则文件 —— 在设置页改完不用重启。
  间隔也是每轮重新取的，改成 0 就停，改回非 0 会立刻跑一轮。
  同一份配置只允许一个进程在写：抢不到租约时 ` + appName + ` tray 会直接报错退出，
  确认上一个进程已经死了就加 --force 接管。

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
  data/runs.jsonl     最近若干轮同步的结果（status 命令读它）
  data/sync.lock      单飞租约，防止两个进程同时写同一批实例
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
	// 实例挂的是哪套方案必须当场看见：它决定了后面每一行「按规则挑了哪个组」。
	for _, instance := range cfg.Instances {
		scheme := cfg.SchemeFor(instance.Name)
		profile := scheme.Name
		if profile == "" {
			profile = "默认（顶层 rules）"
		}
		if scheme.Missing {
			profile = "⚠ 回落（配置里写的「" + scheme.Requested + "」不存在）"
		}
		// 报实际会参与匹配的条数：空的 match 会被 rules.New 跳过。
		fmt.Printf("   · %-14s 方案 %s ｜ %s ｜ 白名单 %d / 黑名单 %d ｜ 副 RSS ≤ %d\n",
			instance.Name, profile, strings.Join(scheme.SourceOrder, ">"),
			scheme.EffectivePrimary(), scheme.EffectiveDeny(), scheme.MaxStandby)
	}

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
		Sources:   cfg.Discover.SourceList(),
		BgmUser:   cfg.Discover.BgmUser,
		Days:      cfg.Discover.WindowDays(),
		JapanOnly: cfg.Discover.JapanOnlyEnabled(),
		Proxy:     cfg.ResolvedProxy(),
		Timeout:   time.Duration(cfg.Sources.TimeoutSeconds) * time.Second,
		Store:     store,
		Exclude:   cfg.Discover.Exclude,
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
	force := fs.Bool("force", false, "忽略已有租约强制写入（确认没有别的进程在写）")
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

	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()

	// 写入前就要拿到租约，而不是「算完计划再抢」。
	//
	// 只保护「写」这一段是不够的：sync 在 T0 读 listAni、算出目标状态，
	// 如果 tray 在 T0→T2 之间写过，sync 在 T2 就会用 T0 的旧视图整份覆盖回去
	// —— 正是租约要防的那件事，只是窗口窄一点。所以读-改-写整段都要在锁里。
	//
	// 只读路径（--preview / 配置里的 preview）不抢：它们不写，没必要互相排队。
	willWrite := *yes && !*preview && !cfg.Sync.Preview
	var hold *lease.Lease
	if willWrite {
		hold, err = lease.Acquire(leasePath(), leaseTTL, "sync", *force)
		if err != nil {
			return fail(err)
		}
		defer func() {
			if releaseErr := hold.Release(); releaseErr != nil {
				fmt.Fprintf(os.Stderr, "⚠ 交还租约时发现异常：%v\n", releaseErr)
			}
		}()
	}

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

	// 写入的租约在上面就已经拿到了：读-改-写整段互斥。
	outcome := daemon.Outcome{Writable: writable, Skipped: len(plan.Skipped)}
	updated, added, errs := planner.Apply(ctx, cfg, plan)
	outcome.Updated, outcome.Added, outcome.Errors = updated, added, errs
	recordRun("cli", outcome, nil)

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
	force := fs.Bool("force", false, "忽略已有租约强制接管（确认上一个进程已经不再写入了再用）")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	// serve 也能在设置页里点「立即同步」，所以它同样是一个写者。
	// 一个配置只允许一个进程在写：这条不变量在这里也要守住。
	hold, err := lease.Acquire(leasePath(), leaseTTL, "serve", *force)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = hold.Release() }()
	stopKeepAlive := hold.KeepAlive(leaseRenewEvery, func(lostErr error) {
		fmt.Fprintf(os.Stderr, "✘ %v\n这份配置被另一个 AniSync 接管了，请关掉重复的那个进程。\n", lostErr)
	})
	defer stopKeepAlive()

	addr := *listen
	if addr == "" {
		addr = cfg.Listen
	}
	server := webui.New(cfg)
	// serve 不跑定时任务（要无人值守请用 tray），但设置页里手点的同步
	// 照样要重读配置、串行化、记进运行日志 —— 否则「点了一次同步」在
	// data/runs.jsonl 里查不到，排查时会出现一段无法解释的空白。
	var serveMu sync.Mutex
	server.SetSyncFunc(newSyncEndpoint(func() (*config.Config, error) {
		fresh, err := config.Load(*path)
		if err != nil {
			return nil, err
		}
		// serve 模式没有常驻循环帮着兜底，点「同步」时更要在页面上直接报出配置错误，
		// 而不是拿一份回落后的配置安静地跑一遍。
		if err := fresh.Validate(); err != nil {
			return nil, fmt.Errorf("配置校验失败: %w", err)
		}
		return fresh, nil
	}, &serveMu, "web"))
	fmt.Printf("%s %s 设置页: http://%s\n配置文件: %s\n", appName, version, addr, cfg.Path())
	fmt.Printf("提示：`%s serve` 只提供设置页，不跑定时同步；要无人值守请用 `%s tray`。\n", appName, appName)
	if err := server.ListenAndServe(addr); err != nil {
		return fail(err)
	}
	return 0
}

// newSyncEndpoint 造一个「设置页点同步」用的入口。
//
// 三件事必须和定时任务保持一致，否则同一个按钮在两种模式下行为不同：
//   - 重新读一遍配置（页面上的 cfg 可能还没落盘，也可能被别的进程改过）；
//   - 用调用方给的锁串行化（页面上连点两下不该并发写同一批订阅）；
//   - 写一条运行日志。
func newSyncEndpoint(loadCfg func() (*config.Config, error), mu *sync.Mutex, mode string) webui.SyncFunc {
	return func(ctx context.Context) (webui.SyncResult, error) {
		fresh, err := loadCfg()
		if err != nil {
			return webui.SyncResult{}, err
		}
		if !mu.TryLock() {
			return webui.SyncResult{Errors: []string{"上一轮还没跑完，这次没有执行"}}, nil
		}
		defer mu.Unlock()
		outcome, plan, err := buildAndApply(ctx, fresh)
		recordRun(mode, outcome, err)
		return webui.SyncResult{
			Updated: outcome.Updated,
			Added:   outcome.Added,
			Errors:  outcome.Errors,
			Plan:    plan,
		}, err
	}
}

func cmdTray(args []string) int {
	fs := flag.NewFlagSet("tray", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	force := fs.Bool("force", false, "忽略已有租约强制接管（确认上一个进程已经不再写入了再用）")
	syncNow := fs.Bool("sync-now", false, "启动后立刻跑一轮，而不是先等一个间隔")
	_ = fs.Parse(args)

	// loadCfg 除了读盘还做校验。
	//
	// 常驻循环与设置页的同步都走它：`Load` 只保证 YAML 能解析，
	// 而「实例挂了一个不存在的方案」这种错是 Validate 才拦的。
	// 少了这一步，后台会拿一份回落后的配置安静地跑 —— 用错规则、不报错。
	loadCfg := func() (*config.Config, error) {
		cfg, err := config.Load(*path)
		if err != nil {
			return nil, err
		}
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("配置校验失败: %w", err)
		}
		return cfg, nil
	}
	cfg, err := loadCfg()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 单飞租约：同一份配置只允许一个进程在写。
	// ANI-RSS 的 setAni 是全量覆盖，两个进程各写一份会让主 RSS 在两个结果之间反复横跳。
	hold, err := lease.Acquire(leasePath(), leaseTTL, "tray", *force)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = hold.Release() }()
	// 发现自己已经不是持有者时必须立刻停手：那是「我被人接管了」的唯一信号。
	// 不停下来就等于两个进程在写同一批订阅，而 setAni 是全量覆盖。
	stopKeepAlive := hold.KeepAlive(leaseRenewEvery, func(lostErr error) {
		fmt.Fprintf(os.Stderr, "✘ %v\n已停止后台写入：这份配置被另一个 AniSync 接管了。\n", lostErr)
		_ = journal.Append(journalPath(), journal.Entry{
			At:    time.Now(),
			Mode:  "daemon",
			OK:    false,
			Error: lostErr.Error(),
			Note:  "租约被接管，已停止写入",
		}, journal.DefaultMaxEntries)
		cancel()
	})
	defer stopKeepAlive()

	// 托盘和 Web 设置页共用一个进程：点「打开设置」直接开浏览器
	server := webui.New(cfg)
	go func() {
		if err := server.ListenAndServe(cfg.Listen); err != nil {
			fmt.Fprintf(os.Stderr, "设置页启动失败（不影响后台同步）: %v\n", err)
		}
	}()

	// 同一进程内也只允许一轮在写：托盘菜单的「立即同步」、设置页的「立即同步」
	// 和定时那一轮是三条独立的触发路径，撞在一起就是并发写同一批订阅。
	var runMu sync.Mutex
	runOnce := func(ctx context.Context, cfg *config.Config) (daemon.Outcome, error) {
		if !runMu.TryLock() {
			// Busy 必须置上：否则调度器会把「让开」当成成功，
			// 把 lastRun 推后一整个间隔 —— 这一次真该跑的同步就此消失。
			return daemon.Outcome{Busy: true, Note: "上一轮还没跑完，这轮跳过"}, nil
		}
		defer runMu.Unlock()
		outcome, _, err := buildAndApply(ctx, cfg)
		return outcome, err
	}

	// 设置页点「立即同步」也走同一条链路：重新读配置、排同一把锁、记同一份日志。
	server.SetSyncFunc(newSyncEndpoint(loadCfg, &runMu, "web"))

	// 托盘菜单里的「立即同步」：先重读配置，再走和定时任务完全同一条链路。
	manualSync := func() (string, error) {
		fresh, err := loadCfg()
		if err != nil {
			return "", err
		}
		syncCtx, cancel := context.WithTimeout(ctx, syncTimeout)
		defer cancel()
		outcome, err := runOnce(syncCtx, fresh)
		recordRun("tray", outcome, err)
		if err != nil {
			return "", err
		}
		return summarizeOutcome(outcome), nil
	}

	go func() {
		_ = daemon.Run(ctx, daemon.Options{
			Load:     loadCfg,
			Run:      runOnce,
			Interval: intervalOf,
			// 默认先等一个间隔：双击启动就立刻重写一百多条订阅，
			// 是个会让人措手不及的惊喜。想立刻跑就用 --sync-now。
			RunOnStart: *syncNow,
			Log:        os.Stdout,
			Record: func(entry journal.Entry) {
				if appendErr := journal.Append(journalPath(), entry, journal.DefaultMaxEntries); appendErr != nil {
					fmt.Fprintf(os.Stderr, "⚠ 运行日志写入失败（这一轮的结果没被记下来）: %v\n", appendErr)
				}
			},
		})
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
		tray.Quit()
	}()

	code := tray.Run(tray.Options{
		Title:      appName,
		ConfigPath: cfg.Path(),
		Settings:   "http://" + cfg.Listen,
		Sync:       manualSync,
	})

	// 关闭顺序有讲究：先让调度循环停下来，再交还租约。
	// 反过来（先放租约再停循环）会留下一小段窗口：租约已经交还，
	// 而本进程可能还有一轮正在写 —— 正好被下一个进程接上，就是并发写。
	cancel()
	stopKeepAlive()
	_ = hold.Release()
	return code
}

// buildAndApply 是「读实例 → 建计划 → 写入」这一整轮，托盘、定时任务与设置页共用。
//
// 返回计划是为了设置页：它要把「这次到底改了什么」画出来，
// 而那条路径和定时任务走的是同一段代码，不能自己再实现一遍。
func buildAndApply(ctx context.Context, cfg *config.Config) (daemon.Outcome, *planner.Plan, error) {
	plan, err := planner.Build(ctx, cfg, planner.Options{
		AddMissing: cfg.Sync.AddMissing,
		Discover:   cfg.Discover.Enabled,
		MaxNew:     cfg.Discover.MaxNew(),
		CachePath:  cachePath(),
	})
	if err != nil {
		return daemon.Outcome{}, nil, err
	}
	outcome := daemon.Outcome{Writable: countWritable(plan), Skipped: len(plan.Skipped)}
	if outcome.Writable == 0 {
		return outcome, plan, nil
	}
	updated, added, errs := planner.Apply(ctx, cfg, plan)
	outcome.Updated, outcome.Added, outcome.Errors = updated, added, errs
	return outcome, plan, nil
}

// intervalOf 把配置里的间隔换算成 Duration；0 表示不自动跑。
func intervalOf(cfg *config.Config) time.Duration {
	if cfg.Sync.IntervalMinutes <= 0 {
		return 0
	}
	return time.Duration(cfg.Sync.IntervalMinutes) * time.Minute
}

// recordRun 给不走调度循环的触发路径（托盘菜单、命令行）补一条运行日志。
func recordRun(mode string, outcome daemon.Outcome, err error) {
	entry := journal.Entry{
		At:       time.Now(),
		Mode:     mode,
		Writable: outcome.Writable,
		Updated:  outcome.Updated,
		Added:    outcome.Added,
		Skipped:  outcome.Skipped,
		Errors:   outcome.Errors,
		Note:     outcome.Note,
		OK:       err == nil,
	}
	if err != nil {
		entry.Error = err.Error()
	}
	// 写日志失败要说出来：运行日志是无人值守时唯一的证据，
	// 而「证据写不进去」这件事本身必须有人知道。吞掉错误的后果是
	// 「记录凭空少了」——读者只会以为那几轮根本没跑。
	if appendErr := journal.Append(journalPath(), entry, journal.DefaultMaxEntries); appendErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ 运行日志写入失败（这一轮的结果没被记下来）: %v\n", appendErr)
	}
}

// summarizeOutcome 把一轮结果压成一句话，给托盘菜单用。
func summarizeOutcome(outcome daemon.Outcome) string {
	if outcome.Note != "" {
		return outcome.Note
	}
	if outcome.Writable == 0 {
		return "没有需要写入的变更"
	}
	text := fmt.Sprintf("更新 %d 条，新增 %d 条", outcome.Updated, outcome.Added)
	if len(outcome.Errors) > 0 {
		text += fmt.Sprintf("，失败 %d 条", len(outcome.Errors))
	}
	return text
}

// cmdStatus 是无人值守时唯一需要的排查入口：它不发任何网络请求，
// 只回答三件事 —— 每个实例挂在哪套方案、租约归谁、最近几轮跑成什么样。
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	asJSON := fs.Bool("json", false, "输出 JSON")
	limit := fs.Int("limit", 5, "显示最近多少轮")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}

	entries, journalErr := journal.Tail(journalPath(), *limit)
	holder, hasHolder := lease.Holder(leasePath())

	type instanceStatus struct {
		Name    string `json:"name"`
		BaseURL string `json:"baseUrl"`
		// Profile 是实际生效的方案名；空 = 顶层 rules。
		// 刻意不塞「（默认，顶层 rules）」这种展示文案：README 说 --json 是给脚本用的，
		// 脚本要判「回落」应该看 requested/missing，而不是匹配一句中文。
		Profile      string                `json:"profile"`
		ProfileLabel string                `json:"profileLabel"`
		Requested    string                `json:"requested,omitempty"`
		Missing      bool                  `json:"missing,omitempty"`
		MaxStandby   int                   `json:"maxStandby"`
		SiteOrder    []string              `json:"siteOrder"`
		RuleCount    int                   `json:"ruleCount"`
		DenyCount    int                   `json:"denyCount"`
		RuleFiles    []config.RuleFileInfo `json:"ruleFiles,omitempty"`
		Enabled      bool                  `json:"enabled"`
	}
	statuses := make([]instanceStatus, 0, len(cfg.Instances))
	for _, instance := range cfg.Instances {
		scheme := cfg.SchemeFor(instance.Name)
		label := scheme.Name
		if label == "" {
			label = "（默认，顶层 rules）"
		}
		if scheme.Missing {
			label = "回落：顶层 rules —— 配置里写的「" + scheme.Requested + "」不存在"
		}
		statuses = append(statuses, instanceStatus{
			Name:         instance.Name,
			BaseURL:      instance.BaseURL,
			Profile:      scheme.Name,
			ProfileLabel: label,
			Requested:    scheme.Requested,
			Missing:      scheme.Missing,
			MaxStandby:   scheme.MaxStandby,
			SiteOrder:    scheme.SourceOrder,
			// 报「实际会参与匹配」的条数：rules.New 会跳过 match 为空的规则，
			// 报总条数会让人以为配了 3 条、其实只有 1 条生效。
			RuleCount: scheme.EffectivePrimary(),
			DenyCount: scheme.EffectiveDeny(),
			RuleFiles: scheme.RuleFiles,
			Enabled:   instance.IsEnabled(),
		})
	}

	// 校验失败不退出：status 正是用来看「配置错在哪」的工具，
	// 拒绝对话只会让人更没法排查。但必须把话说在前面，并给非零退出码 ——
	// 因为下面这张表在配置有错时是**回落之后**的结果，不能当真相用。
	validateErr := cfg.Validate()

	if *asJSON {
		payload := map[string]any{
			"configPath": cfg.Path(),
			"version":    version,
			"instances":  statuses,
			"runs":       entries,
		}
		if validateErr != nil {
			payload["validateError"] = validateErr.Error()
		}
		if hasHolder {
			payload["lease"] = holder
		}
		if journalErr != nil {
			payload["journalError"] = journalErr.Error()
		}
		out, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(out))
		if validateErr != nil {
			return 1
		}
		return 0
	}

	fmt.Printf("配置文件: %s\n", cfg.Path())
	// 运行期数据按设计放在程序同目录，不跟 --config 走。
	// 这两行一起印出来，才不会出现「配置文件在 A、日志却在 B」的困惑。
	fmt.Printf("运行数据: %s\n", workdir.DataDir())
	fmt.Printf("程序版本: %s\n", version)
	if validateErr != nil {
		fmt.Printf("\n⚠ 配置校验没过：%v\n", validateErr)
		fmt.Printf("  下面这张表是**回落之后**的结果，不能当真相用（比如挂错方案名的实例会显示成「默认」）。\n")
	}
	interval := cfg.Sync.IntervalMinutes
	if interval > 0 {
		fmt.Printf("自动同步: 每 %d 分钟一轮\n", interval)
	} else {
		fmt.Printf("自动同步: 关（sync.intervalMinutes = 0）\n")
	}

	fmt.Println()
	fmt.Println("┌─ 实例与方案 ───────────────────────────────────────")
	for _, item := range statuses {
		flagText := ""
		if !item.Enabled {
			flagText = "（已停用）"
		}
		fmt.Printf("│ %s%s\n", item.Name, flagText)
		fmt.Printf("│   方案        %s\n", item.ProfileLabel)
		fmt.Printf("│   站点顺序    %s\n", strings.Join(item.SiteOrder, " > "))
		fmt.Printf("│   规则        %d 条白名单 / %d 条黑名单（已排除空正则），副 RSS 最多 %d 条\n",
			item.RuleCount, item.DenyCount, item.MaxStandby)
		for _, file := range item.RuleFiles {
			fmt.Printf("│   规则文件    %s（%s，%d 条）\n", filepath.Base(file.Path), file.Kind, file.Count)
		}
	}
	fmt.Println("└────────────────────────────────────────────────────")

	fmt.Println()
	if hasHolder {
		fmt.Printf("租约: 被 pid %d（%s）持有，最后一次心跳 %s 前\n",
			holder.PID, noteOr(holder.Note), time.Since(holder.Heartbeat).Round(time.Second))
		if time.Since(holder.Heartbeat) >= leaseTTL {
			fmt.Println("      已经超过 TTL，下一个 `" + appName + " tray` 会直接接管")
		}
	} else {
		fmt.Println("租约: 空闲（没有常驻进程在写）")
	}

	fmt.Println()
	if journalErr != nil {
		fmt.Printf("运行日志: 读取失败 %v\n", journalErr)
		return 1
	}
	if len(entries) == 0 {
		fmt.Printf("运行日志: 还没有记录（%s）\n", journalPath())
		fmt.Printf("  常驻跑起来之后，这里是判断「它到底有没有在跑」的唯一依据。\n")
		return statusExitCode(validateErr)
	}
	fmt.Printf("最近 %d 轮（%s）:\n", len(entries), journalPath())
	for _, entry := range entries {
		mark := "✔"
		if !entry.OK {
			mark = "✘"
		}
		// 时间一律按本机时区显示：日志里存的是带偏移的时间戳，
		// 直接 Format 会按它自己的偏移印出来，和「还有多久」对不上。
		line := fmt.Sprintf("  %s %s  %s", mark, entry.At.Local().Format("2006-01-02 15:04"), runHeadline(entry))
		fmt.Println(line)
		if entry.Panic != "" {
			fmt.Printf("      ! 崩溃已被兜住: %s\n", truncate(entry.Panic, 100))
		}
		if entry.Error != "" {
			fmt.Printf("      %s\n", truncate(entry.Error, 160))
		}
		for _, item := range entry.Errors {
			fmt.Printf("      - %s\n", truncate(item, 140))
		}
		if !entry.NextAt.IsZero() {
			fmt.Printf("      下次 %s（%s）\n", entry.NextAt.Local().Format("2006-01-02 15:04:05"), untilText(entry.NextAt))
		}
	}
	return statusExitCode(validateErr)
}

// statusExitCode 让「配置有错」这件事在退出码上也能被脚本看见。
//
// status 本身照常输出（它是排查工具，不该拒绝回答），
// 但退回非零：否则监控脚本会把「配置写错了」读成「一切正常」。
func statusExitCode(validateErr error) int {
	if validateErr != nil {
		return 1
	}
	return 0
}

// runHeadline 是一条运行日志的摘要。
func runHeadline(entry journal.Entry) string {
	if entry.Note != "" {
		return entry.Note
	}
	if !entry.OK && entry.Error != "" {
		return "失败"
	}
	text := fmt.Sprintf("更新 %d / 新增 %d（计划可写 %d）", entry.Updated, entry.Added, entry.Writable)
	if entry.Skipped > 0 {
		text += fmt.Sprintf("，跳过 %d", entry.Skipped)
	}
	return text
}

// untilText 把「还有多久」说成人话。
func untilText(at time.Time) string {
	delta := time.Until(at)
	if delta <= 0 {
		return "已到点"
	}
	return "还有 " + delta.Round(time.Second).String()
}

// noteOr 给空 note 一个占位。
func noteOr(note string) string {
	if strings.TrimSpace(note) == "" {
		return "未注明"
	}
	return note
}

// ---------- 输出 ----------

func printPlan(plan *planner.Plan, elapsed time.Duration) {
	fmt.Printf("站点数据取自实例：%s\n", plan.SiteInstance)
	if len(plan.Schemes) > 0 {
		fmt.Println("┌─ 实例与方案 ───────────────────────────────────────")
		for _, scheme := range plan.Schemes {
			profile := scheme.Profile
			if profile == "" {
				profile = "默认（顶层 rules）"
			}
			fmt.Printf("│ %-14s %s\n", scheme.Instance, profile)
			fmt.Printf("│   %-12s %s ｜ 白名单 %d 条 / 黑名单 %d 条 ｜ 副 RSS ≤ %d\n", "",
				strings.Join(scheme.SiteOrder, ">"), scheme.RuleCount, scheme.DenyCount, scheme.MaxStandby)
		}
		fmt.Println("└────────────────────────────────────────────────────")
	}
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
	// 每个实例挂了哪套方案，用来判断某条动作是不是被番剧级覆盖改过方案。
	instanceScheme := map[string]string{}
	for _, scheme := range plan.Schemes {
		instanceScheme[scheme.Instance] = scheme.Profile
	}
	fmt.Printf("\n计划：更新 %d 条 / 新增 %d 条 / 无需变更 %d 条（耗时 %s）\n\n",
		updates, adds, noops, elapsed.Round(time.Millisecond))

	for _, action := range plan.Actions {
		switch action.Kind {
		case "update":
			fmt.Printf("● [%s] %s%s%s\n", action.Instance, action.Title,
				targetNote(action), schemeNote(action, instanceScheme))
			for _, change := range action.Changes {
				fmt.Printf("    %-15s %s\n", change.Field+":", truncate(change.From, 70))
				fmt.Printf("    %-15s %s\n", "→", truncate(change.To, 70))
			}
		case "add":
			fmt.Printf("+ [%s] %s（新增）%s%s\n", action.Instance, action.Title,
				targetNote(action), schemeNote(action, instanceScheme))
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

// schemeNote 只在这条订阅的方案和实例挂的那套不一样时才说话。
//
// 番剧级覆盖是唯一能让两者不同的原因；不标出来的话，
// 「这条为什么全收了」在计划里看不出来 —— 而计划正是唯一的证据。
func schemeNote(action planner.Action, instanceScheme map[string]string) string {
	if action.Profile == "" || action.Profile == instanceScheme[action.Instance] {
		return ""
	}
	return "（按番剧覆盖方案：" + action.Profile + "）"
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

// leasePath 返回单飞租约文件路径。
func leasePath() string {
	return filepath.Join(workdir.DataDir(), "sync.lock")
}

// journalPath 返回运行日志文件路径。
func journalPath() string {
	return filepath.Join(workdir.DataDir(), "runs.jsonl")
}

// newProbeClient 给 probe 命令用的 HTTP 客户端。
func newProbeClient(proxy string, timeout time.Duration) (*http.Client, error) {
	return httpx.New(proxy, timeout)
}
