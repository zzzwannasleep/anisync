import 'dart:async';

/// 事件总线的一条事件。FFI 回调参数与 SSE 的一行 `data:` 是同一份 JSON。
class CoreEventTypes {
  CoreEventTypes._();

  static const String log = 'log';
  static const String progress = 'progress';
  static const String jobStart = 'job.start';
  static const String jobDone = 'job.done';
  static const String jobFailed = 'job.failed';
  static const String leaseLost = 'lease.lost';
  static const String instanceProgress = 'instance.progress';
}

/// 日志级别。**一律小写比较。**
///
/// 契约 §7.12 实测行为：`log.write` 存进环形缓冲的 `level` 是**规整后的小写**形式
/// —— 客户端发 `INFO`，`log.recent` 里读回来的是 `info`。
/// 所以任何 `level == 'INFO'` 之类的**大写比较会永远不命中**，而且是静默失效。
///
/// 这两条常量存在的唯一目的：让「比较用小写」这件事有一个可引用的名字与出处，
/// 而不是散落在各处的字面量 —— 旁边还配了测试把大小写假设钉住。
class LogLevels {
  LogLevels._();

  static const String debug = 'debug';
  static const String info = 'info';
  static const String warn = 'warn';
  static const String error = 'error';

  /// 引擎接受的四档（`log.write` 的白名单，§7.12）。
  static const List<String> accepted = <String>[debug, info, warn, error];
}

/// 把要发给 `log.write` 的载荷规整成**白名单内**的一档。
///
/// 为什么需要它：引擎对自造档位回 `bad_request`（§7.12），而客户端对「非 ok 信封」
/// 没有处理 —— 那等于**悄悄没写进去**。一条诊断日志静默地没写进去，
/// 比写进去时档位不对更糟：它把「我没告诉你」伪装成「没什么可告诉」。
///
/// 所以：档位不在白名单 → 降级成 `warn`，**并在正文里留痕**（不是丢弃、不是静默改写）。
/// 这是一条纯函数，只做这个判断；白名单本身只有 [LogLevels.accepted] 一处定义。
({String level, String text}) normalizeClientLog(String level, String text) {
  if (LogLevels.accepted.contains(level)) {
    return (level: level, text: text);
  }
  return (level: LogLevels.warn, text: '[level 不在白名单：$level] $text');
}

/// `{"type":"progress","ts":"...","callId":"…","jobId":null,"data":{...}}`
class CoreEvent {
  const CoreEvent({
    required this.type,
    this.ts,
    this.callId,
    this.jobId,
    this.data = const <String, Object?>{},
  });

  factory CoreEvent.fromJson(Map<String, Object?> json) => CoreEvent(
        type: (json['type'] as String?) ?? '',
        ts: _parseTime(json['ts']),
        callId: json['callId'] as String?,
        jobId: json['jobId'] as String?,
        data: (json['data'] as Map?)?.cast<String, Object?>() ??
            const <String, Object?>{},
      );

  final String type;
  final DateTime? ts;
  final String? callId;
  final String? jobId;
  final Map<String, Object?> data;

  // ---- 便捷取值（`type` 决定哪些字段有意义）----

  /// `log` → level（`debug` / `info` / `warn` / `error`，**引擎规整成小写**）。
  ///
  /// 取值宽容：`level` 不是字符串（畸形帧）时给 `null`，**不抛** ——
  /// 事件是旁路信息，一个畸形的 level 不该把整条事件流带走。
  String? get level {
    final value = data['level'];
    return value is String ? value : null;
  }

  /// 是否 error 级。
  ///
  /// 比较用 [LogLevels.error]（小写）。**不要**在这里改成大写 ——
  /// 引擎写进缓冲的是小写（§7.12），大写比较会永远不命中且静默失效。
  bool get isError =>
      type == CoreEventTypes.jobFailed || level == LogLevels.error;

  /// 是否 warn 级（不含 error）。
  bool get isWarning => !isError && level == LogLevels.warn;

  /// `log` → 文本。
  String? get text => data['text'] as String?;

  /// `progress` / `instance.progress` → 已完成数与总数。
  int? get done => _asInt(data['done']);
  int? get total => _asInt(data['total']);

  /// `progress` → 阶段标签（「正在取站点数据 3/8」）。
  String? get label => data['label'] as String?;

  /// `instance.progress` → 实例名。
  String? get instanceName => data['name'] as String?;

  /// `job.done` → 结果对象。
  Map<String, Object?>? get result =>
      (data['result'] as Map?)?.cast<String, Object?>();

  /// `job.failed` → `{code,message}`。
  Map<String, Object?>? get error =>
      (data['error'] as Map?)?.cast<String, Object?>();

  /// `job.start` → 异步 op 名。
  String? get kind => data['kind'] as String?;

  /// `lease.lost` → `{pid,note}`。
  Map<String, Object?>? get holder =>
      (data['holder'] as Map?)?.cast<String, Object?>();

  static int? _asInt(Object? value) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    if (value is String) return int.tryParse(value);
    return null;
  }

  static DateTime? _parseTime(Object? value) {
    if (value is String && value.isNotEmpty) return DateTime.tryParse(value);
    return null;
  }

  @override
  String toString() => 'CoreEvent($type)';
}

/// 后端推送事件的入口。
abstract class CoreEventSource {
  Stream<CoreEvent> get events;
}
