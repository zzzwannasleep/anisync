import 'json.dart';

/// 单飞租约视图：`{"held":false}` 或 `{"held":true,"pid":..,"note":..,"owner":..,"ageSeconds":..,"expired":..}`。
class LeaseView {
  LeaseView({
    this.held = false,
    this.pid = 0,
    this.note = '',
    this.owner = '',
    this.ageSeconds = 0,
    this.expired = false,
  });

  factory LeaseView.fromJson(Map<String, Object?> json) => LeaseView(
        held: Json.boolOf(json['held']),
        pid: Json.intOf(json['pid']),
        note: Json.str(json['note']),
        owner: Json.str(json['owner']),
        ageSeconds: Json.doubleOf(json['ageSeconds']),
        expired: Json.boolOf(json['expired']),
      );

  bool held;

  /// 持有者进程号，只用于排障。
  int pid;

  /// 持有者在干什么，例如 `tray` / `sync`。
  String note;
  String owner;

  /// 最后一次心跳距今多久。
  double ageSeconds;

  /// 已过期（被强杀后最多要等一个 TTL 才能被别人接管）。
  bool expired;

  Map<String, Object?> toJson() => held
      ? <String, Object?>{
          'held': true,
          'pid': pid,
          'note': note,
          'owner': owner,
          'ageSeconds': ageSeconds,
          'expired': expired,
        }
      : <String, Object?>{'held': false};
}
