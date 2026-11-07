import 'dart:convert';
import 'dart:io';

import 'package:anisync_app/src/core/ops.dart';
import 'package:flutter_test/flutter_test.dart';

/// op 契约门禁：Dart 侧常量表与 `internal/bridge` dump 出来的快照**双向**一致。
///
/// 快照由 Go 侧生成：
/// `go test ./internal/bridge -run TestOpsSnapshot -update`
///
/// 快照不存在时**失败**，不跳过 —— 跳过会让「对拍过了」这句话没有内容。
void main() {
  test('Ops 常量表与 test/fixtures/ops.json 双向一致', () {
    final file = File('test/fixtures/ops.json');
    if (!file.existsSync()) {
      fail(
        '快照未生成：${file.path} 不存在，无法对拍。\n'
        '生成命令：go test ./internal/bridge -run TestOpsSnapshot -update\n'
        '（该文件由 internal/bridge 产出，Dart 侧不自己造。）',
      );
    }

    final decoded = jsonDecode(file.readAsStringSync());
    expect(decoded, isA<Map<String, Object?>>());
    final envelope = (decoded! as Map).cast<String, Object?>();
    expect(envelope['schema'], 1, reason: 'op 快照必须声明 schema 1');

    final rawOps = envelope['ops'];
    expect(rawOps, isA<List<Object?>>(), reason: 'ops.json 里必须有 ops 数组');
    final ops = (rawOps! as List)
        .map((e) => (e as Map).cast<String, Object?>())
        .toList(growable: false);

    final fromGo = ops.map((e) => e['name']).whereType<String>().toSet();
    final fromDart = Ops.all.toSet();

    expect(
      fromDart.difference(fromGo),
      isEmpty,
      reason: 'Dart 侧多出来的 op（Go 那边根本不认）：'
          '${fromDart.difference(fromGo).join(', ')}',
    );
    expect(
      fromGo.difference(fromDart),
      isEmpty,
      reason: 'ops.json 里有、Dart 常量表漏掉的 op：'
          '${fromGo.difference(fromDart).join(', ')}',
    );

    // 异步集合必须正好是那四个：多一个少一个都说明 §4 的实现走样了。
    final asyncFromGo = ops
        .where((e) => e['async'] == true)
        .map((e) => e['name'])
        .whereType<String>()
        .toSet();
    expect(
      asyncFromGo,
      Ops.asyncOps,
      reason: '异步 op 集合不一致：Go=${asyncFromGo.join(', ')} '
          'Dart=${Ops.asyncOps.join(', ')}',
    );

    // 分组表自洽：每个 op 都在某个分组里，且键与 ops.json 的 group 一致。
    final grouped = Ops.groups.values.expand((e) => e).toSet();
    expect(grouped, fromDart, reason: 'Ops.groups 漏了 op 或多出未声明的 op');
    for (final op in ops) {
      final name = op['name'];
      final group = op['group'];
      if (name is String && group is String) {
        expect(
          Ops.groups[group],
          contains(name),
          reason: '分组不一致：ops.json 说 $name 属于 $group',
        );
      }
    }

    // 常量表本身不能有重复项。
    expect(Ops.all.length, fromDart.length, reason: 'Ops.all 里有重复的 op 名');
  });

  test('危险 op 清单与 §8 一致', () {
    expect(
      Ops.dangerOps,
      <String>{
        Ops.subscriptionDelete,
        Ops.journalClear,
        Ops.leaseTakeover,
      },
    );
  });
}
