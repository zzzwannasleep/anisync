import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/errors.dart';
import '../../state/async_state.dart';
import '../strings.dart';
import '../tokens.dart';

/// 数字文本：集数、偏移、条数、耗时、端口全部走它（等宽，右对齐交给调用方）。
class NumText extends StatelessWidget {
  const NumText(this.text, {super.key, this.style, this.color});

  final String text;
  final TextStyle? style;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final base = style ?? DefaultTextStyle.of(context).style;
    return Text(
      text,
      style: base.copyWith(
        fontFeatures: Tokens.tabular,
        color: color ?? base.color,
      ),
      textAlign: TextAlign.right,
    );
  }
}

/// ID / 版本 / 时间戳：等宽 + 斜杠零。
class CodeText extends StatelessWidget {
  const CodeText(this.text, {super.key, this.style, this.maxLines = 1});

  final String text;
  final TextStyle? style;
  final int maxLines;

  @override
  Widget build(BuildContext context) {
    final base = style ?? DefaultTextStyle.of(context).style;
    return Text(
      text,
      maxLines: maxLines,
      overflow: TextOverflow.ellipsis,
      style: base.copyWith(fontFeatures: Tokens.codeFigures),
    );
  }
}

/// 状态点。功能性颜色只用在点上，底色保持中性。
class StatusDot extends StatelessWidget {
  const StatusDot({super.key, required this.color, this.size = 8, this.tooltip});

  final Color color;
  final double size;
  final String? tooltip;

  @override
  Widget build(BuildContext context) {
    final dot = Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: color,
        shape: BoxShape.circle,
      ),
    );
    if (tooltip == null) return dot;
    return Tooltip(message: tooltip!, child: dot);
  }
}

/// 分节卡片：1px 描边优先于阴影，8 圆角。
class SectionCard extends StatelessWidget {
  const SectionCard({
    super.key,
    this.title,
    this.trailing,
    this.subtitle,
    required this.child,
    this.padding = const EdgeInsets.all(Tokens.gap4),
  });

  final String? title;
  final String? subtitle;
  final Widget? trailing;
  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        borderRadius: BorderRadius.circular(Tokens.radiusCard),
        border: Border.all(color: theme.colorScheme.outline),
      ),
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          if (title != null)
            Row(
              children: <Widget>[
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(title!, style: theme.textTheme.titleMedium),
                      if (subtitle != null)
                        Padding(
                          padding: const EdgeInsets.only(top: Tokens.gap1),
                          child: Text(
                            subtitle!,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
                if (title != null) ?trailing,
              ],
            ),
          if (title != null) const SizedBox(height: Tokens.gap3),
          // ListTile（SwitchListTile / CheckboxListTile）必须能画到最近的 Material 上，
          // 否则 Flutter 会断言「背景与 ink 可能不可见」。
          Material(
            type: MaterialType.transparency,
            child: child,
          ),
        ],
      ),
    );
  }
}

/// 表头单元格。
class Th extends StatelessWidget {
  const Th(this.text, {super.key, this.align = TextAlign.left, this.flex = 1});

  final String text;
  final TextAlign align;
  final int flex;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Expanded(
      flex: flex,
      child: Text(
        text,
        textAlign: align,
        style: theme.textTheme.bodySmall?.copyWith(
          fontWeight: FontWeight.w600,
          color: theme.colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}

/// 表格行：hover 底色 80ms，选中态 6% 主色底 + 2px 主色左边。
class DataRow extends StatefulWidget {
  const DataRow({
    super.key,
    required this.cells,
    this.onTap,
    this.selected = false,
    this.accentColor,
  });

  final List<Widget> cells;
  final VoidCallback? onTap;
  final bool selected;
  final Color? accentColor;

  @override
  State<DataRow> createState() => _DataRowState();
}

class _DataRowState extends State<DataRow> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final dark = theme.brightness == Brightness.dark;
    final accent = widget.accentColor ?? theme.colorScheme.primary;
    final background = widget.selected
        ? (dark ? Tokens.rowSelectedDark : Tokens.rowSelectedLight)
        : (_hovered ? (dark ? Tokens.rowHoverDark : Tokens.rowHoverLight) : null);
    return MouseRegion(
      cursor: widget.onTap == null ? MouseCursor.defer : SystemMouseCursors.click,
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: GestureDetector(
        onTap: widget.onTap,
        child: AnimatedContainer(
          duration: Tokens.hoverDuration,
          decoration: BoxDecoration(
            color: background,
            border: Border(
              left: BorderSide(
                color: widget.selected ? accent : Colors.transparent,
                width: 2,
              ),
              bottom: BorderSide(color: theme.colorScheme.outlineVariant),
            ),
          ),
          padding: const EdgeInsets.symmetric(
            horizontal: Tokens.cellX,
            vertical: Tokens.cellY,
          ),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.center,
            children: widget.cells,
          ),
        ),
      ),
    );
  }
}

/// 空态：**为什么是空的** + **下一步做什么** + 低调图标。
class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.why,
    this.icon = Icons.inbox_outlined,
    this.actionLabel,
    this.onAction,
    this.secondary,
  });

  final String why;
  final IconData icon;
  final String? actionLabel;
  final VoidCallback? onAction;
  final String? secondary;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(Tokens.gap6),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(icon, size: 28, color: theme.colorScheme.onSurfaceVariant),
            const SizedBox(height: Tokens.gap3),
            Text(
              why,
              textAlign: TextAlign.center,
              style: theme.textTheme.bodyMedium,
            ),
            if (secondary != null) ...<Widget>[
              const SizedBox(height: Tokens.gap2),
              Text(
                secondary!,
                textAlign: TextAlign.center,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
            if (actionLabel != null && onAction != null) ...<Widget>[
              const SizedBox(height: Tokens.gap4),
              FilledButton(onPressed: onAction, child: Text(actionLabel!)),
            ],
          ],
        ),
      ),
    );
  }
}

/// 错误面板：具体原因 + 错误码 + 一键重试 + 复制原文。
class ErrorPane extends StatelessWidget {
  const ErrorPane({
    super.key,
    required this.error,
    this.onRetry,
    this.compact = false,
  });

  final CoreError error;
  final VoidCallback? onRetry;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final border = Border.all(color: Tokens.danger.withValues(alpha: 0.5));
    final content = Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        const Icon(Icons.error_outline, size: 18, color: Tokens.danger),
        const SizedBox(width: Tokens.gap2),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                // 版本偏斜有它自己的说法：不是「出错了」，是「这个内核不认识这个操作」。
                error.versionSkew ? SErr.skewTitle : error.message,
                style: theme.textTheme.bodyMedium?.copyWith(
                  fontWeight: FontWeight.w500,
                ),
              ),
              const SizedBox(height: Tokens.gap1),
              Row(
                children: <Widget>[
                  CodeText(
                    '${error.code} · ${SErr.describe(error)}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ],
              ),
              if (error.versionSkew) ...<Widget>[
                const SizedBox(height: Tokens.gap2),
                Text(SErr.skewWhy, style: theme.textTheme.bodySmall),
                if (error.detail != null && error.detail!.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(top: Tokens.gap1),
                    child: CodeText(
                      SErr.skewOp.replaceFirst('%s', error.detail!),
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                Padding(
                  padding: const EdgeInsets.only(top: Tokens.gap1),
                  child: Text(
                    SErr.skewWhere,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
              ],
              // 资源不存在时把名字摆出来：刷新前先看看是哪一个。
              if (!error.versionSkew &&
                  error.code == ErrorCodes.notFound &&
                  error.detail != null &&
                  error.detail!.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: Tokens.gap1),
                  child: CodeText(
                    SErr.resourceName.replaceFirst('%s', error.detail!),
                    style: theme.textTheme.bodySmall,
                  ),
                ),
              if (!compact && error.detail != null && error.detail!.isNotEmpty)
                Padding(
                  padding: const EdgeInsets.only(top: Tokens.gap2),
                  child: Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(Tokens.gap2),
                    decoration: BoxDecoration(
                      color: theme.colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(Tokens.radiusInput),
                    ),
                    child: SelectableText(
                      error.detail!,
                      maxLines: 8,
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                ),
            ],
          ),
        ),
        // 版本偏斜时**不给重试**：刷新一百次也不会让旧内核认识一个新 op。
        if (onRetry != null && !error.versionSkew) ...<Widget>[
          const SizedBox(width: Tokens.gap2),
          OutlinedButton(onPressed: onRetry, child: const Text(SC.retry)),
        ],
        const SizedBox(width: Tokens.gap1),
        IconButton(
          tooltip: SC.copyError,
          onPressed: () async {
            await Clipboard.setData(ClipboardData(text: error.fullText));
            if (context.mounted) {
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(content: Text(SC.copied)),
              );
            }
          },
          icon: const Icon(Icons.copy_all_outlined, size: 18),
        ),
      ],
    );

    if (compact) {
      return Container(
        decoration: BoxDecoration(
          color: Tokens.danger.withValues(alpha: 0.06),
          border: border,
          borderRadius: BorderRadius.circular(Tokens.radiusCard),
        ),
        padding: const EdgeInsets.symmetric(
          horizontal: Tokens.gap3,
          vertical: Tokens.gap2,
        ),
        child: content,
      );
    }
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 720),
        child: Container(
          margin: const EdgeInsets.all(Tokens.gap4),
          decoration: BoxDecoration(
            color: theme.colorScheme.surface,
            border: border,
            borderRadius: BorderRadius.circular(Tokens.radiusCard),
          ),
          padding: const EdgeInsets.all(Tokens.gap4),
          child: content,
        ),
      ),
    );
  }
}

/// 骨架行：**匹配最终布局**（同列结构），不是居中转圈。
class SkeletonRows extends StatelessWidget {
  const SkeletonRows({super.key, this.rows = 8, this.height = 34});

  final int rows;
  final double height;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = theme.colorScheme.surfaceContainerHighest;
    return Column(
      children: List<Widget>.generate(
        rows,
        (index) => Container(
          height: height,
          margin: const EdgeInsets.only(bottom: Tokens.gap1),
          decoration: BoxDecoration(
            color: color,
            borderRadius: BorderRadius.circular(Tokens.radiusInput),
          ),
        ),
      ),
    );
  }
}

/// 统一的状态渲染器：判别联合 → 界面。**不出现「加载中 + 无数据 + 无错误」这种组合**。
class AsyncView<T> extends StatelessWidget {
  const AsyncView({
    super.key,
    required this.state,
    required this.builder,
    this.onRetry,
    this.emptyBuilder,
    this.skeleton,
    this.idleBuilder,
    this.emptyIcon = Icons.inbox_outlined,
  });

  final AsyncState<T> state;
  final Widget Function(BuildContext context, T value) builder;
  final VoidCallback? onRetry;
  final Widget Function(BuildContext context, String why)? emptyBuilder;
  final Widget? skeleton;
  final WidgetBuilder? idleBuilder;
  final IconData emptyIcon;

  @override
  Widget build(BuildContext context) {
    switch (state) {
      case AsyncIdle<T>():
        return idleBuilder?.call(context) ?? skeleton ?? const SkeletonRows();
      case AsyncLoading<T>(previous: final previous):
        if (previous != null) {
          return Column(
            children: <Widget>[
              const LinearProgressIndicator(),
              Expanded(child: builder(context, previous)),
            ],
          );
        }
        return skeleton ?? const SkeletonRows();
      case AsyncData<T>(value: final value):
        return builder(context, value);
      case AsyncEmpty<T>(why: final why):
        return emptyBuilder?.call(context, why) ??
            EmptyState(why: why.isEmpty ? SC.emptyResult : why, icon: emptyIcon);
      case AsyncPartial<T>(value: final value, degradations: final degradations):
        return Column(
          children: <Widget>[
            for (final error in degradations)
              Padding(
                padding: const EdgeInsets.only(bottom: Tokens.gap2),
                child: ErrorPane(error: error, compact: true, onRetry: onRetry),
              ),
            Expanded(child: builder(context, value)),
          ],
        );
      case AsyncFailed<T>(error: final error, previous: final previous):
        if (error.isCanceled) {
          return previous == null
              ? const SizedBox.shrink()
              : builder(context, previous);
        }
        if (previous != null) {
          return Column(
            children: <Widget>[
              Padding(
                padding: const EdgeInsets.all(Tokens.gap2),
                child: ErrorPane(error: error, compact: true, onRetry: onRetry),
              ),
              Expanded(child: builder(context, previous)),
            ],
          );
        }
        return ErrorPane(error: error, onRetry: onRetry);
    }
  }
}

/// 危险操作的二次确认。**文案必须写清影响面**，由调用方拼好传进来。
class ConfirmDialog extends StatefulWidget {
  const ConfirmDialog({
    super.key,
    required this.title,
    required this.message,
    this.confirmLabel = SC.confirm,
    this.danger = true,
    this.extra,
  });

  final String title;
  final String message;
  final String confirmLabel;
  final bool danger;
  final Widget? extra;

  /// 弹一次确认，返回是否点了确认。
  static Future<bool> show(
    BuildContext context, {
    required String title,
    required String message,
    String confirmLabel = SC.confirm,
    bool danger = true,
    Widget? extra,
  }) async {
    final result = await showDialog<bool>(
      context: context,
      builder: (context) => ConfirmDialog(
        title: title,
        message: message,
        confirmLabel: confirmLabel,
        danger: danger,
        extra: extra,
      ),
    );
    return result ?? false;
  }

  @override
  State<ConfirmDialog> createState() => _ConfirmDialogState();
}

class _ConfirmDialogState extends State<ConfirmDialog> {
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: Row(
        children: <Widget>[
          if (widget.danger)
            const Padding(
              padding: EdgeInsets.only(right: Tokens.gap2),
              child: Icon(Icons.warning_amber_rounded, color: Tokens.danger),
            ),
          Expanded(child: Text(widget.title)),
        ],
      ),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(widget.message, style: theme.textTheme.bodyMedium),
            if (widget.danger) ...<Widget>[
              const SizedBox(height: Tokens.gap3),
              Text(
                SConfirm.irreversible,
                style: theme.textTheme.bodySmall?.copyWith(color: Tokens.danger),
              ),
            ],
            if (widget.extra != null) ...<Widget>[
              const SizedBox(height: Tokens.gap3),
              widget.extra!,
            ],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text(SC.cancel),
        ),
        FilledButton(
          style: widget.danger
              ? FilledButton.styleFrom(backgroundColor: Tokens.danger)
              : null,
          onPressed: () => Navigator.of(context).pop(true),
          child: Text(widget.confirmLabel),
        ),
      ],
    );
  }
}

/// 三态**数字**字段：未设置 / 覆盖。
///
/// 三态是这一页的核心：`null` = 不碰订阅里已有的值，`0` = 覆盖成 0。两者必须视觉可区分。
class TriStateIntField extends StatefulWidget {
  const TriStateIntField({
    super.key,
    required this.label,
    required this.value,
    required this.onChanged,
    this.hint,
    this.suffix,
  });

  final String label;
  final int? value;
  final ValueChanged<int?> onChanged;
  final String? hint;
  final String? suffix;

  @override
  State<TriStateIntField> createState() => _TriStateIntFieldState();
}

class _TriStateIntFieldState extends State<TriStateIntField> {
  late final TextEditingController _controller = TextEditingController(
    text: widget.value?.toString() ?? '',
  );

  @override
  void didUpdateWidget(covariant TriStateIntField oldWidget) {
    super.didUpdateWidget(oldWidget);
    final next = widget.value?.toString() ?? '';
    if (next != _controller.text && !_isEditing) {
      _controller.text = next;
    }
  }

  bool _isEditing = false;

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  void _submit(String raw) {
    final text = raw.trim();
    if (text.isEmpty) {
      widget.onChanged(null);
      return;
    }
    final parsed = int.tryParse(text);
    if (parsed != null) widget.onChanged(parsed);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isSet = widget.value != null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            SizedBox(
              width: 130,
              child: Text(widget.label, style: theme.textTheme.bodyMedium),
            ),
            SizedBox(
              width: 120,
              child: TextField(
                controller: _controller,
                enabled: isSet,
                onTap: () => _isEditing = true,
                onTapOutside: (_) => _isEditing = false,
                onChanged: _submit,
                keyboardType: const TextInputType.numberWithOptions(signed: true),
                inputFormatters: <TextInputFormatter>[
                  FilteringTextInputFormatter.allow(RegExp(r'^-?\d*')),
                ],
                decoration: InputDecoration(
                  isDense: true,
                  suffixText: widget.suffix,
                  hintText: STri.unset,
                ),
              ),
            ),
            const SizedBox(width: Tokens.gap2),
            // Flexible：窄容器下这句话会被压窄，而不是把整行撑爆。
            Flexible(
              child: isSet
                  ? OutlinedButton(
                      onPressed: () {
                        _controller.text = '';
                        widget.onChanged(null);
                      },
                      child: const Text(STri.backToUnset),
                    )
                  : Text(
                      STri.unsetNumber,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
            ),
          ],
        ),
        if (widget.hint != null)
          Padding(
            padding: const EdgeInsets.only(top: Tokens.gap1, left: 130),
            child: Text(
              widget.hint!,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
      ],
    );
  }
}

/// 三态**布尔**字段：未设置 / 开 / 关。三段选择器，视觉上三档清晰。
class TriStateBoolField extends StatelessWidget {
  const TriStateBoolField({
    super.key,
    required this.label,
    required this.value,
    required this.onChanged,
    this.hint,
    this.unsetLabel = STri.enableUnset,
  });

  final String label;
  final bool? value;
  final ValueChanged<bool?> onChanged;
  final String? hint;
  final String unsetLabel;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            SizedBox(
              width: 130,
              child: Text(label, style: theme.textTheme.bodyMedium),
            ),
            SegmentedButton<String>(
              showSelectedIcon: false,
              style: ButtonStyle(
                visualDensity: VisualDensity.compact,
                padding: const WidgetStatePropertyAll<EdgeInsets>(
                  EdgeInsets.symmetric(horizontal: Tokens.gap2),
                ),
                textStyle: WidgetStatePropertyAll<TextStyle>(
                  theme.textTheme.bodySmall ?? const TextStyle(),
                ),
              ),
              segments: const <ButtonSegment<String>>[
                ButtonSegment<String>(value: 'unset', label: Text(STri.unset)),
                ButtonSegment<String>(value: 'on', label: Text('开')),
                ButtonSegment<String>(value: 'off', label: Text('关')),
              ],
              selected: <String>{
                if (value == null) 'unset' else if (value!) 'on' else 'off',
              },
              onSelectionChanged: (selection) {
                final single = selection.first;
                onChanged(
                  single == 'unset' ? null : single == 'on',
                );
              },
            ),
          ],
        ),
        Padding(
          padding: const EdgeInsets.only(top: Tokens.gap1, left: 130),
          child: Text(
            hint ?? unsetLabel,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ),
      ],
    );
  }
}

/// 表单一行：左标签 + 右控件。
class FieldRow extends StatelessWidget {
  const FieldRow({
    super.key,
    required this.label,
    required this.child,
    this.hint,
    this.labelWidth = 130,
  });

  final String label;
  final Widget child;
  final String? hint;
  final double labelWidth;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: Tokens.gap3),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              SizedBox(
                width: labelWidth,
                child: Padding(
                  padding: const EdgeInsets.only(top: Tokens.gap2),
                  child: Text(label, style: theme.textTheme.bodyMedium),
                ),
              ),
              Expanded(child: child),
            ],
          ),
          if (hint != null)
            Padding(
              padding: EdgeInsets.only(top: Tokens.gap1, left: labelWidth),
              child: Text(
                hint!,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// 键值对展示（设置页 / 概览页用）。
class KeyValueRow extends StatelessWidget {
  const KeyValueRow({
    super.key,
    required this.label,
    required this.value,
    this.mono = false,
    this.onCopy,
  });

  final String label;
  final String value;
  final bool mono;
  final VoidCallback? onCopy;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: Tokens.gap2),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          SizedBox(
            width: 130,
            child: Text(
              label,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ),
          Expanded(
            child: mono
                ? CodeText(value, style: theme.textTheme.bodySmall)
                : Text(value, style: theme.textTheme.bodySmall),
          ),
          if (onCopy != null)
            IconButton(
              tooltip: SC.copy,
              visualDensity: VisualDensity.compact,
              onPressed: onCopy,
              icon: const Icon(Icons.copy_outlined, size: 15),
            ),
        ],
      ),
    );
  }
}

/// 过滤 chip：描边样式，不用实心药丸。
class FilterChipRow<T> extends StatelessWidget {
  const FilterChipRow({
    super.key,
    required this.options,
    required this.selected,
    required this.onSelected,
    required this.labelOf,
  });

  final List<T> options;
  final List<T> selected;
  final ValueChanged<T> onSelected;
  final String Function(T value) labelOf;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: Tokens.gap2,
      children: <Widget>[
        for (final option in options)
          FilterChip(
            label: Text(labelOf(option)),
            selected: selected.contains(option),
            onSelected: (_) => onSelected(option),
            showCheckmark: false,
          ),
      ],
    );
  }
}

/// 分节标题（页面内的分组）。
class SectionHeader extends StatelessWidget {
  const SectionHeader(this.title, {super.key, this.trailing});

  final String title;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(
        top: Tokens.gap4,
        bottom: Tokens.gap2,
      ),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Text(title, style: theme.textTheme.titleMedium),
          ),
          ?trailing,
        ],
      ),
    );
  }
}

/// 横向分隔线（比 Divider 更轻）。
class HairLine extends StatelessWidget {
  const HairLine({super.key});

  @override
  Widget build(BuildContext context) {
    return Divider(height: 1, color: Theme.of(context).colorScheme.outlineVariant);
  }
}

/// 单元格：一行里的一块，可控制 flex 与对齐。
class Cell extends StatelessWidget {
  const Cell({
    super.key,
    required this.child,
    this.flex = 1,
    this.align = MainAxisAlignment.start,
  });

  final Widget child;
  final int flex;
  final MainAxisAlignment align;

  @override
  Widget build(BuildContext context) {
    return Expanded(
      flex: flex,
      child: Row(
        mainAxisAlignment: align,
        children: <Widget>[Flexible(child: child)],
      ),
    );
  }
}

/// 宽表在高密度界面里的常规做法：容器不够宽就**横向滚**，而不是让 RenderFlex 溢出。
///
/// 列多、每列都有固定宽度的表格用这个；窄屏（手机）下才看得出价值。
class HScroll extends StatelessWidget {
  const HScroll({super.key, required this.minWidth, required this.child});

  final double minWidth;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: SizedBox(
          width: constraints.maxWidth < minWidth
              ? minWidth
              : constraints.maxWidth,
          child: child,
        ),
      ),
    );
  }
}

/// 用来给文本加等宽数字特征的样式助手。
TextStyle numeric(TextStyle style) => style.copyWith(
      fontFeatures: <ui.FontFeature>[...?style.fontFeatures, ...Tokens.tabular],
    );
