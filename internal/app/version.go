package app

import "runtime"

// Version 是程序版本。构建脚本通过 -ldflags "-X <pkg>.Version=..." 注入；
// cmd/anisync 也会把自己那份（-X main.version）同步过来，
// 保证 CLI 与桥接层报出同一个版本号。
var Version = "0.1.0"

// Schema 是操作契约的版本号（docs/ops-api.md）。
//
// 加 op、加可选字段 = 兼容；改字段名、改语义、删字段 = 破坏，必须同时升它。
const Schema = 1

// Platform 返回运行平台（GOOS）。
func Platform() string { return runtime.GOOS }

// GoVersion 返回编译期 Go 版本。
func GoVersion() string { return runtime.Version() }

// AppName 是程序显示名。
const AppName = "AniSync"
