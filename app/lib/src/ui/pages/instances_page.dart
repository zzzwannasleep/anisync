import 'package:flutter/material.dart';

import '../../model/config.dart';
import '../../model/instance_view.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';
import 'subscriptions_page.dart';

/// 实例：增删改 + 测试连接 + 进入订阅列表。
class InstancesPage extends StatefulWidget {
  const InstancesPage({
    super.key,
    required this.state,
    required this.onOpenSubscriptions,
    this.initialInstance,
  });

  final AppState state;
  final ValueChanged<String> onOpenSubscriptions;
  final String? initialInstance;

  @override
  State<InstancesPage> createState() => _InstancesPageState();
}

class _InstancesPageState extends State<InstancesPage> {
  String? _openInstance;

  @override
  void initState() {
    super.initState();
    _openInstance = widget.initialInstance;
  }

  @override
  void didUpdateWidget(covariant InstancesPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.initialInstance != oldWidget.initialInstance) {
      _openInstance = widget.initialInstance;
    }
  }

  @override
  Widget build(BuildContext context) {
    final open = _openInstance;
    if (open != null && open.isNotEmpty) {
      return SubscriptionsPage(
        state: widget.state,
        instance: open,
        onBack: () => setState(() => _openInstance = null),
      );
    }
    return _InstanceList(
      state: widget.state,
      onOpenSubscriptions: (name) {
        widget.onOpenSubscriptions(name);
        setState(() => _openInstance = name);
      },
    );
  }
}

class _InstanceList extends StatelessWidget {
  const _InstanceList({required this.state, required this.onOpenSubscriptions});

  final AppState state;
  final ValueChanged<String> onOpenSubscriptions;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final config = state.config.editable;
    final instances = config.instancesOrEmpty;
    final views = <String, InstanceView>{
      for (final view in state.config.instanceViews) view.name: view,
    };

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
                child: Text(
                  SInstances.title,
                  style: theme.textTheme.titleLarge,
                ),
              ),
              OutlinedButton.icon(
                onPressed: () => state.instances.load(),
                icon: const Icon(Icons.refresh, size: 16),
                label: const Text(SC.refresh),
              ),
              const SizedBox(width: Tokens.gap2),
              FilledButton.icon(
                onPressed: () => _edit(context, null),
                icon: const Icon(Icons.add, size: 18),
                label: const Text(SInstances.addInstance),
              ),
            ],
          ),
        ),
        const HairLine(),
        Expanded(
          child: instances.isEmpty
              ? EmptyState(
                  why: SInstances.noInstances,
                  icon: Icons.dns_outlined,
                  actionLabel: SInstances.addInstance,
                  onAction: () => _edit(context, null),
                )
              : ListView(
                  padding: const EdgeInsets.all(Tokens.gap4),
                  children: <Widget>[
                    for (final instance in instances)
                      Padding(
                        padding: const EdgeInsets.only(bottom: Tokens.gap3),
                        child: _InstanceCard(
                          state: state,
                          instance: instance,
                          view: views[instance.name],
                          onEdit: () => _edit(context, instance),
                          onOpenSubscriptions: () =>
                              onOpenSubscriptions(instance.name),
                        ),
                      ),
                  ],
                ),
        ),
      ],
    );
  }

  Future<void> _edit(BuildContext context, Instance? existing) async {
    final config = state.config.editable;
    final result = await showDialog<Instance>(
      context: context,
      builder: (context) => _InstanceEditor(
        instance: existing,
        view: existing == null
            ? null
            : state.config.instanceViews
                .where((view) => view.name == existing.name)
                .cast<InstanceView?>()
                .firstWhere((view) => true, orElse: () => null),
        profiles: config.profilesOrEmpty.map((p) => p.name).toList(),
      ),
    );
    if (result == null) return;
    final list = <Instance>[...config.instancesOrEmpty];
    final index = existing == null
        ? -1
        : list.indexWhere((item) => item.name == existing.name);
    if (index >= 0) {
      list[index] = result;
    } else {
      list.add(result);
    }
    config.instances = list;
    state.config.markDirty();
    final ok = await state.config.save();
    if (!context.mounted) return;
    if (ok) {
      await state.instances.load();
      await state.status.load();
    }
  }
}

class _InstanceCard extends StatelessWidget {
  const _InstanceCard({
    required this.state,
    required this.instance,
    required this.view,
    required this.onEdit,
    required this.onOpenSubscriptions,
  });

  final AppState state;
  final Instance instance;
  final InstanceView? view;
  final VoidCallback onEdit;
  final VoidCallback onOpenSubscriptions;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final test = state.instances.tests[instance.name];
    final testing = state.instances.testing.contains(instance.name);
    final scheme = view?.scheme;
    final profileLabel = view?.profileLabel.isEmpty ?? true
        ? (instance.profile.isEmpty ? SRules.defaultEntry : instance.profile)
        : view!.profileLabel;

    return SectionCard(
      title: instance.name.isEmpty ? instance.baseUrl : instance.name,
      subtitle: instance.baseUrl,
      trailing: Row(
        children: <Widget>[
          StatusDot(
            color: !instance.isEnabled
                ? theme.colorScheme.outline
                : (test == null
                    ? theme.colorScheme.outline
                    : (test.ok ? Tokens.success : Tokens.danger)),
            tooltip: test == null
                ? S.unspecified
                : (test.ok ? SInstances.testOk : SInstances.testFailed),
          ),
          const SizedBox(width: Tokens.gap3),
          OutlinedButton(
            onPressed: testing ? null : () => state.instances.test(instance.name),
            child: Text(testing ? SInstances.testing : SInstances.test),
          ),
          const SizedBox(width: Tokens.gap2),
          OutlinedButton(
            onPressed: onOpenSubscriptions,
            child: const Text(SInstances.openSubscriptions),
          ),
          const SizedBox(width: Tokens.gap2),
          IconButton(
            tooltip: SInstances.editInstance,
            onPressed: onEdit,
            icon: const Icon(Icons.edit_outlined, size: 18),
          ),
        ],
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          KeyValueRow(
            label: SInstances.apiKey,
            value: instance.apiKey.isEmpty
                ? S.unspecified
                : _mask(instance.apiKey),
            mono: true,
          ),
          KeyValueRow(label: SInstances.profile, value: profileLabel),
          KeyValueRow(
            label: SInstances.scheme,
            value: scheme == null
                ? S.unspecified
                : '${scheme.siteOrderOrEmpty.join(' → ')} · '
                    '规则 ${scheme.ruleCount} · 排除 ${scheme.denyCount} · '
                    '副 RSS ${scheme.maxStandby}',
          ),
          if (test != null) ...<Widget>[
            const SizedBox(height: Tokens.gap1),
            if (test.ok)
              KeyValueRow(
                label: SInstances.latency,
                value: '${test.latencyMs} ms',
                mono: true,
              ),
            if (test.ok)
              KeyValueRow(
                label: SInstances.subscriptions,
                value: '${test.subscriptionCount}',
                mono: true,
              ),
            if (!test.ok && (test.error ?? '').isNotEmpty)
              Row(
                children: <Widget>[
                  const Icon(Icons.error_outline, size: 16, color: Tokens.danger),
                  const SizedBox(width: Tokens.gap2),
                  Expanded(
                    child: SelectableText(
                      test.error!,
                      maxLines: 4,
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: Tokens.danger),
                    ),
                  ),
                ],
              ),
          ],
          if (!instance.isEnabled)
            Padding(
              padding: const EdgeInsets.only(top: Tokens.gap2),
              child: Text(
                SSubs.instanceDisabledHint,
                style: theme.textTheme.bodySmall?.copyWith(color: Tokens.warning),
              ),
            ),
        ],
      ),
    );
  }

  static String _mask(String key) {
    if (key.length < 10) return '••••••';
    return '${key.substring(0, 6)}…${key.substring(key.length - 2)}';
  }
}

/// 实例编辑表单。**写的是 `Config.instances[].apiKey`（原文），不是视图里的遮蔽值。**
class _InstanceEditor extends StatefulWidget {
  const _InstanceEditor({
    required this.instance,
    required this.view,
    required this.profiles,
  });

  final Instance? instance;
  final InstanceView? view;
  final List<String> profiles;

  @override
  State<_InstanceEditor> createState() => _InstanceEditorState();
}

class _InstanceEditorState extends State<_InstanceEditor> {
  late final TextEditingController _name =
      TextEditingController(text: widget.instance?.name ?? '');
  late final TextEditingController _baseUrl =
      TextEditingController(text: widget.instance?.baseUrl ?? '');
  late final TextEditingController _apiKey =
      TextEditingController(text: widget.instance?.apiKey ?? '');
  late String _profile = widget.instance?.profile ?? '';
  late bool? _enabled = widget.instance?.enabled;
  bool _reveal = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _baseUrl.dispose();
    _apiKey.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final masked = widget.view?.apiKey ?? '';
    return AlertDialog(
      title: Text(
        widget.instance == null ? SInstances.addInstance : SInstances.editInstance,
      ),
      content: SizedBox(
        width: 560,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              FieldRow(
                label: SInstances.name,
                child: TextField(
                  controller: _name,
                  decoration: const InputDecoration(hintText: '例如：追番库'),
                ),
              ),
              FieldRow(
                label: SInstances.baseUrl,
                child: TextField(
                  controller: _baseUrl,
                  decoration: const InputDecoration(
                    hintText: 'https://ass.example.com/',
                  ),
                ),
              ),
              FieldRow(
                label: SInstances.apiKey,
                hint: SInstances.apiKeyHint,
                child: Row(
                  children: <Widget>[
                    Expanded(
                      child: TextField(
                        controller: _apiKey,
                        obscureText: !_reveal,
                        decoration: InputDecoration(
                          hintText: masked.isEmpty
                              ? 'ANI-RSS → 设置 → 登录 → API Key'
                              : masked,
                        ),
                      ),
                    ),
                    const SizedBox(width: Tokens.gap2),
                    OutlinedButton(
                      onPressed: () => setState(() => _reveal = !_reveal),
                      child: Text(_reveal ? SInstances.hideKey : SInstances.showKey),
                    ),
                  ],
                ),
              ),
              FieldRow(
                label: SInstances.profile,
                hint: SInstances.profileHint,
                child: DropdownButtonFormField<String>(
                  initialValue: _profile,
                  items: <DropdownMenuItem<String>>[
                    const DropdownMenuItem<String>(
                      value: '',
                      child: Text(SRules.defaultEntry),
                    ),
                    for (final name in widget.profiles)
                      DropdownMenuItem<String>(value: name, child: Text(name)),
                  ],
                  onChanged: (value) => setState(() => _profile = value ?? ''),
                ),
              ),
              TriStateBoolField(
                label: SInstances.enabled,
                value: _enabled,
                unsetLabel: STri.enabledUnset,
                onChanged: (value) => setState(() => _enabled = value),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(top: Tokens.gap2),
                  child: Text(
                    _error!,
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: Tokens.danger),
                  ),
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
          onPressed: () {
            final name = _name.text.trim();
            final baseUrl = _baseUrl.text.trim();
            if (name.isEmpty && baseUrl.isEmpty) {
              setState(() => _error = '名称与 baseUrl 至少要填一个');
              return;
            }
            if (baseUrl.isNotEmpty && !looksLikeUrl(baseUrl)) {
              setState(() => _error = 'baseUrl 必须以 http:// 或 https:// 开头');
              return;
            }
            final apiKey = _apiKey.text.trim();
            final instance = Instance(
              name: name,
              baseUrl: baseUrl,
              apiKey: apiKey.isEmpty
                  ? (widget.instance?.apiKey ?? '')
                  : apiKey,
              profile: _profile,
              enabled: _enabled,
            );
            Navigator.of(context).pop(instance);
          },
          child: const Text(SC.save),
        ),
      ],
    );
  }
}

/// baseUrl 格式的即时校验（保存前就能看出来，不用等引擎报错）。
bool looksLikeUrl(String value) {
  final trimmed = value.trim();
  return trimmed.startsWith('http://') || trimmed.startsWith('https://');
}
