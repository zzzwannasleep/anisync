package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// GUI 模式的布局与权限位是**契约**，不是实现细节：
//
//   - 顶层目录必须叫 AniSync-GUI/（叫 AniSync/ 会和 CLI 的 zip 互相覆盖，
//     见 docs/build.md）；
//   - Contents/MacOS/ 下的可执行文件必须带可执行位，而 Windows 是打不出这个位的
//     （os.Stat 在 Windows 上报不出 0755），所以只能由打包器显式写；
//   - GUI 包里不许出现 config.example.yaml 与 samples/：那是命令行工具的样例。
func TestGUIModeLayoutAndExecBits(t *testing.T) {
	root := t.TempDir()
	stage := filepath.Join(root, "stage")
	mustWrite(t, filepath.Join(stage, "AniSync.exe"), "fake exe")
	mustWrite(t, filepath.Join(stage, "anisync_core.dll"), "fake dll")
	mustWrite(t, filepath.Join(stage, "README.md"), "# readme")
	mustWrite(t, filepath.Join(stage, "AniSync.app", "Contents", "MacOS", "AniSync"), "fake macho")
	mustWrite(t, filepath.Join(stage, "AniSync.app", "Contents", "Info.plist"), "<plist/>")

	zipPath := filepath.Join(root, "out", "AniSync-GUI-0.1.11-windows-x64.zip")
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	err := writeZip(zipPath, archiveInput{
		repo:    root,
		top:     "AniSync-GUI",
		plat:    "windows",
		gui:     true,
		staging: stage,
	})
	if err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	modes := entryModes(t, zipPath)

	if _, ok := modes["AniSync-GUI/"]; !ok {
		t.Errorf("缺少顶层目录条目 AniSync-GUI/；实际条目：%v", keys(modes))
	}
	cases := []struct {
		name string
		exec bool
	}{
		{"AniSync-GUI/AniSync.exe", true},
		{"AniSync-GUI/anisync_core.dll", false},
		{"AniSync-GUI/README.md", false},
		{"AniSync-GUI/AniSync.app/Contents/MacOS/AniSync", true},
		{"AniSync-GUI/AniSync.app/Contents/Info.plist", false},
	}
	for _, c := range cases {
		mode, ok := modes[c.name]
		if !ok {
			t.Errorf("缺少条目 %s；实际条目：%v", c.name, keys(modes))
			continue
		}
		gotExec := mode&0o111 != 0
		if gotExec != c.exec {
			t.Errorf("%s 可执行位 = %v（mode=%v），期望 %v", c.name, gotExec, mode, c.exec)
		}
	}
	for name := range modes {
		// 只扫路径中间的 config.example.yaml：本测试用 t.TempDir() 建根目录，
		// 霸王条款式的 strings.Contains(name, ...) 会误报临时目录名。
		if filepath.Base(name) == "config.example.yaml" || filepath.Base(name) == "samples" {
			t.Errorf("GUI 包里不该出现 %s", name)
		}
	}
}

// CLI 布局是我这次改动**必须没有动**的东西：默认顶层目录、默认 zip 名、
// Windows 下 exe 的落点与权限位。build-all.ps1 与 build.yml 都压在这上面。
func TestCLIModeUnchanged(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config.example.yaml"), "listen: :8080")
	mustWrite(t, filepath.Join(root, "samples", "01-demo.yaml"), "key: v")
	bin := filepath.Join(root, "bin", "AniSync-windows-amd64.exe")
	mustWrite(t, bin, "fake binary")

	// 默认 zip 名 = AniSync-<version>-<os>-<arch>.zip，由 run() 里留空的 -name 决定。
	base := "AniSync-0.1.6-windows-amd64"
	zipPath := filepath.Join(root, "out", base+".zip")
	if err := os.MkdirAll(filepath.Dir(zipPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeZip(zipPath, archiveInput{repo: root, top: "AniSync", plat: "windows", bin: bin}); err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	modes := entryModes(t, zipPath)
	for _, name := range []string{
		"AniSync/",
		"AniSync/AniSync.exe",
		"AniSync/config.example.yaml",
		"AniSync/01-demo.yaml",
	} {
		if _, ok := modes[name]; !ok {
			t.Errorf("CLI 布局变了：缺少 %s；实际条目：%v", name, keys(modes))
		}
	}
	if mode := modes["AniSync/AniSync.exe"]; mode&0o111 == 0 {
		t.Errorf("AniSync/AniSync.exe 丢了可执行位（mode=%v）", mode)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entryModes(t *testing.T, zipPath string) map[string]os.FileMode {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("打开 %s: %v", zipPath, err)
	}
	defer zr.Close()
	out := make(map[string]os.FileMode, len(zr.File))
	for _, f := range zr.File {
		out[f.Name] = f.Mode()
	}
	return out
}

func keys(m map[string]os.FileMode) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
