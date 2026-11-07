import 'dart:ffi';
import 'dart:io';

/// 动态库查找失败。**必须**把「找过哪些路径」列全 —— 静默降级成假实现是不可接受的。
class CoreLibraryNotFound implements Exception {
  CoreLibraryNotFound(this.attempts);

  /// 每一条都是一次尝试：`路径 → 失败原因`。
  final List<String> attempts;

  @override
  String toString() {
    final buffer = StringBuffer('找不到 AniSync 引擎动态库（anisync_core）。已尝试：');
    if (attempts.isEmpty) {
      buffer.write('\n  （没有任何候选路径 —— 当前平台可能不受支持）');
    } else {
      for (final item in attempts) {
        buffer.write('\n  - $item');
      }
    }
    buffer.write(
      '\n\n提示：设环境变量 ANISYNC_CORE_LIB 指向动态库绝对路径，'
      '或确认它与 GUI 可执行文件放在同一目录。',
    );
    return buffer.toString();
  }
}

/// 打开成功的动态库 + 它的来源路径（路径用于错误消息与「设置」页展示）。
class CoreLibraryHandle {
  CoreLibraryHandle(this.library, this.path);

  final DynamicLibrary library;
  final String path;
}

/// 按平台算出候选路径。顺序即优先级。
///
/// 契约（`docs/ops-api.md` §1.2）：
/// - Windows：与 GUI 可执行文件同目录
/// - macOS：`<exe>/../Frameworks/`，回退到与可执行文件同目录
/// - Android：`libanisync_core.so`，装进 APK 后由 dlopen 解析
/// - 测试 / 开发：`ANISYNC_CORE_LIB` 优先
List<String> coreLibraryCandidates() {
  if (Platform.isAndroid) {
    return const <String>['libanisync_core.so'];
  }
  String exeDir;
  try {
    exeDir = File(Platform.resolvedExecutable).parent.path;
  } on Object {
    return const <String>[];
  }
  final sep = Platform.pathSeparator;
  String under(String name) => '$exeDir$sep$name';
  if (Platform.isMacOS) {
    // .app 包里可执行文件在 Contents/MacOS/，dylib 放 Contents/Frameworks/。
    return <String>[
      '$exeDir$sep..${sep}Frameworks${sep}libanisync_core.dylib',
      under('libanisync_core.dylib'),
    ];
  }
  if (Platform.isWindows) {
    return <String>[under('anisync_core.dll')];
  }
  return <String>[under('libanisync_core.so'), 'libanisync_core.so'];
}

/// `ANISYNC_CORE_LIB` 是否已设置（设置页要显示它，测试要据此决定是否跑真机回环）。
String? envCoreLibraryPath() {
  final raw = Platform.environment['ANISYNC_CORE_LIB'];
  if (raw == null) return null;
  final trimmed = raw.trim();
  return trimmed.isEmpty ? null : trimmed;
}

/// 打开动态库。`opener` 只为测试注入，正常运行走 `DynamicLibrary.open`。
CoreLibraryHandle openCoreLibrary({
  DynamicLibrary Function(String path)? opener,
  List<String>? extraCandidates,
}) {
  final open = opener ?? DynamicLibrary.open;
  final attempts = <String>[];

  final env = envCoreLibraryPath();
  if (env != null) {
    try {
      return CoreLibraryHandle(open(env), env);
    } on Object catch (error) {
      attempts.add('$env（来自 ANISYNC_CORE_LIB）→ $error');
    }
  }

  final candidates = <String>[
    ...?extraCandidates,
    ...coreLibraryCandidates(),
  ];
  for (final path in candidates) {
    try {
      return CoreLibraryHandle(open(path), path);
    } on Object catch (error) {
      attempts.add('$path → $error');
    }
  }
  throw CoreLibraryNotFound(attempts);
}
