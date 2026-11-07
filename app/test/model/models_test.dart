import 'dart:convert';
import 'dart:io';

import 'package:anisync_app/src/model/backup.dart';
import 'package:anisync_app/src/model/config.dart';
import 'package:anisync_app/src/model/instance_view.dart';
import 'package:anisync_app/src/model/job.dart';
import 'package:anisync_app/src/model/lease.dart';
import 'package:anisync_app/src/model/plan.dart';
import 'package:anisync_app/src/model/profile_view.dart';
import 'package:anisync_app/src/model/run_entry.dart';
import 'package:anisync_app/src/model/season_item.dart';
import 'package:anisync_app/src/model/status.dart';
import 'package:anisync_app/src/model/subscription.dart';
import 'package:flutter_test/flutter_test.dart';

/// 各 model 的 `fromJson` / `toJson` 往返：**缺字段不炸、写回去不漂移**。
void main() {
  Map<String, Object?> fixture(String name) {
    final file = File('test/fixtures/shapes/$name.data.json');
    if (!file.existsSync()) {
      fail('夹具缺失：${file.path}');
    }
    return (jsonDecode(file.readAsStringSync()) as Map).cast<String, Object?>();
  }

  test('缺字段不炸：空对象喂给每个 model 都不抛', () {
    const Map<String, Object?> empty = <String, Object?>{};
    expect(Config.fromJson(empty).listen, isNotEmpty);
    expect(InstanceView.fromJson(empty).enabled, isTrue);
    expect(InstanceListResult.fromJson(empty).instancesOrEmpty, isEmpty);
    expect(InstanceTestResult.fromJson(empty).ok, isFalse);
    expect(SubscriptionView.fromJson(empty).key, '');
    expect(SubscriptionListResult.fromJson(empty).itemsOrEmpty, isEmpty);
    expect(SubscriptionSetResult.fromJson(empty).updated, 0);
    expect(SubscriptionDeleteResult.fromJson(empty).deleted, 0);
    expect(ProfileView.fromJson(empty).name, '');
    expect(ProfileDeleteResult.fromJson(empty).affectedOrEmpty, isEmpty);
    expect(RuleFileDetail.fromJson(empty).path, '');
    expect(PlanView.fromJson(empty).actionsOrEmpty, isEmpty);
    expect(PlanBuildResult.fromJson(empty).planOrEmpty.writableCount, 0);
    expect(SeasonItem.fromJson(empty).bgmId, '');
    expect(DiscoverListResult.fromJson(empty).itemsOrEmpty, isEmpty);
    expect(BgmSearchResult.fromJson(empty).itemsOrEmpty, isEmpty);
    // `ok` 在 Go 侧不带 omitempty，永远会出现；缺字段时按 false 处理（= 不成功）。
    expect(RunEntry.fromJson(empty).errorsOrEmpty, isEmpty);
    expect(RunEntry.fromJson(empty).mode, isEmpty);
    expect(JournalTailResult.fromJson(empty).entriesOrEmpty, isEmpty);
    expect(LeaseView.fromJson(empty).held, isFalse);
    expect(JobView.fromJson(empty).state, '');
    expect(JobListResult.fromJson(empty).jobsOrEmpty, isEmpty);
    expect(StatusResult.fromJson(empty).instancesOrEmpty, isEmpty);
    expect(BackupListResult.fromJson(empty).backupsOrEmpty, isEmpty);
  });

  test('SubscriptionView：往返不丢 raw 与三态偏移', () {
    final data = fixture('instance_subscriptions');
    final result = SubscriptionListResult.fromJson(data);
    final item = result.itemsOrEmpty.first;
    final again = SubscriptionView.fromJson(item.toJson());

    expect(again.key, item.key);
    expect(again.season, item.season);
    expect(again.offset, item.offset);
    expect(again.standbyOffset, item.standbyOffset);
    expect(again.enable, item.enable);
    expect(again.main?.group, item.main?.group);
    expect(again.standbyOrEmpty.length, item.standbyOrEmpty.length);
    expect(again.raw, item.raw, reason: 'raw 必须原样保留：setAni 是全量覆盖');

    // null 的偏移不能被写成 0。
    final second = result.itemsOrEmpty[1];
    final secondAgain = SubscriptionView.fromJson(second.toJson());
    expect(secondAgain.season, isNull);
    expect(secondAgain.offset, isNull);
    expect(secondAgain.standbyOffset, isNull);
  });

  test('PlanView：往返保留 changes 与 kind', () {
    final data = fixture('plan_build');
    final plan = PlanBuildResult.fromJson(data).planOrEmpty;
    final again = PlanView.fromJson(plan.toJson());

    expect(again.actionsOrEmpty.length, plan.actionsOrEmpty.length);
    expect(again.actionsOrEmpty.first.kind, 'update');
    expect(
      again.actionsOrEmpty.first.changesOrEmpty.length,
      plan.actionsOrEmpty.first.changesOrEmpty.length,
    );
    expect(again.hitsByTarget['冰之城墙'], 1);
    expect(again.writableCount, plan.writableCount);
  });

  test('StatusResult：租约空闲时只写 held', () {
    final data = fixture('status_get');
    final status = StatusResult.fromJson(data);
    final again = StatusResult.fromJson(status.toJson());
    expect(again.lease.held, status.lease.held);
    expect(again.lease.toJson(), <String, Object?>{'held': false});
  });

  test('RunEntry：busy 不是失败', () {
    final entry = RunEntry.fromJson(<String, Object?>{
      'at': '2026-11-08T00:31:02Z',
      'mode': 'daemon',
      'ok': true,
      'busy': true,
      'nextAt': '2026-11-08T01:31:02Z',
      'note': '上一轮还没跑完，这轮让开了',
    });
    expect(entry.busy, isTrue);
    expect(entry.isFailure, isFalse);
    final again = RunEntry.fromJson(entry.toJson());
    expect(again.busy, isTrue);
    expect(again.nextAt, isNotNull);
    expect(again.note, isNotEmpty);
  });

  test('JobView：终态判定与耗时', () {
    final job = JobView.fromJson(<String, Object?>{
      'id': 'j1',
      'kind': 'sync.run',
      'state': 'running',
      'startedAt': '2026-11-08T00:00:00Z',
      'message': '正在取站点数据 3/8',
    });
    expect(job.isRunning, isTrue);
    expect(job.isTerminal, isFalse);
    expect(job.message, isNotEmpty);
    final done = JobView.fromJson(<String, Object?>{
      'id': 'j1',
      'kind': 'sync.run',
      'state': 'done',
      'result': <String, Object?>{'updated': 3},
    });
    expect(done.isTerminal, isTrue);
    expect(done.result, isNotNull);
  });

  test('SeasonItem：cover 与 added 保真', () {
    final item = SeasonItem.fromJson(<String, Object?>{
      'bgmId': '665390',
      'title': '冰之城墙',
      'source': 'yuc',
      'cover': 'https://lain.bgm.tv/pic/cover/m/xx.jpg',
      'score': 7.5,
      'added': true,
    });
    expect(item.added, isTrue);
    expect(item.score, 7.5);
    final again = SeasonItem.fromJson(item.toJson());
    expect(again.cover, item.cover);
    expect(again.added, isTrue);
  });

  test('BackupEntry：条数合计', () {
    final entry = BackupEntry.fromJson(<String, Object?>{
      'file': 'backup-1.json',
      'size': 1234,
      'instances': <Object?>[
        <String, Object?>{'name': '追番库', 'count': 3},
        <String, Object?>{'name': '收藏库', 'count': 4},
      ],
    });
    expect(entry.total, 7);
    expect(entry.instancesOrEmpty.length, 2);
  });
}
