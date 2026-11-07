import 'package:flutter/material.dart' hide DataRow;

import '../../model/config.dart';
import '../../model/season_item.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';

/// 「添加番剧」：搜 Bangumi → 选条目 → **选去向**。
///
/// 两条路的后果完全不同，必须让用户在选择的那一刻就看清楚：
/// - 加进 `targets`：只改了配置，实例里要等下一次同步才会出现；
/// - 直接加进实例：立刻写进实例，不经过 targets。
class BgmSearchDialog extends StatefulWidget {
  const BgmSearchDialog({super.key, required this.state});

  final AppState state;

  /// 结果回报：返回给调用方展示「加到哪里了」。
  static Future<String?> show(BuildContext context, AppState state) =>
      showDialog<String>(
        context: context,
        builder: (context) => BgmSearchDialog(state: state),
      );

  @override
  State<BgmSearchDialog> createState() => _BgmSearchDialogState();
}

enum _Destination { targets, instance, both }

class _BgmSearchDialogState extends State<BgmSearchDialog> {
  final TextEditingController _keyword = TextEditingController();
  BgmSubject? _picked;
  _Destination _destination = _Destination.targets;
  String? _instance;
  bool _busy = false;
  String? _result;

  @override
  void dispose() {
    _keyword.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = widget.state;
    final instances = state.config.editable.instancesOrEmpty
        .map((e) => e.name)
        .toList(growable: false);
    if (_instance == null && instances.isNotEmpty) {
      _instance = instances.first;
    }

    return AlertDialog(
      title: const Text(SDiscover.addDialog),
      content: SizedBox(
        width: 760,
        height: 560,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              SDiscover.addDialogHint,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: Tokens.gap3),
            Row(
              children: <Widget>[
                Expanded(
                  child: TextField(
                    controller: _keyword,
                    autofocus: true,
                    decoration: const InputDecoration(
                      isDense: true,
                      hintText: SDiscover.searchHint,
                      prefixIcon: Icon(Icons.search, size: 16),
                    ),
                    onSubmitted: (value) => state.discover.runSearch(value),
                  ),
                ),
                const SizedBox(width: Tokens.gap2),
                FilledButton(
                  onPressed: () => state.discover.runSearch(_keyword.text),
                  child: const Text(SC.search),
                ),
              ],
            ),
            const SizedBox(height: Tokens.gap3),
            Expanded(
              child: AsyncView<BgmSearchResult>(
                state: state.discover.search,
                skeleton: const SkeletonRows(rows: 5, height: 64),
                idleBuilder: (context) => const EmptyState(
                  why: SDiscover.searchEmpty,
                  icon: Icons.search,
                ),
                emptyBuilder: (context, why) => EmptyState(
                  why: SDiscover.noSearchResult,
                  icon: Icons.search_off,
                ),
                builder: (context, result) => ListView(
                  children: <Widget>[
                    for (final subject in result.itemsOrEmpty)
                      _SubjectTile(
                        subject: subject,
                        selected: _picked?.id == subject.id,
                        onTap: () => setState(() => _picked = subject),
                      ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: Tokens.gap3),
            if (_picked != null) ...<Widget>[
              Text(
                '去向',
                style: theme.textTheme.titleMedium,
              ),
              const SizedBox(height: Tokens.gap2),
              RadioGroup<_Destination>(
                groupValue: _destination,
                onChanged: (value) =>
                    setState(() => _destination = value ?? _Destination.targets),
                child: Column(
                  children: <Widget>[
                    for (final option in _Destination.values)
                      RadioListTile<_Destination>(
                        dense: true,
                        contentPadding: EdgeInsets.zero,
                        value: option,
                        title: Text(switch (option) {
                          _Destination.targets => SDiscover.targetChoice,
                          _Destination.instance => SDiscover.instanceChoice,
                          _Destination.both => SDiscover.bothChoice,
                        }),
                        subtitle: Text(switch (option) {
                          _Destination.targets => SDiscover.targetChoiceWhy,
                          _Destination.instance => SDiscover.instanceChoiceWhy,
                          _Destination.both => SDiscover.bothChoiceWhy,
                        }),
                      ),
                  ],
                ),
              ),
              if (_destination != _Destination.targets)
                Padding(
                  padding: const EdgeInsets.only(left: Tokens.gap3),
                  child: SizedBox(
                    width: 320,
                    child: DropdownButtonFormField<String>(
                      initialValue: _instance,
                      decoration: const InputDecoration(
                        isDense: true,
                        labelText: SInstances.title,
                      ),
                      items: <DropdownMenuItem<String>>[
                        for (final name in instances)
                          DropdownMenuItem<String>(
                            value: name,
                            child: Text(name),
                          ),
                      ],
                      onChanged: (value) => setState(() => _instance = value),
                    ),
                  ),
                ),
            ],
            if (_result != null) ...<Widget>[
              const SizedBox(height: Tokens.gap2),
              Row(
                children: <Widget>[
                  const Icon(Icons.check_circle_outline,
                      size: 16, color: Tokens.success),
                  const SizedBox(width: Tokens.gap2),
                  Expanded(
                    child: Text(_result!, style: theme.textTheme.bodyMedium),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(context).pop(_result),
          child: const Text(SC.close),
        ),
        FilledButton(
          onPressed: _picked == null || _busy ? null : () => _submit(state),
          child: Text(_busy ? SC.saving : SC.confirm),
        ),
      ],
    );
  }

  Future<void> _submit(AppState state) async {
    final subject = _picked;
    if (subject == null) return;
    setState(() {
      _busy = true;
      _result = null;
    });

    final messages = <String>[];

    if (_destination != _Destination.instance) {
      final ok = await state.discover.addToTargets(subject);
      messages.add(
        ok
            ? SDiscover.resultTargets.replaceFirst('%d', '1')
            : '${SDiscover.targetChoice} 失败',
      );
    }

    if (_destination != _Destination.targets) {
      final instance = _instance;
      if (instance == null || instance.isEmpty) {
        messages.add('${SDiscover.instanceChoice} 失败：没有可选实例');
      } else {
        final ani = await state.subscriptions.add(
          <String, Object?>{
            'bgmId': subject.id,
            'title': subject.displayName,
          },
          instance: instance,
        );
        messages.add(
          ani == null
              ? '${SDiscover.instanceChoice} 失败'
              : SDiscover.resultInstance
                  .replaceFirst('%s', instance)
                  .replaceFirst('%s', ani.key),
        );
      }
    }

    if (!mounted) return;
    setState(() {
      _busy = false;
      _result = messages.join('；');
    });
  }
}

class _SubjectTile extends StatelessWidget {
  const _SubjectTile({
    required this.subject,
    required this.selected,
    required this.onTap,
  });

  final BgmSubject subject;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return DataRow(
      selected: selected,
      onTap: onTap,
      cells: <Widget>[
        SizedBox(
          width: 44,
          height: 60,
          child: subject.image.isEmpty
              ? const Icon(Icons.image_not_supported_outlined, size: 18)
              : Image.network(
                  subject.image,
                  fit: BoxFit.cover,
                  errorBuilder: (context, error, stack) => const Icon(
                    Icons.broken_image_outlined,
                    size: 18,
                  ),
                ),
        ),
        const SizedBox(width: Tokens.gap3),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                subject.displayName,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 2),
              Row(
                children: <Widget>[
                  CodeText(subject.id, style: theme.textTheme.bodySmall),
                  const SizedBox(width: Tokens.gap2),
                  Text(
                    subject.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
              if (subject.summary.isNotEmpty)
                Text(
                  subject.summary,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
            ],
          ),
        ),
        SizedBox(
          width: 96,
          child: Text(subject.date, style: theme.textTheme.bodySmall),
        ),
        SizedBox(
          width: 56,
          child: NumText(
            subject.score == 0 ? '—' : subject.score.toStringAsFixed(1),
            style: theme.textTheme.bodySmall,
          ),
        ),
      ],
    );
  }
}

/// 详情里显示一条 target 的可读名（多个入口共用）。
String targetLabelOf(Target target) {
  if (target.title.isNotEmpty) return target.title;
  if (target.jpTitle.isNotEmpty) return target.jpTitle;
  return target.bgmId;
}
