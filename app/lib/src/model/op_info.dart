import 'json.dart';

/// `core.ops` 的一条 op 描述（`{name, group, summary, async}`）。
class OpInfo {
  OpInfo({
    this.name = '',
    this.group = '',
    this.summary = '',
    this.isAsync = false,
  });

  factory OpInfo.fromJson(Map<String, Object?> json) => OpInfo(
        name: Json.str(json['name']),
        group: Json.str(json['group']),
        summary: Json.str(json['summary']),
        isAsync: Json.boolOf(json['async']),
      );

  String name;
  String group;
  String summary;
  bool isAsync;

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'group': group,
        'summary': summary,
        'async': isAsync,
      };
}

/// `core.ops` 的响应。
class OpsCatalogue {
  OpsCatalogue({this.schema = 0, this.ops});

  factory OpsCatalogue.fromJson(Map<String, Object?> json) => OpsCatalogue(
        schema: Json.intOf(json['schema']),
        ops: Json.mapList(json['ops']).map(OpInfo.fromJson).toList(),
      );

  int schema;
  List<OpInfo>? ops;

  List<OpInfo> get opsOrEmpty => ops ?? const <OpInfo>[];
}

/// `core.init` / `core.version` 的响应。
class CoreInfo {
  CoreInfo({
    this.version = '',
    this.schema = 0,
    this.platform = '',
    this.configPath = '',
    this.dataDir = '',
    this.goVersion = '',
  });

  factory CoreInfo.fromJson(Map<String, Object?> json) => CoreInfo(
        version: Json.str(json['version']),
        schema: Json.intOf(json['schema']),
        platform: Json.str(json['platform']),
        configPath: Json.str(json['configPath']),
        dataDir: Json.str(json['dataDir']),
        goVersion: Json.str(json['goVersion']),
      );

  String version;
  int schema;
  String platform;
  String configPath;
  String dataDir;
  String goVersion;

  Map<String, Object?> toJson() => <String, Object?>{
        'version': version,
        'schema': schema,
        'platform': platform,
        'configPath': configPath,
        'dataDir': dataDir,
        'goVersion': goVersion,
      };
}

/// `config.get` 的响应。
class ConfigGetResult {
  ConfigGetResult({
    this.path = '',
    this.config,
    this.ruleFiles,
    this.views,
    this.warnings,
  });

  factory ConfigGetResult.fromJson(Map<String, Object?> json) =>
      ConfigGetResult(
        path: Json.str(json['path']),
        config: json['config'],
        ruleFiles: json['ruleFiles'],
        views: json['views'],
        warnings: Json.strList(json['warnings']),
      );

  String path;
  Object? config;
  Object? ruleFiles;
  Object? views;
  List<String>? warnings;

  List<String> get warningsOrEmpty => warnings ?? const <String>[];
}

/// `config.save` 的响应（§7.2）。
///
/// **`valid:false` 不是保存失败**：文件真的写下去了。`config.save` 是「总是落盘」，
/// 校验该在 `check` / `plan` / `sync` 上拦（那些真的会碰实例）。
/// 唯一的硬错误是「引用的外置规则文件读不出来」——那时**不落盘**，
/// 以 `CoreException` 的形式回来，不走这里。
class ConfigWriteResult {
  ConfigWriteResult({
    this.path = '',
    this.valid = true,
    this.errors,
    this.warnings,
  });

  factory ConfigWriteResult.fromJson(Map<String, Object?> json) =>
      ConfigWriteResult(
        path: Json.str(json['path']),
        valid: Json.boolOf(json['valid'], true),
        errors: Json.mapList(json['errors']).map(ConfigIssue.fromJson).toList(),
        warnings: Json.strList(json['warnings']),
      );

  String path;

  /// 这份配置能不能真的跑同步。**外置规则文件读不出来属于硬错误，不会走到这里。**
  bool valid;
  List<ConfigIssue>? errors;
  List<String>? warnings;

  List<ConfigIssue> get errorsOrEmpty => errors ?? const <ConfigIssue>[];
  List<String> get warningsOrEmpty => warnings ?? const <String>[];
}

/// `config.validate` 的一个字段级错误。
class ConfigIssue {
  ConfigIssue({this.field = '', this.message = ''});

  factory ConfigIssue.fromJson(Map<String, Object?> json) => ConfigIssue(
        field: Json.str(json['field']),
        message: Json.str(json['message']),
      );

  String field;
  String message;
}

/// `config.validate` 的响应。
class ConfigValidateResult {
  ConfigValidateResult({this.ok = false, this.errors, this.warnings});

  factory ConfigValidateResult.fromJson(Map<String, Object?> json) =>
      ConfigValidateResult(
        ok: Json.boolOf(json['ok']),
        errors: Json.mapList(json['errors']).map(ConfigIssue.fromJson).toList(),
        warnings: Json.strList(json['warnings']),
      );

  bool ok;
  List<ConfigIssue>? errors;
  List<String>? warnings;

  List<ConfigIssue> get errorsOrEmpty => errors ?? const <ConfigIssue>[];
  List<String> get warningsOrEmpty => warnings ?? const <String>[];
}

/// `config.sample` 的响应。
class ConfigSampleResult {
  ConfigSampleResult({this.yaml = ''});

  factory ConfigSampleResult.fromJson(Map<String, Object?> json) =>
      ConfigSampleResult(yaml: Json.str(json['yaml']));

  String yaml;
}
