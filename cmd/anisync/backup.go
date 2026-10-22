package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
	"github.com/zzzwannasleep/anisync/internal/workdir"
)

// backupVersion 是存档格式版本；以后字段变了靠它判断要不要兼容。
const backupVersion = 1

// backupFile 是一次订阅存档。
//
// 自动发现会往实例里新增订阅，动手前先留一份存档。
// 存档存的是 listAni 的原始对象，所以还原时能一条不差地 setAni 回去。
type backupFile struct {
	Version    int              `json:"version"`
	CreatedAt  string           `json:"createdAt"`
	ConfigPath string           `json:"configPath"`
	Instances  []backupInstance `json:"instances"`
}

// backupInstance 是单个实例的存档。
type backupInstance struct {
	Name    string       `json:"name"`
	BaseURL string       `json:"baseUrl"`
	Total   int          `json:"total"`
	Items   []assapi.Ani `json:"items"`
}

// cmdBackup 把每个实例当前的订阅完整导出成 JSON。
func cmdBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	instance := fs.String("instance", "", "只备份指定实例")
	outDir := fs.String("out", "", "输出目录，默认 <程序目录>/data/backup")
	_ = fs.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}

	dir := *outDir
	if dir == "" {
		dir = filepath.Join(workdir.DataDir(), "backup")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	archive := backupFile{
		Version:    backupVersion,
		CreatedAt:  time.Now().Format(time.RFC3339),
		ConfigPath: cfg.Path(),
	}
	for _, inst := range cfg.Instances {
		if !inst.IsEnabled() {
			continue
		}
		if *instance != "" && inst.Name != *instance {
			continue
		}
		client, err := newInstanceClient(cfg, inst)
		if err != nil {
			return fail(err)
		}
		items, total, err := client.ListAni(ctx)
		if err != nil {
			return fail(fmt.Errorf("[%s] 拉取订阅失败: %w", inst.Name, err))
		}
		archive.Instances = append(archive.Instances, backupInstance{
			Name:    inst.Name,
			BaseURL: inst.Resolve().BaseURL,
			Total:   total,
			Items:   items,
		})
		fmt.Printf("✔ [%s] %d 条订阅\n", inst.Name, len(items))
	}
	if len(archive.Instances) == 0 {
		return fail(fmt.Errorf("没有可备份的实例"))
	}

	raw, err := json.MarshalIndent(archive, "", " ")
	if err != nil {
		return fail(err)
	}
	target := filepath.Join(dir, "anisync-backup-"+time.Now().Format("20060102-150405")+".json")
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		return fail(err)
	}
	fmt.Printf("\n已写入存档: %s\n还原: %s restore %s\n", target, appName, filepath.Base(target))
	return 0
}

// cmdRestore 用存档把订阅改回去。
//
// 默认只做 setAni（把被改动的订阅恢复原样），不动新增出来的订阅；
// 加 --prune 才会把存档里没有的订阅删掉，也就是真正回到备份时的状态。
func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	path := fs.String("config", "", "配置文件路径")
	prune := fs.Bool("prune", false, "把存档里没有的订阅也删掉，回到备份时的状态")
	yes := fs.Bool("y", false, "跳过确认")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		return fail(fmt.Errorf("用法: %s restore [--prune] [-y] <存档文件>", appName))
	}
	archivePath := fs.Arg(0)

	raw, err := os.ReadFile(archivePath)
	if err != nil {
		return fail(err)
	}
	var archive backupFile
	if err := json.Unmarshal(raw, &archive); err != nil {
		return fail(fmt.Errorf("解析存档失败: %w", err))
	}
	if archive.Version != backupVersion {
		return fail(fmt.Errorf("存档版本 %d 与当前程序（%d）不匹配", archive.Version, backupVersion))
	}

	cfg, err := config.Load(*path)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fmt.Printf("存档时间: %s\n", archive.CreatedAt)
	restored, deleted, failures := 0, 0, 0
	var errs []string

	for _, saved := range archive.Instances {
		inst, ok := cfg.InstanceByName(saved.Name)
		if !ok {
			fmt.Printf("! 配置里没有实例 %s，跳过\n", saved.Name)
			continue
		}
		client, err := newInstanceClient(cfg, inst)
		if err != nil {
			return fail(err)
		}

		for _, item := range saved.Items {
			if err := client.SetAni(ctx, item); err != nil {
				failures++
				errs = append(errs, fmt.Sprintf("[%s] %s 还原失败: %v", saved.Name, aniTitle(item), err))
				continue
			}
			restored++
		}
		fmt.Printf("✔ [%s] 还原 %d 条\n", saved.Name, len(saved.Items))

		if !*prune {
			continue
		}
		current, _, err := client.ListAni(ctx)
		if err != nil {
			return fail(fmt.Errorf("[%s] 拉取订阅失败: %w", saved.Name, err))
		}
		wanted := map[string]bool{}
		for _, item := range saved.Items {
			if id := aniID(item); id != "" {
				wanted[id] = true
			}
		}
		var extras []string
		for _, item := range current {
			if id := aniID(item); id != "" && !wanted[id] {
				extras = append(extras, id)
			}
		}
		if len(extras) == 0 {
			continue
		}
		if !*yes {
			fmt.Printf("\n[%s] 有 %d 条订阅不在存档里，--prune 会把它们删掉（只删订阅，不删下载器里的任务）：\n", saved.Name, len(extras))
			for i, id := range extras {
				if i >= 10 {
					fmt.Printf("  …… 还有 %d 条\n", len(extras)-10)
					break
				}
				fmt.Printf("  - %s\n", id)
			}
			fmt.Printf("\n加 -y 直接执行，或去掉 --prune 只还原不删除。\n")
			return 1
		}
		if err := client.DeleteAni(ctx, extras, false); err != nil {
			failures++
			errs = append(errs, fmt.Sprintf("[%s] 删除多余订阅失败: %v", saved.Name, err))
			continue
		}
		deleted += len(extras)
		fmt.Printf("✔ [%s] 删除存档里没有的 %d 条\n", saved.Name, len(extras))
	}

	fmt.Printf("\n完成：还原 %d 条，删除 %d 条，失败 %d 条。\n", restored, deleted, failures)
	for _, e := range errs {
		fmt.Printf("  ✘ %s\n", e)
	}
	if failures > 0 {
		return 1
	}
	return 0
}

// newInstanceClient 按实例建一个客户端（自动展开 ${ENV} 并套用代理）。
func newInstanceClient(cfg *config.Config, inst config.Instance) (*assapi.Client, error) {
	resolved := inst.Resolve()
	return assapi.New(resolved.BaseURL, resolved.APIKey, cfg.ResolvedProxy(), 30*time.Second)
}

func aniID(ani assapi.Ani) string {
	if id, ok := ani["id"].(string); ok {
		return id
	}
	return ""
}

func aniTitle(ani assapi.Ani) string {
	for _, key := range []string{"title", "mikanTitle", "id"} {
		if value, ok := ani[key].(string); ok && value != "" {
			return value
		}
	}
	return "（无标题）"
}
