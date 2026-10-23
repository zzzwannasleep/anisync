package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows PowerShell 把无 BOM 的 UTF-8 文件当 ANSI 读。
//
// 踩过的坑：在 build-all.ps1 里加了一行中文注释，PS 5.1 按 GBK 解出乱码之后
// 把**下一行**一起吃掉了 —— 没有语法错误，没有报错，只是 `$platform` 变成空字符串，
// 于是打包命令少了一个参数（空参数被吞掉后 `-os -arch` 被当成 `-os` 的值）。
// 这种错只能靠约定 + 测试挡住，所以这里盯着。
func TestPowerShellScriptsAreASCIIOnly(t *testing.T) {
	matches, err := filepath.Glob("../*.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("没找到任何 .ps1，路径不对？")
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
			t.Errorf("%s 带了 UTF-8 BOM —— 换成无 BOM 再试，或者干脆保持纯 ASCII", path)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for col, r := range line {
				if r > 127 {
					t.Errorf("%s:%d:%d 出现非 ASCII 字符 %q：PowerShell 5.1 会把这一行连同下一行一起读坏",
						path, i+1, col+1, r)
					break
				}
			}
		}
	}
}
