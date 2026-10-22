package config

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// 保存配置时不能把注释冲掉。
//
// 样例配置的注释占了一半篇幅，那些注释就是文档；`yaml.Marshal` 只认结构体，
// 直接写盘会把它们全抹掉。所以这里换个做法：把「当前值」的节点树合并到
// 「原文件」的节点树上 —— 值换成新的，注释、键序、引号风格都从原树里带过来。
//
// 合并是双向的：
//   - 原树里有、新树里也有 -> 用新值 + 原注释（最常见）
//   - 原树里没有、新树里有 -> 直接用新节点（新加的字段，没有注释可继承）
//   - 原树里有、新树里没有 -> 丢掉（结构体里已经没有这个配置项了）

// mergeNode 把 fresh（当前值的全新节点树）合并到 original（带注释的原节点树）。
func mergeNode(original, fresh *yaml.Node) *yaml.Node {
	if original == nil || fresh == nil {
		return fresh
	}
	// 两边都是文档节点时先剥掉这一层：真正的内容在 Content[0] 里。
	// 漏掉这一步的话，合并会直接落空（文档节点既不是映射也不是序列）。
	// 文件最上面那一整块头部注释挂在文档节点上，所以这里的注释必须从 original 继承。
	if fresh.Kind == yaml.DocumentNode && original.Kind == yaml.DocumentNode {
		if len(fresh.Content) != 1 || len(original.Content) != 1 {
			return fresh
		}
		out := *fresh
		out.HeadComment = original.HeadComment
		out.LineComment = original.LineComment
		out.FootComment = original.FootComment
		out.Content = []*yaml.Node{mergeNode(original.Content[0], fresh.Content[0])}
		return &out
	}
	switch fresh.Kind {
	case yaml.MappingNode:
		if original.Kind != yaml.MappingNode {
			return fresh
		}
		return mergeMapping(original, fresh)
	case yaml.SequenceNode:
		if original.Kind != yaml.SequenceNode {
			return fresh
		}
		return mergeSequence(original, fresh)
	case yaml.ScalarNode:
		if original.Kind != yaml.ScalarNode {
			return fresh
		}
		out := *fresh
		out.HeadComment = original.HeadComment
		out.LineComment = original.LineComment
		out.FootComment = original.FootComment
		// 只继承引号风格。字面块（|）之类的风格换了值以后可能不合法，不碰。
		switch original.Style {
		case yaml.SingleQuotedStyle, yaml.DoubleQuotedStyle:
			if fresh.Style == 0 {
				out.Style = original.Style
			}
		}
		return &out
	default:
		return fresh
	}
}

// mergeMapping 按键合并两层映射。
//
// 键序以原文件为准（改动最小），新加的键排在最后。
// 键节点本身也从原树里取，这样挂在键上的注释（比如文件头那几行）才不会丢。
// Style 也要继承：样例里规则写成一行 `{name: x, match: y}`，丢了 FlowStyle
// 会被展开成五行，文件立刻膨胀一倍。
func mergeMapping(original, fresh *yaml.Node) *yaml.Node {
	out := &yaml.Node{
		Kind:        yaml.MappingNode,
		Tag:         fresh.Tag,
		Style:       original.Style,
		HeadComment: original.HeadComment,
		LineComment: original.LineComment,
		FootComment: original.FootComment,
	}
	index := map[string]int{}
	for i := 0; i+1 < len(fresh.Content); i += 2 {
		index[fresh.Content[i].Value] = i
	}

	seen := map[string]bool{}
	for i := 0; i+1 < len(original.Content); i += 2 {
		key := original.Content[i]
		at, ok := index[key.Value]
		if !ok {
			continue
		}
		seen[key.Value] = true
		out.Content = append(out.Content, key, mergeNode(original.Content[i+1], fresh.Content[at+1]))
	}
	for i := 0; i+1 < len(fresh.Content); i += 2 {
		key := fresh.Content[i]
		if seen[key.Value] {
			continue
		}
		out.Content = append(out.Content, key, fresh.Content[i+1])
	}
	return out
}

// mergeSequence 合并两层列表。
//
// 元素按「身份键」配对，不按下标：中间删掉一条时，后面的注释不会整体错位到别的条目上。
// 配对不上的元素当作新增，不继承任何注释。
func mergeSequence(original, fresh *yaml.Node) *yaml.Node {
	out := &yaml.Node{
		Kind:        yaml.SequenceNode,
		Tag:         fresh.Tag,
		Style:       original.Style,
		HeadComment: original.HeadComment,
		LineComment: original.LineComment,
		FootComment: original.FootComment,
	}
	matched := matchSequence(original, fresh)
	for i, item := range fresh.Content {
		if prev := matched[i]; prev != nil {
			out.Content = append(out.Content, mergeNode(prev, item))
			continue
		}
		out.Content = append(out.Content, item)
	}
	// 流式映射上挂脚注释时，yaml.v3 会在 `}` 前多吐一个逗号（`scope: main,}`），
	// 合法但难看。脚注释本来就是「这串列表之后」的说明，挪到序列节点上语义一样。
	if n := len(out.Content); n > 0 {
		if moved := takeFootComment(out.Content[n-1]); moved != "" {
			if out.FootComment == "" {
				out.FootComment = moved
			} else {
				out.FootComment += "\n" + moved
			}
		}
	}
	return out
}

// takeFootComment 把节点（或它最后一个子节点）的脚注释摘下来并清空。
// 只在合并出来的新节点上调用，不会动到原树。
func takeFootComment(node *yaml.Node) string {
	if node.FootComment != "" {
		moved := node.FootComment
		node.FootComment = ""
		return moved
	}
	if len(node.Content) > 0 {
		return takeFootComment(node.Content[len(node.Content)-1])
	}
	return ""
}

// matchSequence 给每个新元素找出它对应的旧元素（找不到就是 nil）。
// 一个旧元素只会被用一次，所以同名条目也不会串台。
func matchSequence(original, fresh *yaml.Node) []*yaml.Node {
	out := make([]*yaml.Node, len(fresh.Content))
	used := make([]bool, len(original.Content))
	keys := make([]string, len(original.Content))
	for i, node := range original.Content {
		keys[i] = nodeIdentity(node)
	}
	for i, node := range fresh.Content {
		key := nodeIdentity(node)
		if key == "" {
			continue
		}
		for j, prev := range original.Content {
			if used[j] || keys[j] != key {
				continue
			}
			used[j] = true
			out[i] = prev
			break
		}
	}
	return out
}

// identityKeys 是列表元素用来配对的身份键，按可靠性从高到低。
// 规则用 name / match，番剧配置用 title / bgmId，订阅用 id，副 RSS 用 label —— 都够唯一。
var identityKeys = []string{"name", "match", "title", "bgmId", "id", "label", "url"}

// nodeIdentity 给列表元素算一个配对用的键。
// 标量元素直接用值；映射元素取第一个有值的身份键；实在认不出来的返回空串（当新增处理）。
func nodeIdentity(node *yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		return "v:" + node.Value
	case yaml.MappingNode:
		for _, key := range identityKeys {
			if value := mappingValue(node, key); value != "" {
				return key + ":" + value
			}
		}
	}
	return ""
}

func mappingValue(node *yaml.Node, key string) string {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1].Value
		}
	}
	return ""
}

// encodeNode 把节点树编码成 YAML 文本。缩进用 2 空格，和样例文件保持一致。
func encodeNode(node *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// parseLayout 解析一份 YAML 文本的节点树，供保存时合并注释用。
// 解析不了就返回 nil：没有布局可继承，退回「直接写新内容」，不阻断保存。
func parseLayout(raw []byte) *yaml.Node {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	return &doc
}
