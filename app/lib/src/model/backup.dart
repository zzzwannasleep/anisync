import 'json.dart';

/// 备份里某个实例的条数。
class BackupInstanceCount {
  BackupInstanceCount({this.name = '', this.count = 0, this.file = ''});

  factory BackupInstanceCount.fromJson(Map<String, Object?> json) =>
      BackupInstanceCount(
        name: Json.str(json['name']),
        count: Json.intOf(json['count']),
        file: Json.str(json['file']),
      );

  String name;
  int count;
  String file;

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'count': count,
        if (file.isNotEmpty) 'file': file,
      };
}

/// `backup.create` 的响应。
class BackupCreateResult {
  BackupCreateResult({this.instances, this.dir = ''});

  factory BackupCreateResult.fromJson(Map<String, Object?> json) =>
      BackupCreateResult(
        instances: Json
            .mapList(json['instances'])
            .map(BackupInstanceCount.fromJson)
            .toList(),
        dir: Json.str(json['dir']),
      );

  List<BackupInstanceCount>? instances;
  String dir;

  List<BackupInstanceCount> get instancesOrEmpty =>
      instances ?? const <BackupInstanceCount>[];

  int get total => instancesOrEmpty.fold(0, (sum, e) => sum + e.count);
}

/// 一个已有的备份文件。
class BackupEntry {
  BackupEntry({
    this.file = '',
    this.path = '',
    this.createdAt,
    this.size = 0,
    this.instances,
  });

  factory BackupEntry.fromJson(Map<String, Object?> json) => BackupEntry(
        file: Json.str(json['file']),
        path: Json.str(json['path']),
        createdAt: Json.time(json['createdAt']),
        size: Json.intOf(json['size']),
        instances: Json
            .mapList(json['instances'])
            .map(BackupInstanceCount.fromJson)
            .toList(),
      );

  String file;
  String path;
  DateTime? createdAt;
  int size;
  List<BackupInstanceCount>? instances;

  List<BackupInstanceCount> get instancesOrEmpty =>
      instances ?? const <BackupInstanceCount>[];

  int get total => instancesOrEmpty.fold(0, (sum, e) => sum + e.count);

  Map<String, Object?> toJson() => <String, Object?>{
        'file': file,
        'path': path,
        if (createdAt != null) 'createdAt': createdAt!.toIso8601String(),
        'size': size,
        'instances': instances?.map((e) => e.toJson()).toList(),
      };
}

/// `backup.list` 的响应。
class BackupListResult {
  BackupListResult({this.dir = '', this.backups});

  factory BackupListResult.fromJson(Map<String, Object?> json) =>
      BackupListResult(
        dir: Json.str(json['dir']),
        backups:
            Json.mapList(json['backups']).map(BackupEntry.fromJson).toList(),
      );

  String dir;
  List<BackupEntry>? backups;

  List<BackupEntry> get backupsOrEmpty => backups ?? const <BackupEntry>[];
}
