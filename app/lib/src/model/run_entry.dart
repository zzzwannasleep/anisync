import 'json.dart';

/// 一轮同步的结果（`journal.Entry` 原样）。
class RunEntry {
  RunEntry({
    this.at,
    this.mode = '',
    this.ok = false,
    this.ms = 0,
    this.writable = 0,
    this.updated = 0,
    this.added = 0,
    this.skipped = 0,
    this.errors,
    this.error = '',
    this.panic = '',
    this.busy = false,
    this.nextAt,
    this.attempt = 0,
    this.note = '',
  });

  factory RunEntry.fromJson(Map<String, Object?> json) => RunEntry(
        at: Json.time(json['at']),
        mode: Json.str(json['mode']),
        ok: Json.boolOf(json['ok']),
        ms: Json.intOf(json['ms']),
        writable: Json.intOf(json['writable']),
        updated: Json.intOf(json['updated']),
        added: Json.intOf(json['added']),
        skipped: Json.intOf(json['skipped']),
        errors: Json.strList(json['errors']),
        error: Json.str(json['error']),
        panic: Json.str(json['panic']),
        busy: Json.boolOf(json['busy']),
        nextAt: Json.time(json['nextAt']),
        attempt: Json.intOf(json['attempt']),
        note: Json.str(json['note']),
      );

  DateTime? at;

  /// 这轮是谁触发的：`daemon`（常驻调度）/ `cli` / `web`。
  String mode;

  /// 完整跑完且没有写入失败。
  bool ok;
  int ms;

  /// 计划里本可以写入的条数。
  int writable;
  int updated;
  int added;
  int skipped;

  /// 逐条写入失败的原因（已被 Go 裁到可展示长度）。
  List<String>? errors;

  /// 整轮失败的原因（拉配置、连不上实例等）。
  String error;

  /// 被兜住的崩溃。
  String panic;

  /// 这一轮到点时上一轮还没跑完，于是让开了 —— 不是成功也不是失败。
  bool busy;
  DateTime? nextAt;

  /// 第几次连续失败后的重试。
  int attempt;

  /// 一句人话总结。
  String note;

  List<String> get errorsOrEmpty => errors ?? const <String>[];

  /// 「只看失败」的判据。
  bool get isFailure =>
      !ok || error.isNotEmpty || panic.isNotEmpty || errorsOrEmpty.isNotEmpty;

  Map<String, Object?> toJson() => <String, Object?>{
        if (at != null) 'at': at!.toIso8601String(),
        if (mode.isNotEmpty) 'mode': mode,
        'ok': ok,
        if (ms != 0) 'ms': ms,
        if (writable != 0) 'writable': writable,
        if (updated != 0) 'updated': updated,
        if (added != 0) 'added': added,
        if (skipped != 0) 'skipped': skipped,
        if (errors != null) 'errors': errors,
        if (error.isNotEmpty) 'error': error,
        if (panic.isNotEmpty) 'panic': panic,
        if (busy) 'busy': busy,
        if (nextAt != null) 'nextAt': nextAt!.toIso8601String(),
        if (attempt != 0) 'attempt': attempt,
        if (note.isNotEmpty) 'note': note,
      };
}

/// `journal.tail` 的响应。
class JournalTailResult {
  JournalTailResult({this.entries, this.path = ''});

  factory JournalTailResult.fromJson(Map<String, Object?> json) =>
      JournalTailResult(
        entries: Json.mapList(json['entries']).map(RunEntry.fromJson).toList(),
        path: Json.str(json['path']),
      );

  List<RunEntry>? entries;
  String path;

  List<RunEntry> get entriesOrEmpty => entries ?? const <RunEntry>[];
}

/// `journal.clear` 的响应。
class JournalClearResult {
  JournalClearResult({this.ok = false, this.cleared = 0});

  factory JournalClearResult.fromJson(Map<String, Object?> json) =>
      JournalClearResult(
        ok: Json.boolOf(json['ok']),
        cleared: Json.intOf(json['cleared']),
      );

  bool ok;
  int cleared;
}

/// `log.recent` 的一行。
class LogLine {
  LogLine({this.ts, this.level = '', this.text = ''});

  factory LogLine.fromJson(Map<String, Object?> json) => LogLine(
        ts: Json.time(json['ts']),
        level: Json.str(json['level']),
        text: Json.str(json['text']),
      );

  DateTime? ts;
  String level;
  String text;
}

/// `log.recent` 的响应。
class LogRecentResult {
  LogRecentResult({this.lines});

  factory LogRecentResult.fromJson(Map<String, Object?> json) =>
      LogRecentResult(
        lines: Json.mapList(json['lines']).map(LogLine.fromJson).toList(),
      );

  List<LogLine>? lines;

  List<LogLine> get linesOrEmpty => lines ?? const <LogLine>[];
}
