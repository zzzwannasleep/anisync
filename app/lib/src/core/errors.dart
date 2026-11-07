/// `docs/ops-api.md` §3 的错误码。UI 按 `code` 决定展示方式，不解析 message 文本。
class ErrorCodes {
  ErrorCodes._();

  static const String badRequest = 'bad_request';
  static const String notFound = 'not_found';
  static const String invalidConfig = 'invalid_config';
  static const String network = 'network';
  static const String busy = 'busy';
  static const String leaseHeld = 'lease_held';
  static const String canceled = 'canceled';
  static const String internal = 'internal';

  /// Dart 侧自造的两个（不是契约里的 Go 错误码）：
  /// 传输层连不上（FFI 库缺失 / HTTP 不通）与节流等待超时。
  static const String transport = 'transport';

  /// 节点后端返回 401：`X-AniSync-Token` 不对或缺失。也是 Dart 侧自造的。
  static const String unauthorized = 'unauthorized';
}

/// 一个信封错误：`{"code":..,"message":..,"detail":..}`。
class CoreError {
  const CoreError({
    required this.code,
    required this.message,
    this.detail,
    this.versionSkew = false,
  });

  /// 传输层/解析层自己造的错（不是 Go 回的）。
  factory CoreError.transport(String message, {String? detail}) =>
      CoreError(code: ErrorCodes.transport, message: message, detail: detail);

  factory CoreError.fromJson(Map<String, Object?> json) => CoreError(
        code: (json['code'] as String?) ?? ErrorCodes.internal,
        message: (json['message'] as String?) ?? '未知错误',
        detail: json['detail'] as String?,
      );

  final String code;
  final String message;
  final String? detail;

  /// **版本偏斜**：`not_found` 且 `detail` 恰等于刚发出的 op 名（契约 §3 的判据）。
  ///
  /// 这是 Dart 侧**派生**的标记，不是协议字段：判别需要「我发了哪个 op」，
  /// 而 `CoreError` 自己看不到 op，所以由调用点（[markVersionSkew]）在构造后打上。
  ///
  /// 存在的理由：`not_found` 同时承载「资源不存在」与「op 不存在」两种含义，
  /// 前者刷新有用、后者刷新一百次也没用（要升级引擎）。不区分就只能给一句含糊提示。
  final bool versionSkew;

  CoreError withVersionSkew() => versionSkew
      ? this
      : CoreError(
          code: code,
          message: message,
          detail: detail,
          versionSkew: true,
        );

  /// 「调用被取消」是静默状态，UI 不该弹任何东西。
  bool get isCanceled => code == ErrorCodes.canceled;

  /// 可整体重试的错误（网络类）。
  bool get isRetryable =>
      code == ErrorCodes.network ||
      code == ErrorCodes.transport ||
      code == ErrorCodes.internal;

  /// 给「复制错误原文」按钮用的完整文本。
  String get fullText {
    final buffer = StringBuffer('code: $code\nmessage: $message');
    if (detail != null && detail!.isNotEmpty) {
      buffer.write('\ndetail: $detail');
    }
    return buffer.toString();
  }

  Map<String, Object?> toJson() => <String, Object?>{
        'code': code,
        'message': message,
        if (detail != null) 'detail': detail,
      };

  @override
  String toString() => '[$code] $message';
}

/// `{"ok":false,"error":{...}}` 的异常形态。
class CoreException implements Exception {
  const CoreException(this.error);

  final CoreError error;

  String get code => error.code;
  String get message => error.message;

  @override
  String toString() => error.toString();
}

/// 契约 §3 的版本偏斜判据：**`not_found` 且 `detail` 与刚发出的 op 名逐字相同**。
///
/// 判别放在调用点，因为只有调用点知道自己发了哪个 op ——
/// `SErr.byCode` 是 code 级别的，它看不到 op，所以它给不出这个区分。
///
/// 只用 `detail` 做机器判别：`message` 是给人看的、会被改写与翻译。
/// 资源名不会与 op 名逐字相同（op 名一律 `组.动作` 形状）。
CoreError detectVersionSkew(CoreError error, String? op) {
  if (op == null || op.isEmpty) return error;
  if (error.code != ErrorCodes.notFound) return error;
  if (error.detail != op) return error;
  return error.withVersionSkew();
}

/// 契约 §1：**取消有两种合法表现，两种都算取消成功。**
///
/// - 没有自身错误字段的 op（`plan.build` 这类）→ `{"ok":false,"error":{"code":"canceled"}}`
/// - 自带 `error?` 字段的 op（`instance.test` / `instance.subscriptions`）→
///   `ok:true` + `data.error = "context canceled"`
///
/// 第二种是**成功返回里的一个字段**，所以「`ok:true` 就等于没事」是错的判断：
/// 不认它，用户就会看到一条红色的「连接失败」，而它其实是他自己取消的。
bool looksCanceled(Object? text) {
  if (text is! String) return false;
  final value = text.trim().toLowerCase();
  if (value.isEmpty) return false;
  if (value == 'canceled' || value == 'cancelled') return true;
  return value.contains('context canceled') ||
      value.contains('context cancelled') ||
      value.contains('context deadline exceeded') ||
      value.contains('取消');
}
