import 'package:flutter/material.dart' hide DataRow;

import '../../core/errors.dart';
import '../../model/plan.dart';
import '../../model/subscription.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';

/// 订阅列表（实例详情）：搜索 / 过滤 / 逐条编辑 / 批量规划应用 / 删除。
class SubscriptionsPage extends StatefulWidget {
  const SubscriptionsPage({
    super.key,
    required this.state,
    required this.instance,
    this.onBack,
  });

  final AppState state;
  final String instance;
  final VoidCallback? onBack;

  @override
  State<SubscriptionsPage> createState() => _SubscriptionsPageState();
}

class _SubscriptionsPageState extends State<SubscriptionsPage> {
  final TextEditingController _search = TextEditingController();

  @override
  void initState() {
    super.initState();
    final state = widget.state;
    // 放在首帧之后：load() 会立刻 notifyListeners，在 build 期间通知会报错。
    // 只在「换了实例」时自动重读；同一个实例再进来用工具栏的「从实例重读」。
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (state.subscriptions.instanceName != widget.instance) {
        state.subscriptions.load(widget.instance);
      }
    });
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    final subs = state.subscriptions;
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(
            Tokens.gap3,
            Tokens.gap3,
            Tokens.gap4,
            Tokens.gap2,
          ),
          child: Row(
            children: <Widget>[
              if (widget.onBack != null)
                IconButton(
                  tooltip: SC.close,
                  onPressed: widget.onBack,
                  icon: const Icon(Icons.arrow_back, size: 18),
                ),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      '${SSubs.title} · ${widget.instance}',
                      style: theme.textTheme.titleLarge,
                    ),
                    const SizedBox(height: Tokens.gap1),
                    Row(
                      children: <Widget>[
                        NumText(
                          '${subs.all.length}',
                          style: theme.textTheme.bodySmall,
                        ),
                        const SizedBox(width: Tokens.gap1),
                        Text(
                          '条订阅',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                        if (subs.selected.isNotEmpty) ...<Widget>[
                          const SizedBox(width: Tokens.gap3),
                          Text(
                            SSubs.selected.replaceFirst(
                              '%d',
                              '${subs.selected.length}',
                            ),
                            style: theme.textTheme.bodySmall
                                ?.copyWith(color: theme.colorScheme.primary),
                          ),
                        ],
                      ],
                    ),
                  ],
                ),
              ),
              OutlinedButton.icon(
                onPressed: () => subs.load(widget.instance, refresh: true),
                icon: const Icon(Icons.cloud_download_outlined, size: 16),
                label: const Text(SSubs.refreshFromInstance),
              ),
              const SizedBox(width: Tokens.gap2),
              OutlinedButton.icon(
                onPressed: () => _showPlan(context),
                icon: const Icon(Icons.rule_folder_outlined, size: 16),
                label: const Text(SSubs.planTitle),
              ),
            ],
          ),
        ),
        const HairLine(),
        Padding(
          padding: const EdgeInsets.fromLTRB(
            Tokens.gap4,
            Tokens.gap2,
            Tokens.gap4,
            Tokens.gap2,
          ),
          child: Wrap(
            spacing: Tokens.gap2,
            runSpacing: Tokens.gap2,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              SizedBox(
                width: 240,
                child: TextField(
                  controller: _search,
                  onChanged: (value) {
                    subs.query = value;
                    state.touch();
                  },
                  decoration: InputDecoration(
                    isDense: true,
                    hintText: SC.search,
                    prefixIcon: const Icon(Icons.search, size: 16),
                  ),
                ),
              ),
              FilterChipRow<SubscriptionFilter>(
                options: SubscriptionFilter.values,
                selected: <SubscriptionFilter>[subs.filter],
                labelOf: (value) => switch (value) {
                  SubscriptionFilter.all => SSubs.filterAll,
                  SubscriptionFilter.withStandby => SSubs.filterStandby,
                  SubscriptionFilter.noMatch => SSubs.filterNoMatch,
                  SubscriptionFilter.disabled => SSubs.filterDisabled,
                },
                onSelected: (value) {
                  subs.filter = value;
                  state.touch();
                },
              ),
              if (subs.visible.isNotEmpty)
                TextButton(
                  onPressed: () {
                    if (subs.selected.length == subs.visible.length) {
                      subs.clearSelection();
                    } else {
                      subs.selectAll();
                    }
                  },
                  child: Text(
                    subs.selected.length == subs.visible.length
                        ? SSubs.clearSelection
                        : SSubs.selectAllVisible,
                  ),
                ),
              if (subs.selected.isNotEmpty) ...<Widget>[
                FilledButton(
                  onPressed: () => _patchSelected(context),
                  child: const Text(SSubs.editSelected),
                ),
                OutlinedButton(
                  onPressed: () => _deleteSelected(context),
                  child: Text(
                    SSubs.deleteSelected.replaceFirst(
                      '%d',
                      '${subs.selected.length}',
                    ),
                  ),
                ),
              ],
            ],
          ),
        ),
        Expanded(
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: Tokens.gap4),
            child: AsyncView<SubscriptionListResult>(
              state: subs.list,
              onRetry: () => subs.load(widget.instance),
              skeleton: const SkeletonRows(rows: 10, height: 32),
              idleBuilder: (context) => const SkeletonRows(rows: 10, height: 32),
              emptyBuilder: (context, why) => EmptyState(
                why: SSubs.noSubscriptions,
                icon: Icons.playlist_add_outlined,
                secondary: SSubs.emptyNext,
              ),
              builder: (context, result) {
                if (result.error != null && result.error!.isNotEmpty) {
                  // §1：`instance.subscriptions` 自带 `error?` 字段，取消会以
                  // `ok:true` + `error:"context canceled"` 回来 —— 那是取消成功，
                  // 不是失败。不认它，用户会看到一条红色「连接失败」。
                  if (looksCanceled(result.error)) {
                    return EmptyState(
                      why: SC.canceledInData,
                      icon: Icons.do_not_disturb_on_outlined,
                      actionLabel: SC.retry,
                      onAction: () => subs.load(widget.instance, refresh: true),
                    );
                  }
                  return EmptyState(
                    why: '${SC.loadingFailed}：${result.error}',
                    icon: Icons.cloud_off_outlined,
                    actionLabel: SC.retry,
                    onAction: () => subs.load(widget.instance, refresh: true),
                  );
                }
                final rows = subs.visible;
                if (rows.isEmpty) {
                  return EmptyState(
                    why: SSubs.filteredEmpty,
                    icon: Icons.filter_alt_off_outlined,
                    actionLabel: SC.clearFilter,
                    onAction: () {
                      subs
                        ..query = ''
                        ..filter = SubscriptionFilter.all;
                      _search.clear();
                      state.touch();
                    },
                  );
                }
                return LayoutBuilder(
                  builder: (context, constraints) {
                    // 列多且都有固定宽度：窄窗口（含测试用的 800px 视口）下横着滚，
                    // 而不是让 RenderFlex 溢出。Dense 表格的常规做法。
                    final width =
                        constraints.maxWidth < 1024 ? 1024.0 : constraints.maxWidth;
                    return SingleChildScrollView(
                      scrollDirection: Axis.horizontal,
                      child: SizedBox(
                        width: width,
                        child: ListView(
                          padding: const EdgeInsets.only(bottom: Tokens.gap6),
                          children: <Widget>[
                            Row(
                              children: const <Widget>[
                                SizedBox(width: 34),
                                Th(SSubs.columnTitle, flex: 4),
                                Th(SSubs.columnRss, flex: 4),
                                Th(SSubs.columnNumbers, flex: 3),
                                Th(SSubs.columnState, flex: 2),
                                SizedBox(width: 88),
                              ],
                            ),
                            const HairLine(),
                            for (final item in rows) _buildRow(context, item),
                          ],
                        ),
                      ),
                    );
                  },
                );
              },
            ),
          ),
        ),
      ],
    );
  }

  Widget _buildRow(BuildContext context, SubscriptionView item) {
    final state = widget.state;
    final theme = Theme.of(context);
    final selected = state.subscriptions.selected.contains(item.key);
    return DataRow(
      selected: selected,
      cells: <Widget>[
        SizedBox(
          width: 34,
          child: Checkbox(
            value: selected,
            visualDensity: VisualDensity.compact,
            onChanged: (_) => state.subscriptions.toggle(item.key),
          ),
        ),
        Cell(
          flex: 4,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                item.title,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 2),
              Row(
                children: <Widget>[
                  CodeText(item.key, style: theme.textTheme.bodySmall),
                  if (item.issuesOrEmpty.isNotEmpty) ...<Widget>[
                    const SizedBox(width: Tokens.gap2),
                    Tooltip(
                      message: item.issuesOrEmpty.join('\n'),
                      child: Row(
                        children: <Widget>[
                          const Icon(
                            Icons.info_outline,
                            size: 13,
                            color: Tokens.warning,
                          ),
                          const SizedBox(width: 2),
                          NumText(
                            '${item.issuesOrEmpty.length}',
                            style: theme.textTheme.bodySmall
                                ?.copyWith(color: Tokens.warning),
                          ),
                        ],
                      ),
                    ),
                  ],
                ],
              ),
            ],
          ),
        ),
        Cell(
          flex: 4,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Row(
                children: <Widget>[
                  SizedBox(
                    width: 20,
                    child: Text(
                      SSubs.mainGroup,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                  Expanded(
                    child: Text(
                      item.main == null
                          ? SSubs.noMain
                          : '${item.main!.group}${item.main!.title.isEmpty ? '' : ' · ${item.main!.title}'}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                ],
              ),
              for (final ref in item.standbyOrEmpty)
                Row(
                  children: <Widget>[
                    SizedBox(
                      width: 20,
                      child: Text(
                        SSubs.standbyGroup,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                    Expanded(
                      child: Text(
                        ref.group,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                    if (ref.offset != null)
                      NumText(
                        '${ref.offset}',
                        style: theme.textTheme.bodySmall,
                      ),
                  ],
                ),
            ],
          ),
        ),
        Cell(
          flex: 3,
          child: NumText(
            '${item.season ?? '—'} / ${item.offset ?? '—'} / ${item.standbyOffset ?? '—'}',
            style: theme.textTheme.bodySmall,
          ),
        ),
        Cell(
          flex: 2,
          child: Row(
            children: <Widget>[
              StatusDot(
                color: item.enable ? Tokens.success : theme.colorScheme.outline,
                tooltip: item.enable ? '已启用' : SSubs.filterDisabled,
              ),
              const SizedBox(width: Tokens.gap2),
              Flexible(
                child: Text(
                  item.profile.isEmpty ? SSubs.profileInherit : item.profile,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodySmall,
                ),
              ),
            ],
          ),
        ),
        SizedBox(
          width: 88,
          child: Row(
            mainAxisAlignment: MainAxisAlignment.end,
            children: <Widget>[
              IconButton(
                tooltip: SC.edit,
                visualDensity: VisualDensity.compact,
                onPressed: () => _patchOne(context, item),
                icon: const Icon(Icons.edit_outlined, size: 16),
              ),
              IconButton(
                tooltip: SC.delete,
                visualDensity: VisualDensity.compact,
                onPressed: () => _deleteOne(context, item),
                icon: const Icon(Icons.delete_outline, size: 16),
              ),
            ],
          ),
        ),
      ],
    );
  }

  Future<void> _patchOne(BuildContext context, SubscriptionView item) async {
    final state = widget.state;
    state.subscriptions
      ..clearSelection()
      ..toggle(item.key);
    await _patchSelected(context, single: item);
  }

  Future<void> _patchSelected(
    BuildContext context, {
    SubscriptionView? single,
  }) async {
    final state = widget.state;
    final profiles = state.config.editable.profilesOrEmpty
        .map((p) => p.name)
        .toList(growable: false);
    final patch = await showDialog<SubscriptionPatch>(
      context: context,
      builder: (context) => _PatchDialog(
        profiles: profiles,
        initial: single,
      ),
    );
    if (patch == null || patch.isEmpty) return;
    if (!context.mounted) return;
    final result = await state.subscriptions.applyPatch(patch);
    if (!context.mounted || result == null) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(
          SSubs.updatedCount
              .replaceFirst('%d', '${result.updated}')
              .replaceFirst('%d', '${result.skipped}'),
        ),
      ),
    );
  }

  Future<void> _deleteSelected(BuildContext context) async {
    final state = widget.state;
    final keys = state.subscriptions.selected.toList(growable: false);
    await _delete(context, keys);
  }

  Future<void> _deleteOne(BuildContext context, SubscriptionView item) =>
      _delete(context, <String>[item.key]);

  Future<void> _delete(BuildContext context, List<String> keys) async {
    if (keys.isEmpty) return;
    final state = widget.state;
    final withStandby = state.subscriptions.all
        .where((item) => keys.contains(item.key) && item.hasStandby)
        .length;
    final ok = await ConfirmDialog.show(
      context,
      title: SSubs.deleteSelected,
      message: SSubs.deleteConfirm
          .replaceFirst('%s', widget.instance)
          .replaceFirst('%d', '${keys.length}')
          .replaceFirst('%s', withStandby > 0 ? '（其中 $withStandby 条带副 RSS）' : ''),
      confirmLabel: SC.delete,
    );
    if (!ok) return;
    final result = await state.subscriptions.delete(keys, confirm: true);
    if (!context.mounted || result == null) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text('已删除 ${result.deleted} 条')),
    );
  }

  Future<void> _showPlan(BuildContext context) async {
    final state = widget.state;
    await showDialog<void>(
      context: context,
      builder: (context) => _PlanDialog(
        state: state,
        instance: widget.instance,
      ),
    );
  }
}

/// 批改面板：没勾的字段一律不动；三态字段用 TriState 控件。
class _PatchDialog extends StatefulWidget {
  const _PatchDialog({required this.profiles, this.initial});

  final List<String> profiles;
  final SubscriptionView? initial;

  @override
  State<_PatchDialog> createState() => _PatchDialogState();
}

class _PatchDialogState extends State<_PatchDialog> {
  bool _useSeason = false;
  bool _useOffset = false;
  bool _useStandbyOffset = false;
  bool _useEnable = false;
  bool _useProfile = false;
  int? _season;
  int? _offset;
  int? _standbyOffset;
  bool? _enable;
  String? _profile;

  @override
  void initState() {
    super.initState();
    final initial = widget.initial;
    if (initial != null) {
      if (initial.season != null) {
        _useSeason = true;
        _season = initial.season;
      }
      if (initial.offset != null) {
        _useOffset = true;
        _offset = initial.offset;
      }
      if (initial.standbyOffset != null) {
        _useStandbyOffset = true;
        _standbyOffset = initial.standbyOffset;
      }
      _useEnable = true;
      _enable = initial.enable;
      _useProfile = true;
      _profile = initial.profile;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AlertDialog(
      title: const Text(SSubs.patchTitle),
      content: SizedBox(
        width: 560,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Text(
                SSubs.patchHint,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: Tokens.gap3),
              _triRow('季度 season', _useSeason, _season,
                  (on) => setState(() => _useSeason = on),
                  (value) => setState(() => _season = value)),
              _triRow('主 RSS 偏移 offset', _useOffset, _offset,
                  (on) => setState(() => _useOffset = on),
                  (value) => setState(() => _offset = value)),
              _triRow('副 RSS 偏移 standbyOffset', _useStandbyOffset,
                  _standbyOffset,
                  (on) => setState(() => _useStandbyOffset = on),
                  (value) => setState(() => _standbyOffset = value)),
              const SizedBox(height: Tokens.gap2),
              CheckboxListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                value: _useEnable,
                onChanged: (value) => setState(() => _useEnable = value ?? false),
                title: Text('启用开关', style: theme.textTheme.bodyMedium),
              ),
              if (_useEnable)
                TriStateBoolField(
                  label: '',
                  value: _enable,
                  unsetLabel: STri.enableUnset,
                  onChanged: (value) => setState(() => _enable = value),
                ),
              CheckboxListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                value: _useProfile,
                onChanged: (value) => setState(() => _useProfile = value ?? false),
                title: Text('方案覆盖', style: theme.textTheme.bodyMedium),
              ),
              if (_useProfile)
                DropdownButtonFormField<String>(
                  initialValue: _profile ?? '',
                  items: <DropdownMenuItem<String>>[
                    const DropdownMenuItem<String>(
                      value: '',
                      child: Text(SSubs.profileClear),
                    ),
                    for (final name in widget.profiles)
                      DropdownMenuItem<String>(value: name, child: Text(name)),
                  ],
                  onChanged: (value) => setState(() => _profile = value ?? ''),
                ),
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text(SC.cancel),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(
            SubscriptionPatch(
              season: _useSeason ? _season : null,
              offset: _useOffset ? _offset : null,
              standbyOffset: _useStandbyOffset ? _standbyOffset : null,
              enable: _useEnable ? _enable : null,
              // profile 的三态：不勾 = 不动；勾了选「清除覆盖」= 空串（显式清除）。
              profile: _useProfile ? (_profile ?? '') : null,
            ),
          ),
          child: const Text(SSubs.apply),
        ),
      ],
    );
  }

  Widget _triRow(
    String label,
    bool enabled,
    int? value,
    ValueChanged<bool> onEnabled,
    ValueChanged<int?> onValue,
  ) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        CheckboxListTile(
          dense: true,
          contentPadding: EdgeInsets.zero,
          value: enabled,
          onChanged: (next) => onEnabled(next ?? false),
          title: Text(label, style: Theme.of(context).textTheme.bodyMedium),
        ),
        if (enabled)
          Padding(
            padding: const EdgeInsets.only(left: Tokens.gap4, bottom: Tokens.gap2),
            child: TriStateIntField(
              label: '',
              value: value,
              onChanged: onValue,
            ),
          ),
      ],
    );
  }
}

/// 计划预览：actions / changes / skipped，确认后 apply。
class _PlanDialog extends StatefulWidget {
  const _PlanDialog({required this.state, required this.instance});

  final AppState state;
  final String instance;

  @override
  State<_PlanDialog> createState() => _PlanDialogState();
}

class _PlanDialogState extends State<_PlanDialog> {
  PlanView? _plan;
  String? _error;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _build();
  }

  Future<void> _build() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    final plan = await buildPlan(widget.state, instance: widget.instance);
    if (!mounted) return;
    setState(() {
      _loading = false;
      _plan = plan;
      _error = plan == null ? SC.loadingFailed : null;
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final plan = _plan;
    return AlertDialog(
      title: Text('${SSubs.planTitle} · ${widget.instance}'),
      content: SizedBox(
        width: 720,
        height: 460,
        child: _loading
            ? const SkeletonRows(rows: 8)
            : _error != null
                ? ErrorPane(
                    error: CoreError(
                      code: ErrorCodes.internal,
                      message: _error!,
                    ),
                    onRetry: _build,
                  )
                : plan == null || plan.actionsOrEmpty.isEmpty
                    ? const EmptyState(
                        why: SSubs.planEmpty,
                        icon: Icons.check_circle_outline,
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: <Widget>[
                          Row(
                            children: <Widget>[
                              Text(
                                SSubs.planWritable
                                    .replaceFirst('%d', '${plan.writableCount}'),
                                style: theme.textTheme.bodyMedium,
                              ),
                              const Spacer(),
                              if (plan.siteInstance.isNotEmpty)
                                Text(
                                  '站点数据来自 ${plan.siteInstance}',
                                  style: theme.textTheme.bodySmall,
                                ),
                            ],
                          ),
                          const SizedBox(height: Tokens.gap2),
                          Expanded(
                            child: ListView(
                              children: <Widget>[
                                for (final action in plan.actionsOrEmpty)
                                  Padding(
                                    padding: const EdgeInsets.only(
                                      bottom: Tokens.gap2,
                                    ),
                                    child: _ActionTile(action: action),
                                  ),
                              ],
                            ),
                          ),
                        ],
                      ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text(SC.close),
        ),
        if (plan != null && plan.actionsOrEmpty.isNotEmpty)
          FilledButton(
            onPressed: () async {
              final ok = await ConfirmDialog.show(
                context,
                title: SSubs.applyPlan,
                message: SSubs.applyPlanConfirm
                    .replaceFirst('%s', widget.instance)
                    .replaceFirst('%d', '${plan.writableCount}'),
                confirmLabel: SSubs.applyPlan,
              );
              if (!ok) return;
              await applyPlan(widget.state);
              if (context.mounted) Navigator.of(context).pop();
            },
            child: const Text(SSubs.applyPlan),
          ),
      ],
    );
  }
}

class _ActionTile extends StatelessWidget {
  const _ActionTile({required this.action});

  final PlanAction action;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final color = switch (action.kind) {
      'add' => Tokens.success,
      'error' => Tokens.danger,
      'noop' => theme.colorScheme.onSurfaceVariant,
      _ => Tokens.info,
    };
    return Container(
      decoration: BoxDecoration(
        borderRadius: BorderRadius.circular(Tokens.radiusInput),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      padding: const EdgeInsets.all(Tokens.gap3),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              StatusDot(color: color),
              const SizedBox(width: Tokens.gap2),
              Expanded(
                child: Text(
                  action.title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodyMedium,
                ),
              ),
              CodeText(action.kind, style: theme.textTheme.bodySmall),
              if (action.target.isNotEmpty) ...<Widget>[
                const SizedBox(width: Tokens.gap2),
                Text(
                  '按 ${action.target}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.primary,
                  ),
                ),
              ],
              if (action.profile.isNotEmpty) ...<Widget>[
                const SizedBox(width: Tokens.gap2),
                Text(
                  '方案 ${action.profile}',
                  style: theme.textTheme.bodySmall,
                ),
              ],
            ],
          ),
          for (final change in action.changesOrEmpty)
            Padding(
              padding: const EdgeInsets.only(top: Tokens.gap1, left: Tokens.gap4),
              child: Text(
                '${change.field}: ${change.from} → ${change.to}',
                style: theme.textTheme.bodySmall,
              ),
            ),
          if (action.reason.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: Tokens.gap1, left: Tokens.gap4),
              child: Text(
                action.reason,
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ),
        ],
      ),
    );
  }
}

/// 订阅页的「下一步做什么」提示已集中在 `strings.dart` 的 `SSubs.emptyNext`。
