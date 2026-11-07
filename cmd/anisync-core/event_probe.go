//go:build cgo

package main

// 事件字符串所有权的**测试探针**。
//
// 为什么它不是一个 _test.go：**cgo 不支持在测试文件里 `import "C"`**
// （`go test` 会直接报 `use of cgo in test ... not supported`）。
// 而这条测试要证明的正是 C 侧的行为 —— 回调只存指针、返回之后才由调用方读，
// 所以 C 的那一半必须待在一个普通文件里，测试文件（无 cgo）通过下面这几个
// Go 包装函数来驱动它。
//
// 生产路径不会调用这里的任何函数：它们只在 `go test ./cmd/anisync-core` 里被用到。
// 之所以接受这点体积，是因为「回调返回之后指针仍然可读」这件事
// **只能在真正的 C 回调时序下证明**，用 Go 假替身证明不了。

/*
#include <stddef.h>

// 与 main_cgo.go 的 preamble 里那个**逐字相同**。cgo 的函数指针类型按「文件自己的
// preamble」解析（另一个文件里定义的 C 名字在 Go 侧看不见），所以这里必须再写一遍；
// C11 允许同名同类型的 typedef 重复声明，两个 preamble 拼进同一个翻译单元也不会冲突。
typedef void (*AniSyncEventFn)(const char* eventJson);

// 复刻 NativeCallable.listener 的时序：回调体只把参数存下来、立刻返回，
// **不读那串内存**。真 Dart 回调做的事就是「存下来、过一会儿在 isolate 里读」。
static const char* anisync_probe_stash = NULL;
static int anisync_probe_hits = 0;

static void anisync_probe_cb(const char* eventJson) {
	if (anisync_probe_hits == 0) {
		anisync_probe_stash = eventJson;
	}
	anisync_probe_hits++;
}

static const char* anisync_probe_take(void) { return anisync_probe_stash; }
static int anisync_probe_hit_count(void) { return anisync_probe_hits; }
static void anisync_probe_reset(void) { anisync_probe_stash = NULL; anisync_probe_hits = 0; }

// C 的函数指针在 Go 里不能直接取地址，所以走一次 C 侧中转。
static AniSyncEventFn anisync_probe_cb_value(void) { return anisync_probe_cb; }
*/
import "C"

import (
	"sync"
	"unsafe"
)

// probeInstallCallback 把事件回调换成「只存不读」的探针回调，并清空计数。
func probeInstallCallback() int {
	return int(AniSyncSetEventCallback(C.anisync_probe_cb_value()))
}

// probeUninstallCallback 清掉回调（会停掉事件泵并等它退出）。
func probeUninstallCallback() {
	AniSyncSetEventCallback(nil)
}

// probeReset 清空暂存的指针与命中计数。
func probeReset() { C.anisync_probe_reset() }

// probeHits 返回回调被调到的次数。
func probeHits() int { return int(C.anisync_probe_hit_count()) }

// probeReadStashed 读回「回调当时存下的那串内存」。
//
// 这一步是整条测试的重点：它发生在**回调返回之后**，
// 也就是 Dart 真正去读那串内存的时刻。
func probeReadStashed() (string, bool) {
	p := C.anisync_probe_take()
	if p == nil {
		return "", false
	}
	return C.GoString(p), true
}

// probeFreeStashed 用契约里给 Dart 的那条路（AniSyncFree）释放暂存的字符串。
func probeFreeStashed() bool {
	p := C.anisync_probe_take()
	if p == nil {
		return false
	}
	AniSyncFree(p)
	return true
}

// probeInterceptStrings 把分配/释放钩子换成计数器，返回三个普通 Go 闭包。
//
// 放在这个 cgo 文件里而不是测试文件里：钩子的签名带 `*C.char`，
// 而测试文件不能 `import "C"`。测试侧只看到 int / []string，边界干净。
func probeInterceptStrings() (restore func(), allocCount func() int, releasedCount func() int) {
	realAlloc := cStringAlloc
	realRelease := releaseString

	var mu sync.Mutex
	allocs := 0
	frees := 0

	cStringAlloc = func(s string) *C.char {
		mu.Lock()
		allocs++
		mu.Unlock()
		return realAlloc(s)
	}
	releaseString = func(p *C.char) {
		mu.Lock()
		frees++
		mu.Unlock()
		realRelease(p)
	}
	return func() {
			cStringAlloc = realAlloc
			releaseString = realRelease
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return allocs
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return frees
		}
}

// 让编译器确认 unsafe 仍被本文件使用（C 函数指针转换需要它）。
var _ = unsafe.Pointer(nil)
