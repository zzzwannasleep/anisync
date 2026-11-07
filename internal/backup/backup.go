// Package backup 是订阅存档：把每个实例当前的订阅导出成 JSON，必要时再导回去。
//
// 为什么值得单独成包：自动发现会往实例里新增订阅，动手前先留一份存档；
// 这份能力现在有三个入口（CLI `backup` / `restore`、桥接层的 backup.* op），
// 三个入口各写一份「怎么读、怎么比、怎么删」的话，
// 「用 CLI 能还原、用界面还原不了」这种差别迟早会出现。
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/assapi"
	"github.com/zzzwannasleep/anisync/internal/config"
)

// Version 是存档格式版本；以后字段变了靠它判断要不要兼容。
const Version = 1

// ErrCanceled 表示调用方在确认环节取消了这次还原（CLI 的「不加 -y」）。
var ErrCanceled = errors.New("已取消")

// File 是一次订阅存档。
//
// 自动发现会往实例里新增订阅，动手前先留一份存档。
// 存档存的是 listAni 的原始对象，所以还原时能一条不差地 setAni 回去。
type File struct {
	Version    int        `json:"version"`
	CreatedAt  string     `json:"createdAt"`
	ConfigPath string     `json:"configPath"`
	Instances  []Instance `json:"instances"`
}

// Instance 是单个实例的存档。
type Instance struct {
	Name    string       `json:"name"`
	BaseURL string       `json:"baseUrl"`
	Total   int          `json:"total"`
	Items   []assapi.Ani `json:"items"`
}

// InstanceSummary 是「这次存了哪个实例、几条」。
type InstanceSummary struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	File  string `json:"file,omitempty"`
}

// CreateOptions 控制一次导出。
type CreateOptions struct {
	// Instance 只备份指定实例；空表示全部启用实例。
	Instance string
	// Dir 是输出目录；空表示 <数据目录>/backup。
	Dir string
	// Progress 每备份完一个实例回调一次（给人看的进度）。
	Progress func(name string, count int)
}

// CreateResult 是一次导出的结果。
type CreateResult struct {
	// Path 是刚落盘的存档文件绝对路径。
	Path string
	// Dir 是输出目录。
	Dir string
	// Archive 是写进文件的那份内容。
	Archive File
	// Instances 是本次每个实例的条数摘要。
	Instances []InstanceSummary
}

// Dir 返回默认存档目录。
func Dir() string { return app.BackupDir() }

// Create 把每个实例当前的订阅完整导出成 JSON 文件。
func Create(ctx context.Context, cfg *config.Config, opts CreateOptions) (CreateResult, error) {
	dir := opts.Dir
	if dir == "" {
		dir = Dir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CreateResult{}, err
	}

	archive := File{
		Version:    Version,
		CreatedAt:  time.Now().Format(time.RFC3339),
		ConfigPath: cfg.Path(),
	}
	summaries := make([]InstanceSummary, 0, len(cfg.Instances))
	for _, inst := range cfg.Instances {
		if !inst.IsEnabled() {
			continue
		}
		if opts.Instance != "" && inst.Name != opts.Instance {
			continue
		}
		client, err := app.ClientFor(cfg, inst)
		if err != nil {
			return CreateResult{}, err
		}
		items, total, err := client.ListAni(ctx)
		if err != nil {
			return CreateResult{}, fmt.Errorf("[%s] 拉取订阅失败: %w", inst.Name, err)
		}
		archive.Instances = append(archive.Instances, Instance{
			Name:    inst.Name,
			BaseURL: inst.Resolve().BaseURL,
			Total:   total,
			Items:   items,
		})
		summaries = append(summaries, InstanceSummary{Name: inst.Name, Count: len(items)})
		if opts.Progress != nil {
			opts.Progress(inst.Name, len(items))
		}
	}
	if len(archive.Instances) == 0 {
		return CreateResult{}, errors.New("没有可备份的实例")
	}

	path, err := Write(dir, archive)
	if err != nil {
		return CreateResult{}, err
	}
	for i := range summaries {
		summaries[i].File = filepath.Base(path)
	}
	return CreateResult{Path: path, Dir: dir, Archive: archive, Instances: summaries}, nil
}

// Write 把一份存档写进目录，返回落盘路径。
//
// 缩进刻意是**一个空格**：存档是拿来给人肉眼比对与 diff 的，
// 已经在磁盘上的那些文件也是这个形状，换掉等于让历史存档与新存档没法直接比。
func Write(dir string, archive File) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(archive, "", " ")
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, "anisync-backup-"+time.Now().Format("20060102-150405")+".json")
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		return "", err
	}
	return target, nil
}

// Read 读一份存档并检查版本。
func Read(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var archive File
	if err := json.Unmarshal(raw, &archive); err != nil {
		return File{}, fmt.Errorf("解析存档失败: %w", err)
	}
	if archive.Version != Version {
		return File{}, fmt.Errorf("存档版本 %d 与当前程序（%d）不匹配", archive.Version, Version)
	}
	return archive, nil
}

// ArchiveInfo 是存档目录里的一条记录。
type ArchiveInfo struct {
	File      string          `json:"file"`
	Path      string          `json:"path"`
	CreatedAt string          `json:"createdAt"`
	Size      int64           `json:"size"`
	Instances []InstanceCount `json:"instances"`
}

// InstanceCount 是存档里某个实例的条数。
type InstanceCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// List 列出目录里的存档，按文件名倒序（最新的在前）。
//
// 文件名带时间戳（anisync-backup-20060102-150405.json），所以按名字排就是按时间排，
// 不必去读每个文件的 mtime —— 拷贝或解压之后 mtime 就不再可信了。
func List(dir string) ([]ArchiveInfo, error) {
	if dir == "" {
		dir = Dir()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []ArchiveInfo{}, nil
		}
		return nil, err
	}
	var out []ArchiveInfo
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		row := ArchiveInfo{File: entry.Name(), Path: path, Size: info.Size(), Instances: []InstanceCount{}}
		// 读不出来（半截文件、手工改坏）也要列出来：用户需要知道它在那儿，
		// 而不是「我明明存过一份，界面上什么都没有」。
		if archive, readErr := Read(path); readErr == nil {
			row.CreatedAt = archive.CreatedAt
			for _, inst := range archive.Instances {
				row.Instances = append(row.Instances, InstanceCount{Name: inst.Name, Count: len(inst.Items)})
			}
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File > out[j].File })
	return out, nil
}

// RestoreOptions 控制一次还原。
type RestoreOptions struct {
	// Path 是存档文件路径。
	Path string
	// Prune 把存档里没有的订阅也删掉，回到备份时的状态。
	Prune bool
	// Instance 只还原指定实例；空表示存档里的全部。
	Instance string
}

// RestoreHooks 是还原过程中的回调，用来把进度打给 CLI，或向用户要确认。
//
// 全是可选的：桥接层不传就得到一份「静默还原」。
type RestoreHooks struct {
	// MissingInstance 存档里有、但当前配置里没有的实例。
	MissingInstance func(name string)
	// Restored 一个实例的订阅还原完毕。
	Restored func(name string, saved int)
	// PruneConfirm 报告某个实例里「存档中没有」的订阅；返回 false 表示取消整次还原。
	PruneConfirm func(name string, extras []string) bool
	// Pruned 某个实例的多余订阅删除完毕。
	Pruned func(name string, deleted int)
}

// RestoreResult 是一次还原的结果。
type RestoreResult struct {
	Restored int      `json:"restored"`
	Deleted  int      `json:"deleted"`
	Failures int      `json:"failures"`
	Errors   []string `json:"errors"`
}

// Restore 用存档把订阅改回去。
//
// 默认只做 setAni（把被改动的订阅恢复原样），不动新增出来的订阅；
// Prune 才会把存档里没有的订阅删掉，也就是真正回到备份时的状态。
func Restore(ctx context.Context, cfg *config.Config, opts RestoreOptions, hooks RestoreHooks) (RestoreResult, error) {
	archive, err := Read(opts.Path)
	if err != nil {
		return RestoreResult{}, err
	}

	result := RestoreResult{Errors: []string{}}
	for _, saved := range archive.Instances {
		if opts.Instance != "" && saved.Name != opts.Instance {
			continue
		}
		inst, ok := cfg.InstanceByName(saved.Name)
		if !ok {
			if hooks.MissingInstance != nil {
				hooks.MissingInstance(saved.Name)
			}
			continue
		}
		client, err := app.ClientFor(cfg, inst)
		if err != nil {
			return result, err
		}

		for _, item := range saved.Items {
			if err := client.SetAni(ctx, item); err != nil {
				result.Failures++
				result.Errors = append(result.Errors, fmt.Sprintf("[%s] %s 还原失败: %v", saved.Name, Title(item), err))
				continue
			}
			result.Restored++
		}
		if hooks.Restored != nil {
			hooks.Restored(saved.Name, len(saved.Items))
		}

		if !opts.Prune {
			continue
		}
		current, _, err := client.ListAni(ctx)
		if err != nil {
			return result, fmt.Errorf("[%s] 拉取订阅失败: %w", saved.Name, err)
		}
		extras := Extras(current, saved.Items)
		if len(extras) == 0 {
			continue
		}
		if hooks.PruneConfirm != nil && !hooks.PruneConfirm(saved.Name, extras) {
			return result, ErrCanceled
		}
		if err := client.DeleteAni(ctx, extras, false); err != nil {
			result.Failures++
			result.Errors = append(result.Errors, fmt.Sprintf("[%s] 删除多余订阅失败: %v", saved.Name, err))
			continue
		}
		result.Deleted += len(extras)
		if hooks.Pruned != nil {
			hooks.Pruned(saved.Name, len(extras))
		}
	}
	return result, nil
}

// Extras 返回「现在有、但存档里没有」的订阅 id。
func Extras(current, saved []assapi.Ani) []string {
	wanted := map[string]bool{}
	for _, item := range saved {
		if id := ID(item); id != "" {
			wanted[id] = true
		}
	}
	var extras []string
	for _, item := range current {
		if id := ID(item); id != "" && !wanted[id] {
			extras = append(extras, id)
		}
	}
	return extras
}

// ID 取一条订阅的实例侧 id。
func ID(ani assapi.Ani) string {
	if id, ok := ani["id"].(string); ok {
		return id
	}
	return ""
}

// Title 取一条订阅的可读标题。
func Title(ani assapi.Ani) string {
	for _, key := range []string{"title", "mikanTitle", "id"} {
		if value, ok := ani[key].(string); ok && value != "" {
			return value
		}
	}
	return "（无标题）"
}
