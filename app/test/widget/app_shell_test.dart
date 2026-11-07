import 'dart:io';

import 'package:anisync_app/src/state/app_state.dart';
import 'package:anisync_app/src/state/prefs.dart';
import 'package:anisync_app/src/ui/app_shell.dart';
import 'package:anisync_app/src/ui/strings.dart';
import 'package:anisync_app/src/ui/theme.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/fake_backend.dart';

/// 壳与全部 8 个页面都能构建出来：窄屏 / 宽屏两种导航都过一遍。
///
/// 这一支的价值是**编译 + 首次构建**覆盖：`flutter analyze` 管类型，
/// 这里管「页面真的能 build 出来」（越界、空指针、Key 冲突这类）。
void main() {
  Future<AppState> buildState() async {
    final prefs = PrefsStore(
      directory: Directory.systemTemp.createTempSync('anisync-shell').path,
    );
    final state = AppState(prefs: prefs);
    // 接一个替身但**不 open**：页面里的自动加载走它，拿到空数据进空态。
    await state.attach(FakeBackend(), open: false);
    return state;
  }

  Future<void> pumpShell(
    WidgetTester tester,
    AppState state, {
    required double width,
  }) async {
    tester.view.physicalSize = Size(width, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      MaterialApp(
        theme: buildAppTheme(Brightness.light),
        home: AppShell(state: state),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('宽屏：NavigationRail，7 个导航项逐个点过去都不炸', (tester) async {
    final state = await buildState();
    await pumpShell(tester, state, width: 1280);

    expect(find.byType(NavigationRail), findsNothing);
    for (final label in S.navLabels) {
      expect(find.text(label), findsWidgets, reason: '导航里缺少「$label」');
    }
    for (var i = 0; i < S.navLabels.length; i++) {
      await tester.tap(find.text(S.navLabels[i]).first);
      await tester.pumpAndSettle();
      expect(
        tester.takeException(),
        isNull,
        reason: '页面「${S.navLabels[i]}」在 1280px 下渲染出错',
      );
    }
  });

  testWidgets('窄屏：NavigationBar，断点 900dp 生效', (tester) async {
    final state = await buildState();
    await pumpShell(tester, state, width: 600);

    expect(find.byType(NavigationBar), findsOneWidget);
    // 断点生效：窄屏用底部导航，而不是把 208px 的侧栏硬塞进来。
    expect(find.byType(NavigationRail), findsNothing);
    for (var i = 0; i < S.navLabels.length; i++) {
      await tester.tap(find.text(S.navLabels[i]).first);
      await tester.pumpAndSettle();
      expect(
        tester.takeException(),
        isNull,
        reason: '页面「${S.navLabels[i]}」在 600px 下渲染出错',
      );
    }
  });

  testWidgets('没有后端时不崩：走空态而不是抛异常', (tester) async {
    final prefs = PrefsStore(
      directory: Directory.systemTemp.createTempSync('anisync-shell2').path,
    );
    final state = AppState(prefs: prefs);
    await pumpShell(tester, state, width: 1280);

    expect(tester.takeException(), isNull);
    expect(find.text(SOverview.noWorkspace), findsWidgets);
  });
}
