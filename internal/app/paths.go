// Package app 是 AniSync 的编排层：CLI、Web 设置页、FFI 桥三方共用的唯一实现。
//
// 放在这里的东西有一个共同点：它们都「不只是被一个入口用」。
// 一轮同步的读-改-写、方案与状态汇总、季节表判定、实例读写封装 ——
// 这些东西在 CLI、设置页、桥接层里各写一份的话，行为只会在某一条路径上悄悄漂移，
// 而漂移的代价是「同一个按钮，换个入口结果不一样」。
//
// 本包不 import 任何前端形态（webui / bridge / cmd），只依赖领域包。
package app

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/zzzwannasleep/anisync/internal/workdir"
)

// workspace 是显式指定的工作区；没有指定时全部走 workdir 的既有语义。
var (
	wsMu         sync.RWMutex
	wsConfigPath string
	wsDataDir    string
)

// SetWorkspace 钉住工作区：配置文件在哪、运行期数据放哪。
//
// configPath 为空 = 用 workdir.ConfigPath()（可执行文件同目录的 config.yaml），
// 此时什么都不改 —— 桌面端「删掉文件夹即卸载干净」靠的就是这条路径，
// 「没人调用时行为逐字节不变」也靠它。
//
// configPath 非空 = 显式切换工作区（移动端 / 桥接层的 core.init）：
//   - dataDir 为空时取 dirname(configPath)/data（与 core.init 的契约一致）；
//   - 同时把 workdir 的 root 指到 configPath 所在目录：配置文件在哪，
//     「程序同目录」这个承诺就落在哪。桌面端本来就是同一个目录，所以这一条不改变既有行为。
func SetWorkspace(configPath, dataDir string) string {
	explicit := strings.TrimSpace(configPath) != ""
	if !explicit {
		configPath = workdir.ConfigPath()
	}
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		if explicit {
			dataDir = filepath.Join(filepath.Dir(configPath), workdir.DataDirName)
		} else {
			// 显式工作区之外走既有语义：数据仍在 <程序目录>/data，
			// 不跟 --config 走（README 里那条承诺）。
			dataDir = ""
		}
	}
	wsMu.Lock()
	wsConfigPath = configPath
	wsDataDir = dataDir
	wsMu.Unlock()
	if explicit {
		workdir.SetRoot(filepath.Dir(configPath))
	}
	return configPath
}

// ConfigPath 返回生效的配置文件路径。
func ConfigPath() string {
	wsMu.RLock()
	explicit := wsConfigPath
	wsMu.RUnlock()
	if explicit != "" {
		return explicit
	}
	return workdir.ConfigPath()
}

// DataDir 返回生效的运行期数据目录。
func DataDir() string {
	wsMu.RLock()
	explicit := wsDataDir
	wsMu.RUnlock()
	if explicit != "" {
		return explicit
	}
	return workdir.DataDir()
}

// CachePath 返回站点数据缓存文件路径。
func CachePath() string { return filepath.Join(DataDir(), "cache.json") }

// LeasePath 返回单飞租约文件路径。
func LeasePath() string { return filepath.Join(DataDir(), "sync.lock") }

// JournalPath 返回运行日志文件路径。
func JournalPath() string { return filepath.Join(DataDir(), "runs.jsonl") }

// BackupDir 返回订阅存档目录。
func BackupDir() string { return filepath.Join(DataDir(), "backup") }
