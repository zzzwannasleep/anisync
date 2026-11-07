import 'dart:convert';
import 'dart:io';

import 'package:anisync_app/src/core/errors.dart';
import 'package:anisync_app/src/model/subscription.dart';
import 'package:anisync_app/src/state/app_state.dart';
import 'package:anisync_app/src/state/async_state.dart';
import 'package:anisync_app/src/state/prefs.dart';
import 'package:anisync_app/src/ui/pages/subscriptions_page.dart';
import 'package:anisync_app/src/ui/strings.dart';
import 'package:anisync_app/src/ui/theme.dart';
import 'package:anisync_app/src/ui/widgets/common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// 订阅列表在「加载中 / 空 / 失败 / 有数据」四态下的渲染。
void main() {
  const String instance = '追番库';

  AppState buildState() {
    final prefs = PrefsStore(
      directory: Directory.systemTemp.createTempSync('anisync-prefs').path,
    );
    final state = AppState(prefs: prefs);
    // 先认领实例名，避免页面在首帧后自己去打网络（测试里没有后端）。
    state.subscriptions.instanceName = instance;
    return state;
  }

  Future<void> pump(WidgetTester tester, AppState state) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: buildAppTheme(Brightness.light),
        home: Scaffold(
          body: SubscriptionsPage(
            state: state,
            instance: instance,
            onBack: () {},
          ),
        ),
      ),
    );
    await tester.pump();
  }

  testWidgets('加载中：骨架行', (tester) async {
    final state = buildState();
    state.subscriptions.list = const AsyncLoading<SubscriptionListResult>();
    await pump(tester, state);

    expect(find.byType(SkeletonRows), findsWidgets);
    expect(find.byType(EmptyState), findsNothing);
  });

  testWidgets('空：说明为什么空 + 下一步做什么', (tester) async {
    final state = buildState();
    state.subscriptions.list = const AsyncEmpty<SubscriptionListResult>(why: '');
    await pump(tester, state);

    expect(find.byType(EmptyState), findsOneWidget);
    expect(find.text(SSubs.noSubscriptions), findsOneWidget);
    expect(find.text(SSubs.emptyNext), findsOneWidget);
  });

  testWidgets('失败：原因 + 重试（能复制错误原文）', (tester) async {
    final state = buildState();
    state.subscriptions.list = const AsyncFailed<SubscriptionListResult>(
      CoreError(
        code: ErrorCodes.network,
        message: '连不上实例 追番库',
        detail: 'SocketException: connection refused',
      ),
    );
    await pump(tester, state);

    expect(find.byType(ErrorPane), findsOneWidget);
    expect(find.text('连不上实例 追番库'), findsOneWidget);
    expect(find.text(SC.retry), findsOneWidget);
  });

  testWidgets('有数据：渲染 main / standby 组名与集偏移，数字等宽', (tester) async {
    final state = buildState();
    final file = File('test/fixtures/shapes/instance_subscriptions.data.json');
    if (!file.existsSync()) {
      fail('形状夹具缺失：${file.path}');
    }
    final json = (jsonDecode(file.readAsStringSync()) as Map)
        .cast<String, Object?>();
    final result = SubscriptionListResult.fromJson(json);
    state.subscriptions.list = AsyncData<SubscriptionListResult>(result);

    await pump(tester, state);

    expect(find.text(result.itemsOrEmpty.first.title), findsOneWidget);
    expect(find.textContaining('旧组'), findsWidgets);
    expect(find.textContaining('ANi'), findsWidgets);
    // 集偏移列：season / offset / standbyOffset 拼一行。
    expect(find.text('1 / 0 / -12'), findsOneWidget);
    // 数字必须走等宽组件。
    expect(find.byType(NumText), findsWidgets);
    expect(find.byType(EmptyState), findsNothing);
    expect(find.byType(ErrorPane), findsNothing);
  });

  testWidgets('筛选空了：给「清空筛选」而不是空盒子', (tester) async {
    final state = buildState();
    final file = File('test/fixtures/shapes/instance_subscriptions.data.json');
    final json = (jsonDecode(file.readAsStringSync()) as Map)
        .cast<String, Object?>();
    state.subscriptions.list =
        AsyncData<SubscriptionListResult>(SubscriptionListResult.fromJson(json));
    state.subscriptions.query = '不存在的关键字';
    await pump(tester, state);

    expect(find.text(SSubs.filteredEmpty), findsOneWidget);
    expect(find.text(SC.clearFilter), findsOneWidget);
  });
}
