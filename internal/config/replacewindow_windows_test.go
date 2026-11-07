//go:build windows

package config

// replaceWindowIsTransient 在 Windows 上为 true。
//
// MoveFileEx(REPLACE_EXISTING) 是「先让目标让位、再改名」：目标名字会有一小段
// 时间**根本不存在**。所以「读不到」在这里可能是「正在被替换」。
func replaceWindowIsTransient() bool { return true }
