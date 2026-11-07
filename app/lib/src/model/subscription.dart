import 'config.dart';
import 'json.dart';
import 'plan.dart';

/// `subscription.set` 的一次字段级变更 —— 与 `planner.Change` 是同一个类型。

/// 一条 RSS 引用（主 RSS 与每条副 RSS 都是这个形状）。
class RssRef {
  RssRef({this.url = '', this.group = '', this.title = '', this.offset});

  factory RssRef.fromJson(Map<String, Object?> json) => RssRef(
        url: Json.str(json['url']),
        group: Json.str(json['group']),
        title: Json.str(json['title']),
        offset: Json.intOrNull(json['offset']),
      );

  String url;
  String group;
  String title;

  /// 只有副 RSS 会带自己的集偏移。
  int? offset;

  Map<String, Object?> toJson() => <String, Object?>{
        'url': url,
        'group': group,
        'title': title,
        if (offset != null) 'offset': offset,
      };
}

/// 实例里真实存在的一条订阅（`docs/ops-api.md` §6.3）。
class SubscriptionView {
  SubscriptionView({
    this.key = '',
    this.id = '',
    this.title = '',
    this.bgmId = '',
    this.bgmUrl = '',
    this.image = '',
    this.season,
    this.offset,
    this.standbyOffset,
    this.enable = false,
    this.profile = '',
    this.main,
    this.standby,
    this.issues,
    this.raw,
  });

  factory SubscriptionView.fromJson(Map<String, Object?> json) {
    final standby = Json.mapList(json['standby']);
    return SubscriptionView(
      key: Json.str(json['key']),
      id: Json.str(json['id']),
      title: Json.str(json['title']),
      bgmId: Json.str(json['bgmId']),
      bgmUrl: Json.str(json['bgmUrl']),
      image: Json.str(json['image']),
      season: Json.intOrNull(json['season']),
      offset: Json.intOrNull(json['offset']),
      standbyOffset: Json.intOrNull(json['standbyOffset']),
      enable: Json.boolOf(json['enable']),
      profile: Json.str(json['profile']),
      main: json['main'] == null
          ? null
          : RssRef.fromJson(Json.map(json['main'])),
      standby: json['standby'] == null
          ? null
          : standby.map(RssRef.fromJson).toList(),
      issues: Json.strList(json['issues']),
      raw: Json.mapOrNull(json['raw']),
    );
  }

  /// **稳定标识**：`id:<id>` / `bgm:<bgmId>` / `title:<去空白小写>`。UI 只认它。
  String key;
  String id;
  String title;
  String bgmId;
  String bgmUrl;
  String image;

  /// 实例里可能是任意值（对象、字符串、null）；读不出数字就给 `null`，**不猜**。
  int? season;
  int? offset;
  int? standbyOffset;

  bool enable;

  /// 该订阅自己的方案覆盖；空 = 跟实例走。
  String profile;

  RssRef? main;
  List<RssRef>? standby;

  /// 读取过程中的降级说明（例如「副 RSS 第 2 条不是对象，已跳过」）。
  List<String>? issues;

  /// 实例返回的原始对象，**逐字不改**。`setAni` 是全量覆盖，改完要整份交回去。
  Map<String, Object?>? raw;

  List<RssRef> get standbyOrEmpty => standby ?? const <RssRef>[];
  List<String> get issuesOrEmpty => issues ?? const <String>[];

  bool get hasStandby => standbyOrEmpty.isNotEmpty;

  /// 「未匹配规则」在 UI 上的判据：没有主 RSS 且没有副 RSS。
  bool get hasNoMatch => main == null && standbyOrEmpty.isEmpty;

  Map<String, Object?> toJson() => <String, Object?>{
        'key': key,
        'id': id,
        'title': title,
        'bgmId': bgmId,
        'bgmUrl': bgmUrl,
        'image': image,
        if (season != null) 'season': season,
        if (offset != null) 'offset': offset,
        if (standbyOffset != null) 'standbyOffset': standbyOffset,
        'enable': enable,
        'profile': profile,
        if (main != null) 'main': main!.toJson(),
        if (standby != null) 'standby': standby!.map((e) => e.toJson()).toList(),
        if (issues != null) 'issues': issues,
        if (raw != null) 'raw': raw,
      };
}

/// `instance.subscriptions` 的响应。
class SubscriptionListResult {
  SubscriptionListResult({this.name = '', this.total = 0, this.items, this.error});

  factory SubscriptionListResult.fromJson(Map<String, Object?> json) =>
      SubscriptionListResult(
        name: Json.str(json['name']),
        total: Json.intOf(json['total']),
        items: Json
            .mapList(json['items'])
            .map(SubscriptionView.fromJson)
            .toList(),
        error: Json.strOrNull(json['error']),
      );

  String name;
  int total;
  List<SubscriptionView>? items;

  /// 「读实例失败」时非空：此时「已添加」判定不可信，界面要显式说明。
  String? error;

  List<SubscriptionView> get itemsOrEmpty =>
      items ?? const <SubscriptionView>[];
}

/// `subscription.set` 的一次字段级变更。与 `planner.Change` 同一个类，避免两份定义漂移。
typedef SubscriptionChange = Change;

/// `subscription.set` 的逐条结果。
class SubscriptionSetItemResult {
  SubscriptionSetItemResult({this.key = '', this.changed, this.error});

  factory SubscriptionSetItemResult.fromJson(Map<String, Object?> json) =>
      SubscriptionSetItemResult(
        key: Json.str(json['key']),
        changed: Json
            .mapList(json['changed'])
            .map(SubscriptionChange.fromJson)
            .toList(),
        error: Json.strOrNull(json['error']),
      );

  String key;
  List<SubscriptionChange>? changed;
  String? error;

  List<SubscriptionChange> get changedOrEmpty =>
      changed ?? const <SubscriptionChange>[];
}

/// `subscription.set` 的响应。
class SubscriptionSetResult {
  SubscriptionSetResult({this.updated = 0, this.skipped = 0, this.results});

  factory SubscriptionSetResult.fromJson(Map<String, Object?> json) =>
      SubscriptionSetResult(
        updated: Json.intOf(json['updated']),
        skipped: Json.intOf(json['skipped']),
        results: Json
            .mapList(json['results'])
            .map(SubscriptionSetItemResult.fromJson)
            .toList(),
      );

  int updated;
  int skipped;
  List<SubscriptionSetItemResult>? results;

  List<SubscriptionSetItemResult> get resultsOrEmpty =>
      results ?? const <SubscriptionSetItemResult>[];
}

/// `subscription.set` 的单条变更请求。
class SubscriptionPatch {
  SubscriptionPatch({
    this.season,
    this.offset,
    this.standbyOffset,
    this.enable,
    this.profile,
  });

  /// **没写的字段一律不动**：所以这些字段可空，`null` 时整个键省略。
  int? season;
  int? offset;
  int? standbyOffset;
  bool? enable;

  /// `''` 表示**清除**该订阅的方案覆盖（与省略区分）。
  String? profile;

  bool get isEmpty =>
      season == null &&
      offset == null &&
      standbyOffset == null &&
      enable == null &&
      profile == null;

  Map<String, Object?> toJson() => <String, Object?>{
        if (season != null) 'season': season,
        if (offset != null) 'offset': offset,
        if (standbyOffset != null) 'standbyOffset': standbyOffset,
        if (enable != null) 'enable': enable,
        if (profile != null) 'profile': profile,
      };
}

/// `subscription.set` 的一条请求项。
class SubscriptionSetItem {
  SubscriptionSetItem({required this.key, this.raw, SubscriptionPatch? patch})
      : patch = patch ?? SubscriptionPatch();

  final String key;

  /// 省略时后端按 `key` 重新读一次 `listAni` 定位；给了就用给的那份（UI 拿到的就是最新的）。
  final Map<String, Object?>? raw;
  final SubscriptionPatch patch;

  Map<String, Object?> toJson() => <String, Object?>{
        'key': key,
        if (raw != null) 'raw': raw,
        'patch': patch.toJson(),
      };
}

/// `subscription.delete` 的响应。
class SubscriptionDeleteResult {
  SubscriptionDeleteResult({this.deleted = 0, this.errors});

  factory SubscriptionDeleteResult.fromJson(Map<String, Object?> json) =>
      SubscriptionDeleteResult(
        deleted: Json.intOf(json['deleted']),
        errors: Json.strList(json['errors']),
      );

  int deleted;
  List<String>? errors;

  List<String> get errorsOrEmpty => errors ?? const <String>[];
}

/// 给「清除方案覆盖」用的哨兵：`patch.profile = ''` 才是清除，`null` 是「不动」。
const String clearProfileOverride = '';

/// 方案下拉的可选值：空 + 所有方案名。
List<String> profileChoices(Config config) => <String>[
      '',
      ...config.profilesOrEmpty.map((p) => p.name),
    ];
