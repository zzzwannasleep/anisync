import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:isolate';

import 'package:ffi/ffi.dart';

import 'backend.dart';
import 'core_event.dart';
import 'errors.dart';
import 'ffi_library.dart';
import 'ops.dart';

// ---------------------------------------------------------------------------
// 原生签名（`docs/ops-api.md` §1，符号名固定不许改）
// ---------------------------------------------------------------------------

typedef _CallNative = Pointer<Utf8> Function(
  Pointer<Utf8> op,
  Pointer<Utf8> payloadJson,
  Pointer<Utf8> callId,
);
typedef _CallDart = Pointer<Utf8> Function(
  Pointer<Utf8> op,
  Pointer<Utf8> payloadJson,
  Pointer<Utf8> callId,
);

typedef _CancelNative = Int32 Function(Pointer<Utf8> callId);
typedef _CancelDart = int Function(Pointer<Utf8> callId);

typedef _FreeNative = Void Function(Pointer<Utf8> pointer);
typedef _FreeDart = void Function(Pointer<Utf8> pointer);

typedef _VersionNative = Pointer<Utf8> Function();
typedef _VersionDart = Pointer<Utf8> Function();

typedef _EventFnNative = Void Function(Pointer<Utf8> eventJson);
typedef _SetEventCbNative = Int32 Function(
  Pointer<NativeFunction<_EventFnNative>> callback,
);
typedef _SetEventCbDart = int Function(
  Pointer<NativeFunction<_EventFnNative>> callback,
);

/// 进程内 FFI 后端。
///
/// 三条纪律：
/// 1. **所有阻塞调用在常驻 worker isolate 上执行** —— UI isolate 只发请求、收结果，
///    这样即使 `AniSyncCall` 阻塞几十秒（`plan.build`），界面照样跟手。
/// 2. 事件回调用 `NativeCallable.listener` 注册，回调里**只做一件事**：把字符串丢进
///    `SendPort`，立即返回（Go 侧推送是非阻塞的）。
/// 3. 找不到动态库时抛 [CoreLibraryNotFound]，**绝不静默降级成假实现**。
class FfiBackend implements Backend {
  FfiBackend({this.libraryOpener, this.extraCandidates});

  /// 只为测试注入：默认走 `DynamicLibrary.open`。
  final DynamicLibrary Function(String path)? libraryOpener;

  /// 额外的候选路径（排在环境变量之后、平台默认路径之前）。
  final List<String>? extraCandidates;

  final StreamController<CoreEvent> _events =
      StreamController<CoreEvent>.broadcast();

  CoreLibraryHandle? _handle;
  _CancelDart? _cancel;
  _FreeDart? _free;
  _VersionDart? _version;
  _SetEventCbDart? _setEventCallback;
  NativeCallable<_EventFnNative>? _eventCallable;
  Isolate? _worker;
  SendPort? _workerPort;
  ReceivePort? _workerReplies;
  StreamSubscription<Object?>? _workerReplySub;
  int _nextRequestId = 1;
  final Map<int, Completer<Map<String, Object?>>> _pending =
      <int, Completer<Map<String, Object?>>>{};

  /// 在飞的调用：requestId → op。读失败时要报「当时在跑哪个 op」，
  /// 这是「关于失败的事实」里唯一需要现场采集的一项。
  final Map<int, String> _inFlightOps = <int, String>{};

  /// **设计外状态的计数器**（契约 §1.1）：「事件字符串读失败」累计次数。
  ///
  /// 这条路径按设计不可达（生产者保证事件一律由 `encoding/json` 产出、
  /// 且不交出自己不再拥有的指针）。**正因为不可达，它一旦非零就是
  /// 「设计外的某件事发生了」的唯一证据** —— 没有计数器，那段防御代码
  /// 就只是一层没人知道有没有触发过的免责声明。
  int _eventReadFailures = 0;

  /// 被引擎限速丢掉的行数（`{ok:true, dropped:N}`）。**这不是错误**，
  /// 是被设计过的背压：一个客户端不该能把引擎的日志缓冲刷掉。
  int _eventLogWriteDropped = 0;

  /// 成功读成 Dart 字符串的事件帧数。
  int _eventReadCount = 0;

  /// 真正调用过 `AniSyncFree` 的事件帧数。
  ///
  /// 与 [eventReadCount] 配成一对：**读成功就必须释放恰好一次**（契约 §1.1 的
  /// 所有权转移）。这两个数相等，是「每次事件都没泄漏」唯一能被观测到的证据 ——
  /// 没有它，「读完就释放」就只是一句注释。
  int _eventReleasedCount = 0;

  /// 防止「读失败 → 写日志 → 写日志又失败 → 再写日志」的回环。
  bool _writingClientLog = false;

  bool _opened = false;

  @override
  int get eventReadFailures => _eventReadFailures;

  @override
  int get eventLogWriteDropped => _eventLogWriteDropped;

  /// 事件帧读成功 / 释放成功的计数（成对断言，证明没有泄漏）。
  @override
  int get eventReadCount => _eventReadCount;

  @override
  int get eventReleasedCount => _eventReleasedCount;

  /// 动态库的实际来源路径（设置页展示、错误消息用）。
  String? get libraryPath => _handle?.path;

  @override
  String get label {
    final path = _handle?.path;
    return path == null ? '本机引擎（未加载）' : '本机引擎 · ${_shortName(path)}';
  }

  @override
  Stream<CoreEvent> get events => _events.stream;

  @override
  Future<void> open() async {
    if (_opened) return;
    final handle = openCoreLibrary(
      opener: libraryOpener,
      extraCandidates: extraCandidates,
    );
    _handle = handle;

    _cancel = handle.library.lookupFunction<_CancelNative, _CancelDart>(
      'AniSyncCancel',
    );
    _free = handle.library.lookupFunction<_FreeNative, _FreeDart>('AniSyncFree');
    _version = handle.library
        .lookupFunction<_VersionNative, _VersionDart>('AniSyncCoreVersion');
    _setEventCallback =
        handle.library.lookupFunction<_SetEventCbNative, _SetEventCbDart>(
      'AniSyncSetEventCallback',
    );

    // 事件：回调里只把字符串丢进事件流，立即返回。
    //
    // ⚠️ `NativeCallable.listener` 的语义是「把调用投进本 isolate 的事件循环」，
    // 也就是说这里面的代码在**回调返回之后**才执行 —— 所以释放责任必须在调用方，
    // Go 侧绝不能在这里返回后才释放（契约 §1.1 硬规定）。
    final callable = NativeCallable<_EventFnNative>.listener(
      (Pointer<Utf8> pointer) {
        if (pointer == nullptr) return;
        final release = _free;
        String text;
        try {
          text = pointer.toDartString();
        } on Object catch (error) {
          // 读失败**不释放**：这个指针的合法性已经不再被保证，
          // 对来路不明的指针调 free 不是「清一次内存」，是**堆破坏** ——
          // 它不在这里崩，它在很远处、很久之后崩，现场与病因完全脱钩。
          // 宁可泄漏一帧（几十字节），不换一次堆破坏。
          //
          // 同理：这里**不去读它**（不许 asTypedList / hex dump）。
          // 刚决定不信任这个指针，就不能转头又去读它 —— 那是同一类未定义行为，
          // 只是这次没有解码器替我们挡住。上报的只能是「关于失败的事实」。
          _reportEventReadFailure(error);
          return;
        }
        // 契约 §1.1：事件字符串由 Go 用 CString 分配、永不自行释放，
        // 所有权随回调转移 —— 读成 Dart 字符串之后由我们释放。
        _eventReadCount++;
        if (release != null) {
          release(pointer);
          _eventReleasedCount++;
        }
        _emitRaw(text);
      },
    );
    _eventCallable = callable;
    _setEventCallback!(callable.nativeFunction);

    await _startWorker(handle.path);
    _opened = true;
  }

  @override
  Future<Map<String, Object?>> call(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) async {
    final envelope = await callRaw(op, payload, callId: callId);
    // 传 op：`not_found` 的版本偏斜判别需要知道「我发的是哪个 op」（契约 §3）。
    return dataOf(envelope, op: op);
  }

  @override
  Future<Map<String, Object?>> callRaw(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) async {
    final port = _workerPort;
    if (!_opened || port == null) {
      throw CoreException(
        CoreError.transport('FFI 后端尚未打开', detail: '先调用 open()'),
      );
    }
    final id = _nextRequestId++;
    final completer = Completer<Map<String, Object?>>();
    _pending[id] = completer;
    _inFlightOps[id] = op;
    port.send(<Object?>[
      id,
      op,
      jsonEncode(payload),
      callId ?? newCallId(),
    ]);
    return completer.future;
  }

  @override
  Future<bool> cancel(String callId) async {
    final fn = _cancel;
    if (fn == null) return false;
    final pointer = callId.toNativeUtf8();
    try {
      return fn(pointer) == 1;
    } finally {
      malloc.free(pointer);
    }
  }

  /// 引擎版本（`AniSyncCoreVersion`，静态字符串，不需要释放）。
  String? coreVersion() {
    final fn = _version;
    if (fn == null) return null;
    final pointer = fn();
    if (pointer == nullptr) return null;
    return pointer.toDartString();
  }

  @override
  Future<void> dispose() async {
    _opened = false;
    final setCallback = _setEventCallback;
    if (setCallback != null) {
      setCallback(nullptr);
    }
    _eventCallable?.close();
    _eventCallable = null;

    final worker = _worker;
    if (worker != null) {
      worker.kill(priority: Isolate.immediate);
      _worker = null;
    }
    _workerPort = null;
    await _workerReplySub?.cancel();
    _workerReplySub = null;
    _workerReplies?.close();
    _workerReplies = null;

    for (final completer in _pending.values) {
      if (!completer.isCompleted) {
        completer.completeError(
          CoreException(
            CoreError.transport('后端已关闭', detail: 'FFI 后端 dispose 时仍有在飞的调用'),
          ),
        );
      }
    }
    _pending.clear();
    await _events.close();
  }

  Future<void> _startWorker(String libraryPath) async {
    final handshake = ReceivePort();
    final worker = await Isolate.spawn<List<Object?>>(
      _workerMain,
      <Object?>[handshake.sendPort, libraryPath],
    );
    _worker = worker;
    final port = await handshake.first as SendPort;
    handshake.close();
    _workerPort = port;

    final replies = ReceivePort();
    _workerReplies = replies;
    _workerReplySub = replies.listen(_onWorkerReply);
    port.send(<Object?>['@attach', replies.sendPort]);
  }

  void _onWorkerReply(Object? message) {
    if (message is! List || message.isEmpty) return;
    final id = message[0];
    if (id is! int) return;
    _inFlightOps.remove(id);
    final completer = _pending.remove(id);
    if (completer == null || completer.isCompleted) return;
    final envelopeJson = message[1];
    if (envelopeJson is String) {
      final decoded = jsonDecode(envelopeJson);
      completer.complete(normalizeEnvelope(decoded));
      return;
    }
    completer.complete(<String, Object?>{
      'ok': false,
      'error': CoreError(
        code: ErrorCodes.internal,
        message: 'FFI 调用没有返回内容',
        detail: message.length > 2 ? '${message[2]}' : null,
      ).toJson(),
    });
  }

  /// 「事件字符串读失败」的上报。**只报关于失败的事实，不报失败的数据。**
  ///
  /// 三件事一起做（缺一不可，两者用途不同）：
  /// 1. 本地计数器 —— 「同类事件发生了多少次」的**聚合视图**（环形缓冲会被冲掉，它不会）
  /// 2. 一条 warn 事件 —— 立即、就地可见
  /// 3. 一行 `log.write` —— 进引擎环形缓冲。这是**断线期间唯一不会丢**的记录：
  ///    事件是旁路、会丢（SSE 重连 / 切前台 / isolate 重启都可能丢），缓冲不会。
  ///
  /// 注意 `log.write` **刻意不推事件**（否则「收到 log 事件 → 再 log.write」是无尽回环），
  /// 所以要回看自己写过的行走 `log.recent`，别指望在事件流里看到它。
  void _reportEventReadFailure(Object error) {
    _eventReadFailures++;
    final ops = _inFlightOps.values.toSet().toList(growable: false);
    // `$error` 是**解码器给出的描述**（种类 + 偏移），不是 buffer 的内容 ——
    // 已实测五种畸形输入，异常文本里没有任何原始字节（只有 offset）。
    // 这不是「读了一次那个指针」：它由那次失败的解码自己产出。
    final facts = <String>[
      'event-string-read-failed',
      '第 $_eventReadFailures 次',
      'in-flight: ${ops.isEmpty ? '(none)' : ops.join('|')}',
      'decoder: ${error.runtimeType}: $error',
      '按契约 §1.1 这不该发生（生产者保证 JSON + 不交出已不拥有的指针）',
      '已跳过释放该指针（宁可泄漏一帧，不换一次堆破坏）',
    ].join(' · ');

    _events.add(CoreEvent(
      type: CoreEventTypes.log,
      data: <String, Object?>{
        'level': 'warn',
        'text': '事件读失败：$facts',
      },
    ));
    unawaited(_writeClientLog('warn', '事件读失败：$facts'));
  }

  /// 往引擎环形缓冲写一行客户端日志（`log.write`，§7.12）。
  ///
  /// `level` 只许四档，自造档位会被 `bad_request` 拒；`text` 超 2000 字符由引擎截断。
  /// 限速 20 行/秒，超出回 `{ok:true, dropped:N}` —— **那是背压不是错误，不重试**。
  Future<void> _writeClientLog(String level, String text) async {
    if (_writingClientLog || !_opened || _workerPort == null) return;
    // 白名单先本地过一遍：引擎对自造档位回 `bad_request`，而这里对非 ok 信封
    // 不做处理 —— 那就是「悄悄没写进去」。降级成 warn 并在正文留痕，
    // 比静默丢弃诚实（见 `normalizeClientLog` 的注释）。
    final payload = normalizeClientLog(level, text);
    _writingClientLog = true;
    try {
      final envelope = await callRaw(Ops.logWrite, <String, Object?>{
        'level': payload.level,
        'text': payload.text,
      });
      if (envelope['ok'] == true) {
        final data = envelope['data'];
        if (data is Map) {
          final dropped = data['dropped'];
          if (dropped is int && dropped > 0) {
            _eventLogWriteDropped += dropped;
          }
        }
      }
    } on Object {
      // 连日志都写不进去时**不再上报**：那会变成「失败 → 写日志 → 又失败」的回环。
      // 本地计数器已经记下了这件事，界面上看得到。
    } finally {
      _writingClientLog = false;
    }
  }

  void _emitRaw(String text) {    try {
      final decoded = jsonDecode(text);
      if (decoded is Map) {
        _events.add(CoreEvent.fromJson(decoded.cast<String, Object?>()));
        return;
      }
      _events.add(CoreEvent(type: CoreEventTypes.log, data: <String, Object?>{
        'level': 'warn',
        'text': '收到无法解析的事件：$text',
      }));
    } on Object catch (error) {
      _events.add(CoreEvent(type: CoreEventTypes.log, data: <String, Object?>{
        'level': 'warn',
        'text': '事件解析失败：$error',
      }));
    }
  }

  static String _shortName(String path) {
    final parts = path.split(RegExp(r'[\\/]'));
    return parts.isEmpty ? path : parts.last;
  }
}

// ---------------------------------------------------------------------------
// worker isolate：所有阻塞的 AniSyncCall 都在这里跑
// ---------------------------------------------------------------------------

void _workerMain(List<Object?> init) {
  final handshake = init[0] as SendPort;
  final libraryPath = init[1] as String;
  final requests = ReceivePort();
  handshake.send(requests.sendPort);

  DynamicLibrary? library;
  _CallDart? callFn;
  _FreeDart? freeFn;
  SendPort? replies;

  requests.listen((Object? message) {
    if (message is List && message.isNotEmpty && message[0] == '@attach') {
      replies = message[1] as SendPort;
      return;
    }
    if (message is! List || message.length < 4) return;
    final id = message[0] as int;
    final op = message[1] as String;
    final payloadJson = message[2] as String;
    final callId = message[3] as String;
    final reply = replies;
    if (reply == null) return;

    Pointer<Utf8>? opPtr;
    Pointer<Utf8>? payloadPtr;
    Pointer<Utf8>? callIdPtr;
    Pointer<Utf8>? resultPtr;
    try {
      library ??= DynamicLibrary.open(libraryPath);
      callFn ??= library!.lookupFunction<_CallNative, _CallDart>('AniSyncCall');
      freeFn ??= library!.lookupFunction<_FreeNative, _FreeDart>('AniSyncFree');

      opPtr = op.toNativeUtf8();
      payloadPtr = payloadJson.toNativeUtf8();
      callIdPtr = callId.toNativeUtf8();
      resultPtr = callFn!(opPtr, payloadPtr, callIdPtr);
      if (resultPtr == nullptr) {
        reply.send(<Object?>[
          id,
          null,
          'AniSyncCall 返回了空指针（Go 侧不该发生）',
        ]);
        return;
      }
      final text = resultPtr.toDartString();
      freeFn!(resultPtr);
      resultPtr = nullptr;
      reply.send(<Object?>[id, text, null]);
    } on Object catch (error) {
      final freeOnFailure = freeFn;
      if (resultPtr != null && freeOnFailure != null) {
        try {
          freeOnFailure(resultPtr);
        } on Object {
          // 已经尽力释放；异常本身要原样报回去，不能被释放失败吞掉。
        }
      }
      reply.send(<Object?>[id, null, '$error']);
    } finally {
      if (opPtr != null) malloc.free(opPtr);
      if (payloadPtr != null) malloc.free(payloadPtr);
      if (callIdPtr != null) malloc.free(callIdPtr);
    }
  });
}
