import 'package:flutter/material.dart';

import '../../model/config.dart';
import '../../model/profile_view.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';
import '../widgets/config_strip.dart';

/// 规则页：**两级**。
///
/// 1. 顶层「默认（顶层规则）」——**不可删除**。没挂方案的实例用的就是这套，
///    而绝大多数存量配置正是这种。少了它，老配置用户在 GUI 里根本改不了规则。
/// 2. 各命名方案——被实例挂上才生效。
class ProfilesPage extends StatelessWidget {
  const ProfilesPage({super.key, required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final config = state.config.editable;
    final profiles = config.profilesOrEmpty;
    final selected = state.profiles.selectedName;
    final selectedProfile =
        selected.isEmpty ? null : config.profileByName(selected);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
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
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              SizedBox(
                width: 268,
                child: _ProfileList(
                  state: state,
                  profiles: profiles,
                  selectedName: selected,
                ),
              ),
              const VerticalDivider(width: 1),
              Expanded(
                child: _ProfileDetail(
                  state: state,
                  profile: selectedProfile,
                  isDefault: selectedProfile == null,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

class _ProfileList extends StatelessWidget {
  const _ProfileList({
    required this.state,
    required this.profiles,
    required this.selectedName,
  });

  final AppState state;
  final List<Profile> profiles;
  final String selectedName;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final config = state.config.editable;
    final owners = <String, List<String>>{};
    for (final instance in config.instancesOrEmpty) {
      if (instance.profile.isEmpty) continue;
      owners.putIfAbsent(instance.profile, () => <String>[]).add(instance.name);
    }

    return ListView(
      padding: const EdgeInsets.all(Tokens.gap3),
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(bottom: Tokens.gap2),
          child: Text(SRules.title, style: theme.textTheme.titleLarge),
        ),
        _ProfileTile(
          title: SRules.defaultEntry,
          subtitle: SRules.defaultEntryWhy,
          selected: selectedName.isEmpty,
          pinned: true,
          onTap: () {
            state.profiles.selectedName = '';
            state.touch();
          },
        ),
        const SizedBox(height: Tokens.gap2),
        for (final profile in profiles)
          _ProfileTile(
            title: profile.name,
            subtitle: owners[profile.name] == null
                ? SRules.usedByNone
                : SRules.usedBy
                    .replaceFirst('%s', owners[profile.name]!.join(' / ')),
            selected: selectedName == profile.name,
            onTap: () {
              state.profiles.selectedName = profile.name;
              state.touch();
            },
          ),
        const SizedBox(height: Tokens.gap3),
        OutlinedButton.icon(
          onPressed: () => _create(context),
          icon: const Icon(Icons.add, size: 16),
          label: const Text(SRules.addProfile),
        ),
      ],
    );
  }

  Future<void> _create(BuildContext context) async {
    final name = await showDialog<String>(
      context: context,
      builder: (context) => const _NameDialog(),
    );
    if (name == null || name.isEmpty) return;
    final profile = Profile(name: name, rules: Rules(maxStandby: 1));
    final ok = await state.profiles.upsert(profile);
    if (ok) {
      state.profiles.selectedName = name;
      state.touch();
    }
  }
}

class _ProfileTile extends StatelessWidget {
  const _ProfileTile({
    required this.title,
    required this.subtitle,
    required this.selected,
    required this.onTap,
    this.pinned = false,
  });

  final String title;
  final String subtitle;
  final bool selected;
  final VoidCallback onTap;
  final bool pinned;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(Tokens.radiusInput),
      child: Container(
        margin: const EdgeInsets.only(bottom: Tokens.gap1),
        padding: const EdgeInsets.symmetric(
          horizontal: Tokens.gap3,
          vertical: Tokens.gap2,
        ),
        decoration: BoxDecoration(
          color: selected
              ? theme.colorScheme.primary.withValues(alpha: 0.07)
              : null,
          borderRadius: BorderRadius.circular(Tokens.radiusInput),
          border: Border(
            left: BorderSide(
              color: selected ? theme.colorScheme.primary : Colors.transparent,
              width: 2,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                if (pinned)
                  Padding(
                    padding: const EdgeInsets.only(right: Tokens.gap1),
                    child: Icon(
                      Icons.push_pin_outlined,
                      size: 13,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                Expanded(
                  child: Text(
                    title,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      fontWeight: selected ? FontWeight.w600 : null,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 2),
            Text(
              subtitle,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _NameDialog extends StatefulWidget {
  const _NameDialog();

  @override
  State<_NameDialog> createState() => _NameDialogState();
}

class _NameDialogState extends State<_NameDialog> {
  final TextEditingController _controller = TextEditingController();

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: const Text(SRules.addProfile),
      content: SizedBox(
        width: 380,
        child: TextField(
          controller: _controller,
          autofocus: true,
          decoration: const InputDecoration(hintText: SRules.newProfileName),
          onSubmitted: (value) => Navigator.of(context).pop(value.trim()),
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text(SC.cancel),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(_controller.text.trim()),
          child: const Text(SC.confirm),
        ),
      ],
    );
  }
}

/// 详情区：顶层 = 改 `config.rules` / `config.sources.order`；方案 = 改 `profiles[i]`。
class _ProfileDetail extends StatefulWidget {
  const _ProfileDetail({
    required this.state,
    required this.profile,
    required this.isDefault,
  });

  final AppState state;
  final Profile? profile;
  final bool isDefault;

  @override
  State<_ProfileDetail> createState() => _ProfileDetailState();
}

class _ProfileDetailState extends State<_ProfileDetail> {
  int _tab = 0;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = widget.state;
    final config = state.config.editable;
    final profile = widget.profile;
    final rules = profile?.rules ?? config.rules;
    final title = widget.isDefault ? SRules.defaultEntry : (profile?.name ?? '');

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
                constraints: const BoxConstraints(maxWidth: 560),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Wrap(
                      spacing: Tokens.gap2,
                      runSpacing: Tokens.gap1,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: <Widget>[
                        Text(title, style: theme.textTheme.titleLarge),
                        if (widget.isDefault)
                          Chip(
                            label: const Text(SRules.defaultCannotDelete),
                            visualDensity: VisualDensity.compact,
                            labelStyle: theme.textTheme.bodySmall,
                          ),
                      ],
                    ),
                    const SizedBox(height: Tokens.gap1),
                    Text(
                      widget.isDefault
                          ? SRules.defaultEntryWhy
                          : SRules.usedBy.replaceFirst(
                              '%s',
                              _owners(state).isEmpty
                                  ? SRules.usedByNone
                                  : _owners(state).join(' / '),
                            ),
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
              Row(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  if (!widget.isDefault)
                    OutlinedButton.icon(
                      onPressed: () => _delete(context),
                      icon: const Icon(Icons.delete_outline, size: 16),
                      label: const Text(SRules.deleteProfile),
                    ),
                  const SizedBox(width: Tokens.gap2),
                  FilledButton.icon(
                    onPressed: state.config.dirty ? () => _save() : null,
                    icon: const Icon(Icons.save_outlined, size: 16),
                    label: Text(state.config.dirty ? SC.save : SC.saved),
                  ),
                ],
              ),
            ],
          ),
        ),
        const HairLine(),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: Tokens.gap4),
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: <Widget>[
                for (var i = 0; i < 4; i++)
                  Padding(
                    padding: const EdgeInsets.only(right: Tokens.gap2),
                    child: TextButton(
                      onPressed: () => setState(() => _tab = i),
                      style: TextButton.styleFrom(
                        foregroundColor: _tab == i
                            ? theme.colorScheme.primary
                            : theme.colorScheme.onSurfaceVariant,
                      ),
                      child: Text(
                        <String>[
                          SRules.tabRules,
                          SRules.tabDeny,
                          SRules.tabFiles,
                          SRules.tabSites,
                        ][i],
                      ),
                    ),
                  ),
                if (widget.isDefault && state.config.dirty)
                  Padding(
                    padding: const EdgeInsets.only(right: Tokens.gap2),
                    child: Text(
                      SC.unsaved,
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: Tokens.warning),
                    ),
                  ),
              ],
            ),
          ),
        ),
        const HairLine(),
        Expanded(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(Tokens.gap4),
            // 列多且每列固定宽度：窄屏（手机）下横向滚，而不是让 RenderFlex 溢出。
            child: HScroll(
              minWidth: switch (_tab) {
                0 => 900,
                1 => 640,
                2 => 560,
                _ => 520,
              },
              child: switch (_tab) {
                0 => _RulesEditor(
                    rules: rules,
                    onChange: () {
                      state.config.markDirty();
                    },
                  ),
                1 => _DenyEditor(
                    rules: rules,
                    onChange: () {
                      state.config.markDirty();
                    },
                  ),
                2 => _RuleFilesView(
                    state: state,
                    profile: profile,
                    isDefault: widget.isDefault,
                  ),
                _ => _SiteOrderEditor(
                    state: state,
                    profile: profile,
                    isDefault: widget.isDefault,
                  ),
              },
            ),
          ),
        ),
      ],
    );
  }

  List<String> _owners(AppState state) => state.config.editable.instancesOrEmpty
      .where((instance) => instance.profile == widget.profile?.name)
      .map((instance) => instance.name)
      .toList(growable: false);

  Future<void> _save() async {
    final profile = widget.profile;
    if (profile == null) {
      await widget.state.config.save();
    } else {
      await widget.state.profiles.upsert(profile);
    }
  }

  Future<void> _delete(BuildContext context) async {
    final profile = widget.profile;
    if (profile == null) return;
    final owners = _owners(widget.state);
    final ok = await ConfirmDialog.show(
      context,
      title: SRules.deleteProfile,
      message: SRules.deleteProfileConfirm
          .replaceFirst('%s', profile.name)
          .replaceFirst('%d', '${owners.length}')
          .replaceFirst('%s', owners.isEmpty ? SRules.usedByNone : owners.join(' / ')),
      confirmLabel: SC.delete,
    );
    if (!ok) return;
    await widget.state.profiles.delete(profile.name, confirm: true);
  }
}

class _RulesEditor extends StatelessWidget {
  const _RulesEditor({required this.rules, required this.onChange});

  final Rules rules;
  final VoidCallback onChange;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final list = rules.primaryOrEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: TriStateIntField(
                label: SRules.maxStandby,
                value: rules.maxStandby,
                hint: SRules.maxStandbyHint,
                onChanged: (value) {
                  rules.maxStandby = value ?? rules.maxStandby;
                  onChange();
                },
              ),
            ),
          ],
        ),
        const SizedBox(height: Tokens.gap4),
        Row(
          children: const <Widget>[
            SizedBox(width: 160, child: Text(SRules.colName)),
            SizedBox(width: 240, child: Text(SRules.colMatch)),
            SizedBox(width: 80, child: Text(SRules.colPriority)),
            SizedBox(width: 130, child: Text(SRules.colScope)),
            SizedBox(width: 130, child: Text(SRules.colIn)),
            Spacer(),
          ],
        ),
        const HairLine(),
        for (var i = 0; i < list.length; i++)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: Tokens.gap1),
            child: Row(
              children: <Widget>[
                SizedBox(
                  width: 160,
                  child: TextFormField(
                    initialValue: list[i].name,
                    decoration: const InputDecoration(isDense: true),
                    onChanged: (value) {
                      list[i].name = value;
                      onChange();
                    },
                  ),
                ),
                const SizedBox(width: Tokens.gap2),
                SizedBox(
                  width: 232,
                  child: TextFormField(
                    initialValue: list[i].match,
                    style: theme.textTheme.bodySmall,
                    decoration: const InputDecoration(isDense: true),
                    onChanged: (value) {
                      list[i].match = value;
                      onChange();
                    },
                  ),
                ),
                const SizedBox(width: Tokens.gap2),
                SizedBox(
                  width: 72,
                  child: TextFormField(
                    initialValue: '${list[i].priority}',
                    decoration: const InputDecoration(isDense: true),
                    onChanged: (value) {
                      final parsed = int.tryParse(value.trim());
                      if (parsed != null) {
                        list[i].priority = parsed;
                        onChange();
                      }
                    },
                  ),
                ),
                const SizedBox(width: Tokens.gap2),
                SizedBox(
                  width: 122,
                  child: DropdownButtonFormField<String>(
                    initialValue: list[i].scope.isEmpty
                        ? RuleScope.main
                        : list[i].scope,
                    decoration: const InputDecoration(isDense: true),
                    items: const <DropdownMenuItem<String>>[
                      DropdownMenuItem<String>(
                        value: RuleScope.main,
                        child: Text(SRules.scopeMain),
                      ),
                      DropdownMenuItem<String>(
                        value: RuleScope.standby,
                        child: Text(SRules.scopeStandby),
                      ),
                      DropdownMenuItem<String>(
                        value: RuleScope.both,
                        child: Text(SRules.scopeBoth),
                      ),
                    ],
                    onChanged: (value) {
                      list[i].scope = value ?? RuleScope.main;
                      onChange();
                    },
                  ),
                ),
                const SizedBox(width: Tokens.gap2),
                SizedBox(
                  width: 122,
                  child: DropdownButtonFormField<String>(
                    initialValue:
                        list[i].inValue.isEmpty ? RuleIn.any : list[i].inValue,
                    decoration: const InputDecoration(isDense: true),
                    items: const <DropdownMenuItem<String>>[
                      DropdownMenuItem<String>(
                        value: RuleIn.any,
                        child: Text(SRules.inAny),
                      ),
                      DropdownMenuItem<String>(
                        value: RuleIn.group,
                        child: Text(SRules.inGroup),
                      ),
                      DropdownMenuItem<String>(
                        value: RuleIn.title,
                        child: Text(SRules.inTitle),
                      ),
                    ],
                    onChanged: (value) {
                      final next = value ?? RuleIn.any;
                      list[i].inValue = next == RuleIn.any ? '' : next;
                      onChange();
                    },
                  ),
                ),
                IconButton(
                  tooltip: SC.delete,
                  visualDensity: VisualDensity.compact,
                  onPressed: () {
                    rules.primary = <Rule>[...list]..removeAt(i);
                    onChange();
                  },
                  icon: const Icon(Icons.close, size: 16),
                ),
              ],
            ),
          ),
        const SizedBox(height: Tokens.gap2),
        OutlinedButton.icon(
          onPressed: () {
            rules.primary = <Rule>[
              ...list,
              Rule(name: '', match: '', priority: 100),
            ];
            onChange();
          },
          icon: const Icon(Icons.add, size: 16),
          label: const Text(SRules.addRule),
        ),
        const SizedBox(height: Tokens.gap2),
        Text(
          SRules.priorityHint,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ],
    );
  }
}

class _DenyEditor extends StatelessWidget {
  const _DenyEditor({required this.rules, required this.onChange});

  final Rules rules;
  final VoidCallback onChange;

  @override
  Widget build(BuildContext context) {
    final list = rules.denyOrEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        for (var i = 0; i < list.length; i++)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: Tokens.gap1),
            child: Row(
              children: <Widget>[
                SizedBox(
                  width: 300,
                  child: TextFormField(
                    initialValue: list[i].match,
                    decoration: const InputDecoration(
                      isDense: true,
                      hintText: SRules.colMatch,
                    ),
                    onChanged: (value) {
                      list[i].match = value;
                      onChange();
                    },
                  ),
                ),
                const SizedBox(width: Tokens.gap2),
                Expanded(
                  child: TextFormField(
                    initialValue: list[i].why,
                    decoration: const InputDecoration(
                      isDense: true,
                      hintText: SRules.colWhy,
                    ),
                    onChanged: (value) {
                      list[i].why = value;
                      onChange();
                    },
                  ),
                ),
                IconButton(
                  tooltip: SC.delete,
                  visualDensity: VisualDensity.compact,
                  onPressed: () {
                    rules.deny = <DenyRule>[...list]..removeAt(i);
                    onChange();
                  },
                  icon: const Icon(Icons.close, size: 16),
                ),
              ],
            ),
          ),
        const SizedBox(height: Tokens.gap2),
        OutlinedButton.icon(
          onPressed: () {
            rules.deny = <DenyRule>[...list, DenyRule(match: '', why: '')];
            onChange();
          },
          icon: const Icon(Icons.add, size: 16),
          label: const Text(SRules.addDeny),
        ),
      ],
    );
  }
}

class _RuleFilesView extends StatelessWidget {
  const _RuleFilesView({
    required this.state,
    required this.profile,
    required this.isDefault,
  });

  final AppState state;
  final Profile? profile;
  final bool isDefault;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final files = isDefault
        ? state.config.editable.fileRulesOrEmpty
        : profile?.ruleFilesOrEmpty ?? const <RuleFileInfo>[];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(
          SRules.filesHint,
          style: theme.textTheme.bodySmall?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
        const SizedBox(height: Tokens.gap3),
        if (files.isEmpty)
          Text(
            SRules.filesEmpty,
            style: theme.textTheme.bodySmall,
          )
        else
          for (final file in files)
            Padding(
              padding: const EdgeInsets.only(bottom: Tokens.gap2),
              child: Row(
                children: <Widget>[
                  SizedBox(
                    width: 320,
                    child: CodeText(file.path, style: theme.textTheme.bodySmall),
                  ),
                  SizedBox(
                    width: 80,
                    child: Text(file.kind, style: theme.textTheme.bodySmall),
                  ),
                  SizedBox(
                    width: 60,
                    child: NumText(
                      '${file.count}',
                      style: theme.textTheme.bodySmall,
                    ),
                  ),
                  const SizedBox(width: Tokens.gap2),
                  TextButton(
                    onPressed: () => _open(context, file),
                    child: const Text(SRules.fileContent),
                  ),
                ],
              ),
            ),
      ],
    );
  }

  Future<void> _open(BuildContext context, RuleFileInfo file) async {
    await state.profiles.loadRuleFile(isDefault ? '' : (profile?.name ?? ''), file.path);
    if (!context.mounted) return;
    await showDialog<void>(
      context: context,
      builder: (context) => AnimatedBuilder(
        animation: state,
        builder: (context, _) => AlertDialog(
          title: Text(file.path),
          content: SizedBox(
            width: 640,
            height: 420,
            child: AsyncView<RuleFileDetail>(
              state: state.profiles.ruleFile,
              skeleton: const SkeletonRows(rows: 8),
              onRetry: () => state.profiles.loadRuleFile(
                isDefault ? '' : (profile?.name ?? ''),
                file.path,
              ),
              builder: (context, detail) => SingleChildScrollView(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    for (final rule in detail.rulesOrEmpty)
                      CodeText(
                        '${rule.name}  ${rule.match}  优先级 ${rule.priority}',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    for (final rule in detail.denyRulesOrEmpty)
                      CodeText(
                        '${rule.match}  ${rule.why}',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                  ],
                ),
              ),
            ),
          ),
          actions: <Widget>[
            TextButton(
              onPressed: () => Navigator.of(context).pop(),
              child: const Text(SC.close),
            ),
          ],
        ),
      ),
    );
  }
}

class _SiteOrderEditor extends StatelessWidget {
  const _SiteOrderEditor({
    required this.state,
    required this.profile,
    required this.isDefault,
  });

  final AppState state;
  final Profile? profile;
  final bool isDefault;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final config = state.config.editable;
    final order = isDefault
        ? config.sources.orderOrEmpty
        : (profile?.sources?.order ?? const <String>[]);
    final remaining = knownSites
        .where((site) => !order.contains(site))
        .toList(growable: false);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(SRules.sitesHint, style: theme.textTheme.bodySmall),
        if (!isDefault && (profile?.sources == null))
          Padding(
            padding: const EdgeInsets.only(top: Tokens.gap2),
            child: Text(
              SRules.sitesInherit,
              style: theme.textTheme.bodySmall?.copyWith(color: Tokens.warning),
            ),
          ),
        const SizedBox(height: Tokens.gap3),
        for (var i = 0; i < order.length; i++)
          Padding(
            padding: const EdgeInsets.only(bottom: Tokens.gap1),
            child: Row(
              children: <Widget>[
                SizedBox(
                  width: 32,
                  child: NumText('${i + 1}', style: theme.textTheme.bodySmall),
                ),
                SizedBox(
                  width: 160,
                  child: Text(
                    siteLabels[order[i]] ?? order[i],
                    style: theme.textTheme.bodyMedium,
                  ),
                ),
                CodeText(order[i], style: theme.textTheme.bodySmall),
                const Spacer(),
                IconButton(
                  tooltip: '上移',
                  visualDensity: VisualDensity.compact,
                  onPressed: i == 0
                      ? null
                      : () => _write(context, _reorder(order, i, i - 1)),
                  icon: const Icon(Icons.arrow_upward, size: 15),
                ),
                IconButton(
                  tooltip: '下移',
                  visualDensity: VisualDensity.compact,
                  onPressed: i == order.length - 1
                      ? null
                      : () => _write(context, _reorder(order, i, i + 1)),
                  icon: const Icon(Icons.arrow_downward, size: 15),
                ),
                IconButton(
                  tooltip: SC.delete,
                  visualDensity: VisualDensity.compact,
                  onPressed: () => _write(
                    context,
                    <String>[...order]..removeAt(i),
                  ),
                  icon: const Icon(Icons.close, size: 15),
                ),
              ],
            ),
          ),
        const SizedBox(height: Tokens.gap2),
        Wrap(
          spacing: Tokens.gap2,
          children: <Widget>[
            for (final site in remaining)
              OutlinedButton(
                onPressed: () => _write(context, <String>[...order, site]),
                child: Text('+ ${siteLabels[site] ?? site}'),
              ),
          ],
        ),
        if (!isDefault && profile?.sources != null) ...<Widget>[
          const SizedBox(height: Tokens.gap3),
          OutlinedButton(
            onPressed: () {
              profile!.sources = null;
              state.config.markDirty();
              state.touch();
            },
            child: const Text(SRules.sitesInheritAction),
          ),
        ],
      ],
    );
  }

  static List<String> _reorder(List<String> order, int from, int to) {
    final list = <String>[...order];
    final item = list.removeAt(from);
    list.insert(to, item);
    return list;
  }

  void _write(BuildContext context, List<String> order) {
    if (isDefault) {
      state.config.editable.sources.order = order;
    } else {
      profile!.sources = ProfileSources(order: order);
    }
    state.config.markDirty();
    state.touch();
  }
}
