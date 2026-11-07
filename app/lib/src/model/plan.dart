import 'config.dart';
import 'json.dart';

/// 一条字段级差异（`planner.Change`）。
class Change {
  Change({this.field = '', this.from = '', this.to = ''});

  factory Change.fromJson(Map<String, Object?> json) => Change(
        field: Json.str(json['field']),
        from: Json.str(json['from']),
        to: Json.str(json['to']),
      );

  String field;

  /// 值在契约里就是字符串（数字也会被 Go 转成文本），所以这里不猜类型。
  String from;
  String to;

  Map<String, Object?> toJson() =>
      <String, Object?>{'field': field, 'from': from, 'to': to};

  @override
  String toString() => '$field: $from → $to';
}

/// 对一个订阅要做的事（`planner.Action`）。
class PlanAction {
  PlanAction({
    this.instance = '',
    this.aniId = '',
    this.title = '',
    this.kind = '',
    this.reason = '',
    this.changes,
    this.target = '',
    this.profile = '',
  });

  factory PlanAction.fromJson(Map<String, Object?> json) => PlanAction(
        instance: Json.str(json['instance']),
        aniId: Json.str(json['aniId']),
        title: Json.str(json['title']),
        kind: Json.str(json['kind']),
        reason: Json.str(json['reason']),
        changes: Json.mapList(json['changes']).map(Change.fromJson).toList(),
        target: Json.str(json['target']),
        profile: Json.str(json['profile']),
      );

  String instance;
  String aniId;
  String title;

  /// `update` / `add` / `noop` / `error`。
  String kind;
  String reason;
  List<Change>? changes;

  /// 命中的番剧级配置名（`targets` 里的 title / bgmId）。
  String target;

  /// 这条订阅实际生效的方案名；空 = 顶层 rules。
  String profile;

  List<Change> get changesOrEmpty => changes ?? const <Change>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'instance': instance,
        'aniId': aniId,
        'title': title,
        'kind': kind,
        if (reason.isNotEmpty) 'reason': reason,
        if (changes != null && changes!.isNotEmpty)
          'changes': changes!.map((e) => e.toJson()).toList(),
        if (target.isNotEmpty) 'target': target,
        if (profile.isNotEmpty) 'profile': profile,
      };
}

/// 每个站点在本次计划里的产出。
class SiteSummary {
  SiteSummary({
    this.site = '',
    this.candidates = 0,
    this.requests = 0,
    this.failures = 0,
    this.error = '',
  });

  factory SiteSummary.fromJson(Map<String, Object?> json) => SiteSummary(
        site: Json.str(json['site']),
        candidates: Json.intOf(json['candidates']),
        requests: Json.intOf(json['requests']),
        failures: Json.intOf(json['failures']),
        error: Json.str(json['error']),
      );

  String site;
  int candidates;
  int requests;
  int failures;
  String error;

  Map<String, Object?> toJson() => <String, Object?>{
        'site': site,
        'candidates': candidates,
        'requests': requests,
        'failures': failures,
        if (error.isNotEmpty) 'error': error,
      };
}

/// 被跳过的订阅及原因。
class SkippedReason {
  SkippedReason({this.instance = '', this.title = '', this.reason = ''});

  factory SkippedReason.fromJson(Map<String, Object?> json) => SkippedReason(
        instance: Json.str(json['instance']),
        title: Json.str(json['title']),
        reason: Json.str(json['reason']),
      );

  String instance;
  String title;
  String reason;

  Map<String, Object?> toJson() =>
      <String, Object?>{'instance': instance, 'title': title, 'reason': reason};
}

/// 「这个实例这次按哪套口味在跑」（`planner.SchemeSummary`）。
///
/// 无人值守时这是唯一能回答该问题的地方：只看计划里的逐条变更，看不出规则来自哪个方案。
class SchemeSummary {
  SchemeSummary({
    this.instance = '',
    this.profile = '',
    this.maxStandby = 1,
    this.siteOrder,
    this.ruleCount = 0,
    this.denyCount = 0,
    this.ruleFiles,
  });

  factory SchemeSummary.fromJson(Map<String, Object?> json) => SchemeSummary(
        instance: Json.str(json['instance']),
        profile: Json.str(json['profile']),
        maxStandby: Json.intOf(json['maxStandby'], 1),
        siteOrder: Json.strListOrNull(json['siteOrder']),
        ruleCount: Json.intOf(json['ruleCount']),
        denyCount: Json.intOf(json['denyCount']),
        ruleFiles: Json
            .mapList(json['ruleFiles'])
            .map(RuleFileInfo.fromJson)
            .toList(),
      );

  String instance;

  /// 空 = 用顶层 rules。
  String profile;
  int maxStandby;
  List<String>? siteOrder;
  int ruleCount;
  int denyCount;
  List<RuleFileInfo>? ruleFiles;

  List<String> get siteOrderOrEmpty => siteOrder ?? const <String>[];
  List<RuleFileInfo> get ruleFilesOrEmpty =>
      ruleFiles ?? const <RuleFileInfo>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'instance': instance,
        if (profile.isNotEmpty) 'profile': profile,
        'maxStandby': maxStandby,
        'siteOrder': siteOrder,
        'ruleCount': ruleCount,
        'denyCount': denyCount,
        if (ruleFiles != null && ruleFiles!.isNotEmpty)
          'ruleFiles': ruleFiles!.map((e) => e.toJson()).toList(),
      };
}

/// 一次完整的打点计划（`planner.Plan`）。
class PlanView {
  PlanView({
    this.actions,
    this.sites,
    this.skipped,
    this.siteInstance = '',
    this.discovered,
    this.discoverNotes,
    this.schemes,
  });

  factory PlanView.fromJson(Map<String, Object?> json) => PlanView(
        actions: Json.mapList(json['actions']).map(PlanAction.fromJson).toList(),
        sites: Json.mapList(json['sites']).map(SiteSummary.fromJson).toList(),
        skipped:
            Json.mapList(json['skipped']).map(SkippedReason.fromJson).toList(),
        siteInstance: Json.str(json['siteInstance']),
        discovered: Json.mapList(json['discovered']),
        discoverNotes: Json.strListOrNull(json['discoverNotes']),
        schemes:
            Json.mapList(json['schemes']).map(SchemeSummary.fromJson).toList(),
      );

  List<PlanAction>? actions;
  List<SiteSummary>? sites;
  List<SkippedReason>? skipped;

  /// 本次借用站点数据的实例名。
  String siteInstance;

  /// 自动发现扫到的番剧（`discover.Item` + 由发现新增的标记）。
  List<Map<String, Object?>>? discovered;
  List<String>? discoverNotes;
  List<SchemeSummary>? schemes;

  List<PlanAction> get actionsOrEmpty => actions ?? const <PlanAction>[];
  List<SiteSummary> get sitesOrEmpty => sites ?? const <SiteSummary>[];
  List<SkippedReason> get skippedOrEmpty => skipped ?? const <SkippedReason>[];
  List<SchemeSummary> get schemesOrEmpty => schemes ?? const <SchemeSummary>[];

  /// 可写入的条数（`update` + `add`）。
  int get writableCount => actionsOrEmpty
      .where((a) => a.kind == 'update' || a.kind == 'add')
      .length;

  /// 按 `actions[].target` 数出每条番剧级配置命中了多少条订阅。
  /// 键是 `targets` 里那个可读名（title / jpTitle / bgmId）。
  Map<String, int> get hitsByTarget {
    final out = <String, int>{};
    for (final action in actionsOrEmpty) {
      if (action.target.isEmpty) continue;
      out[action.target] = (out[action.target] ?? 0) + 1;
    }
    return out;
  }

  Map<String, Object?> toJson() => <String, Object?>{
        'actions': actions?.map((e) => e.toJson()).toList(),
        'sites': sites?.map((e) => e.toJson()).toList(),
        'skipped': skipped?.map((e) => e.toJson()).toList(),
        if (siteInstance.isNotEmpty) 'siteInstance': siteInstance,
        if (discovered != null && discovered!.isNotEmpty)
          'discovered': discovered,
        if (discoverNotes != null && discoverNotes!.isNotEmpty)
          'discoverNotes': discoverNotes,
        if (schemes != null && schemes!.isNotEmpty)
          'schemes': schemes!.map((e) => e.toJson()).toList(),
      };
}

/// `plan.build` 的响应壳。
class PlanBuildResult {
  PlanBuildResult({this.plan});

  factory PlanBuildResult.fromJson(Map<String, Object?> json) => PlanBuildResult(
        plan: PlanView.fromJson(Json.map(json['plan'])),
      );

  PlanView? plan;

  PlanView get planOrEmpty => plan ?? PlanView();
}
