import '../core/errors.dart';

/// 一个数据面的状态。**判别联合，不是一堆布尔** ——
/// `isLoading && !error && data` 那种写法能表达出不可能的状态，这里从类型上禁掉。
sealed class AsyncState<T> {
  const AsyncState();

  /// 已经拿到的值（重试时用来保留旧数据，避免闪白）。
  T? get valueOrNull => switch (this) {
        AsyncData<T>(:final value) => value,
        AsyncPartial<T>(:final value) => value,
        AsyncLoading<T>(:final previous) => previous,
        AsyncFailed<T>(:final previous) => previous,
        _ => null,
      };
}

/// 还没开始（或后端未 attach）。
final class AsyncIdle<T> extends AsyncState<T> {
  const AsyncIdle();
}

/// 正在加载；`previous` 非空时 UI 可以继续显示旧数据 + 顶部进度条。
final class AsyncLoading<T> extends AsyncState<T> {
  const AsyncLoading({this.previous});

  final T? previous;
}

/// 有数据且完整。
final class AsyncData<T> extends AsyncState<T> {
  const AsyncData(this.value);

  final T value;
}

/// 数据拉成功但是没有内容。`why` 由页面写：**为什么是空的**。
final class AsyncEmpty<T> extends AsyncState<T> {
  const AsyncEmpty({required this.why});

  final String why;
}

/// 失败。`previous` 保留上一次成功的数据，界面可以「部分可用 + 红条」。
final class AsyncFailed<T> extends AsyncState<T> {
  const AsyncFailed(this.error, {this.previous});

  final CoreError error;
  final T? previous;

  /// 调用被取消是静默状态，不弹任何东西。
  bool get isSilent => error.isCanceled;
}

/// 部分数据可用：主体成功，某些片段降级（例如某个实例读不到、某个来源挂了）。
final class AsyncPartial<T> extends AsyncState<T> {
  const AsyncPartial(this.value, this.degradations);

  final T value;
  final List<CoreError> degradations;
}

/// 给页面的小助手：把一次加载包成状态迁移。
class AsyncController<T> {
  AsyncController(this._emit, {AsyncState<T>? initial})
      : state = initial ?? AsyncIdle<T>();

  final void Function() _emit;
  AsyncState<T> state;

  void set(AsyncState<T> next) {
    state = next;
    _emit();
  }

  void loading() =>
      set(AsyncLoading<T>(previous: state.valueOrNull));

  void data(T value) => set(AsyncData<T>(value));

  void empty(String why) => set(AsyncEmpty<T>(why: why));

  void failed(CoreError error) =>
      set(AsyncFailed<T>(error, previous: state.valueOrNull));

  /// 统一包一次调用：抛 [CoreException] 时进失败态，并把错误原样留着给「复制原文」。
  Future<T?> run(Future<T> Function() body, {bool Function(T value)? isEmpty}) async {
    loading();
    try {
      final value = await body();
      if (isEmpty != null && isEmpty(value)) {
        empty('');
      } else {
        data(value);
      }
      return value;
    } on CoreException catch (error) {
      if (!error.error.isCanceled) failed(error.error);
      return null;
    } on Object catch (error) {
      failed(
        CoreError.transport('未预期的错误', detail: '$error'),
      );
      return null;
    }
  }
}
