package config

import (
	"path/filepath"
	"testing"
)

// 样例配置是给用户直接抄的，一旦配置结构变了而样例没跟着改，用户抄过去就是一堆报错。
// 这个测试把 samples/ 下每一份主配置都真正加载并校验一遍，让样例不会悄悄烂掉。
//
// 只挑「数字开头」的文件：samples/ 里还有 deny.yaml / standby.yaml 这些外置规则文件，
// 它们是片段不是配置，拿 Load 去读必然失败。外置文件本身由 Load 顺带校验 ——
// 路径写错、格式不对都会让这里直接红。
func TestSampleConfigsAreValid(t *testing.T) {
	// 样例里的 apiKey 写成 ${ANISYNC_KEY}，校验时会展开，所以给个占位值。
	t.Setenv("ANISYNC_KEY", "sample-placeholder-key")

	paths, err := filepath.Glob(filepath.Join("..", "..", "samples", "[0-9]*.yaml"))
	if err != nil {
		t.Fatalf("扫描样例目录失败: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("samples/ 下没有找到任何编号开头的主配置样例")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("加载失败: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("样例校验不通过: %v", err)
			}
			// 样例的价值在于「抄过去就能用」，所以必须真的配了实例和规则。
			if len(cfg.Instances) == 0 {
				t.Fatal("样例里没有 instances")
			}
			if len(cfg.AllPrimary()) == 0 {
				t.Fatal("样例里没有规则，抄过去什么也匹配不到")
			}
			// 黑白名单走外置文件是样例的约定：主配置只留引用，换口味不动主配置。
			if len(cfg.Rules.PrimaryFiles) == 0 {
				t.Fatal("样例应通过 rules.primaryFiles 引用白名单文件")
			}
			if len(cfg.Rules.DenyFiles) == 0 {
				t.Fatal("样例应通过 rules.DenyFiles 引用黑名单文件")
			}
			if len(cfg.FileRules) == 0 {
				t.Fatal("外置规则文件一个都没加载进来")
			}
		})
	}
}
