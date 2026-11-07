package bridge

import (
	"context"

	"github.com/zzzwannasleep/anisync/internal/backup"
)

// ---------- 7.10 backup ----------

// backupCreatePayload 是 backup.create 的 payload。
type backupCreatePayload struct {
	Instance string `json:"instance"`
	Dir      string `json:"dir"`
}

func opBackupCreate(c *Call) (any, error) {
	var payload backupCreatePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	cfg, err := currentConfig(c)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, InvalidConfig(err)
	}
	dir := firstNonEmpty(payload.Dir)
	if dir != "" {
		if err := ensureDir(dir); err != nil {
			return nil, Classify(err)
		}
	}

	result, createErr := backup.Create(c.Ctx(), cfg, backup.CreateOptions{
		Instance: firstNonEmpty(payload.Instance),
		Dir:      dir,
		Progress: func(name string, count int) {
			c.Log("info", "已导出 "+name+" 的 "+itoa(count)+" 条订阅")
		},
	})
	if createErr != nil {
		return nil, Classify(createErr)
	}
	instances := make([]map[string]any, 0, len(result.Instances))
	for _, summary := range result.Instances {
		instances = append(instances, map[string]any{
			"name":  summary.Name,
			"count": summary.Count,
			"file":  summary.File,
		})
	}
	return map[string]any{"instances": instances, "dir": result.Dir}, nil
}

// backupListPayload 是 backup.list 的 payload。
type backupListPayload struct {
	Dir string `json:"dir"`
}

func opBackupList(c *Call) (any, error) {
	var payload backupListPayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	dir := firstNonEmpty(payload.Dir)
	if dir == "" {
		dir = backup.Dir()
	}
	items, err := backup.List(dir)
	if err != nil {
		return nil, Classify(err)
	}
	return map[string]any{"dir": dir, "backups": items}, nil
}

// backupRestorePayload 是 backup.restore 的 payload。
type backupRestorePayload struct {
	File     string `json:"file"`
	Prune    bool   `json:"prune"`
	Instance string `json:"instance"`
}

func opBackupRestore(c *Call) (any, error) {
	var payload backupRestorePayload
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	path := firstNonEmpty(payload.File)
	if path == "" {
		return nil, BadRequest("payload.file 不能为空")
	}
	return c.StartJob("backup.restore", func(ctx context.Context, job *Job) (any, error) {
		cfg, err := currentConfig(c)
		if err != nil {
			return nil, err
		}
		// 先确认存档读得出来，再校验配置：存档根本不存在时，
		// 「找不到这个文件」才是用户要看到的那句话。
		if _, err := backup.Read(path); err != nil {
			// detail 补上存档路径：UI 要显示「哪个文件没有」，而它不许解析 message。
			return nil, withResourceDetail(err, path)
		}
		if err := cfg.Validate(); err != nil {
			return nil, InvalidConfig(err)
		}
		job.SetMessage("正在还原存档")
		result, restoreErr := backup.Restore(ctx, cfg, backup.RestoreOptions{
			Path:     path,
			Prune:    payload.Prune,
			Instance: firstNonEmpty(payload.Instance),
		}, backup.RestoreHooks{
			MissingInstance: func(name string) {
				c.Log("warn", "配置里没有实例 "+name+"，已跳过")
			},
			Restored: func(name string, saved int) {
				job.SetMessage("已还原 " + name)
				c.Log("info", "已还原 "+name+" 的 "+itoa(saved)+" 条订阅")
			},
			PruneConfirm: func(name string, extras []string) bool {
				c.Log("info", name+" 有 "+itoa(len(extras))+" 条订阅不在存档里，将一并删除")
				// 界面走的是「确认按钮 → 这里」的路径：能走到 restore 就说明用户已经确认过。
				// 删订阅这件事本身在 CLI 上另有 -y 闸门，那条闸门留在 cmd 里。
				return true
			},
		})
		if restoreErr != nil {
			return nil, Classify(restoreErr)
		}
		return map[string]any{
			"restored": result.Restored,
			"pruned":   result.Deleted,
			"errors":   nonNilStrings(result.Errors),
		}, nil
	})
}
