//go:build cgo

package main

import (
	"strings"
	"testing"
	"time"
)

// TestEventStringStaysReadableAfterCallbackReturns 是 ① 的因果证明（契约 §1.1）。
//
// 规则：事件字符串的所有权**随回调转移给调用方** —— 本库推完再也不碰它，
// Dart 侧读成字符串之后调 `AniSyncFree`。违反它的后果是 Dart 读到已释放内存
// （`toDartString()` 抛 `Utf8Codec.decode`），而且只在负载下偶发。
//
// 这条测试走的是**真路径**：真 `AniSyncSetEventCallback` → 真事件泵 → 真
// `deliverEvent` → 真 C 回调（回调只存指针就返回，复刻 `NativeCallable.listener`
// 的时序，见 event_probe.go）。它做三件事：
//
//  1. 确认事件确实经过了分配点（否则这条测试什么都没测到）；
//  2. **断言没有任何释放动作碰过这串内存** —— 确定性失败点：
//     修复前那句 `defer C.free(cstr)` 一执行，这里立刻红；
//  3. 在**回调返回之后**把内存读回来，断言内容完整 —— 也就是 Dart 那一刻会看到什么。
func TestEventStringStaysReadableAfterCallbackReturns(t *testing.T) {
	restore, allocCount, releasedCount := probeInterceptStrings()
	defer restore()

	probeReset()
	if rc := probeInstallCallback(); rc != 0 {
		t.Fatalf("设置事件回调失败: rc=%d", rc)
	}
	defer probeUninstallCallback()

	const marker = "事件字符串所有权测试"
	core.Log("info", marker)

	// 事件泵是异步的，等它把这一帧推出去。
	deadline := time.Now().Add(3 * time.Second)
	for probeHits() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("等不到事件回调：事件泵没把帧推出去")
		}
		time.Sleep(2 * time.Millisecond)
	}
	// 再等一小会儿，让「不该发生的事」来得及发生：
	// 修复前那句 free 就发生在回调返回后的微秒级。
	// 不这样的话，这条测试会靠「我检查得比它快」通过，而不是靠实现正确。
	time.Sleep(50 * time.Millisecond)

	if allocCount() == 0 {
		t.Fatal("事件路径没有经过 cStringAlloc：所有权规则根本没被执行到，这条测试没有意义")
	}

	// (2) 确定性失败点。
	if frees := releasedCount(); frees > 0 {
		t.Fatalf("事件回调路径上释放了字符串（%d 次）：NativeCallable.listener 的回调体"+
			"在回调返回之后才执行，Dart 读到的是已释放内存 —— "+
			"所有权必须随回调转移给调用方，由 Dart 侧调 AniSyncFree 释放（契约 §1.1）", frees)
	}

	// (3) 回调返回之后仍然可读，且内容完整。
	got, ok := probeReadStashed()
	if !ok {
		t.Fatal("回调没有拿到字符串指针")
	}
	if !strings.Contains(got, marker) {
		t.Errorf("回调返回之后读到的内容不完整：\n got=%q\n want 含 %q", got, marker)
	}
	if !strings.Contains(got, `"type":"log"`) || !strings.Contains(got, `"data"`) {
		t.Errorf("事件 JSON 形状不对：%q", got)
	}

	// 顺带钉住「Dart 照契约调 AniSyncFree 能真的释放它」。
	if !probeFreeStashed() {
		t.Fatal("没有可释放的探针指针")
	}
	if frees := releasedCount(); frees == 0 {
		t.Error("AniSyncFree 没有走到释放点：Dart 侧照契约调它也释放不了")
	}
	probeReset()
}

// TestEventStringAllocationIsPerEvent 钉住「每帧一条独立分配」。
//
// 顺手把「共用一块静态缓冲」这种做法挡在门外：那看起来省事，
// 但回调返回之后 Dart 才读，第二帧一写就把第一帧的内容覆盖了
// —— 症状同样是「偶发读到半截 JSON」，而且更难查。
func TestEventStringAllocationIsPerEvent(t *testing.T) {
	restore, allocCount, _ := probeInterceptStrings()
	defer restore()

	probeReset()
	if rc := probeInstallCallback(); rc != 0 {
		t.Fatalf("设置事件回调失败: rc=%d", rc)
	}
	defer probeUninstallCallback()

	for i := 0; i < 3; i++ {
		core.Log("info", "第几帧")
	}
	deadline := time.Now().Add(3 * time.Second)
	for probeHits() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("等不到 3 次回调，实际 %d 次", probeHits())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if allocCount() < 3 {
		t.Errorf("3 帧事件应该有 3 次独立分配，实际 %d 次", allocCount())
	}
	probeReset()
}
