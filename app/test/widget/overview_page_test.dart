import 'dart:convert';
import 'dart:io';

import 'package:anisync_app/src/core/errors.dart';
import 'package:anisync_app/src/model/status.dart';
import 'package:anisync_app/src/state/app_state.dart';
import 'package:anisync_app/src/state/async_state.dart';
import 'package:anisync_app/src/state/prefs.dart';
import 'package:anisync_app/src/ui/pages/overview_page.dart';
import 'package:anisync_app/src/ui/strings.dart';
import 'package:anisync_app/src/ui/theme.dart';
import 'package:anisync_app/src/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// 概览页在「加载中 / 空 / 失败 / 有数据」四态下的渲染。
///
/// 这一支是 unhappy 路线的验收：骨架、空态的下一步、错误的重试与复制，缺一个都算缺口。
void main() {
  AppState buildState() {
    final prefs = PrefsStore(
      directory: Directory.systemTemp.createTempSync('anisync-prefs').path,
    );
    return AppState(prefs: prefs);
  }

  Future<void> pump(WidgetTester tester, AppState state) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildAppTheme(Brightness.light),
        home: Scaffold(
          body: OverviewPage(state: state, onOpenInstances: () {}),
        ),
      ),
    );
    await tester.pump();
  }

  testWidgets('加载中：画骨架行，不是空白也不是居中转圈', (tester) async {
    final state = buildState();
    state.status.status = const AsyncLoading<StatusResult>();
    await pump(tester, state);

    expect(find.byType(SkeletonRows), findsWidgets);
    expect(find.byType(EmptyState), findsNothing);
    expect(find.byType(ErrorPane), findsNothing);
  });

  testWidgets('空：说明为什么是空的 + 给下一步按钮', (tester) async {
    final state = buildState();
    state.status.status = const AsyncEmpty<StatusResult>(why: '');
    await pump(tester, state);

    expect(find.byType(EmptyState), findsOneWidget);
    expect(find.text(SOverview.goInstances), findsOneWidget);
    expect(find.byType(SkeletonRows), findsNothing);
  });

  testWidgets('失败：具体原因 + 一键重试 + 复制错误原文', (tester) async {
    final state = buildState();
    state.status.status = const AsyncFailed<StatusResult>(
      CoreError(
        code: ErrorCodes.transport,
        message: '连不上本机引擎',
        detail: '找不到 anisync_core.dll：已尝试 D:\\x\\anisync_core.dll',
      ),
    );
    await pump(tester, state);

    expect(find.byType(ErrorPane), findsOneWidget);
    expect(find.text('连不上本机引擎'), findsOneWidget);
    expect(find.text(SC.retry), findsOneWidget);
    expect(find.byType(SelectableText), findsOneWidget);
  });

  testWidgets('有数据：渲染实例健康表与「立即同步」', (tester) async {
    final state = buildState();
    final file = File('test/fixtures/shapes/status_get.data.json');
    if (!file.existsSync()) {
      fail('形状夹具缺失：${file.path}');
    }
    final json = (jsonDecode(file.readAsStringSync()) as Map)
        .cast<String, Object?>();
    state.status.status = AsyncData<StatusResult>(StatusResult.fromJson(json));

    await pump(tester, state);

    expect(find.text(SOverview.syncNow), findsOneWidget);
    expect(find.text(SOverview.lease), findsOneWidget);
    // 已添加的实例名与方案名要出现在健康表里。
    final status = StatusResult.fromJson(json);
    if (status.instancesOrEmpty.isNotEmpty) {
      expect(
        find.text(status.instancesOrEmpty.first.instance),
        findsWidgets,
      );
    }
  });

  testWidgets('失败但有旧数据：保留旧数据 + 顶部紧凑错误条', (tester) async {
    final state = buildState();
    final file = File('test/fixtures/shapes/status_get.data.json');
    final json = (jsonDecode(file.readAsStringSync()) as Map)
        .cast<String, Object?>();
    state.status.status = AsyncFailed<StatusResult>(
      const CoreError(code: ErrorCodes.network, message: '一次刷新失败'),
      previous: StatusResult.fromJson(json),
    );
    await pump(tester, state);

    expect(find.byType(ErrorPane), findsOneWidget);
    expect(find.text(SOverview.syncNow), findsOneWidget);
  });
}
