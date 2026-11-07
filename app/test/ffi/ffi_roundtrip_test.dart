import 'dart:convert';
import 'dart:io';

import 'package:anisync_app/src/core/errors.dart';
import 'package:anisync_app/src/core/ffi_backend.dart';
import 'package:anisync_app/src/core/ffi_library.dart';
import 'package:anisync_app/src/core/ops.dart';
import 'package:anisync_app/src/model/config.dart';
import 'package:anisync_app/src/model/op_info.dart';
import 'package:flutter_test/flutter_test.dart';

/// 真机 FFI 回环：加载**真实的** `anisync_core` 动态库，跑一条完整链路。
///
/// 环境变量 `ANISYNC_CORE_LIB` 指向库文件时才跑；否则**跳过并说明原因**，
/// 绝不伪造通过（也不静默降级成假实现）。
void main() {
  final libraryPath = envCoreLibraryPath();

  if (libraryPath == null) {
    test(
      'FFI 真机回环：core.version → core.ops → config.default → config.validate → config.save → config.get',
      () {},
      skip: 'ANISYNC_CORE_LIB 未设置：没有真实动态库可加载，跳过。'
          '设置示例（T4 编出库之后）：\n'
          r'  $env:ANISYNC_CORE_LIB="D:\WorkProgram\ASSPLUGIN\anisync\app\build\native\windows-amd64\anisync_core.dll"',
    );
    return;
  }

  late Directory workspace;
  late FfiBackend backend;

  setUpAll(() async {
    // 用独立临时工作区：不能把 config.yaml 写进仓库或测试运行目录。
    workspace = Directory.systemTemp.createTempSync('anisync-ffi-workspace');
    backend = FfiBackend(extraCandidates: <String>[libraryPath]);
    await backend.open();
  });

  tearDownAll(() async {
    await backend.dispose();
    if (workspace.existsSync()) {
      workspace.deleteSync(recursive: true);
    }
  });

  test('加载到的就是 ANISYNC_CORE_LIB 指向的那一个库', () {
    expect(backend.libraryPath, libraryPath);
    expect(backend.coreVersion(), isNotNull);
  });

  test('一条链每一步都 ok:true', () async {
    // 先钉一个临时工作区：否则 config.save 会写到「可执行文件同目录」。
    final init = await backend.callRaw(Ops.coreInit, <String, Object?>{
      'configPath': '${workspace.path}${Platform.pathSeparator}config.yaml',
      'dataDir': '${workspace.path}${Platform.pathSeparator}data',
    });
    expect(init['ok'], true, reason: 'core.init 失败：${init['error']}');

    final version = await backend.callRaw(Ops.coreVersion, <String, Object?>{});
    expect(version['ok'], true, reason: 'core.version 失败：${version['error']}');
    final info = CoreInfo.fromJson(_data(version));
    expect(info.schema, 1);
    expect(info.platform, isNotEmpty);

    final ops = await backend.callRaw(Ops.coreOps, <String, Object?>{});
    expect(ops['ok'], true, reason: 'core.ops 失败：${ops['error']}');
    final catalogue = OpsCatalogue.fromJson(_data(ops));
    expect(catalogue.schema, 1);
    expect(
      catalogue.opsOrEmpty.map((e) => e.name).toSet(),
      Ops.all.toSet(),
      reason: '真库注册的 op 与 Dart 常量表不一致',
    );
    expect(
      catalogue.opsOrEmpty.where((e) => e.isAsync).map((e) => e.name).toSet(),
      Ops.asyncOps,
    );

    final defaulted = await backend.callRaw(Ops.configDefault, <String, Object?>{});
    expect(
      defaulted['ok'],
      true,
      reason: 'config.default 失败：${defaulted['error']}',
    );
    final config = Config.fromJson(_data(defaulted)['config']! as Map<String, Object?>);
    expect(config.listen, isNotEmpty);

    // `config.save` 会先过一遍 `Config.Validate`，而「一个实例都没有」是硬错误。
    // 所以先塞一个最小可用实例，否则这条链根本走不到 save。
    config.instances = <Instance>[
      Instance(
        name: '回环测试库',
        baseUrl: 'https://ass.example.com/',
        apiKey: 'k',
      ),
    ];

    final validate = await backend.callRaw(Ops.configValidate, <String, Object?>{
      'config': config.toJson(),
    });
    expect(
      validate['ok'],
      true,
      reason: 'config.validate 失败：${validate['error']}',
    );

    final saved = await backend.callRaw(Ops.configSave, <String, Object?>{
      'config': config.toJson(),
    });
    expect(saved['ok'], true, reason: 'config.save 失败：${saved['error']}');

    final reloaded = await backend.callRaw(Ops.configGet, <String, Object?>{});
    expect(
      reloaded['ok'],
      true,
      reason: 'config.get 失败：${reloaded['error']}',
    );
    final after = Config.fromJson(
      _data(reloaded)['config']! as Map<String, Object?>,
    );
    // 往返之后三态字段仍然是 null（不许被填成 false / 0）。
    expect(after.sync.offset, isNull);
    expect(after.sync.season, isNull);
    expect(after.discover.japanOnly, isNull);
    expect(after.listen, config.listen);
  });

  test('未知 op：单值 not_found + detail 恰等于 op 名（版本偏斜）', () async {
    const unknownOp = 'does.not.exist';

    // 协议事实（信封层）
    final result = await backend.callRaw(unknownOp, <String, Object?>{});
    expect(result['ok'], false, reason: '未知 op 必须是 ok:false，不许崩');
    final raw = CoreError.fromJson(
      (result['error']! as Map).cast<String, Object?>(),
    );
    // §3：op 名不存在一律回 `not_found`（不是 `bad_request` —— 那是 payload 形状错）。
    expect(raw.code, ErrorCodes.notFound);
    // 判别用 `detail`，不用 `message`：后者会被翻译和改写。
    expect(raw.detail, unknownOp);
    expect(raw.message, isNotEmpty, reason: 'message 只给人看，必须有内容');

    // 派生标记（调用点判别）
    await expectLater(
      backend.call(unknownOp, <String, Object?>{}),
      throwsA(
        isA<CoreException>().having(
          (e) => e.error.versionSkew,
          'versionSkew',
          isTrue,
        ),
      ),
    );
  });

  test('事件路径：读成功就释放，释放次数 == 读成功次数', () async {
    // 契约 §1.1 的所有权转移：Go 永不释放，调用方读完释放。
    // 这一对计数器是「没有泄漏」唯一能被观测到的证据 —— 光有注释不算。
    final read = backend.eventReadCount;
    final released = backend.eventReleasedCount;
    expect(
      released,
      read,
      reason: '读成功 $read 帧、释放 $released 帧：不相等就是泄漏或重复释放',
    );
    expect(
      backend.eventReadFailures,
      0,
      reason: '读失败按契约不该发生（生产者保证 JSON + 不交出已不拥有的指针）',
    );
    // 非空洞：这一轮若一帧事件都没来，上面那条相等断言等于没测。
    expect(
      read,
      greaterThan(0),
      reason: '这一轮一条事件都没收到：相等断言是空洞的，说明该换个能触发事件的 op',
    );
  });

  test('资源分支：真库回 not_found + detail 是资源名，且**不被误判成版本偏斜**', () async {
    // t23 之后资源分支也会填 `detail`（契约 §3 的判别表承诺过）。
    // 这条对着**真实引擎**确认两件事：
    //   1. `detail` 真的是资源名（不是 op 名、不是 `job=zzz` 这种拼接）；
    //   2. 我们的判别（`detail == op`）**不会**把资源分支误判成「版本偏斜」。
    // 误判的后果很具体：把「刷新一下就好」说成「去升级引擎」，用户会去干一件没用的事。
    const op = 'job.get';
    final payload = <String, Object?>{'id': 'no-such-job-zzz'};

    final raw = await backend.callRaw(op, payload);
    expect(raw['ok'], false, reason: '$op 指向不存在的任务，应当失败');
    final error = CoreError.fromJson(
      (raw['error']! as Map).cast<String, Object?>(),
    );
    expect(error.code, ErrorCodes.notFound);
    expect(error.detail, isNotEmpty, reason: '$op 的资源分支必须带 detail（§3）');
    expect(error.detail, isNot(op), reason: 'detail 不许等于 op 名，否则会被误判成版本偏斜');
    expect(error.detail, contains('no-such-job-zzz'), reason: 'detail 应当是那个资源名');

    // 走 `call()` 那条路（判别在调用点做）：不许打成版本偏斜。
    await expectLater(
      backend.call(op, payload),
      throwsA(
        isA<CoreException>().having(
          (e) => e.error.versionSkew,
          'versionSkew',
          isFalse,
        ),
      ),
    );
  });

  test('log.write：写进去的行带 [client] 前缀，且不出现在事件流里', () async {
    const marker = 't14-ffi-roundtrip-marker';
    final write = await backend.callRaw(Ops.logWrite, <String, Object?>{
      'level': 'warn',
      'text': marker,
    });
    expect(write['ok'], true, reason: 'log.write 失败：${write['error']}');

    final recent = await backend.callRaw(Ops.logRecent, <String, Object?>{
      'limit': 50,
    });
    expect(recent['ok'], true);
    final lines = ((recent['data']! as Map)['lines']! as List)
        .map((e) => (e as Map).cast<String, Object?>())
        .toList();
    final hit = lines.where((line) => '${line['text']}'.contains(marker));
    expect(
      hit,
      isNotEmpty,
      reason: '写进去的行必须能在 log.recent 里回看（断线兜底视图就靠它）',
    );
    expect(
      '${hit.first['text']}'.startsWith('[client] '),
      isTrue,
      reason: '引擎必须加 [client] 前缀：客户端写的行与引擎自己写的行要可区分（§7.12）',
    );
  });

  test('level 白名单：自造档位被 bad_request 拒（§7.12）', () async {
    final result = await backend.callRaw(Ops.logWrite, <String, Object?>{
      'level': 'verbose',
      'text': '自造档位',
    });
    expect(result['ok'], false);
    final error = CoreError.fromJson(
      (result['error']! as Map).cast<String, Object?>(),
    );
    expect(error.code, ErrorCodes.badRequest);
  });

  test('config.get 的 JSON 与 Dart 侧 toJson 形状一致（键集合）', () async {
    final result = await backend.callRaw(Ops.configGet, <String, Object?>{});
    final fromGo = (_data(result)['config']! as Map).cast<String, Object?>();
    final config = Config.fromJson(fromGo);
    final fromDart = config.toJson();

    // 只读派生（fileRules）在 Go 侧有、Dart 侧刻意不写回。
    final goKeys = fromGo.keys.toSet()..remove('fileRules');
    expect(fromDart.keys.toSet(), goKeys, reason: 'Config 的键集合必须逐字一致');
  });
}

Map<String, Object?> _data(Map<String, Object?> envelope) =>
    (envelope['data']! as Map).cast<String, Object?>();

/// 让 linter 别把 jsonEncode / File 之类的 import 当成多余。
// ignore: unused_element
String _debugJson(Object? value) => jsonEncode(value);
