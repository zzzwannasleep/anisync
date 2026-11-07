import 'json.dart';
import 'lease.dart';
import 'plan.dart';
import 'run_entry.dart';

/// `status.get` 的响应。
///
/// 这个 op **不发任何网络请求**（既有承诺），所以它可以在离线时照常工作。
class StatusResult {
  StatusResult({
    this.version = '',
    this.platform = '',
    this.configPath = '',
    this.dataDir = '',
    this.instances,
    LeaseView? lease,
    this.runs,
    this.nextRunAt,
  }) : lease = lease ?? LeaseView();

  factory StatusResult.fromJson(Map<String, Object?> json) => StatusResult(
        version: Json.str(json['version']),
        platform: Json.str(json['platform']),
        configPath: Json.str(json['configPath']),
        dataDir: Json.str(json['dataDir']),
        instances:
            Json.mapList(json['instances']).map(SchemeSummary.fromJson).toList(),
        lease: LeaseView.fromJson(Json.map(json['lease'])),
        runs: Json.mapList(json['runs']).map(RunEntry.fromJson).toList(),
        nextRunAt: Json.time(json['nextRunAt']),
      );

  String version;
  String platform;
  String configPath;
  String dataDir;

  /// 每个实例生效的方案（`SchemeView`）。
  List<SchemeSummary>? instances;
  LeaseView lease;
  List<RunEntry>? runs;
  DateTime? nextRunAt;

  List<SchemeSummary> get instancesOrEmpty =>
      instances ?? const <SchemeSummary>[];
  List<RunEntry> get runsOrEmpty => runs ?? const <RunEntry>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'version': version,
        'platform': platform,
        'configPath': configPath,
        'dataDir': dataDir,
        'instances': instances?.map((e) => e.toJson()).toList(),
        'lease': lease.toJson(),
        'runs': runs?.map((e) => e.toJson()).toList(),
        if (nextRunAt != null) 'nextRunAt': nextRunAt!.toIso8601String(),
      };
}
