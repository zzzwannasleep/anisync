import 'dart:convert';
import 'dart:io';

import 'package:anisync_app/src/model/config.dart';
import 'package:anisync_app/src/model/op_info.dart';
import 'package:anisync_app/src/model/plan.dart';
import 'package:anisync_app/src/model/profile_view.dart';
import 'package:anisync_app/src/model/status.dart';
import 'package:anisync_app/src/model/subscription.dart';
import 'package:flutter_test/flutter_test.dart';

/// 形状对拍：拿 Go 侧**对着 httptest 假实例跑出来**的样例响应反序列化。
/// 字段名一改就红。
///
/// 两份夹具用法不同：
/// - `*.data.json` 只有 `data` 那一层 —— 直接喂 `Model.fromJson`。
/// - `*.json` 是完整信封 `{"ok":true,"data":{...}}` —— 只用来断言「响应永远是信封」。
void main() {
  final fixtures = Directory('test/fixtures/shapes');

  Map<String, Object?> readJson(String name) {
    final file = File('${fixtures.path}/$name');
    if (!file.existsSync()) {
      fail(
        '形状夹具缺失：${file.path}\n'
        '生成命令：go test ./internal/bridge -run TestGoldenShapes -update',
      );
    }
    return (jsonDecode(file.readAsStringSync()) as Map).cast<String, Object?>();
  }

  /// 信封层：`ok:true` 且 `data` 是对象。
  Map<String, Object?> readEnvelopeData(String op) {
    final envelope = readJson('$op.json');
    expect(envelope['ok'], true, reason: '$op 的样例响应必须是 ok:true');
    final data = envelope['data'];
    expect(data, isA<Map<String, Object?>>(), reason: '$op 的 data 必须是对象');
    return (data! as Map).cast<String, Object?>();
  }

  test('夹具目录存在（否则这一支等于没跑）', () {
    if (!fixtures.existsSync()) {
      fail('形状夹具目录不存在：${fixtures.path}');
    }
  });

  test('config.get：Config / views 全部可反序列化', () {
    final data = readEnvelopeData('config_get');
    final result = ConfigGetResult.fromJson(data);

    expect(result.path, isNotEmpty);
    final config = Config.fromJson((data['config']! as Map).cast<String, Object?>());
    expect(config.listen, '127.0.0.1:51837');
    expect(config.instancesOrEmpty, isNotEmpty);
    expect(config.instancesOrEmpty.first.name, isNotEmpty);
    // 不带 omitempty 的三个键必须原样在（空串也算「在」）。
    expect(config.proxy, '');
    expect(config.sources.instance, isNotEmpty);
    expect(config.sources.orderOrEmpty, isNotEmpty);

    // 三态：夹具里 instances[].enabled 缺席 → 必须是 null，不能是 false。
    expect(
      config.instancesOrEmpty.first.enabled,
      isNull,
      reason: 'instances[].enabled 缺席 = 启用（null），不许落成 false',
    );
    // targets[].season 写了 2；offset / standbyOffset 缺席 → null。
    expect(config.targetsOrEmpty.first.season, 2);
    expect(config.targetsOrEmpty.first.offset, isNull);
    expect(config.targetsOrEmpty.first.standbyOffset, isNull);
    expect(config.targetsOrEmpty.first.enable, isNull);
    // sync 三个三态全缺 → 全 null。
    expect(config.sync.season, isNull);
    expect(config.sync.offset, isNull);
    expect(config.sync.standbyOffset, isNull);
    // profiles[].rules.deny 是 null（缺键），不是 []。
    expect(config.profilesOrEmpty.first.rules.deny, isNull);
    expect(config.profilesOrEmpty.first.rules.maxStandby, 12);

    // views：派生层是只读的，instances[].enabled 在这里是**生效值** bool。
    final views = ConfigViews.fromJson(
      (data['views']! as Map).cast<String, Object?>(),
    );
    expect(views.instancesOrEmpty, isNotEmpty);
    expect(views.instancesOrEmpty.first.enabled, true);
    expect(views.instancesOrEmpty.first.scheme.instance, isNotEmpty);
    expect(views.instancesOrEmpty.first.apiKey, isNot(equals('abcdefghijklmnop')),
        reason: 'InstanceView.apiKey 必须是遮蔽值，不是原文');
    expect(views.profilesOrEmpty.first.name, isNotEmpty);
    expect(views.profilesOrEmpty.first.rules.maxStandby, 12);

    // ruleFiles 的唯一形状 {path, kind, count}：一个文件一行，不聚合，**没有 owner 字段**
    // （owner 是外层对象的属性：InstanceView 的持有者是 instance，顶层没有名字）。
    for (final raw in (data['ruleFiles']! as List)) {
      final entry = (raw as Map).cast<String, Object?>();
      expect(entry.keys.toSet(), <String>{'path', 'kind', 'count'});
      expect(entry['path'], isNotEmpty);
      expect(<String>['primary', 'deny'], contains(entry['kind']));
    }
    for (final info in config.fileRulesOrEmpty) {
      expect(info.path, isNotEmpty);
      expect(<String>['primary', 'deny'], contains(info.kind));
      expect(info.count, greaterThanOrEqualTo(0));
    }
    expect(result.warningsOrEmpty, isEmpty);
  });

  test('instance.subscriptions：主/副 RSS 与三态偏移', () {
    final data = readEnvelopeData('instance_subscriptions');
    final result = SubscriptionListResult.fromJson(data);

    expect(result.total, 2);
    final items = result.itemsOrEmpty;
    expect(items.length, 2);

    final first = items.first;
    expect(first.key, 'id:1234');
    expect(first.enable, true);
    expect(first.season, 1);
    expect(first.offset, 0);
    expect(first.standbyOffset, -12);
    expect(first.main?.group, '旧组');
    expect(first.standbyOrEmpty.single.group, 'ANi');
    expect(first.standbyOrEmpty.single.offset, -12);
    expect(first.raw, isNotNull, reason: 'raw 必须原样保留（setAni 是全量覆盖）');
    expect(first.raw!['subgroup'], '旧组');

    // 第二条：读不出数字的字段一律 null + issues 说明，不猜。
    final second = items[1];
    expect(second.key, 'bgm:400602');
    expect(second.season, isNull);
    expect(second.offset, isNull);
    expect(second.standbyOffset, isNull);
    expect(second.issuesOrEmpty.length, 3);
    expect(second.standby, isEmpty);
  });

  test('plan.build：actions / changes / schemes', () {
    final data = readEnvelopeData('plan_build');
    final result = PlanBuildResult.fromJson(data);
    final plan = result.planOrEmpty;

    expect(plan.actionsOrEmpty.length, 2);
    final action = plan.actionsOrEmpty.first;
    expect(action.instance, isNotEmpty);
    expect(action.aniId, '1234');
    expect(action.kind, 'update');
    expect(action.target, '冰之城墙');
    expect(action.profile, '收藏');
    expect(action.changesOrEmpty.length, 4);
    expect(action.changesOrEmpty.first.field, 'url');
    expect(action.changesOrEmpty.first.from, isNotEmpty);
    expect(action.changesOrEmpty.first.to, isNotEmpty);
    expect(action.reason, isEmpty);

    expect(plan.schemesOrEmpty.single.instance, isNotEmpty);
    expect(plan.schemesOrEmpty.single.maxStandby, 12);
    expect(plan.siteInstance, isNotEmpty);
    expect(plan.sitesOrEmpty.single.site, 'anibt');
    expect(plan.writableCount, 2);
    // 按 target 反查「命中几条订阅」：番剧配置页要用它。
    expect(plan.hitsByTarget['冰之城墙'], 1);
  });

  test('status.get：实例方案表 / 租约 / 最近几轮', () {
    final data = readEnvelopeData('status_get');
    final status = StatusResult.fromJson(data);

    expect(status.version, isNotEmpty);
    expect(status.platform, isNotEmpty);
    expect(status.configPath, isNotEmpty);
    expect(status.dataDir, isNotEmpty);
    expect(status.instancesOrEmpty.single.instance, isNotEmpty);
    expect(status.instancesOrEmpty.single.maxStandby, 12);
    expect(status.instancesOrEmpty.single.siteOrderOrEmpty, isNotEmpty);
    expect(status.lease.held, false);
    expect(status.runsOrEmpty, isEmpty);
  });

  test('core.ops 的快照可反序列化（分组 / 是否异步）', () {
    // ops.json 本身就是 core.ops 的 data 形状。
    final file = File('test/fixtures/ops.json');
    if (!file.existsSync()) {
      fail('快照未生成：${file.path}');
    }
    final catalogue = OpsCatalogue.fromJson(
      (jsonDecode(file.readAsStringSync()) as Map).cast<String, Object?>(),
    );
    expect(catalogue.schema, 1);
    // 38 = §7 的 op 总数（37 + §7.12 新增的 log.write）。
    expect(catalogue.opsOrEmpty.length, 38);
    expect(
      catalogue.opsOrEmpty.where((e) => e.isAsync).map((e) => e.name).toSet(),
      <String>{
        'sync.run',
        'plan.apply',
        'backup.restore',
        'discover.autoAdd',
      },
    );
    final byName = <String, OpInfo>{
      for (final op in catalogue.opsOrEmpty) op.name: op,
    };
    expect(byName['journal.clear']?.isAsync, false);
    expect(byName['journal.clear']?.group, 'journal');
    expect(byName['lease.takeover']?.group, 'lease');
    // `log.write` 是唯一一个**由客户端写入引擎状态**的 op，且不是异步的
    // （它只往环形缓冲追加一行，没有 jobId）。
    expect(byName['log.write'], isNotNull);
    expect(byName['log.write']?.isAsync, false);
    expect(byName['log.write']?.group, 'log');
  });
}
