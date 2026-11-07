package bridge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 契约文档 §7 的**双向门禁**：文档里的 op 清单 ↔ 注册表。
//
// 为什么需要它（verifier 的 F1，一条结构性盲区）：`app/test/fixtures/ops.json`
// 是从注册表 dump 的，Dart 侧拿自己的常量表与它对拍。于是链条是
//
//	改文档（§7 加一个 op） → 注册表没动 → 夹具没动 → Dart 没动 → 全绿 ✗
//
// **契约文档是唯一一个能静默漂移的工件** —— 而这一路恰好反复在走这条路径
// （先改文档、再让某人实现）。没有红，不等于没有漂移。
//
// 这道门禁补的就是那一环：文档说有的 op，注册表必须有；注册表有的，文档也要写。
const (
	contractDocPath = "../../docs/ops-api.md"
	// opsSectionStart / End 把边界**写死**。
	//
	// 不去扫 §3 / §8 的正文：那里的 op 名出现在句子里（错误码表、确认闸门清单、
	// 「异步 op 的 result 形状」那段散文），扫进来全是假阳性。
	// 边界写死了才是门禁，靠关键词捞出来的是噪音。
	opsSectionStart = "### 7.1"
	opsSectionEnd   = "\n## 8"
	// contractSectionTables 是 §7 应有的子表数量（7.1 ~ 7.12）。
	// 文档重组（多一组 / 少一组）时这条会先炸 —— 那正是要人看一眼的时候。
	contractSectionTables = 12
)

// opNamePattern 是 op 名单元格的形状：反引号包起来的 identifier.identifier。
//
// 只认「整格就是这个名字」的行首列，所以 `{configPath?, dataDir?}`、
// `Config.Save()` 这类 payload / data 单元格不会被误收。
var opNamePattern = regexp.MustCompile("^`[a-z][a-z0-9]*\\.[a-zA-Z][a-zA-Z0-9]*`$")

// contractOps 是从文档里解析出来的 op 清单。
type contractOps struct {
	// names 按出现顺序（= 文档里的分组顺序 = 实现顺序）。
	names []string
	// async 是文档标了「异步」的那些 op。
	async map[string]bool
}

// readContractDoc 读契约文档正文。
//
// 读不到就**直接失败**：一道「读不到文档就跳过」的门禁等于没有门禁，
// 而且它会一直绿着，直到有人真的改坏了文档才发现。
func readContractDoc(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("解析仓库根失败: %v", err)
	}
	path := filepath.Join(root, "docs", "ops-api.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到契约文档 %s（这道门禁就是为它存在的，不许跳过）: %v", path, err)
	}
	return string(raw)
}

// contractSection 截出 §7 那一段。
func contractSection(doc string) (string, error) {
	start := strings.Index(doc, opsSectionStart)
	if start < 0 {
		return "", fmt.Errorf("契约文档里找不到 §7 的起点 %q —— 边界移动了，"+
			"门禁必须当场报出来，而不是安静地扫到别的地方去", opsSectionStart)
	}
	rest := doc[start:]
	end := strings.Index(rest, opsSectionEnd)
	if end < 0 {
		return "", fmt.Errorf("契约文档里找不到 §7 的终点 %q", opsSectionEnd)
	}
	return rest[:end], nil
}

// parseContractOps 解析 §7 子表里的 op 名与「异步」标记。
func parseContractOps(section string) (contractOps, error) {
	out := contractOps{async: map[string]bool{}}
	seen := map[string]bool{}
	tables := 0

	for _, rawLine := range strings.Split(section, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### 7.") {
			tables++
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			// 表格之外的一切（散文、JSON 例子、分隔线）都不看。
			continue
		}
		cells := strings.Split(trimmed, "|")
		if len(cells) < 3 {
			continue
		}
		cell := strings.TrimSpace(cells[1])
		if !opNamePattern.MatchString(cell) {
			continue
		}
		name := strings.Trim(cell, "`")
		if !seen[name] {
			seen[name] = true
			out.names = append(out.names, name)
		}
		if strings.Contains(trimmed, "异步") {
			out.async[name] = true
		}
	}

	if tables != contractSectionTables {
		return out, fmt.Errorf("§7 的子表数量是 %d，期望 %d —— **这是文档结构变了，不是断言写错了**。\n"+
			"  下一步：看一眼 §7 是不是真的多/少了一组（比如新增 `theme.*`）；\n"+
			"  · 确认属实 → 把 contractSectionTables 这个数字改成 %d，**不要动断言逻辑**；\n"+
			"  · 若只是顺手动了一下格式 → 把文档改回去，这个数字不该跟着变。",
			tables, contractSectionTables, tables)
	}
	if len(out.names) == 0 {
		return out, fmt.Errorf("§7 里一个 op 名都没解析出来：不是文档写法变了，" +
			"就是这道门禁的解析坏了 —— 两种情况都必须红")
	}
	return out, nil
}

// implOpNames 返回注册表里的 op 名（门禁的实现侧）。
func implOpNames() []string {
	ops := Ops()
	names := make([]string, 0, len(ops))
	for _, op := range ops {
		names = append(names, op.Name)
	}
	return names
}

// opGate 是门禁本身：拿文档正文与实现侧名单，回报两侧的对称差。
func opGate(doc string) (onlyInDoc, onlyInImpl []string, err error) {
	return opGateWith(doc, implOpNames())
}

// opGateWith 与 opGate 同一条路径，但实现侧名单由调用方给。
//
// 写成「文本进、差异出」的纯函数，是为了让负对照**走同一条代码路径**：
// 一条只在正例上跑过的门禁，证明不了它会不会红。
// 实现侧可传参还让一道历史红灯可以被**复现**（见 F1 那条负对照）。
func opGateWith(doc string, implNames []string) (onlyInDoc, onlyInImpl []string, err error) {
	section, err := contractSection(doc)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := parseContractOps(section)
	if err != nil {
		return nil, nil, err
	}

	impl := map[string]bool{}
	for _, name := range implNames {
		impl[name] = true
	}

	for _, name := range parsed.names {
		if !impl[name] {
			onlyInDoc = append(onlyInDoc, name)
		}
	}
	docSet := map[string]bool{}
	for _, name := range parsed.names {
		docSet[name] = true
	}
	for _, name := range implNames {
		if !docSet[name] {
			onlyInImpl = append(onlyInImpl, name)
		}
	}
	sort.Strings(onlyInDoc)
	sort.Strings(onlyInImpl)
	return onlyInDoc, onlyInImpl, nil
}

// gateMessage 把差异渲染成一条人话（缺的名字**逐个列出**，不只给数量）。
func gateMessage(onlyInDoc, onlyInImpl []string) string {
	var b strings.Builder
	if len(onlyInDoc) > 0 {
		b.WriteString("\n· **文档领先于实现**：契约 §7 里写了、注册表里没有 ——\n")
		for _, name := range onlyInDoc {
			b.WriteString("    - " + name + "\n")
		}
	}
	if len(onlyInImpl) > 0 {
		b.WriteString("\n· **实现领先于文档**：注册表里有、契约 §7 里没写 ——\n")
		for _, name := range onlyInImpl {
			b.WriteString("    - " + name + "\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\n契约文档与注册表必须双向一致（加 op 时两边一起改，或先改文档再实现）。\n")
	return b.String()
}

// TestOpsMatchContractDocument 是这道门禁的正例：当前仓库状态下两侧必须完全一致。
func TestOpsMatchContractDocument(t *testing.T) {
	doc := readContractDoc(t)
	onlyInDoc, onlyInImpl, err := opGate(doc)
	if err != nil {
		t.Fatal(err)
	}
	if msg := gateMessage(onlyInDoc, onlyInImpl); msg != "" {
		t.Fatalf("契约 §7 与 op 注册表不一致：%s", msg)
	}
	section, _ := contractSection(doc)
	parsed, _ := parseContractOps(section)
	t.Logf("契约 §7 与注册表双向一致：%d 个 op（文档 %d 个名字，%d 个标了异步）",
		len(Ops()), len(parsed.names), len(parsed.async))
}

// TestAsyncOpsMatchContractDocument 把「异步恰好 4 个」升级成**和文档对**。
//
// 原来那条只和注册表自己对（写死 4 个名字）：文档把某个 op 改标成异步、
// 或者新增第 5 个异步 op，它都不会响。
func TestAsyncOpsMatchContractDocument(t *testing.T) {
	doc := readContractDoc(t)
	section, err := contractSection(doc)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseContractOps(section)
	if err != nil {
		t.Fatal(err)
	}

	implAsync := map[string]bool{}
	for _, op := range Ops() {
		if op.Async {
			implAsync[op.Name] = true
		}
	}

	for name := range parsed.async {
		if !implAsync[name] {
			t.Errorf("文档把 %s 标成异步，注册表里它不是 async:true", name)
		}
	}
	for name := range implAsync {
		if !parsed.async[name] {
			t.Errorf("注册表把 %s 标成 async:true，文档里没写「异步」", name)
		}
	}
	if len(parsed.async) != len(implAsync) {
		t.Fatalf("异步集合大小不一致：文档 %d 个，注册表 %d 个", len(parsed.async), len(implAsync))
	}
	names := make([]string, 0, len(parsed.async))
	for name := range parsed.async {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Logf("异步 op 两侧一致：%s", strings.Join(names, ", "))
}

// TestContractGateNegativeControls 是这条门禁的**负对照**。
//
// 「一个从来没红过的门禁不能证明任何事，只能证明它没报错。」
// 这里构造两种漂移，断言门禁**确实会红**，而且点名了那个 op：
//
//	文档多一个（文档领先于实现）—— F1 的形状，也就是 log.write 那次
//	文档少一个（实现领先于文档）
//
// 两次都走 opGate 这条**与正例完全相同**的代码路径，所以它红 = 正例也会红。
func TestContractGateNegativeControls(t *testing.T) {
	doc := readContractDoc(t)

	t.Run("文档多一个 op 名", func(t *testing.T) {
		injected := strings.Replace(doc,
			"| `core.ops` |",
			"| `core.notImplementedYet` | `{}` | `{}` |\n| `core.ops` |", 1)
		if injected == doc {
			t.Fatal("注入失败：文档里找不到 core.ops 那一行，负对照没有真的生效")
		}
		onlyInDoc, onlyInImpl, err := opGate(injected)
		if err != nil {
			t.Fatalf("门禁本身出错（不是我们要的「红」）: %v", err)
		}
		if len(onlyInDoc) == 0 {
			t.Fatal("文档里凭空多了一个 op，门禁居然没红 —— 这条门禁是假的")
		}
		msg := gateMessage(onlyInDoc, onlyInImpl)
		if !strings.Contains(msg, "core.notImplementedYet") {
			t.Errorf("红是红了，但没点名那个 op：%s", msg)
		}
		if !strings.Contains(msg, "文档领先于实现") {
			t.Errorf("没报出这是「文档领先于实现」：%s", msg)
		}
		t.Logf("负对照 ①（文档多一个）红灯原文：%s", msg)
	})

	t.Run("文档少一个 op 名", func(t *testing.T) {
		removed := removeLineContaining(doc, "| `core.ops` |")
		if removed == doc {
			t.Fatal("删除失败：文档里找不到 core.ops 那一行，负对照没有真的生效")
		}
		onlyInDoc, onlyInImpl, err := opGate(removed)
		if err != nil {
			t.Fatalf("门禁本身出错（不是我们要的「红」）: %v", err)
		}
		if len(onlyInImpl) == 0 {
			t.Fatal("文档里删掉了一个真 op，门禁居然没红 —— 这条门禁是假的")
		}
		msg := gateMessage(onlyInDoc, onlyInImpl)
		if !strings.Contains(msg, "core.ops") {
			t.Errorf("红是红了，但没点名那个 op：%s", msg)
		}
		if !strings.Contains(msg, "实现领先于文档") {
			t.Errorf("没报出这是「实现领先于文档」：%s", msg)
		}
		t.Logf("负对照 ②（文档少一个）红灯原文：%s", msg)
	})
}

// removeLineContaining 删掉包含 needle 的第一行。
func removeLineContaining(text, needle string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.Contains(line, needle) {
			return strings.Join(append(lines[:i:i], lines[i+1:]...), "\n")
		}
	}
	return text
}

// TestContractGateReproducesTheHistoricalF1 把 verifier 报 F1 那天的状态**复现**一次。
//
// 那天的情形是：`log.write` 已经写进文档，注册表里还没有它（37 个 op）。
// 这道门禁当时并不存在，所以整条链全绿 —— 而漂移已经发生了。
//
// 这里用「实现侧名单里去掉 log.write」把那一刻还原出来，断言门禁**会红**，
// 并且**点名 log.write**。它回答的不是「现在红不红」（现在当然绿，t13 已经实现了），
// 而是「如果当初有这道门禁，它会不会响」——会。
func TestContractGateReproducesTheHistoricalF1(t *testing.T) {
	doc := readContractDoc(t)

	withoutLogWrite := make([]string, 0, len(Ops()))
	for _, name := range implOpNames() {
		if name != "log.write" {
			withoutLogWrite = append(withoutLogWrite, name)
		}
	}
	if len(withoutLogWrite) != len(implOpNames())-1 {
		t.Fatal("构造失败：注册表里没有 log.write，这条负对照失去意义")
	}

	onlyInDoc, onlyInImpl, err := opGateWith(doc, withoutLogWrite)
	if err != nil {
		t.Fatalf("门禁本身出错: %v", err)
	}
	msg := gateMessage(onlyInDoc, onlyInImpl)
	if len(onlyInDoc) != 1 || onlyInDoc[0] != "log.write" {
		t.Fatalf("F1 那天的状态应当只报 log.write 一个，实际 %v", onlyInDoc)
	}
	if !strings.Contains(msg, "文档领先于实现") {
		t.Errorf("应报出「文档领先于实现」：%s", msg)
	}
	t.Logf("F1 那天的红灯（复现，实现侧去掉 log.write）：%s", msg)
}
