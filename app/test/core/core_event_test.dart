import 'package:anisync_app/src/core/core_event.dart';
import 'package:anisync_app/src/model/run_entry.dart';
import 'package:anisync_app/src/state/app_state.dart';
import 'package:flutter_test/flutter_test.dart';

/// `level` 的**大小写假设**必须被钉住。
///
/// 契约 §7.12 实测：`log.write` 存进环形缓冲的 `level` 是**规整后的小写**形式
/// —— 客户端发 `INFO`，`log.recent` 里读回来的是 `info`。
/// 所以任何大写比较都会**永远不命中，而且是静默失效**（不报错、不抛异常，只是那一支永不进入）。
///
/// 这一支测试的作用：把「我们按小写比较」这件事写死。谁把比较改成大写、
/// 或者引擎哪天不再规整，这里立刻红，而不是让某条日志永远不上色。
void main() {
  CoreEvent logEvent(Object? level) => CoreEvent(
        type: CoreEventTypes.log,
        data: <String, Object?>{'level': level, 'text': 'x'},
      );

  group('LogLevels 与契约 §7.12 的白名单一致', () {
    test('四档，全小写', () {
      expect(
        LogLevels.accepted,
        <String>['debug', 'info', 'warn', 'error'],
      );
      for (final level in LogLevels.accepted) {
        expect(level, level.toLowerCase(), reason: '白名单里不许出现大写');
      }
    });
  });

  group('isError / isWarning：比较用小写', () {
    test('小写 error → isError', () {
      expect(logEvent('error').isError, isTrue);
      expect(logEvent('error').isWarning, isFalse);
    });

    test('**大写 ERROR → 不命中**（这就是为什么不能写大写比较）', () {
      // 不是「希望」它不命中，而是**如实记录引擎的规整行为**：
      // 引擎会把客户端发的 INFO 存成 info，所以 Dart 侧收到的本来就是小写。
      // 若这里变成 true，说明有人把比较改成了大小写不敏感 —— 那时这段注释要跟着改。
      expect(logEvent('ERROR').isError, isFalse);
      expect(logEvent('Warn').isWarning, isFalse);
    });

    test('warn 级：是 warning，不是 error', () {
      expect(logEvent('warn').isWarning, isTrue);
      expect(logEvent('warn').isError, isFalse);
    });

    test('debug / info → 既不是 error 也不是 warning', () {
      for (final level in <String>['debug', 'info']) {
        expect(logEvent(level).isError, isFalse);
        expect(logEvent(level).isWarning, isFalse);
      }
    });

    test('level 缺失或不是字符串 → 都不命中，且不抛', () {
      expect(logEvent(null).isError, isFalse);
      expect(logEvent(null).isWarning, isFalse);
      expect(logEvent(3).isError, isFalse);
      expect(logEvent(<String, Object?>{}).isWarning, isFalse);
    });

    test('非 log 类型但 level=error（例如 job.failed 之外）→ 按 level 判', () {
      final progress = CoreEvent(
        type: CoreEventTypes.progress,
        data: <String, Object?>{'level': 'error'},
      );
      expect(progress.isError, isTrue);
    });

    test('job.failed 没有 level 也算 error（type 本身就是结论）', () {
      const failed = CoreEvent(type: CoreEventTypes.jobFailed);
      expect(failed.isError, isTrue);
    });
  });

  group('normalizeClientLog：白名单外的档位不静默丢弃', () {
    test('白名单内 → 原样通过', () {
      for (final level in LogLevels.accepted) {
        final out = normalizeClientLog(level, '正文');
        expect(out.level, level);
        expect(out.text, '正文');
      }
    });

    test('白名单外 → 降级成 warn，且正文留痕（不是丢弃）', () {
      final out = normalizeClientLog('verbose', '正文');
      expect(out.level, LogLevels.warn);
      expect(
        out.text,
        contains('level 不在白名单：verbose'),
        reason: '必须留痕：静默改写等于「我没告诉你」伪装成「没什么可告诉」',
      );
      expect(out.text, contains('正文'), reason: '正文不能丢');
    });

    test('大写档位也不在白名单里（引擎只认小写，§7.12）', () {
      final out = normalizeClientLog('INFO', 'x');
      expect(out.level, LogLevels.warn);
      expect(out.text, contains('INFO'));
    });
  });

  group('log.recent → 事件缓冲：level 原样保留，Dart 侧不做大小写加工', () {
    test('seed 之后缓冲里的 level 与 LogLine 一致（我们不转大写/小写）', () {
      final log = EventLog();
      log.seed(<LogLine>[
        LogLine(level: 'info', text: '引擎自己写的行'),
        LogLine(level: 'warn', text: '客户端写的行（引擎已加 [client] 前缀与前缀）'),
        LogLine(level: 'error', text: '错误行'),
      ]);

      final events = log.events;
      expect(events.length, 3);
      expect(events.map((e) => e.level).toList(), <String?>[
        'info',
        'warn',
        'error',
      ], reason: '原样搬运：Dart 侧不许自己 toUpperCase/toLowerCase');
      // 于是上面那套小写比较在兜底视图里同样成立。
      expect(events[2].isError, isTrue);
      expect(events[1].isWarning, isTrue);
      expect(events[0].isError, isFalse);
    });

    test('环形缓冲：超过 500 行丢最旧的（level 随之出栈，不影响比较）', () {
      final log = EventLog();
      final lines = <LogLine>[
        for (var i = 0; i < EventLog.capacity + 10; i++)
          LogLine(level: i.isEven ? 'info' : 'error', text: 'line $i'),
      ];
      log.seed(lines);
      expect(log.events.length, EventLog.capacity);
      expect(log.events.last.level, 'error');
    });
  });
}
