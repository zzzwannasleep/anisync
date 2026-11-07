import 'package:anisync_app/src/core/errors.dart';
import 'package:anisync_app/src/ui/strings.dart';
import 'package:anisync_app/src/ui/theme.dart';
import 'package:anisync_app/src/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// `not_found` 的**三档渲染**，逐档钉住。
///
/// 为什么单独钉第三档：契约 §3 承诺「资源不存在 → `detail` = 资源名」，但 core-go 的
/// 资源分支一度**不填 `detail`**（t23 在补）。客户端面对「协议承诺了、对方没兑现」，
/// 正确姿态是**降级并如实呈现**，不是去猜 `message` 里的词。
///
/// t23 落地后这一档会**走不到** —— 但**「走不到」和「不该有」是两件事**：
/// 走不到的分支最容易被下一次清理顺手删掉，而删掉就等于把一次未兑现的承诺变成一次崩溃。
/// 所以它必须**由测试钉住**，而不是靠「别忘了」。
void main() {
  Future<void> pump(WidgetTester tester, CoreError error,
      {VoidCallback? onRetry}) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildAppTheme(Brightness.light),
        home: Scaffold(
          body: ErrorPane(error: error, onRetry: onRetry ?? () {}),
        ),
      ),
    );
    await tester.pump();
  }

  testWidgets('第一档：版本偏斜 —— 讲清版本不匹配，且**不给重试**', (tester) async {
    await pump(
      tester,
      const CoreError(
        code: ErrorCodes.notFound,
        message: '不认识的 op', // 措辞由后端给，界面不依赖它
        detail: 'log.write',
        versionSkew: true,
      ),
    );

    expect(find.text(SErr.skewTitle), findsOneWidget);
    expect(find.text(SErr.skewWhy), findsOneWidget);
    expect(find.text(SErr.skewOp.replaceFirst('%s', 'log.write')), findsOneWidget);
    // 重试不会让旧内核认识一个新 op —— 那个按钮是误导。
    expect(find.text(SC.retry), findsNothing);
  });

  testWidgets('第二档：资源不存在且有 detail —— 把资源名显示出来', (tester) async {
    await pump(
      tester,
      const CoreError(
        code: ErrorCodes.notFound,
        message: '实例不存在',
        detail: '追番库',
      ),
    );

    expect(
      find.text(SErr.resourceName.replaceFirst('%s', '追番库')),
      findsOneWidget,
    );
    expect(find.text(SC.retry), findsOneWidget, reason: '资源类问题刷新有用，该给重试');
    expect(find.text(SErr.skewTitle), findsNothing);
  });

  testWidgets('第三档：detail 缺失的降级 —— 显示 message，且**不伪造资源名**',
      (tester) async {
    await pump(
      tester,
      const CoreError(
        code: ErrorCodes.notFound,
        // 真实资源名在这里，但**机器判别不许碰它**：
        // message 会被翻译、被人改、被改写措辞（契约 §3）。
        message: '找不到名为「追番库」的实例',
      ),
    );

    // 给人看的文本必须还在（它是 message，本来就是写给人看的）。
    expect(find.text('找不到名为「追番库」的实例'), findsOneWidget);
    // 但没有 detail，就不许出现「资源名」那条通道 —— 那是 detail 的位置。
    expect(find.textContaining(SErr.resourceName.split('%s').first), findsNothing);
    // 也不许把它判成版本偏斜（那需要 detail 恰等于 op 名）。
    expect(find.text(SErr.skewTitle), findsNothing);
    // 降级归降级，重试仍然有效。
    expect(find.text(SC.retry), findsOneWidget);
  });

  testWidgets('版本偏斜的判定**只看 detail**：message 措辞像也不算', (tester) async {
    await pump(
      tester,
      const CoreError(
        code: ErrorCodes.notFound,
        message: '这个操作不存在，请升级引擎', // 措辞像版本偏斜
        detail: '追番库', // 但 detail 是资源名
      ),
    );
    expect(find.text(SErr.skewTitle), findsNothing);
    expect(find.text(SC.retry), findsOneWidget);
  });
}
