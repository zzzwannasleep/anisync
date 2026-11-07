import 'json.dart';
import 'plan.dart';

/// 一个实例的**只读派生视图**（字段与 `docs/ops-api.md` §6.1 一致）。
///
/// ⚠️ `apiKey` 是**遮蔽值**（前 6 后 2；长度 <10 时全遮蔽）。
/// 编辑实例必须改 `Config.instances` 里的原文，把这个遮蔽值写回去会当场毁掉用户的密钥。
class InstanceView {
  InstanceView({
    this.name = '',
    this.baseUrl = '',
    this.apiKey = '',
    this.hasApiKey = false,
    this.enabled = true,
    this.profile = '',
    this.profileLabel = '',
    SchemeSummary? scheme,
  }) : scheme = scheme ?? SchemeSummary();

  factory InstanceView.fromJson(Map<String, Object?> json) => InstanceView(
        name: Json.str(json['name']),
        baseUrl: Json.str(json['baseUrl']),
        apiKey: Json.str(json['apiKey']),
        hasApiKey: Json.boolOf(json['hasApiKey']),
        enabled: Json.boolOf(json['enabled'], true),
        profile: Json.str(json['profile']),
        profileLabel: Json.str(json['profileLabel']),
        scheme: SchemeSummary.fromJson(Json.map(json['scheme'])),
      );

  String name;
  String baseUrl;

  /// 遮蔽后的密钥，只用于展示。
  String apiKey;
  bool hasApiKey;

  /// 这里是**生效值**（不是三态）：三态只在 `Config.instances[]` 上。
  bool enabled;

  /// 空 = 用顶层 rules。
  String profile;

  /// 空 = 「默认（顶层 rules）」。
  String profileLabel;
  SchemeSummary scheme;

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'baseUrl': baseUrl,
        'apiKey': apiKey,
        'hasApiKey': hasApiKey,
        'enabled': enabled,
        'profile': profile,
        'profileLabel': profileLabel,
        'scheme': scheme.toJson(),
      };
}

/// `instance.list` / `instance.test` 的响应壳。
class InstanceListResult {
  InstanceListResult({this.instances});

  factory InstanceListResult.fromJson(Map<String, Object?> json) =>
      InstanceListResult(
        instances: Json
            .mapList(json['instances'])
            .map(InstanceView.fromJson)
            .toList(),
      );

  List<InstanceView>? instances;

  List<InstanceView> get instancesOrEmpty => instances ?? const <InstanceView>[];
}

/// `instance.test` 的结果。
class InstanceTestResult {
  InstanceTestResult({
    this.name = '',
    this.ok = false,
    this.latencyMs = 0,
    this.about,
    this.subscriptionCount = 0,
    this.error,
  });

  factory InstanceTestResult.fromJson(Map<String, Object?> json) =>
      InstanceTestResult(
        name: Json.str(json['name']),
        ok: Json.boolOf(json['ok']),
        latencyMs: Json.intOf(json['latencyMs']),
        about: Json.mapOrNull(json['about']),
        subscriptionCount: Json.intOf(json['subscriptionCount']),
        error: Json.strOrNull(json['error']),
      );

  String name;
  bool ok;
  int latencyMs;
  Map<String, Object?>? about;
  int subscriptionCount;
  String? error;

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'ok': ok,
        'latencyMs': latencyMs,
        if (about != null) 'about': about,
        'subscriptionCount': subscriptionCount,
        if (error != null) 'error': error,
      };
}
