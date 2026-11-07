import 'package:flutter/material.dart';

import '../../model/run_entry.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';
import '../widgets/run_row.dart';

/// 运行日志：journal 列表 + 「只看失败」+ 清空记录 + 事件流兜底。
class JournalPage extends StatelessWidget {
  const JournalPage({super.key, required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final journal = state.journal;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(
            Tokens.gap4,
            Tokens.gap4,
            Tokens.gap4,
            Tokens.gap2,
          ),
          child: Wrap(
            spacing: Tokens.gap2,
            runSpacing: Tokens.gap2,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 520),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(SJournal.title, style: theme.textTheme.titleLarge),
                    const SizedBox(height: Tokens.gap1),
                    CodeText(
                      state.journal.journal.valueOrNull?.path.isEmpty ?? true
                          ? S.unspecified
                          : state.journal.journal.valueOrNull!.path,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              FilterChip(
                label: const Text(SJournal.onlyFailures),
                selected: journal.onlyFailures,
                showCheckmark: false,
                onSelected: (value) {
                  journal.onlyFailures = value;
                  state.touch();
                },
              ),
              OutlinedButton.icon(
                onPressed: () => journal.load(),
                icon: const Icon(Icons.refresh, size: 16),
                label: const Text(SC.refresh),
              ),
              OutlinedButton.icon(
                onPressed: () => journal.loadLogs(),
                icon: const Icon(Icons.bolt_outlined, size: 16),
                label: const Text(SJournal.useFallback),
              ),
              OutlinedButton.icon(
                onPressed: () async {
                  final count = journal.journal.valueOrNull?.entriesOrEmpty.length ?? 0;
                  final path = journal.journal.valueOrNull?.path ?? '';
                  final ok = await ConfirmDialog.show(
                    context,
                    title: SJournal.clear,
                    message: SJournal.clearConfirm
                        .replaceFirst('%d', '$count')
                        .replaceFirst('%s', path.isEmpty ? S.unspecified : path),
                    confirmLabel: SJournal.clear,
                  );
                  if (!ok || !context.mounted) return;
                  final result = await journal.clear(confirm: true);
                  if (!context.mounted || result == null) return;
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(
                      content: Text(
                        SJournal.cleared.replaceFirst('%d', '${result.cleared}'),
                      ),
                    ),
                  );
                },
                icon: const Icon(Icons.delete_sweep_outlined, size: 16),
                label: const Text(SJournal.clear),
              ),
            ],
          ),
        ),
        const HairLine(),
        Expanded(
          child: Padding(
            padding: const EdgeInsets.all(Tokens.gap4),
            child: AsyncView<JournalTailResult>(
              state: journal.journal,
              onRetry: () => journal.load(),
              skeleton: const Padding(
                padding: EdgeInsets.all(Tokens.gap2),
                child: SkeletonRows(rows: 10),
              ),
              emptyBuilder: (context, why) => EmptyState(
                why: SJournal.empty,
                icon: Icons.receipt_long_outlined,
                secondary: SJournal.emptyNext,
              ),
              builder: (context, _) {
                final entries = journal.entries;
                if (entries.isEmpty) {
                  return EmptyState(
                    why: SJournal.filteredEmpty,
                    icon: Icons.check_circle_outline,
                    actionLabel: SC.clearFilter,
                    onAction: () {
                      journal.onlyFailures = false;
                      state.touch();
                    },
                  );
                }
                return SectionCard(
                  title: '${entries.length} 轮',
                  padding: EdgeInsets.zero,
                  child: SingleChildScrollView(
                    child: Column(
                      children: <Widget>[
                        Padding(
                          padding: const EdgeInsets.symmetric(
                            horizontal: Tokens.cellX,
                            vertical: Tokens.cellY,
                          ),
                          child: Row(
                            children: const <Widget>[
                              SizedBox(width: 16),
                              SizedBox(width: Tokens.gap2),
                              SizedBox(
                                width: 152,
                                child: Text(SJournal.colWhen),
                              ),
                              SizedBox(width: 56, child: Text(SJournal.colMode)),
                              SizedBox(
                                width: 132,
                                child: Text(SJournal.colCounts),
                              ),
                              SizedBox(width: Tokens.gap3),
                              SizedBox(width: 72, child: Text(SJournal.colMs)),
                              Spacer(),
                              Text(SJournal.colResult),
                            ],
                          ),
                        ),
                        const HairLine(),
                        for (final entry in entries) RunEntryRow(entry: entry),
                      ],
                    ),
                  ),
                );
              },
            ),
          ),
        ),
      ],
    );
  }
}
