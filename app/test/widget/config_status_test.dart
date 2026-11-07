import 'dart:io';

import 'package:anisync_app/src/model/op_info.dart';
import 'package:anisync_app/src/model/status.dart';
import 'package:anisync_app/src/state/app_state.dart';
import 'package:anisync_app/src/state/async_state.dart';
import 'package:anisync_app/src/state/prefs.dart';
import 'package:anisync_app/src/ui/pages/overview_page.dart';
import 'package:anisync_app/src/ui/strings.dart';
import 'package:anisync_app/src/ui/theme.dart';
import 'package:anisync_app/src/ui/widgets/config_strip.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import '../support/fake_backend.dart';

/// `config.save` 的两种失败在界面上的样子（§7.2）。
///
/// 三条硬要求：
/// 1. `valid:false` 时**保存仍然成功**，界面说的是「已保存」而不是「失败」；
/// 2. errors 显著、**非 modal**（不能用弹窗挡住用户改配置）；
/// 3. 概览页也看得见 —— 不能让用户点了「立即同步」才发现。
void main() {
  AppState buildState() {
    final prefs = PrefsStore(
      directory: Directory.systemTemp.createTempSync('anisync-strip').path,
    );
    return AppState(prefs: prefs);
  }

  Future<void> pumpOverview(WidgetTester tester, AppState state) async {
    tester.view.physicalSize = const Size(1280, 900);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
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

  testWidgets('valid:false：说「已保存」，列出 errors，且不弹窗', (tester) async {
    final state = buildState();
    // 状态还在加载 —— 状态条也必须看得见（它挂在 AsyncView 外面）。
    state.status.status = const AsyncLoading<StatusResult>();
    state.config.valid = false;
    state.config.issues = <ConfigIssue>[
      ConfigIssue(field: 'instances', message: '还没有配置任何 ANI-RSS 实例'),
      ConfigIssue(field: 'targets[1]', message: '既没有 title 也没有 bgmId'),
    ];

    await pumpOverview(tester, state);

    expect(find.byType(ConfigStatusStrip), findsOneWidget);
    expect(
      find.textContaining(SSave.savedButUnusableTitle),
      findsOneWidget,
      reason: '必须是「已保存，但还不能同步」，不是「保存失败」',
    );
    expect(find.textContaining('还没有配置任何 ANI-RSS 实例'), findsOneWidget);
    // **非 modal**：不许有对话框挡着。
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.byType(Dialog), findsNothing);
  });

  testWidgets('展开后能逐条看到 errors 与字段名', (tester) async {
    final state = buildState();
    state.config.valid = false;
    state.config.issues = <ConfigIssue>[
      ConfigIssue(field: 'instances', message: '还没有配置任何 ANI-RSS 实例'),
      ConfigIssue(field: 'targets[1]', message: '既没有 title 也没有 bgmId'),
    ];
    await pumpOverview(tester, state);

    await tester.tap(find.textContaining('2 处'));
    await tester.pumpAndSettle();

    expect(find.textContaining('targets[1]'), findsWidgets);
    expect(find.textContaining('既没有 title 也没有 bgmId'), findsWidgets);
  });

  testWidgets('硬错误：红色、说「没有保存」、指名文件', (tester) async {
    final state = buildState();
    state.config.hardError = '读取规则文件失败（primaryFiles 里的 "raws.yaml"）: 系统找不到指定的文件';
    state.config.hardErrorDetail = 'raws.yaml';

    await pumpOverview(tester, state);

    expect(find.text(SSave.hardErrorTitle), findsOneWidget);
    expect(find.textContaining('raws.yaml'), findsWidgets);
    expect(find.byType(AlertDialog), findsNothing);
  });

  testWidgets('valid:true 且无硬错误 → 不显示任何状态条（不刷屏）', (tester) async {
    final state = buildState();
    state.config.valid = true;
    state.config.issues = <ConfigIssue>[];
    await pumpOverview(tester, state);

    expect(find.byType(ConfigStatusStrip), findsOneWidget);
    expect(find.textContaining(SSave.savedButUnusableTitle), findsNothing);
    expect(find.text(SSave.hardErrorTitle), findsNothing);
  });

  testWidgets('客户端诊断：设计外状态的计数器非零时给 warning 说明', (tester) async {
    final state = buildState();
    final backend = FakeBackend()
      ..eventReadFailures = 3
      ..eventLogWriteDropped = 7
      ..eventReadCount = 128
      ..eventReleasedCount = 128;
    await state.attach(backend, open: false);

    await tester.pumpWidget(
      MaterialApp(
        theme: buildAppTheme(Brightness.light),
        home: Scaffold(body: ClientDiagnosticsCard(state: state)),
      ),
    );
    await tester.pump();

    expect(find.text('3'), findsOneWidget);
    expect(find.text('7'), findsOneWidget);
    // 读 / 释放成对出现：相等时不给告警。
    expect(find.text('128 / 128'), findsOneWidget);
    expect(find.text(SSave.eventFrameMismatch), findsNothing);
    expect(find.text(SSave.designExternal), findsOneWidget);
    expect(find.text(SSave.backpressure), findsOneWidget);
  });

  testWidgets('客户端诊断：读/释放不相等时要显式告警（泄漏或重复释放）', (tester) async {
    final state = buildState();
    final backend = FakeBackend()
      ..eventReadCount = 128
      ..eventReleasedCount = 127;
    await state.attach(backend, open: false);

    await tester.pumpWidget(
      MaterialApp(
        theme: buildAppTheme(Brightness.light),
        home: Scaffold(body: ClientDiagnosticsCard(state: state)),
      ),
    );
    await tester.pump();

    expect(find.text('128 / 127'), findsOneWidget);
    expect(find.text(SSave.eventFrameMismatch), findsOneWidget);
  });
}
