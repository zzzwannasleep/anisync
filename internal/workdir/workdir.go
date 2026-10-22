// Package workdir 决定 AniSync 把运行期文件写在哪里。
//
// 约定：所有文件都留在程序自己所在的目录里。发布物是一个 zip，
// 解压出来就是一个自包含的文件夹，删掉文件夹就等于卸载干净：
//
//	AniSync/
//	  AniSync.exe          程序本体（单文件，无运行时依赖）
//	  config.yaml          配置
//	  data/cache.json      站点数据缓存
//
// 不往 %APPDATA%、~/.config、注册表或系统临时目录里写任何东西。
package workdir

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// ConfigName 是配置文件名。
	ConfigName = "config.yaml"
	// DataDirName 是缓存等运行期数据的目录名。
	DataDirName = "data"
)

// Dir 返回工作目录。
//
// 正常情况下就是可执行文件所在目录。两个例外：
//   - `go run` 会把二进制放进临时构建目录，此时回退到当前工作目录，免得开发时把配置写进系统临时区；
//   - macOS 的 .app 是目录包，可执行文件在 AniSync.app/Contents/MacOS/ 里，
//     配置写在包内部会被当成篡改包内容，因此上浮到 .app 所在目录。
func Dir() string {
	exe, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		if root, ok := bundleRoot(dir); ok {
			return root
		}
		if !isTempDir(dir) {
			return dir
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// ConfigPath 返回默认配置路径：<工作目录>/config.yaml。
func ConfigPath() string { return filepath.Join(Dir(), ConfigName) }

// DataDir 返回运行期数据目录：<工作目录>/data。
func DataDir() string { return filepath.Join(Dir(), DataDirName) }

// bundleRoot 识别 macOS 的 .app 包，返回包所在目录。
func bundleRoot(dir string) (string, bool) {
	sep := string(filepath.Separator)
	if !strings.HasSuffix(dir, sep+"Contents"+sep+"MacOS") {
		return "", false
	}
	app := filepath.Dir(filepath.Dir(dir))
	if !strings.HasSuffix(strings.ToLower(app), ".app") {
		return "", false
	}
	return filepath.Dir(app), true
}

// isTempDir 判断目录是否落在系统临时区或 go run 的构建目录里。
func isTempDir(dir string) bool {
	temp, err := filepath.Abs(os.TempDir())
	if err != nil || temp == "" {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if strings.EqualFold(abs, temp) {
		return true
	}
	// go run 的构建目录形如 %TEMP%\go-build1234\b001\exe
	return strings.Contains(strings.ToLower(abs), "go-build")
}
