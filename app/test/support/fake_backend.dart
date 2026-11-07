import 'dart:async';

import 'package:anisync_app/src/core/backend.dart';
import 'package:anisync_app/src/core/core_event.dart';
import 'package:anisync_app/src/core/errors.dart';

/// 测试用的后端替身。
///
/// **只存在于 `test/` 下**：`lib/` 里不许有假实现（FFI 加载失败必须抛错，不能静默降级）。
class FakeBackend implements Backend {
  FakeBackend({
    Map<String, Map<String, Object?>>? data,
    Map<String, CoreError>? failures,
  })  : _data = data ?? <String, Map<String, Object?>>{},
        _failures = failures ?? <String, CoreError>{};

  final Map<String, Map<String, Object?>> _data;
  final Map<String, CoreError> _failures;

  /// 每个 op 被调用了多少次（断言「有没有真的去调」）。
  final Map<String, int> calls = <String, int>{};

  final StreamController<CoreEvent> _events =
      StreamController<CoreEvent>.broadcast();

  bool opened = false;
  bool disposed = false;

  /// 测试里可以直接摆一个非零值，验证「设计外状态」的显示路径。
  @override
  int eventReadFailures = 0;

  @override
  int eventLogWriteDropped = 0;

  /// 事件帧读成功 / 释放成功。生产里由 FFI 后端计数，测试里直接摆值。
  @override
  int eventReadCount = 0;

  @override
  int eventReleasedCount = 0;

  @override
  String get label => '测试替身';

  @override
  Stream<CoreEvent> get events => _events.stream;

  @override
  Future<void> open() async {
    opened = true;
  }

  @override
  Future<Map<String, Object?>> call(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) async =>
      dataOf(await callRaw(op, payload, callId: callId));

  @override
  Future<Map<String, Object?>> callRaw(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) async {
    calls[op] = (calls[op] ?? 0) + 1;
    final failure = _failures[op];
    if (failure != null) {
      return <String, Object?>{'ok': false, 'error': failure.toJson()};
    }
    return <String, Object?>{
      'ok': true,
      'data': _data[op] ?? <String, Object?>{},
    };
  }

  @override
  Future<bool> cancel(String callId) async => true;

  @override
  Future<void> dispose() async {
    disposed = true;
    await _events.close();
  }

  /// 往事件流里塞一条（测 jobs / 事件缓冲区用）。
  void emit(CoreEvent event) => _events.add(event);
}

/// 一个永远不返回的调用，用来验证「加载态」。
class HangingBackend extends FakeBackend {
  final Completer<Map<String, Object?>> _never =
      Completer<Map<String, Object?>>();

  @override
  Future<Map<String, Object?>> callRaw(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) {
    calls[op] = (calls[op] ?? 0) + 1;
    return _never.future;
  }
}
