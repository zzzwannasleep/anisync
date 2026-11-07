//go:build !windows

package config

// replaceWindowIsTransient 在 POSIX 上为 false。
//
// rename(2) 是原子的：目标名字要么指向旧文件、要么指向新文件，不会消失。
// 所以那边读不到就是真的读不到 —— 这条测试在 POSIX 上只覆盖契约
// （不 panic、不返回半截配置），不伪造一个不存在的窗口。
func replaceWindowIsTransient() bool { return false }
