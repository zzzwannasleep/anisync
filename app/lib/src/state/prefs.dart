import 'dart:convert';
import 'dart:io';

/// 应用自己的偏好：最近工作区、上次后端、主题。
///
/// **不进 `config.yaml`** —— 那是 Go 引擎的配置，GUI 的界面偏好混进去会污染它。
/// 落 `<dataDir>/app-prefs.json`，原子写（先写 `.tmp` 再 rename）。
class PrefsStore {
  PrefsStore({required this.directory, this.fileName = 'app-prefs.json'});

  final String directory;
  final String fileName;

  List<String> recentWorkspaces = <String>[];
  String lastBackend = backendFfi;
  String nodeUrl = 'http://127.0.0.1:51837';
  String nodeToken = '';
  String theme = 'system';

  static const String backendFfi = 'ffi';
  static const String backendNode = 'node';

  String get path => '$directory${Platform.pathSeparator}$fileName';

  Future<void> load() async {
    try {
      final file = File(path);
      if (!await file.exists()) return;
      final decoded = jsonDecode(await file.readAsString());
      if (decoded is! Map) return;
      final map = decoded.cast<String, Object?>();
      recentWorkspaces = _stringList(map['recentWorkspaces']);
      lastBackend = _string(map['lastBackend'], backendFfi);
      nodeUrl = _string(map['nodeUrl'], nodeUrl);
      nodeToken = _string(map['nodeToken'], '');
      theme = _string(map['theme'], 'system');
    } on Object {
      // 偏好文件坏掉不该挡住启动：用默认值继续，下次保存时覆盖。
    }
  }

  Future<void> save() async {
    try {
      final dir = Directory(directory);
      if (!await dir.exists()) {
        await dir.create(recursive: true);
      }
      final target = File(path);
      final temp = File('$path.tmp');
      await temp.writeAsString(
        const JsonEncoder.withIndent('  ').convert(toJson()),
        flush: true,
      );
      await temp.rename(target.path);
    } on Object {
      // 保存偏好失败不是致命错误：界面偏好丢了不影响引擎。
    }
  }

  /// 记一个工作区到最近列表（去重、最近在前、最多 8 条）。
  void rememberWorkspace(String workspace) {
    final value = workspace.trim();
    if (value.isEmpty) return;
    recentWorkspaces = <String>[
      value,
      ...recentWorkspaces.where((item) => item != value),
    ].take(8).toList();
  }

  Map<String, Object?> toJson() => <String, Object?>{
        'recentWorkspaces': recentWorkspaces,
        'lastBackend': lastBackend,
        'nodeUrl': nodeUrl,
        'nodeToken': nodeToken,
        'theme': theme,
      };

  static String _string(Object? value, String fallback) =>
      value is String && value.isNotEmpty ? value : fallback;

  static List<String> _stringList(Object? value) {
    if (value is List) {
      return value.whereType<String>().toList(growable: false);
    }
    return <String>[];
  }
}
