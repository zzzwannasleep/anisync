import 'package:anisync_app/src/core/backend.dart';
import 'package:anisync_app/src/core/errors.dart';
import 'package:anisync_app/src/core/node_backend.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

/// 契约 §3 的版本偏斜判别 + §1 的取消双形态。
///
/// 这两条都是**纯判别**：判别方法错了，界面给出的提示就会把人带偏
/// （「刷新试试」对着版本偏斜说一百遍也没用）。
void main() {
  group('detectVersionSkew（§3 的机器判据）', () {
    const op = 'profile.ruleFile';

    CoreError notFound(String? detail) => CoreError(
          code: ErrorCodes.notFound,
          message: '找不到',
          detail: detail,
        );

    test('not_found + detail 恰等于 op 名 → 版本偏斜', () {
      expect(detectVersionSkew(notFound(op), op).versionSkew, isTrue);
    });

    test('not_found + detail 是资源名 → 不是版本偏斜', () {
      final error = detectVersionSkew(notFound('追番库'), op);
      expect(error.versionSkew, isFalse);
      expect(error.detail, '追番库', reason: '资源名要留着，界面要显示是哪一个');
    });

    test('其它 code 一律不是版本偏斜（哪怕 detail 撞上了）', () {
      for (final code in <String>[
        ErrorCodes.badRequest,
        ErrorCodes.network,
        ErrorCodes.internal,
      ]) {
        expect(
          detectVersionSkew(
            CoreError(code: code, message: 'm', detail: op),
            op,
          ).versionSkew,
          isFalse,
          reason: '$code 不该被判成版本偏斜',
        );
      }
    });

    test('判别只看 detail，不看 message（message 会被改写与翻译）', () {
      final error = CoreError(
        code: ErrorCodes.notFound,
        message: '这个操作不存在，请升级', // 措辞像版本偏斜，但 detail 不是 op
        detail: '某实例',
      );
      expect(detectVersionSkew(error, op).versionSkew, isFalse);
      final other = CoreError(
        code: ErrorCodes.notFound,
        message: '随便什么话', // 措辞完全不像，但 detail 是 op
        detail: op,
      );
      expect(detectVersionSkew(other, op).versionSkew, isTrue);
    });

    test('op 为空时不判（没有可比对的 op 名）', () {
      expect(detectVersionSkew(notFound(null), null).versionSkew, isFalse);
      expect(detectVersionSkew(notFound(''), '').versionSkew, isFalse);
    });
  });

  group('looksCanceled（§1 取消的第二种表现）', () {
    test('自带 error 字段的 op 回的取消文本要被认出来', () {
      for (final text in <String>[
        'context canceled',
        'context cancelled',
        'Context Canceled',
        'canceled',
        'context deadline exceeded',
        '请求已被取消',
      ]) {
        expect(looksCanceled(text), isTrue, reason: '「$text」应当算取消');
      }
    });

    test('普通错误不算取消', () {
      for (final text in <String?>[
        'connection refused',
        '401 Unauthorized',
        '实例 追番库 缺少 apiKey',
        '',
        null,
      ]) {
        expect(looksCanceled(text), isFalse, reason: '「$text」不是取消');
      }
    });
  });

  group('NodeBackend：判别在两个后端上是同一份实现', () {
    test('远端旧节点不认识新 op → not_found + detail == op + versionSkew', () async {
      const op = 'log.write';
      final backend = NodeBackend(
        baseUrl: 'http://node.local:51837',
        token: 't',
        client: MockClient((request) async {
          // 旧节点：op 不认识。它回的 detail 就是它收到的那个 op 名。
          expect(request.url.path, '/api/v1/call');
          expect(request.headers['X-AniSync-Token'], 't');
          return http.Response(
            '{"ok":false,"error":{"code":"not_found",'
            '"message":"不认识的 op","detail":"$op"}}',
            200,
            headers: <String, String>{'content-type': 'application/json'},
          );
        }),
      );

      final raw = await backend.callRaw(op, <String, Object?>{});
      expect((raw['error']! as Map)['code'], ErrorCodes.notFound);

      await expectLater(
        backend.call(op, <String, Object?>{}),
        throwsA(
          isA<CoreException>().having(
            (e) => e.error.versionSkew,
            'versionSkew',
            isTrue,
          ),
        ),
      );
      await backend.dispose();
    });

    test('资源不存在（detail 不是 op）→ 不判成版本偏斜', () async {
      const op = 'instance.list';
      final backend = NodeBackend(
        baseUrl: 'http://node.local:51837',
        client: MockClient((request) async => http.Response(
              '{"ok":false,"error":{"code":"not_found",'
              '"message":"实例不存在","detail":"追番库"}}',
              200,
            )),
      );
      await expectLater(
        backend.call(op, <String, Object?>{}),
        throwsA(
          isA<CoreException>().having(
            (e) => e.error.versionSkew,
            'versionSkew',
            isFalse,
          ),
        ),
      );
      await backend.dispose();
    });

    test('401 → unauthorized（不是 internal）', () async {
      final backend = NodeBackend(
        baseUrl: 'http://node.local:51837',
        token: 'bad',
        client: MockClient((request) async => http.Response('nope', 401)),
      );
      final raw = await backend.callRaw('core.version', <String, Object?>{});
      expect((raw['error']! as Map)['code'], ErrorCodes.unauthorized);
      await backend.dispose();
    });
  });

  group('dataOf：信封解包', () {
    test('ok:true 且没有 data → 空对象，不是崩', () {
      expect(dataOf(<String, Object?>{'ok': true}), <String, Object?>{});
    });

    test('ok:false 但没有 error 对象 → internal，不是崩', () {
      expect(
        () => dataOf(<String, Object?>{'ok': false}),
        throwsA(isA<CoreException>().having(
          (e) => e.code,
          'code',
          ErrorCodes.internal,
        )),
      );
    });
  });
}
