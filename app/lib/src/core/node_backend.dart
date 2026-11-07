import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:http/http.dart' as http;

import 'backend.dart';
import 'core_event.dart';
import 'errors.dart';

/// 远端节点后端：`POST /api/v1/call` + SSE `/api/v1/events`（`docs/ops-api.md` §2）。
///
/// 与 `FfiBackend` 实现同一个 [Backend]，所以 UI 层不区分自己接的是本地库还是远端节点。
class NodeBackend implements Backend {
  NodeBackend({
    required String baseUrl,
    this.token = '',
    http.Client? client,
    Duration? timeout,
  })  : _base = _normalizeBase(baseUrl),
        _client = client ?? http.Client(),
        _timeout = timeout ?? const Duration(seconds: 60);

  final String _base;
  final String token;
  final http.Client _client;
  final Duration _timeout;

  final StreamController<CoreEvent> _events =
      StreamController<CoreEvent>.broadcast();
  StreamSubscription<String>? _sseSubscription;
  bool _disposed = false;
  StreamSubscription<String>? _sseControl;

  /// 服务器信息（`/api/v1/health`）。
  Map<String, Object?>? health;

  @override
  String get label => '远端节点 · $_base';

  /// 契约 §1.1 的那个计数器是**FFI 专属**的失效模式（`NativeCallable.listener`
  /// 的时序问题）。HTTP 后端不存在它 —— SSE 帧解析失败是另一类，走 warn 事件。
  @override
  int get eventReadFailures => 0;

  @override
  int get eventLogWriteDropped => 0;

  /// 同上：这一对是 FFI 的事件帧所有权计数，HTTP 后端没有对应的寿命问题。
  @override
  int get eventReadCount => 0;

  @override
  int get eventReleasedCount => 0;

  String get baseUrl => _base;

  @override
  Stream<CoreEvent> get events => _events.stream;

  @override
  Future<void> open() async {
    _disposed = false;
    unawaited(_listenEvents());
  }

  @override
  Future<Map<String, Object?>> call(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) async {
    final envelope = await callRaw(op, payload, callId: callId);
    // 版本偏斜在这里最可能真实发生：手机装了新版 App，家里跑着旧版 `serve`。
    // 判别只有一份实现（`dataOf`），两个后端共用。
    return dataOf(envelope, op: op);
  }

  @override
  Future<Map<String, Object?>> callRaw(
    String op,
    Map<String, Object?> payload, {
    String? callId,
  }) async {
    final uri = Uri.parse('$_base/api/v1/call');
    try {
      final response = await _client
          .post(
            uri,
            headers: _headers(json: true),
            body: jsonEncode(<String, Object?>{
              'op': op,
              'payload': payload,
            }),
          )
          .timeout(_timeout);
      return _envelopeFrom(response);
    } on TimeoutException catch (error) {
      return _transportError('请求超时（${_timeout.inSeconds}s）', '$error');
    } on SocketException catch (error) {
      return _transportError('连不上节点 $_base', '$error');
    } on http.ClientException catch (error) {
      return _transportError('HTTP 客户端错误', '$error');
    } on Object catch (error) {
      return _transportError('调用 $op 失败', '$error');
    }
  }

  @override
  Future<bool> cancel(String callId) async {
    // §2 的 HTTP 契约里**没有**取消通道：`POST /api/v1/call` 不带 callId，
    // 也没有取消端点。进程内取消靠 `AniSyncCancel`（FFI 专属），
    // 异步 op 的取消走 `job.cancel`。所以这里如实回 false ——
    // 编一个「看起来像取消」的请求发出去，比不支持更糟。
    return false;
  }

  /// `GET /api/v1/health`。
  Future<Map<String, Object?>> checkHealth() async {
    try {
      final response = await _client
          .get(Uri.parse('$_base/api/v1/health'), headers: _headers())
          .timeout(const Duration(seconds: 10));
      final decoded = jsonDecode(response.body);
      final envelope = normalizeEnvelope(decoded);
      if (envelope['ok'] == true) {
        final data = envelope['data'];
        if (data is Map) health = data.cast<String, Object?>();
      }
      return envelope;
    } on Object catch (error) {
      return _transportError('健康检查失败', '$error');
    }
  }

  @override
  Future<void> dispose() async {
    _disposed = true;
    await _sseSubscription?.cancel();
    _sseSubscription = null;
    await _sseControl?.cancel();
    _sseControl = null;
    _client.close();
    await _events.close();
  }

  Map<String, String> _headers({bool json = false}) => <String, String>{
        if (json) 'Content-Type': 'application/json; charset=utf-8',
        'Accept': 'application/json',
        if (token.isNotEmpty) 'X-AniSync-Token': token,
      };

  Map<String, Object?> _envelopeFrom(http.Response response) {
    if (response.statusCode == 401) {
      return <String, Object?>{
        'ok': false,
        'error': CoreError(
          code: ErrorCodes.unauthorized,
          message: '节点拒绝了这次请求：token 不对或没带',
          detail: 'HTTP 401',
        ).toJson(),
      };
    }
    Object? decoded;
    try {
      decoded = jsonDecode(response.body);
    } on Object catch (error) {
      return _transportError(
        '节点返回的不是 JSON（HTTP ${response.statusCode}）',
        '$error\n${_clip(response.body)}',
      );
    }
    return normalizeEnvelope(decoded);
  }

  Map<String, Object?> _transportError(String message, String? detail) =>
      <String, Object?>{
        'ok': false,
        'error':
            CoreError.transport(message, detail: detail).toJson(),
      };

  static String _clip(String text) =>
      text.length <= 400 ? text : '${text.substring(0, 400)}…';

  static String _normalizeBase(String raw) {
    var value = raw.trim();
    if (value.isEmpty) return 'http://127.0.0.1:51837';
    while (value.endsWith('/')) {
      value = value.substring(0, value.length - 1);
    }
    if (!value.startsWith('http://') && !value.startsWith('https://')) {
      value = 'http://$value';
    }
    return value;
  }

  // ---- SSE：断线指数退避重连，重连后由调用方用 log.recent 补缺口 ----

  Future<void> _listenEvents() async {
    var attempt = 0;
    while (!_disposed) {
      try {
        final request = http.Request('GET', Uri.parse('$_base/api/v1/events'));
        request.headers.addAll(<String, String>{
          'Accept': 'text/event-stream',
          'Cache-Control': 'no-cache',
          if (token.isNotEmpty) 'X-AniSync-Token': token,
        });
        final response = await _client.send(request);
        if (response.statusCode != 200) {
          throw http.ClientException('HTTP ${response.statusCode}');
        }
        attempt = 0;
        final completer = Completer<void>();
        _sseControl = response.stream
            .transform(utf8.decoder)
            .transform(const LineSplitter())
            .listen(
          _onSseLine,
          onError: (Object error) {
            if (!completer.isCompleted) completer.complete();
          },
          onDone: () {
            if (!completer.isCompleted) completer.complete();
          },
          cancelOnError: true,
        );
        await completer.future;
      } on Object catch (error) {
        _events.add(CoreEvent(
          type: CoreEventTypes.log,
          data: <String, Object?>{
            'level': 'warn',
            'text': '事件流断开：$error',
          },
        ));
      }
      if (_disposed) break;
      attempt++;
      await Future<void>.delayed(_backoff(attempt));
    }
  }

  void _onSseLine(String line) {
    if (line.isEmpty || line.startsWith(':')) return;
    if (!line.startsWith('data:')) return;
    final payload = line.substring(5).trimLeft();
    if (payload.isEmpty) return;
    try {
      final decoded = jsonDecode(payload);
      if (decoded is Map) {
        _events.add(CoreEvent.fromJson(decoded.cast<String, Object?>()));
      }
    } on Object catch (error) {
      _events.add(CoreEvent(
        type: CoreEventTypes.log,
        data: <String, Object?>{
          'level': 'warn',
          'text': '事件解析失败：$error',
        },
      ));
    }
  }

  /// 1s → 2s → 4s → … → 上限 30s。
  static Duration _backoff(int attempt) {
    final seconds = 1 << (attempt.clamp(0, 5));
    return Duration(seconds: seconds > 30 ? 30 : seconds);
  }
}
