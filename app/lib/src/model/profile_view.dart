import 'config.dart';
import 'instance_view.dart';
import 'json.dart';

/// 一个方案的只读视图（`docs/ops-api.md` §6.4）。
class ProfileView {
  ProfileView({this.name = '', Rules? rules, this.sources, this.ruleFiles})
      : rules = rules ?? Rules();

  factory ProfileView.fromJson(Map<String, Object?> json) => ProfileView(
        name: Json.str(json['name']),
        rules: Rules.fromJson(Json.map(json['rules'])),
        sources: json['sources'] == null
            ? null
            : ProfileSources.fromJson(Json.map(json['sources'])),
        ruleFiles: Json
            .mapList(json['ruleFiles'])
            .map(RuleFileInfo.fromJson)
            .toList(),
      );

  String name;
  Rules rules;
  ProfileSources? sources;
  List<RuleFileInfo>? ruleFiles;

  List<RuleFileInfo> get ruleFilesOrEmpty =>
      ruleFiles ?? const <RuleFileInfo>[];
}

/// `config.get` 的 `views` 派生块：一次调用就把界面要用的表算好。
class ConfigViews {
  ConfigViews({this.instances, this.profiles});

  factory ConfigViews.fromJson(Map<String, Object?> json) => ConfigViews(
        instances: Json
            .mapList(json['instances'])
            .map(InstanceView.fromJson)
            .toList(),
        profiles:
            Json.mapList(json['profiles']).map(ProfileView.fromJson).toList(),
      );

  List<InstanceView>? instances;
  List<ProfileView>? profiles;

  List<InstanceView> get instancesOrEmpty =>
      instances ?? const <InstanceView>[];
  List<ProfileView> get profilesOrEmpty => profiles ?? const <ProfileView>[];
}

/// `profile.ruleFile` 的结果：外置规则文件的内容，**只读**。
///
/// 注意字段名是 `path`（不是 `file`）—— 与 `RuleFileInfo.path` 统一，
/// 见 `docs/ops-api.md` 顶部的变更记录「去歧义」那一行。
class RuleFileDetail {
  RuleFileDetail({
    this.owner = '',
    this.path = '',
    this.kind = '',
    this.rules,
    this.denyRules,
  });

  factory RuleFileDetail.fromJson(Map<String, Object?> json) => RuleFileDetail(
        owner: Json.str(json['owner']),
        path: Json.str(json['path']),
        kind: Json.str(json['kind']),
        rules: Json.mapList(json['rules']).map(Rule.fromJson).toList(),
        denyRules:
            Json.mapList(json['denyRules']).map(DenyRule.fromJson).toList(),
      );

  /// 归属：空 = 顶层，否则是方案名。
  ///
  /// `RuleFileInfo` 本身**没有** owner —— owner 是外层对象的属性
  /// （`InstanceView` 的持有者是 `instance`，`ProfileView` 的是 `name`，顶层没有名字）。
  String owner;
  String path;

  /// `primary` / `deny`。
  String kind;
  List<Rule>? rules;
  List<DenyRule>? denyRules;

  List<Rule> get rulesOrEmpty => rules ?? const <Rule>[];
  List<DenyRule> get denyRulesOrEmpty => denyRules ?? const <DenyRule>[];
}

/// `profile.delete` 的响应。
class ProfileDeleteResult {
  ProfileDeleteResult({this.affected, this.viewNames});

  factory ProfileDeleteResult.fromJson(Map<String, Object?> json) =>
      ProfileDeleteResult(
        affected: Json
            .mapList(json['affected'])
            .map((e) => Json.str(e['instance']))
            .toList(),
      );

  /// 受影响的实例名。
  List<String>? affected;
  List<String>? viewNames;

  List<String> get affectedOrEmpty => affected ?? const <String>[];
}
