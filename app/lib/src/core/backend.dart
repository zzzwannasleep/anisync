import 'core_event.dart';
import 'errors.dart';

/// 两种落地（进程内 FFI / 远端节点 HTTP）共用的接口。UI 层不区分自己接的是哪一个。
abstract class Backend {
  /// 打开后端（加载动态库 / 建 HTTP 客户端 / 起事件流）。
  Future<void> open();

  /// 发一次调用，返回信封里的 `data`；`ok:false` 时抛 [CoreException]。
  ///
  /// [callId] 由调用方生成（UUID v4）；同一个 `callId` 只允许一次在飞，重复用会拿到
  /// `bad_request`。需要中断时调 [cancel]。
  Future<Map<String, Object?>> call(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  });

  /// 发一次调用，返回**完整信封** `{ok, data?, error?}`。
  ///
  /// 契约测试与 `ffi_roundtrip` 要断言每一步的 `ok`，所以需要一个不抛异常的入口。
  Future<Map<String, Object?>> callRaw(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  });

  /// 取消一次进行中的调用。返回 true = 确实取消了一个在飞的 callId。
  Future<bool> cancel(String callId);

  /// 事件流（`log` / `progress` / `job.*` / `lease.lost` / `instance.progress`）。
  Stream<CoreEvent> get events;

  /// 后端类型标签，给状态条显示用。
  String get label;

  /// 「设计外状态」的本地计数：**事件字符串读失败**累计次数。
  ///
  /// 非 FFI 后端没有这个失效模式，默认 0。FFI 后端覆写它 ——
  /// 契约 §1.1 要求这条路径上必须有计数器：它按设计不可达，
  /// 所以一旦非零就是「设计外的事发生了」的唯一证据。
  int get eventReadFailures => 0;

  /// 被引擎限速丢掉的客户端日志行数（`dropped`）。**不是错误，是背压。**
  int get eventLogWriteDropped => 0;

  /// 事件帧**读成功** / **释放成功**的计数（成对）。
  ///
  /// 契约 §1.1 的所有权转移要求「读成功就必须释放恰好一次」，这一对数字相等
  /// 就是「没有泄漏」唯一能被观测到的证据 —— 光有注释不算。设置页的诊断块显示它们。
  int get eventReadCount => 0;
  int get eventReleasedCount => 0;

  Future<void> dispose();
}

/// callId 生成：UUID v4。不引 uuid 包，十几个字符的事。
String newCallId([Random0? random]) {
  final rnd = random ?? Random0();
  final bytes = List<int>.generate(16, (_) => rnd.nextByte());
  bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
  bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 10
  String hex(int start, int end) => bytes
      .sublist(start, end)
      .map((b) => b.toRadixString(16).padLeft(2, '0'))
      .join();
  return '${hex(0, 4)}-${hex(4, 6)}-${hex(6, 8)}-${hex(8, 10)}-${hex(10, 16)}';
}

/// `dart:math` 的 Random 在这里只用 NextByte，单独包一层便于测试注入。
class Random0 {
  Random0([int? seed]) : _seed = seed ?? DateTime.now().microsecondsSinceEpoch;

  int _seed;

  int nextByte() {
    // xorshift32 足够：callId 只要求「同一进程内不重样」。
    var x = _seed & 0xffffffff;
    x ^= (x << 13) & 0xffffffff;
    x ^= x >> 17;
    x ^= (x << 5) & 0xffffffff;
    _seed = x & 0xffffffff;
    return _seed & 0xff;
  }
}

/// 把后端回的原始 JSON 归一成信封。缺 `ok` 视为协议错误。
Map<String, Object?> normalizeEnvelope(Object? raw) {
  if (raw is Map) {
    final map = raw.cast<String, Object?>();
    if (map['ok'] is bool) return map;
    return <String, Object?>{
      'ok': false,
      'error': CoreError(
        code: ErrorCodes.internal,
        message: '后端返回的信封里没有 ok 字段',
        detail: map.toString(),
      ).toJson(),
    };
  }
  return <String, Object?>{
    'ok': false,
    'error': CoreError(
      code: ErrorCodes.internal,
      message: '后端返回的不是 JSON 对象',
      detail: raw?.toString(),
    ).toJson(),
  };
}

/// 从信封里取 `data`，`ok:false` 时抛 [CoreException]。
///
/// `op` 是**刚发出的那个 op 名**：有了它才能做 §3 的版本偏斜判别
/// （`not_found` 且 `detail == op`）。两个后端都走这里，所以判别只有一份实现。
Map<String, Object?> dataOf(Map<String, Object?> envelope, {String? op}) {
  if (envelope['ok'] == true) {
    final data = envelope['data'];
    if (data is Map) return data.cast<String, Object?>();
    return const <String, Object?>{};
  }
  final error = envelope['error'];
  if (error is Map) {
    throw CoreException(
      detectVersionSkew(
        CoreError.fromJson(error.cast<String, Object?>()),
        op,
      ),
    );
  }
  throw CoreException(
    CoreError(
      code: ErrorCodes.internal,
      message: '调用失败，但响应里没有 error 对象',
      detail: envelope.toString(),
    ),
  );
}
