import 'package:flutter/material.dart';

import '../../model/run_entry.dart';
import '../strings.dart';
import '../tokens.dart';
import 'common.dart';

/// 一轮同步的结果行：可展开看逐条失败原因。概览页与运行日志页共用。
class RunEntryRow extends StatefulWidget {
  const RunEntryRow({super.key, required this.entry, this.showMode = true});

  final RunEntry entry;
  final bool showMode;

  @override
  State<RunEntryRow> createState() => _RunEntryRowState();
}

class _RunEntryRowState extends State<RunEntryRow> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final entry = widget.entry;
    final hasDetail = entry.errorsOrEmpty.isNotEmpty ||
        entry.error.isNotEmpty ||
        entry.panic.isNotEmpty;
    final color = entry.busy
        ? Tokens.warning
        : (entry.isFailure ? Tokens.danger : Tokens.success);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        InkWell(
          onTap: hasDetail ? () => setState(() => _expanded = !_expanded) : null,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: Tokens.gap2),
            child: Row(
              children: <Widget>[
                StatusDot(color: color, tooltip: entry.busy ? SJournal.busy : null),
                const SizedBox(width: Tokens.gap2),
                SizedBox(
                  width: 152,
                  child: CodeText(
                    _stamp(entry.at),
                    style: theme.textTheme.bodySmall,
                  ),
                ),
                if (widget.showMode)
                  SizedBox(
                    width: 56,
                    child: Text(
                      entry.mode.isEmpty ? S.unspecified : entry.mode,
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                SizedBox(
                  width: 132,
                  child: NumText(
                    '${entry.writable} / ${entry.updated} / ${entry.added} / ${entry.skipped}',
                    style: theme.textTheme.bodySmall,
                  ),
                ),
                const SizedBox(width: Tokens.gap3),
                SizedBox(
                  width: 72,
                  child: NumText(
                    '${entry.ms} ms',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
                const Spacer(),
                if (entry.busy)
                  Padding(
                    padding: const EdgeInsets.only(right: Tokens.gap2),
                    child: Text(
                      SJournal.busy,
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: Tokens.warning),
                    ),
                  )
                else if (entry.isFailure)
                  Padding(
                    padding: const EdgeInsets.only(right: Tokens.gap2),
                    child: Text(
                      SC.loadingFailed,
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: Tokens.danger),
                    ),
                  ),
                if (entry.note.isNotEmpty)
                  Flexible(
                    child: Text(
                      entry.note,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                if (hasDetail)
                  Icon(
                    _expanded ? Icons.expand_less : Icons.expand_more,
                    size: 16,
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
              ],
            ),
          ),
        ),
        if (_expanded && hasDetail)
          Container(
            width: double.infinity,
            margin: const EdgeInsets.only(
              left: Tokens.gap4,
              bottom: Tokens.gap3,
            ),
            padding: const EdgeInsets.all(Tokens.gap3),
            decoration: BoxDecoration(
              color: theme.colorScheme.surfaceContainerHighest,
              borderRadius: BorderRadius.circular(Tokens.radiusInput),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                if (entry.error.isNotEmpty)
                  SelectableText(
                    '整轮失败：${entry.error}',
                    style: theme.textTheme.bodySmall,
                  ),
                if (entry.panic.isNotEmpty)
                  SelectableText(
                    '被兜住的崩溃：${entry.panic}',
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: Tokens.danger),
                  ),
                for (final line in entry.errorsOrEmpty)
                  SelectableText('· $line', style: theme.textTheme.bodySmall),
                if (entry.nextAt != null) ...<Widget>[
                  const SizedBox(height: Tokens.gap2),
                  CodeText(
                    '下次运行：${_stamp(entry.nextAt)}',
                    style: theme.textTheme.bodySmall,
                  ),
                ],
                if (entry.attempt > 0) ...<Widget>[
                  const SizedBox(height: Tokens.gap1),
                  CodeText(
                    '第 ${entry.attempt} 次重试',
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ],
            ),
          ),
      ],
    );
  }

  static String _stamp(DateTime? at) {
    if (at == null) return S.unspecified;
    final local = at.toLocal();
    final iso = local.toIso8601String();
    return iso.length >= 19 ? iso.substring(0, 19) : iso;
  }
}
