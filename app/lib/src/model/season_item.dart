import 'json.dart';

/// 一条本季新番（`discover.Item` + `added`）。
class SeasonItem {
  SeasonItem({
    this.bgmId = '',
    this.title = '',
    this.jpTitle = '',
    this.source = '',
    this.airAt,
    this.cover = '',
    this.summary = '',
    this.doing = 0,
    this.score = 0,
    this.weekday = 0,
    this.added = false,
  });

  factory SeasonItem.fromJson(Map<String, Object?> json) => SeasonItem(
        bgmId: Json.str(json['bgmId']),
        title: Json.str(json['title']),
        jpTitle: Json.str(json['jpTitle']),
        source: Json.str(json['source']),
        airAt: Json.time(json['airAt']),
        cover: Json.str(json['cover']),
        summary: Json.str(json['summary']),
        doing: Json.intOf(json['doing']),
        score: Json.doubleOf(json['score']),
        weekday: Json.intOf(json['weekday']),
        added: Json.boolOf(json['added']),
      );

  String bgmId;
  String title;
  String jpTitle;
  String source;
  DateTime? airAt;

  /// 已统一成 https 的封面地址。
  String cover;
  String summary;

  /// Bangumi「在看」人数。
  int doing;

  /// Bangumi 评分；没人评分时为 0。
  double score;

  /// 放送星期，1 = 周一，0 = 未知。
  int weekday;

  /// 实例里已经有它（由 Go 侧判定）。
  bool added;

  Map<String, Object?> toJson() => <String, Object?>{
        'bgmId': bgmId,
        'title': title,
        if (jpTitle.isNotEmpty) 'jpTitle': jpTitle,
        'source': source,
        if (airAt != null) 'airAt': airAt!.toIso8601String(),
        if (cover.isNotEmpty) 'cover': cover,
        if (summary.isNotEmpty) 'summary': summary,
        if (doing != 0) 'doing': doing,
        if (score != 0) 'score': score,
        if (weekday != 0) 'weekday': weekday,
        'added': added,
      };
}

/// `discover.list` 的响应（与现有 `/api/season` 同形）。
class DiscoverListResult {
  DiscoverListResult({
    this.items,
    this.total = 0,
    this.added = 0,
    this.filtered = 0,
    this.source = '',
    this.warnings,
    this.unmatched,
    this.instanceError,
    this.subscribed,
  });

  factory DiscoverListResult.fromJson(Map<String, Object?> json) =>
      DiscoverListResult(
        items: Json.mapList(json['items']).map(SeasonItem.fromJson).toList(),
        total: Json.intOf(json['total']),
        added: Json.intOf(json['added']),
        filtered: Json.intOf(json['filtered']),
        source: Json.str(json['source']),
        warnings: Json.strList(json['warnings']),
        unmatched: Json.intOrNull(json['unmatched']),
        instanceError: Json.strOrNull(json['instanceError']),
        subscribed: Json.intOrNull(json['subscribed']),
      );

  List<SeasonItem>? items;
  int total;

  /// 其中已添加的条数。
  int added;

  /// 被 `japanOnly` 过滤掉的条数 —— 必须显示，否则用户只会看到列表变短了。
  int filtered;
  String source;

  /// 单个来源的失败信息；只有全部来源都失败才算致命错误。
  List<String>? warnings;

  /// 来源里有、但在 Bangumi 上认不出条目的条数。
  int? unmatched;

  /// 读实例失败的原因。非空时「已添加」判定不可信，界面要显式说明。
  String? instanceError;

  /// 读到的订阅总数。
  int? subscribed;

  List<SeasonItem> get itemsOrEmpty => items ?? const <SeasonItem>[];
  List<String> get warningsOrEmpty => warnings ?? const <String>[];
}

/// `bgm.search` 的一条结果。
class BgmSubject {
  BgmSubject({
    this.id = '',
    this.name = '',
    this.nameCN = '',
    this.image = '',
    this.summary = '',
    this.date = '',
    this.score = 0,
  });

  factory BgmSubject.fromJson(Map<String, Object?> json) => BgmSubject(
        id: Json.str(json['id']),
        name: Json.str(json['name']),
        nameCN: Json.str(json['nameCN']),
        image: Json.str(json['image']),
        summary: Json.str(json['summary']),
        date: Json.str(json['date']),
        score: Json.doubleOf(json['score']),
      );

  String id;
  String name;
  String nameCN;
  String image;
  String summary;
  String date;
  double score;

  /// 卡片标题：中文名优先。
  String get displayName => nameCN.isNotEmpty ? nameCN : name;

  Map<String, Object?> toJson() => <String, Object?>{
        'id': id,
        'name': name,
        'nameCN': nameCN,
        'image': image,
        'summary': summary,
        'date': date,
        'score': score,
      };
}

/// `bgm.search` 的响应。
class BgmSearchResult {
  BgmSearchResult({this.items});

  factory BgmSearchResult.fromJson(Map<String, Object?> json) =>
      BgmSearchResult(
        items: Json.mapList(json['items']).map(BgmSubject.fromJson).toList(),
      );

  List<BgmSubject>? items;

  List<BgmSubject> get itemsOrEmpty => items ?? const <BgmSubject>[];
}
