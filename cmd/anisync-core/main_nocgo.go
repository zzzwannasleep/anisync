//go:build !cgo

// Command anisync-core 是 AniSync 引擎的 c-shared 入口。
//
// c-shared 动态库必须开 CGO_ENABLED=1 才能构建（Go 需要 C 工具链产出
// 平台动态库）。这个文件是**没有 cgo 时的占位**：它让
// `go build ./...` / `go test ./...` 在没有 C 编译器的机器上照样跑得动，
// 而不是在 cmd/anisync-core 这一包上直接失败 ——
// 「库需要 cgo」是事实，但不该让整棵树的测试跟着挂掉。
//
// 真正要构建动态库时：
//
//	CGO_ENABLED=1 CC=<gcc> go build -buildmode=c-shared -o anisync_core.dll ./cmd/anisync-core
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "anisync-core：本入口需要 CGO_ENABLED=1。")
	fmt.Fprintln(os.Stderr, "  它是给 Flutter 用的 c-shared 动态库，构建命令：")
	fmt.Fprintln(os.Stderr, "    CGO_ENABLED=1 go build -buildmode=c-shared -o anisync_core.dll ./cmd/anisync-core")
	fmt.Fprintln(os.Stderr, "  纯命令行用户请用 cmd/anisync（AniSync 本体）。")
	os.Exit(1)
}
