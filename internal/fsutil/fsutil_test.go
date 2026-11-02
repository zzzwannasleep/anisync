package fsutil

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestAtomicWriteFileReplacesContent 是基本契约：写进去的就是读出来的。
func TestAtomicWriteFileReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "x.json")
	for _, want := range []string{"第一版", "第二版（更长一点的内容）", "第三版"} {
		if err := AtomicWriteFile(path, []byte(want), 0o600); err != nil {
			t.Fatalf("写 %q 失败: %v", want, err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读失败: %v", err)
		}
		if string(got) != want {
			t.Fatalf("内容不对：期望 %q，实际 %q", want, string(got))
		}
	}
	// 目录不存在时自动建。
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("应该自动建目录: %v", err)
	}
}

// TestAtomicWriteFileNeverLeavesPartialContent 是这条函数唯一真正的承诺。
//
// 并发写同一路径不是推荐用法（调用方该自己加锁），但只要发生了，
// 读的人也不能读到空文件或写了一半的内容 —— 那是最难解释的一种现象
// （「运行日志突然空了」）。这里让一个读者一直读、多个写者一直写，
// 断言读者看到的每一版都是某一次完整写入的内容。
func TestAtomicWriteFileNeverLeavesPartialContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs.jsonl")
	payloads := []string{
		"{\"a\":1}\n",
		"{\"a\":11}\n{\"b\":22}\n",
		"{\"c\":333}\n",
	}
	want := map[string]bool{payloads[0]: true, payloads[1]: true, payloads[2]: true}
	if err := AtomicWriteFile(path, []byte(payloads[0]), 0o600); err != nil {
		t.Fatalf("初始写失败: %v", err)
	}

	var (
		wg   sync.WaitGroup
		stop = make(chan struct{})
		bad  []string
		mu   sync.Mutex
	)
	// 读者：不允许读到空内容或非预期内容。
	// 用 ReadFile（带 Windows 占用重试）而不是 os.ReadFile：在生产里
	// 「文件正被 rename 覆盖的那一瞬，打开它也会失败」，裸读会假失败。
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			raw, err := ReadFile(path)
			if err != nil {
				mu.Lock()
				bad = append(bad, "读失败: "+err.Error())
				mu.Unlock()
				return
			}
			if len(raw) == 0 {
				mu.Lock()
				bad = append(bad, "读到了空文件")
				mu.Unlock()
				return
			}
			if !want[string(raw)] {
				mu.Lock()
				bad = append(bad, "读到了写了一半的内容: "+string(raw))
				mu.Unlock()
				return
			}
		}
	}()

	var writers sync.WaitGroup
	for i := 0; i < 6; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			for round := 0; round < 30; round++ {
				_ = AtomicWriteFile(path, []byte(payloads[(i+round)%len(payloads)]), 0o600)
			}
		}(i)
	}
	writers.Wait()
	close(stop)
	wg.Wait()

	if len(bad) > 0 {
		t.Fatalf("并发读写期间出现了不该出现的内容：%v", bad)
	}
	// 收尾之后文件必须仍然是某一次完整写入的内容。
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || !want[string(raw)] {
		t.Fatalf("收尾后文件内容异常: %q（err=%v）", string(raw), err)
	}
}
