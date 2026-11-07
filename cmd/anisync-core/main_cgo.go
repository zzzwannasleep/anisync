//go:build cgo

// Command anisync-core 是 AniSync 引擎的 c-shared 入口。
//
// 它只做一件事：把 docs/ops-api.md §1 的 5 个 C 符号接到 internal/bridge 上。
// 所有能力（op 路由器、视图形状、事件、任务）都在纯 Go 的 internal/bridge 里，
// 这一层不许长业务逻辑 —— 长了就意味着「FFI 走一遍、HTTP 节点走另一遍」。
//
// 产物（同一份源码，三个平台各一个名字）：
//
//	Windows        anisync_core.dll      与 GUI 可执行文件同目录
//	macOS          libanisync_core.dylib <exe>/../Frameworks/，回退到同目录
//	Linux / Android libanisync_core.so   Android 放进 jniLibs/<abi>/
//
// 构建（本机 Windows 示例）：
//
//	CGO_ENABLED=1 CC=<w64devkit>/bin/gcc.exe \
//	  go build -buildmode=c-shared -o build/native/windows-amd64/anisync_core.dll ./cmd/anisync-core
package main

/*
#include <stdlib.h>

// AniSyncEventFn 是事件回调：参数是一帧事件 JSON，回调必须立即返回。
typedef void (*AniSyncEventFn)(const char* eventJson);

// 在 C 侧调一次函数指针。
//
// 为什么不直接在 Go 里调：C 的函数指针类型在 Go 里不能直接当函数用，
// 必须经一次 C 侧的中转（cgo 的规则，不是设计选择）。
static void anisync_invoke_event(AniSyncEventFn fn, const char* eventJson) {
	if (fn != NULL && eventJson != NULL) {
		fn(eventJson);
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"sync"
	"unsafe"

	"github.com/zzzwannasleep/anisync/internal/app"
	"github.com/zzzwannasleep/anisync/internal/bridge"
)

// version 由构建脚本通过 -ldflags "-X main.version=..." 注入。
var version = "0.1.0"

// core 是全局唯一的桥接容器：一个进程一个。
var core = bridge.New()

// cStringAlloc / releaseString 是本包对 C 字符串的**唯一分配点与释放点**。
//
// 所有权规则（契约 §1.1）：
//   - `AniSyncCall` 返回的串：调用方读完后调 `AniSyncFree` 释放；
//   - **事件回调传过去的串：同样归调用方** —— 本库推完就再也不碰它。
//
// 为什么事件串绝不能自己释放：`NativeCallable.listener` 的语义是
// **回调体在回调返回之后才执行**（参数被投进 isolate 的消息队列）。
// 回调一返回就 free，等于让 Dart 去读一块已经释放的内存 ——
// 现象是 `toDartString()` 抛 `Utf8Codec.decode`，而且只在负载下偶发。
//
// 这两个变量不是「为了测试而留的空钩子」：`releaseString` 就是 `AniSyncFree`
// 的实现，而测试要证明的正是「事件路径**没有**走过它」——
// 一个不许发生的调用，只能靠能观察到它的那个调用点来钉住。
var (
	cStringAlloc  = func(s string) *C.char { return C.CString(s) }
	releaseString = func(p *C.char) { C.free(unsafe.Pointer(p)) }
)

// 事件回调的状态。
//
// eventMu 同时保护「回调指针」与「正在调用回调」这两件事：
// 换回调（或清空回调）必须等当前那一次调用返回，
// 否则 Dart 侧已经拆掉的那块内存会被再调一次，宿主进程当场崩。
var (
	eventMu   sync.Mutex
	eventFn   C.AniSyncEventFn
	eventStop chan struct{}
	eventDone chan struct{}
)

func main() {
	// c-shared 库不需要真正跑起来；这个 main 只是为了满足 package main 的约束。
	// Dart 侧 dlopen 之后立刻可以调用 AniSyncCall。
}

func init() {
	// 版本号报给桥接层：core.version / status.get 里的 version 必须和 CLI 一致。
	app.Version = version
}

// AniSyncCall 执行一次 op，返回 UTF-8 JSON 信封；调用方用 AniSyncFree 释放。
// 它会阻塞到该 op 完成或返回 jobId（快 op 几十毫秒，慢 op 几十秒）。
//
//export AniSyncCall
func AniSyncCall(op *C.char, payloadJSON *C.char, callID *C.char) (out *C.char) {
	defer func() {
		// panic 绝不允许穿过 CGO 边界：那会直接带走宿主进程（Flutter 的 UI 进程）。
		// internal/bridge 内部已经兜了一层，这里是第二道，
		// 而且必须**返回一封合法的错误信封** —— 返回 nil 会让 Dart 侧拿到一个空指针。
		if r := recover(); r != nil {
			out = C.CString(`{"ok":false,"error":{"code":"internal","message":"cgo 边界捕获到崩溃，进程已保住"}}`)
		}
	}()

	opName := ""
	if op != nil {
		opName = C.GoString(op)
	}
	payload := []byte{}
	if payloadJSON != nil {
		payload = []byte(C.GoString(payloadJSON))
	}
	callIDStr := ""
	if callID != nil {
		callIDStr = C.GoString(callID)
	}

	envelope := core.Handle(context.Background(), opName, payload, callIDStr)
	return cStringAlloc(string(envelope))
}

// AniSyncCancel 取消一次进行中的调用；返回 1=已取消，0=没有这个 callId。
//
//export AniSyncCancel
func AniSyncCancel(callID *C.char) C.int {
	if callID == nil {
		return 0
	}
	if core.Cancel(C.GoString(callID)) {
		return 1
	}
	return 0
}

// AniSyncSetEventCallback 设置全局事件回调；fn 为 NULL 时清除。返回 0 表示成功。
//
//export AniSyncSetEventCallback
func AniSyncSetEventCallback(fn C.AniSyncEventFn) C.int {
	stopEventPump()

	eventMu.Lock()
	eventFn = fn
	eventMu.Unlock()

	if fn == nil {
		return 0
	}
	startEventPump()
	return 0
}

// startEventPump 起一个 goroutine 把事件总线上的帧转给 C 回调。
func startEventPump() {
	events, stop := core.Events().Subscribe()
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	eventStop = stopCh
	eventDone = doneCh

	go func() {
		defer stop()
		defer close(doneCh)
		for {
			select {
			case <-stopCh:
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				raw, err := json.Marshal(event)
				if err != nil {
					continue
				}
				deliverEvent(string(raw))
			}
		}
	}()
}

// deliverEvent 把一帧事件交给 C 回调。
//
// 循环等待 eventMu：它保证「回调正在被调用时不会被换掉」。
// 回调本身必须是立即返回的（Dart 侧只负责把字符串丢进 SendPort），
// 所以这里等待的时间是微秒级，不会拖慢事件流。
//
// **这里没有 free，这是刻意的。** 见 cStringAlloc / releaseString 的注释：
// 那串内存的所有权随回调转移给调用方，由 Dart 侧读完调 AniSyncFree。
func deliverEvent(payload string) {
	eventMu.Lock()
	defer eventMu.Unlock()
	if eventFn == nil {
		return
	}
	cstr := cStringAlloc(payload)
	C.anisync_invoke_event(eventFn, cstr)
}

// stopEventPump 停掉当前推送 goroutine（可重复调用）。
func stopEventPump() {
	if eventStop == nil {
		return
	}
	close(eventStop)
	<-eventDone
	eventStop = nil
	eventDone = nil
}

// AniSyncFree 释放一个由本库分配、且所有权已转移给调用方的字符串。
//
// 两处会用到它：`AniSyncCall` 的返回值，以及**事件回调收到的那串**
// （契约 §1.1：所有权随回调转移，Dart 读成字符串之后必须调它）。
// `AniSyncCoreVersion` 返回的静态串**不要**传进来。
//
//export AniSyncFree
func AniSyncFree(p *C.char) {
	if p != nil {
		releaseString(p)
	}
}

// versionOnce / versionPtr 让 AniSyncCoreVersion 返回一个静态字符串：
// 它只被创建一次，之后一直有效，调用方不需要（也不该）释放它。
// 契约 §1 明确写了「静态字符串，不需要释放」。
var (
	versionOnce sync.Once
	versionPtr  *C.char
)

// AniSyncCoreVersion 返回引擎版本号；静态字符串，不需要释放。
//
//export AniSyncCoreVersion
func AniSyncCoreVersion() *C.char {
	versionOnce.Do(func() {
		versionPtr = C.CString(version)
	})
	return versionPtr
}
