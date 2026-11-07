import 'package:flutter/material.dart';

import '../../model/op_info.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import 'common.dart';

/// 配置的「能不能用」状态条。**常驻、非 modal。**
///
/// 两条线必须分开，因为它们是两类东西：
///
/// | 情况 | 表现 | 为什么 |
/// | --- | --- | --- |
/// | 校验类问题（没配实例…） | **黄色**「已保存，但还不能同步」 | 文件真写下去了；「不完整」用户看得见，同步会被拦 |
/// | 外置规则文件读不出来 | **红色**「没有保存」+ 指名文件 | **没落盘**；放行会让 `plan`/`sync` 少一套规则地跑而配置文本看不出问题 |
///
/// 非 modal 是硬要求：用户要一边看错误一边改，弹窗挡在中间就没法改。
class ConfigStatusStrip extends StatelessWidget {
  const ConfigStatusStrip({
    super.key,
    required this.state,
    this.onFix,
    this.dense = false,
  });

  final AppState state;

  /// 「去修正」的去处（通常是配置页）。为空时不显示这个按钮。
  final VoidCallback? onFix;

  final bool dense;

  @override
  Widget build(BuildContext context) {
    final config = state.config;
    final children = <Widget>[];

    if (config.hardError != null) {
      children.add(_HardErrorStrip(
        message: config.hardError!,
        detail: config.hardErrorDetail,
        onDismiss: config.clearHardError,
      ));
    }

    if (config.hasSavedButUnusable) {
      children.add(_SavedInvalidStrip(
        issues: config.issues,
        onFix: onFix,
      ));
    } else if (config.valid == false && config.issues.isEmpty) {
      // valid:false 但没有 errors 明细：如实说「不能同步」，不编原因。
      children.add(_SavedInvalidStrip(issues: const <ConfigIssue>[], onFix: onFix));
    }

    if (children.isEmpty) return const SizedBox.shrink();
    // 横向内边距交给调用方：它会随「放进 ListView 还是放进 Column」而不同。
    return Padding(
      padding: EdgeInsets.only(bottom: dense ? Tokens.gap1 : Tokens.gap2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          for (final child in children)
            Padding(
              padding: const EdgeInsets.only(bottom: Tokens.gap2),
              child: child,
            ),
        ],
      ),
    );
  }
}

/// 黄色：保存成功但不可用。
class _SavedInvalidStrip extends StatefulWidget {
  const _SavedInvalidStrip({required this.issues, this.onFix});

  final List<ConfigIssue> issues;
  final VoidCallback? onFix;

  @override
  State<_SavedInvalidStrip> createState() => _SavedInvalidStripState();
}

class _SavedInvalidStripState extends State<_SavedInvalidStrip> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final first = widget.issues.isEmpty ? null : widget.issues.first;
    return Container(
      decoration: BoxDecoration(
        color: Tokens.warning.withValues(alpha: 0.08),
        border: Border.all(color: Tokens.warning.withValues(alpha: 0.45)),
        borderRadius: BorderRadius.circular(Tokens.radiusCard),
      ),
      padding: const EdgeInsets.all(Tokens.gap3),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              const Icon(Icons.info_outline, size: 16, color: Tokens.warning),
              const SizedBox(width: Tokens.gap2),
              Expanded(
                child: Text(
                  first == null
                      ? SSave.savedButUnusableTitle
                      : '${SSave.savedButUnusableTitle}：${first.message}',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w500,
                  ),
                ),
              ),
              if (widget.issues.isNotEmpty)
                TextButton(
                  onPressed: () => setState(() => _expanded = !_expanded),
                  child: Text(
                    _expanded ? SC.hideDetail : '${widget.issues.length} 处',
                  ),
                ),
              if (widget.onFix != null)
                TextButton(
                  onPressed: widget.onFix,
                  child: const Text(SSave.fixNow),
                ),
            ],
          ),
          Padding(
            padding: const EdgeInsets.only(left: Tokens.gap4, top: Tokens.gap1),
            child: Text(SSave.savedButUnusableBody, style: theme.textTheme.bodySmall),
          ),
          if (_expanded)
            Padding(
              padding: const EdgeInsets.only(left: Tokens.gap4, top: Tokens.gap2),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  for (final issue in widget.issues)
                    SelectableText(
                      issue.field.isEmpty
                          ? '· ${issue.message}'
                          : '· ${issue.field}：${issue.message}',
                      style: theme.textTheme.bodySmall,
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// 红色：硬错误，没落盘。
class _HardErrorStrip extends StatelessWidget {
  const _HardErrorStrip({
    required this.message,
    this.detail,
    required this.onDismiss,
  });

  final String message;
  final String? detail;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      decoration: BoxDecoration(
        color: Tokens.danger.withValues(alpha: 0.07),
        border: Border.all(color: Tokens.danger.withValues(alpha: 0.5)),
        borderRadius: BorderRadius.circular(Tokens.radiusCard),
      ),
      padding: const EdgeInsets.all(Tokens.gap3),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              const Icon(Icons.error_outline, size: 16, color: Tokens.danger),
              const SizedBox(width: Tokens.gap2),
              Expanded(
                child: Text(
                  SSave.hardErrorTitle,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    fontWeight: FontWeight.w500,
                  ),
                ),
              ),
              TextButton(
                onPressed: onDismiss,
                child: const Text(SSave.dismiss),
              ),
            ],
          ),
          Padding(
            padding: const EdgeInsets.only(left: Tokens.gap4, top: Tokens.gap1),
            child: Text(SSave.hardErrorBody, style: theme.textTheme.bodySmall),
          ),
          Padding(
            padding: const EdgeInsets.only(left: Tokens.gap4, top: Tokens.gap2),
            child: SelectableText(
              detail == null || detail!.isEmpty
                  ? message
                  : SSave.hardErrorFile.replaceFirst('%s', detail!),
              style: theme.textTheme.bodySmall?.copyWith(color: Tokens.danger),
            ),
          ),
          Padding(
            padding: const EdgeInsets.only(left: Tokens.gap4, top: Tokens.gap1),
            child: SelectableText(message, style: theme.textTheme.bodySmall),
          ),
        ],
      ),
    );
  }
}

/// 客户端诊断块：**设计外状态**的计数器。
///
/// 契约 §1.1 要求这条路径上放计数器：它按设计不可达，所以一旦非零就是
/// 「设计外的某件事发生了」的唯一证据。`log.write` 是流水（会写进引擎环形缓冲，
/// 断线兜底时看得到），计数器是聚合视图（缓冲会被冲掉，它不会）——**两者都要**。
class ClientDiagnosticsCard extends StatelessWidget {
  const ClientDiagnosticsCard({super.key, required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final backend = state.backend;
    final failures = backend?.eventReadFailures ?? 0;
    final dropped = backend?.eventLogWriteDropped ?? 0;
    final read = backend?.eventReadCount ?? 0;
    final released = backend?.eventReleasedCount ?? 0;

    return SectionCard(
      title: SSave.diagnostics,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          // 这一对是「没有泄漏」唯一能被观测到的证据：读成功就必须释放恰好一次
          // （契约 §1.1 的所有权转移）。两个数字不等 = 泄漏或重复释放。
          KeyValueRow(
            label: SSave.eventFrames,
            value: '$read / $released',
            mono: true,
          ),
          if (read != released)
            Padding(
              padding: const EdgeInsets.only(bottom: Tokens.gap2),
              child: Text(
                SSave.eventFrameMismatch,
                style: theme.textTheme.bodySmall?.copyWith(color: Tokens.danger),
              ),
            ),
          KeyValueRow(
            label: SSave.eventReadFailures,
            value: '$failures',
            mono: true,
          ),
          if (failures > 0)
            Padding(
              padding: const EdgeInsets.only(bottom: Tokens.gap2),
              child: Text(
                SSave.designExternal,
                style: theme.textTheme.bodySmall?.copyWith(color: Tokens.warning),
              ),
            ),
          KeyValueRow(
            label: SSave.eventDropped,
            value: '$dropped',
            mono: true,
          ),
          if (dropped > 0)
            Text(
              SSave.backpressure,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
        ],
      ),
    );
  }
}
