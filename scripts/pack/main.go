// Command pack 把一个平台的构建产物打成自包含的 zip。
//
// 发布物**一律是 zip**，解压出来就是一个 AniSync/ 文件夹：
//
//	AniSync/
//	  AniSync.exe          程序本体（macOS 上是 AniSync.app）
//	  config.example.yaml  配置示例
//	  README.md            样例说明
//	  01-… 02-… 03-… 04-…  四份现成配置
//	  subs.yaml raws.yaml all.yaml deny.yaml
//
// 这样「程序产生的文件只在解压出来的那个文件夹里」这条约定才立得住：
// 运行期文件按 internal/workdir 的规则写在程序同目录，也就是这个文件夹里。
//
// 样例摊平到根目录，而不是放进 samples/ 子目录：样例之间按相对路径互相引用
// （03 号配置里写着 primaryFiles: [raws.yaml, subs.yaml]），
// 摊平之后「把 03 复制成 config.yaml」这一步才直接可用。
//
// 用 Go 写而不是 zip / Compress-Archive，有两个原因：
//   - archive/zip 按规范给非 ASCII 文件名置 UTF-8 标志位（bit 11），
//     样例文件名是中文，这一点必须可靠；
//   - 权限位由这里显式给（见 modeExec / appMode），不去读源文件系统 ——
//     否则在 Windows 上打出来的包解压到 Linux/macOS 会不可执行。
//
// 用法：
//
//	go run ./scripts/pack -version 0.1.6 -os windows -arch amd64 \
//	  -bin dist/AniSync-windows-amd64.exe
//	go run ./scripts/pack -version 0.1.6 -os macos -arch arm64 \
//	  -app dist/AniSync.app
//
// GUI（Flutter 应用）走 -gui 模式：顶层目录与 zip 基名都换成 AniSync-GUI，
// 只把 -staging 目录的内容原样放进去，不附 CLI 的样例与 config.example.yaml：
//
//	go run ./scripts/pack -version 0.1.11 -os windows -arch amd64 \
//	  -gui -staging dist/gui-stage -top AniSync-GUI -name AniSync-GUI-0.1.11-windows-x64
//
// 为什么 CLI 与 GUI 共用一个打包器：权限位与 UTF-8 标志位这两条只有在 Go 里
// 才做得对（见上）。分开写两份，迟早有一份会退化成 Compress-Archive 那套。
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 权限位写死，不读源文件系统：在 Windows 上 os.Stat 报不出可执行位，
// 照抄过去的话，解压到 Linux/macOS 的程序会跑不起来。
const (
	modeDir  fs.FileMode = 0o755
	modeExec fs.FileMode = 0o755
	modeFile fs.FileMode = 0o644
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "打包失败: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		root     = flag.String("root", ".", "仓库根目录（config.example.yaml 与 samples/ 在这里）")
		version  = flag.String("version", "", "版本号，进 zip 文件名")
		out      = flag.String("out", "dist", "输出目录")
		platform = flag.String("os", "", "平台名，只用于 zip 文件名（windows / macos / linux）")
		arch     = flag.String("arch", "", "架构名，只用于 zip 文件名")
		bin      = flag.String("bin", "", "二进制路径（与 -app 二选一）")
		app      = flag.String("app", "", ".app 目录路径（与 -bin 二选一，macOS 用）")
		top      = flag.String("top", "AniSync", "zip 里的顶层文件夹名")
		gui      = flag.Bool("gui", false, "GUI 布局：只打包 -staging 的内容，不附样例与 config.example.yaml")
		staging  = flag.String("staging", "", "-gui 模式下要打包的目录，其内容成为 <top>/ 的直接内容")
		name     = flag.String("name", "", "zip 基名（不含 .zip）；留空则用 AniSync-<version>-<os>-<arch>")
	)
	flag.Parse()

	switch {
	case *version == "":
		return fmt.Errorf("缺少 -version")
	case *platform == "":
		return fmt.Errorf("缺少 -os")
	case *arch == "":
		return fmt.Errorf("缺少 -arch")
	case *gui && *staging == "":
		return fmt.Errorf("-gui 需要 -staging")
	case *gui && (*bin != "" || *app != ""):
		return fmt.Errorf("-gui 不能与 -bin / -app 同时使用")
	case !*gui && (*bin == "") == (*app == ""):
		return fmt.Errorf("-bin 与 -app 必须给且只给一个")
	}

	repo, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	// 基名留空 = 既有行为，逐字节不变：build-all.ps1 与 build.yml 都依赖这个名字。
	base := *name
	if base == "" {
		base = fmt.Sprintf("AniSync-%s-%s-%s", *version, *platform, *arch)
	}
	zipPath := filepath.Join(*out, base+".zip")

	if err := writeZip(zipPath, archiveInput{
		repo:    repo,
		top:     *top,
		bin:     *bin,
		app:     *app,
		plat:    *platform,
		gui:     *gui,
		staging: *staging,
	}); err != nil {
		return err
	}

	info, err := os.Stat(zipPath)
	if err != nil {
		return err
	}
	fmt.Printf("已生成 %s（%.1f MB）\n", zipPath, float64(info.Size())/(1<<20))
	return nil
}

type archiveInput struct {
	repo    string
	top     string
	bin     string
	app     string
	plat    string
	gui     bool
	staging string
}

func writeZip(zipPath string, in archiveInput) error {
	file, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer file.Close()

	zw := zip.NewWriter(file)
	if err := writeArchive(zw, in); err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

func writeArchive(zw *zip.Writer, in archiveInput) error {
	// 顶层文件夹显式写一条：解压工具才会无条件建出它。
	if err := addDir(zw, in.top); err != nil {
		return err
	}

	if in.gui {
		// GUI 布局：-staging 里的东西原样成为 <top>/ 的直接内容。
		// 刻意**不**附 config.example.yaml 与 samples/：那是命令行工具的样例，
		// 放进 GUI 包里只会让人以为 GUI 会读它们（GUI 读的是引擎自己的工作区）。
		if err := addTree(zw, in.staging, in.top); err != nil {
			return fmt.Errorf("打包 %s: %w", in.staging, err)
		}
		return nil
	}

	if in.app != "" {
		// .app 自身的目录条目：WalkDir 会跳过根，这里补上，
		// 免得严格一点的解压工具对「有 Contents/ 却没有 AniSync.app/」犯嘀咕。
		if err := addDir(zw, in.top+"/AniSync.app"); err != nil {
			return err
		}
		if err := addTree(zw, in.app, in.top+"/AniSync.app"); err != nil {
			return fmt.Errorf("打包 %s: %w", in.app, err)
		}
	} else {
		name := "AniSync"
		if in.plat == "windows" {
			name += ".exe"
		}
		if err := addFile(zw, in.bin, in.top+"/"+name, modeExec); err != nil {
			return fmt.Errorf("打包 %s: %w", in.bin, err)
		}
	}

	if err := addFile(zw, filepath.Join(in.repo, "config.example.yaml"), in.top+"/config.example.yaml", modeFile); err != nil {
		return err
	}

	names, err := sampleFiles(filepath.Join(in.repo, "samples"))
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := addFile(zw, filepath.Join(in.repo, "samples", name), in.top+"/"+name, modeFile); err != nil {
			return err
		}
	}
	return nil
}

// sampleFiles 列出 samples/ 里要打进包的文件，文件名排序保证产物可复现。
func sampleFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读 %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("%s 里没有文件", dir)
	}
	sort.Strings(names)
	return names, nil
}

func addDir(zw *zip.Writer, name string) error {
	hdr := &zip.FileHeader{Name: name + "/", Method: zip.Store}
	// SetMode 会带上 Unix 文件类型位（S_IFDIR）并把 CreatorVersion 标成 Unix，
	// 比自己拼 ExternalAttrs 稳妥。
	hdr.SetMode(fs.ModeDir | modeDir)
	_, err := zw.CreateHeader(hdr)
	return err
}

// addFile 写入一个文件；目录会转给 addTree。
func addFile(zw *zip.Writer, path, name string, mode fs.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return addTree(zw, path, name)
	}
	hdr := &zip.FileHeader{
		Name:     filepath.ToSlash(name),
		Method:   zip.Deflate,
		Modified: info.ModTime(),
	}
	hdr.SetMode(mode)

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// addTree 递归写入一个目录（只用于 .app），prefix 是它在 zip 里的路径。
func addTree(zw *zip.Writer, base, prefix string) error {
	return filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := prefix + "/" + filepath.ToSlash(rel)
		if entry.IsDir() {
			return addDir(zw, name)
		}
		return addFile(zw, path, name, appMode(rel))
	})
}

// appMode 给 .app / GUI staging 里的条目定权限。
//
// 可执行位必须显式给，不能靠源文件系统的 mode：CI 的 darwin 构建跑在 macOS 上
// 照抄没问题，但本地在别的系统上打同一个包时，照抄会打出不可执行的 .app。
//
// 两种布局都要命中：
//   - -app 模式：rel 从 .app 内部算起（Contents/MacOS/AniSync）
//   - -gui 模式：rel 从 staging 算起，.app 只是路径中间的一段
//     （AniSync.app/Contents/MacOS/AniSync）
func appMode(rel string) fs.FileMode {
	slash := filepath.ToSlash(rel)
	if strings.HasPrefix(slash, "Contents/MacOS/") || strings.Contains(slash, ".app/Contents/MacOS/") {
		return modeExec
	}
	// GUI 的 Windows 布局：exe 直接躺在顶层。
	if !strings.Contains(slash, "/") && strings.HasSuffix(strings.ToLower(slash), ".exe") {
		return modeExec
	}
	return modeFile
}
