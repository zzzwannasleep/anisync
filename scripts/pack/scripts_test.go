package main

import (
	"os"
	"path/filepath"
	"regexp"
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

// bash 的变量名一直读到「第一个不是变量名字符的字节」为止。变量后面紧跟非 ASCII
// 字符（中文标点、全角括号）时，在 C locale 下那些字节会被算进变量名 ——
// `$app（` 于是变成读一个叫 "app（" 的变量，`set -u` 下直接 unbound variable。
//
// 踩过一次：package-macos.sh 里的 echo "已生成 $app（$arch，v$version）"
// 让两个 macOS 构建任务都挂了。规则：变量后面跟非 ASCII 就加大括号。
func TestShellScriptsBraceVariablesBeforeNonASCII(t *testing.T) {
	// $name 或 $1 后面直接跟一个非 ASCII 字符
	risky := regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*[^\x00-\x7F]|\$[0-9][^\x00-\x7F]`)

	matches, err := filepath.Glob("../*.sh")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("没找到任何 .sh，路径不对？")
	}
	for _, path := range matches {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			// 整行注释跳过：注释不会真的去展开变量，而且这里正需要在注释里
			// 把踩过的写法原样写出来当警示。
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if found := risky.FindString(line); found != "" {
				t.Errorf("%s:%d 变量后面紧跟非 ASCII 字符（%q）：改成 ${...}，否则 C locale 下会被当成变量名的一部分",
					path, i+1, found)
			}
		}
	}
}
