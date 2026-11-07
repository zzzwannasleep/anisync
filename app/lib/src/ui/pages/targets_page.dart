import 'package:flutter/material.dart';

import '../../model/config.dart';
import '../../model/plan.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';
import '../widgets/config_strip.dart';

/// 番剧配置（`targets`）：一等公民。
///
/// 它规定季度与集偏移、决定新订阅是否启用、可以按番剧覆盖方案；
/// **和「实例里真实存在的订阅」不是一回事** —— 实例里手动加的订阅不在 targets 里，
/// 一条 target 也可能一条订阅都没命中。所以它单独一页，不和订阅列表混在一起。
class TargetsPage extends StatefulWidget {
  const TargetsPage({
    super.key,
    required this.state,
    required this.onOpenDiscover,
  });

  final AppState state;
  final VoidCallback onOpenDiscover;

  @override
  State<TargetsPage> createState() => _TargetsPageState();
}

class _TargetsPageState extends State<TargetsPage> {
  PlanView? _plan;
  bool _planLoading = false;
  String? _planError;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _loadPlan());
  }

  Future<void> _loadPlan() async {
    setState(() {
      _planLoading = true;
      _planError = null;
    });
    final plan = await buildPlan(widget.state);
    if (!mounted) return;
    setState(() {
      _planLoading = false;
      _plan = plan;
      _planError = plan == null ? SC.loadingFailed : null;
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = widget.state;
    final config = state.config.editable;
    final targets = config.targetsOrEmpty;
    final hits = _plan?.hitsByTarget ?? const <String, int>{};

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
          child: Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(STargets.title, style: theme.textTheme.titleLarge),
                    const SizedBox(height: Tokens.gap1),
                    Text(
                      STargets.subtitle,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: _planLoading ? null : _loadPlan,
                icon: const Icon(Icons.calculate_outlined, size: 16),
                label: Text(
                  _planLoading ? SC.loading : STargets.colHits,
                ),
              ),
              const SizedBox(width: Tokens.gap2),
              FilledButton.icon(
                onPressed: () => _addTarget(context),
                icon: const Icon(Icons.add, size: 18),
                label: const Text(STargets.addTarget),
              ),
              const SizedBox(width: Tokens.gap2),
              FilledButton.icon(
                onPressed: state.config.dirty ? () => _save() : null,
                icon: const Icon(Icons.save_outlined, size: 16),
                label: Text(state.config.dirty ? SC.save : SC.saved),
              ),
            ],
          ),
        ),
        if (_planError != null)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: Tokens.gap4),
            child: Text(
              STargets.hitsUnknown.replaceFirst('%s', _planError!),
              style: theme.textTheme.bodySmall?.copyWith(color: Tokens.warning),
            ),
          ),
        const HairLine(),
        Padding(
          padding: const EdgeInsets.fromLTRB(
            Tokens.gap4,
            Tokens.gap2,
            Tokens.gap4,
            0,
          ),
          child: ConfigStatusStrip(state: state, dense: true),
        ),
        Expanded(
          child: targets.isEmpty
              ? EmptyState(
                  why: STargets.empty,
                  icon: Icons.movie_filter_outlined,
                  actionLabel: STargets.emptyAction,
                  onAction: widget.onOpenDiscover,
                )
              : ListView(
                  padding: const EdgeInsets.all(Tokens.gap4),
                  children: <Widget>[
                    for (var i = 0; i < targets.length; i++)
                      Padding(
                        padding: const EdgeInsets.only(bottom: Tokens.gap3),
                        child: _TargetCard(
                          key: ValueKey<String>('target-$i-${targets[i].bgmId}'),
                          state: state,
                          target: targets[i],
                          hits: _hitsFor(targets[i], hits),
                          onDelete: () => _deleteTarget(context, i),
                        ),
                      ),
                  ],
                ),
        ),
      ],
    );
  }

  int? _hitsFor(Target target, Map<String, int> hits) {
    for (final candidate in <String>[
      target.title,
      target.jpTitle,
      target.bgmId,
    ]) {
      if (candidate.isEmpty) continue;
      final value = hits[candidate];
      if (value != null) return value;
    }
    return _plan == null ? null : 0;
  }

  Future<void> _save() async {
    final ok = await widget.state.config.validate();
    if (ok != null && !ok.ok) {
      final first = ok.errorsOrEmpty.isEmpty ? null : ok.errorsOrEmpty.first;
      widget.state.banner = first == null
          ? SC.loadingFailed
          : '配置预检未通过：${first.message}';
      widget.state.touch();
      return;
    }
    await widget.state.config.save();
    if (mounted) await _loadPlan();
  }

  void _addTarget(BuildContext context) {
    final config = widget.state.config.editable;
    final targets = <Target>[...config.targetsOrEmpty, Target()];
    config.targets = targets;
    widget.state.config.markDirty();
    widget.state.touch();
  }

  Future<void> _deleteTarget(BuildContext context, int index) async {
    final config = widget.state.config.editable;
    final targets = <Target>[...config.targetsOrEmpty];
    final removed = targets.removeAt(index);
    final label = removed.title.isEmpty ? removed.bgmId : removed.title;
    final ok = await ConfirmDialog.show(
      context,
      title: STargets.title,
      message: STargets.deleteConfirm.replaceFirst('%s', label),
      confirmLabel: SC.delete,
    );
    if (!ok) return;
    config.targets = targets;
    widget.state.config.markDirty();
    widget.state.touch();
  }
}

class _TargetCard extends StatelessWidget {
  const _TargetCard({
    super.key,
    required this.state,
    required this.target,
    required this.hits,
    required this.onDelete,
  });

  final AppState state;
  final Target target;
  final int? hits;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final config = state.config.editable;
    final instances = config.instancesOrEmpty.map((e) => e.name).toList();
    final profiles = config.profilesOrEmpty.map((p) => p.name).toList();

    return SectionCard(
      title: target.title.isEmpty
          ? (target.bgmId.isEmpty ? STargets.addTarget : target.bgmId)
          : target.title,
      subtitle: target.bgmId.isEmpty ? null : 'bgmId ${target.bgmId}',
      trailing: Row(
        children: <Widget>[
          if (hits == null)
            Text(
              STargets.hitsUnknown.replaceFirst('%s', SC.loading),
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            )
          else
            Tooltip(
              message: STargets.hitsHint,
              child: Row(
                children: <Widget>[
                  NumText('$hits', style: theme.textTheme.bodySmall),
                  const SizedBox(width: Tokens.gap1),
                  Text(STargets.colHits, style: theme.textTheme.bodySmall),
                ],
              ),
            ),
          const SizedBox(width: Tokens.gap2),
          IconButton(
            tooltip: SC.delete,
            visualDensity: VisualDensity.compact,
            onPressed: onDelete,
            icon: const Icon(Icons.delete_outline, size: 16),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          FieldRow(
            label: STargets.colTitle,
            child: TextFormField(
              initialValue: target.title,
              decoration: const InputDecoration(isDense: true),
              onChanged: (value) {
                target.title = value;
                state.config.markDirty();
                state.touch();
              },
            ),
          ),
          FieldRow(
            label: 'jpTitle',
            hint: STargets.jpTitleHint,
            child: TextFormField(
              initialValue: target.jpTitle,
              decoration: const InputDecoration(isDense: true),
              onChanged: (value) {
                target.jpTitle = value;
                state.config.markDirty();
              },
            ),
          ),
          FieldRow(
            label: 'bgmId',
            hint: STargets.bgmIdHint,
            child: TextFormField(
              initialValue: target.bgmId,
              decoration: const InputDecoration(isDense: true),
              onChanged: (value) {
                target.bgmId = value;
                state.config.markDirty();
              },
            ),
          ),
          FieldRow(
            label: 'mikanId',
            child: TextFormField(
              initialValue: target.mikanId,
              decoration: const InputDecoration(isDense: true),
              onChanged: (value) {
                target.mikanId = value;
                state.config.markDirty();
              },
            ),
          ),
          FieldRow(
            label: STargets.colInstances,
            hint: STargets.allInstances,
            child: Wrap(
              spacing: Tokens.gap2,
              children: <Widget>[
                for (final name in instances)
                  FilterChip(
                    label: Text(name),
                    selected: target.instances?.contains(name) ?? false,
                    showCheckmark: false,
                    onSelected: (selected) {
                      final current = <String>[...?target.instances];
                      if (selected) {
                        if (!current.contains(name)) current.add(name);
                      } else {
                        current.remove(name);
                      }
                      target.instances = current.isEmpty ? null : current;
                      state.config.markDirty();
                      state.touch();
                    },
                  ),
              ],
            ),
          ),
          FieldRow(
            label: STargets.colProfile,
            child: DropdownButtonFormField<String>(
              initialValue: target.profile,
              decoration: const InputDecoration(isDense: true),
              items: <DropdownMenuItem<String>>[
                const DropdownMenuItem<String>(
                  value: '',
                  child: Text(STri.unset),
                ),
                for (final name in profiles)
                  DropdownMenuItem<String>(value: name, child: Text(name)),
              ],
              onChanged: (value) {
                target.profile = value ?? '';
                state.config.markDirty();
                state.touch();
              },
            ),
          ),
          TriStateBoolField(
            label: STargets.colEnable,
            value: target.enable,
            onChanged: (value) {
              target.enable = value;
              state.config.markDirty();
              state.touch();
            },
          ),
          const SizedBox(height: Tokens.gap3),
          TriStateIntField(
            label: '季度 season',
            value: target.season,
            hint: STri.whySeason,
            onChanged: (value) {
              target.season = value;
              state.config.markDirty();
              state.touch();
            },
          ),
          const SizedBox(height: Tokens.gap2),
          TriStateIntField(
            label: '主 RSS 偏移 offset',
            value: target.offset,
            hint: STri.whySeason,
            onChanged: (value) {
              target.offset = value;
              state.config.markDirty();
              state.touch();
            },
          ),
          const SizedBox(height: Tokens.gap2),
          TriStateIntField(
            label: '副 RSS 偏移 standbyOffset',
            value: target.standbyOffset,
            hint: STri.standbyOffsetHint,
            onChanged: (value) {
              target.standbyOffset = value;
              state.config.markDirty();
              state.touch();
            },
          ),
        ],
      ),
    );
  }
}
