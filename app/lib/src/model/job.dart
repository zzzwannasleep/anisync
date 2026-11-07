import 'json.dart';
import 'plan.dart';

/// job 状态。
class JobState {
  JobState._();

  static const String running = 'running';
  static const String done = 'done';
  static const String failed = 'failed';
  static const String canceled = 'canceled';
}

/// 一个异步 op 的运行态（`docs/ops-api.md` §7.8）。
class JobView {
  JobView({
    this.id = '',
    this.kind = '',
    this.state = '',
    this.startedAt,
    this.finishedAt,
    this.message = '',
    this.error,
    this.result,
  });

  factory JobView.fromJson(Map<String, Object?> json) => JobView(
        id: Json.str(json['id']),
        kind: Json.str(json['kind']),
        state: Json.str(json['state']),
        startedAt: Json.time(json['startedAt']),
        finishedAt: Json.time(json['finishedAt']),
        message: Json.str(json['message']),
        error: Json.mapOrNull(json['error']),
        result: Json.mapOrNull(json['result']),
      );

  String id;

  /// 异步 op 名，例如 `sync.run`。
  String kind;

  /// `running` / `done` / `failed` / `canceled`。
  String state;
  DateTime? startedAt;
  DateTime? finishedAt;

  /// 人话进度，例如「正在取站点数据 3/8」。
  String message;

  /// `{code,message}`。
  Map<String, Object?>? error;
  Map<String, Object?>? result;

  bool get isRunning => state == JobState.running;
  bool get isDone => state == JobState.done;
  bool get isFailed => state == JobState.failed;
  bool get isCanceled => state == JobState.canceled;

  /// 终态：不再变。
  bool get isTerminal => !isRunning;

  /// 从 `startedAt` 到现在的耗时；未开始或无值时为 `null`。
  Duration? elapsed(DateTime now) {
    final start = startedAt;
    if (start == null) return null;
    return (finishedAt ?? now).difference(start);
  }

  // ---- §7.8：异步 op 的 result 形状 ----
  //
  //   sync.run         {writable, updated, added, skipped, errors, busy, note, plan}
  //   plan.apply       {updated, added, errors, plan}
  //   discover.autoAdd {planned, added, errors}
  //   backup.restore   {restored, pruned, errors}
  //
  // `errors` 是 `string[]`（journal 的 Clip 会裁到最多前 5 条）。

  /// 结果里的一个数字字段（没有就是 `null`，不猜成 0）。
  int? resultCount(String key) => Json.intOrNull(result?[key]);

  /// 逐条失败原因。
  List<String> get resultErrors => Json.strList(result?['errors']);

  bool get resultBusy => Json.boolOf(result?['busy']);

  String get resultNote => Json.str(result?['note']);

  /// 结果里的完整计划 —— 界面既要回答「改了几条」，也要能展开回答「改了哪几条」。
  PlanView? get resultPlan {
    final raw = result?['plan'];
    if (raw is Map) return PlanView.fromJson(raw.cast<String, Object?>());
    return null;
  }

  /// 「改了几条」那句摘要用到的数字，按 op 取对应的键。
  String summary() {
    switch (kind) {
      case 'sync.run':
        return '可写 ${resultCount('writable') ?? '—'} · '
            '更新 ${resultCount('updated') ?? '—'} · '
            '新增 ${resultCount('added') ?? '—'} · '
            '跳过 ${resultCount('skipped') ?? '—'}';
      case 'plan.apply':
        return '更新 ${resultCount('updated') ?? '—'} · '
            '新增 ${resultCount('added') ?? '—'}';
      case 'discover.autoAdd':
        return '计划 ${resultCount('planned') ?? '—'} · '
            '新增 ${resultCount('added') ?? '—'}';
      case 'backup.restore':
        return '还原 ${resultCount('restored') ?? '—'} · '
            '清理 ${resultCount('pruned') ?? '—'}';
      default:
        return '';
    }
  }

  Map<String, Object?> toJson() => <String, Object?>{
        'id': id,
        'kind': kind,
        'state': state,
        if (startedAt != null) 'startedAt': startedAt!.toIso8601String(),
        if (finishedAt != null) 'finishedAt': finishedAt!.toIso8601String(),
        'message': message,
        if (error != null) 'error': error,
        if (result != null) 'result': result,
      };
}

/// `job.list` 的响应。
class JobListResult {
  JobListResult({this.jobs});

  factory JobListResult.fromJson(Map<String, Object?> json) => JobListResult(
        jobs: Json.mapList(json['jobs']).map(JobView.fromJson).toList(),
      );

  List<JobView>? jobs;

  List<JobView> get jobsOrEmpty => jobs ?? const <JobView>[];
}

/// `job.get` 的响应。
class JobGetResult {
  JobGetResult({this.job});

  factory JobGetResult.fromJson(Map<String, Object?> json) => JobGetResult(
        job: JobView.fromJson(Json.map(json['job'])),
      );

  JobView? job;
}
