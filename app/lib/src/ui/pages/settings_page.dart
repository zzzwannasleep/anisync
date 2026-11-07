import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../model/backup.dart';
import '../../model/config.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';
import '../widgets/config_strip.dart';

/// 设置：工作区 / 后端 / 同步默认值 / 代理 / 发现来源 / 全局偏移 / 备份 / 版本。
class SettingsPage extends StatefulWidget {
  const SettingsPage({
    super.key,
    required this.state,
    required this.onWorkspaceChanged,
  });

  final AppState state;
  final VoidCallback onWorkspaceChanged;

  @override
  State<SettingsPage> createState() => _SettingsPageState();
}

class _SettingsPageState extends State<SettingsPage> {
  late final TextEditingController _workspace = TextEditingController(
    text: widget.state.workspacePath,
  );
  late final TextEditingController _dataDir = TextEditingController(
    text: widget.state.dataDir,
  );
  late final TextEditingController _nodeUrl = TextEditingController(
    text: widget.state.prefs.nodeUrl,
  );
  late final TextEditingController _nodeToken = TextEditingController(
    text: widget.state.prefs.nodeToken,
  );
  late final TextEditingController _proxy = TextEditingController(
    text: widget.state.config.editable.proxy,
  );
  late final TextEditingController _listen = TextEditingController(
    text: widget.state.config.editable.listen,
  );
  late final TextEditingController _bgmUser = TextEditingController(
    text: widget.state.config.editable.discover.bgmUser,
  );
  late final TextEditingController _exclude = TextEditingController(
    text: widget.state.config.editable.discover.exclude?.join('\n') ?? '',
  );
  String? _notice;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      widget.state.backups.load();
    });
  }

  @override
  void dispose() {
    _workspace.dispose();
    _dataDir.dispose();
    _nodeUrl.dispose();
    _nodeToken.dispose();
    _proxy.dispose();
    _listen.dispose();
    _bgmUser.dispose();
    _exclude.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = widget.state;
    final config = state.config.editable;
    final discover = config.discover;
    final sync = config.sync;

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
                child: Text(SSettings.title, style: theme.textTheme.titleLarge),
              ),
              if (_notice != null)
                Padding(
                  padding: const EdgeInsets.only(right: Tokens.gap3),
                  child: Text(
                    _notice!,
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: theme.colorScheme.primary),
                  ),
                ),
              FilledButton.icon(
                onPressed: state.config.dirty ? () => _save() : null,
                icon: const Icon(Icons.save_outlined, size: 16),
                label: Text(state.config.dirty ? SC.save : SC.saved),
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
            0,
          ),
          child: ConfigStatusStrip(state: state, dense: true),
        ),
        Expanded(
          child: ListView(
            padding: const EdgeInsets.all(Tokens.gap4),
            children: <Widget>[
              _card(
                title: SSettings.sectionWorkspace,
                subtitle: SSettings.workspaceHint,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    FieldRow(
                      label: SSettings.workspacePath,
                      child: TextField(
                        controller: _workspace,
                        decoration: const InputDecoration(isDense: true),
                      ),
                    ),
                    FieldRow(
                      label: SSettings.dataDir,
                      child: TextField(
                        controller: _dataDir,
                        decoration: const InputDecoration(
                          isDense: true,
                          hintText: '留空 = config.yaml 同级的 data/',
                        ),
                      ),
                    ),
                    Wrap(
                      spacing: Tokens.gap2,
                      runSpacing: Tokens.gap2,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: <Widget>[
                        FilledButton(
                          onPressed: () async {
                            final path = _workspace.text.trim();
                            if (path.isEmpty) {
                              setState(() => _notice = SSettings.workspaceEmpty);
                              return;
                            }
                            final ok = await state.switchWorkspace(
                              path,
                              dataDir: _dataDir.text.trim(),
                            );
                            if (!mounted) return;
                            setState(() => _notice =
                                ok ? SSettings.workspaceApplied : SC.loadingFailed);
                            if (ok) widget.onWorkspaceChanged();
                          },
                          child: const Text(SSettings.applyWorkspace),
                        ),
                        Text(
                          '当前：${state.workspacePath.isEmpty ? S.unspecified : state.workspacePath}',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ],
                    ),
                    if (state.prefs.recentWorkspaces.isNotEmpty) ...<Widget>[
                      const SizedBox(height: Tokens.gap3),
                      Text(
                        SSettings.recentWorkspaces,
                        style: theme.textTheme.bodySmall,
                      ),
                      const SizedBox(height: Tokens.gap1),
                      Wrap(
                        spacing: Tokens.gap2,
                        children: <Widget>[
                          for (final item in state.prefs.recentWorkspaces)
                            OutlinedButton(
                              onPressed: () {
                                _workspace.text = item;
                                setState(() {});
                              },
                              child: Text(item, style: theme.textTheme.bodySmall),
                            ),
                        ],
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionBackend,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Wrap(
                      spacing: Tokens.gap2,
                      runSpacing: Tokens.gap2,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: <Widget>[
                        ChoiceChip(
                          label: const Text(SSettings.backendLocal),
                          selected: state.backendKind == BackendKind.ffi,
                          showCheckmark: false,
                          onSelected: (_) => state.useLocalEngine(),
                        ),
                        ChoiceChip(
                          label: const Text(SSettings.backendNode),
                          selected: state.backendKind == BackendKind.node,
                          showCheckmark: false,
                          onSelected: (_) => state.useNode(
                            _nodeUrl.text.trim(),
                            _nodeToken.text.trim(),
                          ),
                        ),
                        StatusDot(
                          color: state.core.ready ? Tokens.success : Tokens.warning,
                        ),
                        Text(
                          state.core.ready ? SC.connected : SC.loadingFailed,
                          style: theme.textTheme.bodySmall,
                        ),
                      ],
                    ),
                    const SizedBox(height: Tokens.gap3),
                    FieldRow(
                      label: SSettings.nodeUrl,
                      child: TextField(
                        controller: _nodeUrl,
                        decoration: const InputDecoration(
                          isDense: true,
                          hintText: 'http://127.0.0.1:51837',
                        ),
                      ),
                    ),
                    FieldRow(
                      label: SSettings.nodeToken,
                      hint: SSettings.nodeTokenHint,
                      child: TextField(
                        controller: _nodeToken,
                        obscureText: true,
                        decoration: const InputDecoration(isDense: true),
                      ),
                    ),
                    OutlinedButton(
                      onPressed: () => state.useNode(
                        _nodeUrl.text.trim(),
                        _nodeToken.text.trim(),
                      ),
                      child: const Text(SSettings.connect),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionSync,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    FieldRow(
                      label: SSettings.interval,
                      hint: SSettings.intervalHint,
                      child: SizedBox(
                        width: 160,
                        child: TextFormField(
                          initialValue: '${sync.intervalMinutes}',
                          decoration: const InputDecoration(isDense: true),
                          onChanged: (value) {
                            final parsed = int.tryParse(value.trim());
                            if (parsed != null) {
                              sync.intervalMinutes = parsed;
                              state.config.markDirty();
                              state.touch();
                            }
                          },
                        ),
                      ),
                    ),
                    SwitchListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      value: sync.preview,
                      onChanged: (value) {
                        sync.preview = value;
                        state.config.markDirty();
                        state.touch();
                      },
                      title: Text(
                        SSettings.preview,
                        style: theme.textTheme.bodyMedium,
                      ),
                    ),
                    SwitchListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      value: sync.addMissing,
                      onChanged: (value) {
                        sync.addMissing = value;
                        state.config.markDirty();
                        state.touch();
                      },
                      title: Text(
                        SSettings.addMissing,
                        style: theme.textTheme.bodyMedium,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionOffsets,
                subtitle: SSettings.offsetsHint,
                child: Column(
                  children: <Widget>[
                    TriStateIntField(
                      label: '季度 season',
                      value: sync.season,
                      onChanged: (value) {
                        sync.season = value;
                        state.config.markDirty();
                        state.touch();
                      },
                    ),
                    const SizedBox(height: Tokens.gap2),
                    TriStateIntField(
                      label: '主 RSS 偏移 offset',
                      value: sync.offset,
                      onChanged: (value) {
                        sync.offset = value;
                        state.config.markDirty();
                        state.touch();
                      },
                    ),
                    const SizedBox(height: Tokens.gap2),
                    TriStateIntField(
                      label: '副 RSS 偏移 standbyOffset',
                      value: sync.standbyOffset,
                      onChanged: (value) {
                        sync.standbyOffset = value;
                        state.config.markDirty();
                        state.touch();
                      },
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionNetwork,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    FieldRow(
                      label: SSettings.proxy,
                      hint: SSettings.proxyHint,
                      child: TextField(
                        controller: _proxy,
                        decoration: const InputDecoration(
                          isDense: true,
                          hintText: 'http://127.0.0.1:7890',
                        ),
                        onChanged: (value) {
                          config.proxy = value;
                          state.config.markDirty();
                        },
                      ),
                    ),
                    FieldRow(
                      label: SSettings.listen,
                      hint: SSettings.listenHint,
                      child: TextField(
                        controller: _listen,
                        decoration: const InputDecoration(isDense: true),
                        onChanged: (value) {
                          config.listen = value;
                          state.config.markDirty();
                        },
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionDiscover,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    SwitchListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      value: discover.enabled,
                      onChanged: (value) {
                        discover.enabled = value;
                        state.config.markDirty();
                        state.touch();
                      },
                      title: Text('打开自动发现', style: theme.textTheme.bodyMedium),
                    ),
                    FieldRow(
                      label: SSettings.discoverSources,
                      child: Wrap(
                        spacing: Tokens.gap2,
                        children: <Widget>[
                          for (final source in discoverSources)
                            FilterChip(
                              label: Text(discoverSourceLabels[source] ?? source),
                              selected:
                                  discover.sourcesOrEmpty.contains(source),
                              showCheckmark: false,
                              onSelected: (selected) {
                                final current = <String>[
                                  ...discover.sourcesOrEmpty,
                                ];
                                if (selected) {
                                  if (!current.contains(source)) {
                                    current.add(source);
                                  }
                                } else {
                                  current.remove(source);
                                }
                                discover.sources = current;
                                state.config.markDirty();
                                state.touch();
                              },
                            ),
                        ],
                      ),
                    ),
                    FieldRow(
                      label: SSettings.bgmUser,
                      child: TextField(
                        controller: _bgmUser,
                        decoration: const InputDecoration(isDense: true),
                        onChanged: (value) {
                          discover.bgmUser = value;
                          state.config.markDirty();
                        },
                      ),
                    ),
                    FieldRow(
                      label: SSettings.discoverDays,
                      child: SizedBox(
                        width: 160,
                        child: TextFormField(
                          initialValue: '${discover.days}',
                          decoration: const InputDecoration(isDense: true),
                          onChanged: (value) {
                            final parsed = int.tryParse(value.trim());
                            if (parsed != null) {
                              discover.days = parsed;
                              state.config.markDirty();
                            }
                          },
                        ),
                      ),
                    ),
                    FieldRow(
                      label: SSettings.discoverLimit,
                      child: SizedBox(
                        width: 160,
                        child: TextFormField(
                          initialValue: '${discover.limit}',
                          decoration: const InputDecoration(isDense: true),
                          onChanged: (value) {
                            final parsed = int.tryParse(value.trim());
                            if (parsed != null) {
                              discover.limit = parsed;
                              state.config.markDirty();
                            }
                          },
                        ),
                      ),
                    ),
                    TriStateBoolField(
                      label: SSettings.discoverEnable,
                      value: discover.enable,
                      onChanged: (value) {
                        discover.enable = value;
                        state.config.markDirty();
                        state.touch();
                      },
                    ),
                    const SizedBox(height: Tokens.gap2),
                    TriStateBoolField(
                      label: SSettings.discoverJapanOnly,
                      value: discover.japanOnly,
                      unsetLabel: STri.japanOnlyUnset,
                      onChanged: (value) {
                        discover.japanOnly = value;
                        state.config.markDirty();
                        state.touch();
                      },
                    ),
                    const SizedBox(height: Tokens.gap2),
                    FieldRow(
                      label: 'exclude',
                      hint: SSettings.excludeHint,
                      child: TextField(
                        controller: _exclude,
                        maxLines: 3,
                        decoration: const InputDecoration(isDense: true),
                        onChanged: (value) {
                          final lines = value
                              .split('\n')
                              .map((line) => line.trim())
                              .where((line) => line.isNotEmpty)
                              .toList(growable: false);
                          discover.exclude = lines.isEmpty ? null : lines;
                          state.config.markDirty();
                        },
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionBackup,
                child: AnimatedBuilder(
                  animation: state,
                  builder: (context, _) => Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Row(
                        children: <Widget>[
                          FilledButton.icon(
                            onPressed: () async {
                              final result = await state.backups.create();
                              if (!context.mounted) return;
                              ScaffoldMessenger.of(context).showSnackBar(
                                SnackBar(
                                  content: Text(
                                    result == null
                                        ? SC.loadingFailed
                                        : '已创建备份：${result.instancesOrEmpty.length} 个实例 / ${result.total} 条订阅',
                                  ),
                                ),
                              );
                            },
                            icon: const Icon(Icons.archive_outlined, size: 16),
                            label: const Text(SSettings.backupCreate),
                          ),
                          const SizedBox(width: Tokens.gap2),
                          OutlinedButton.icon(
                            onPressed: () => state.backups.load(),
                            icon: const Icon(Icons.refresh, size: 16),
                            label: const Text(SC.refresh),
                          ),
                        ],
                      ),
                      const SizedBox(height: Tokens.gap3),
                      AsyncView<BackupListResult>(
                        state: state.backups.backups,
                        skeleton: const SkeletonRows(rows: 3, height: 28),
                        emptyBuilder: (context, why) =>
                            Text(SSettings.backupEmpty, style: theme.textTheme.bodySmall),
                        builder: (context, result) => Column(
                          children: <Widget>[
                            for (final entry in result.backupsOrEmpty)
                              _BackupRow(
                                entry: entry,
                                onRestore: () => _restore(entry),
                              ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: Tokens.gap4),
              _card(
                title: SSettings.sectionInfo,
                child: Column(
                  children: <Widget>[
                    KeyValueRow(
                      label: SOverview.version,
                      value: state.core.current?.version ?? S.unspecified,
                    ),
                    KeyValueRow(
                      label: SSettings.schemaVersion,
                      value: '${state.core.current?.schema ?? 0}',
                    ),
                    KeyValueRow(
                      label: SOverview.platform,
                      value: state.core.current?.platform ?? S.unspecified,
                    ),
                    KeyValueRow(
                      label: SSettings.goVersion,
                      value: state.core.current?.goVersion ?? S.unspecified,
                      mono: true,
                    ),
                    KeyValueRow(
                      label: SSettings.libraryPath,
                      value: state.core.libraryPath.isEmpty
                          ? SSettings.libraryPathMissing
                          : state.core.libraryPath,
                      mono: true,
                    ),
                    KeyValueRow(
                      label: SSettings.opsCatalogue,
                      value: state.core.catalogue.valueOrNull == null
                          ? S.unspecified
                          : '${state.core.catalogue.valueOrNull!.opsOrEmpty.length} 个 op',
                    ),
                    const SizedBox(height: Tokens.gap2),
                    Wrap(
                      spacing: Tokens.gap2,
                      runSpacing: Tokens.gap2,
                      children: <Widget>[
                        OutlinedButton.icon(
                          onPressed: () async {
                            final sample = await state.config.sample();
                            if (!context.mounted || sample == null) return;
                            await showDialog<void>(
                              context: context,
                              builder: (context) => AlertDialog(
                                title: const Text(SSettings.showSample),
                                content: SizedBox(
                                  width: 720,
                                  height: 480,
                                  child: SingleChildScrollView(
                                    child: SelectableText(
                                      sample.yaml,
                                      style: theme.textTheme.bodySmall,
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
                            );
                          },
                          icon: const Icon(Icons.description_outlined, size: 16),
                          label: const Text(SSettings.showSample),
                        ),
                        const SizedBox(width: Tokens.gap2),
                        OutlinedButton.icon(
                          onPressed: () async {
                            final current = state.config.value;
                            if (current == null) return;
                            final text = const JsonEncoder.withIndent('  ')
                                .convert(current.toJson());
                            await Clipboard.setData(ClipboardData(text: text));
                            if (!context.mounted) return;
                            ScaffoldMessenger.of(context).showSnackBar(
                              const SnackBar(content: Text(SC.copied)),
                            );
                          },
                          icon: const Icon(Icons.data_object, size: 16),
                          label: const Text(SSettings.jsonRoundTrip),
                        ),
                      ],
                    ),
                  ],
                ),
              ),              const SizedBox(height: Tokens.gap4),
              ClientDiagnosticsCard(state: state),
              const SizedBox(height: Tokens.gap6),
            ],
          ),
        ),
      ],
    );
  }

  Widget _card({
    required String title,
    String? subtitle,
    required Widget child,
  }) =>
      SectionCard(title: title, subtitle: subtitle, child: child);

  Future<void> _save() async {
    final ok = await widget.state.config.save();
    if (!mounted) return;
    setState(() => _notice = ok ? SC.saved : SC.loadingFailed);
  }

  Future<void> _restore(BackupEntry entry) async {
    final state = widget.state;
    final ok = await ConfirmDialog.show(
      context,
      title: SSettings.backupRestore,
      message: SSettings.backupRestoreConfirm
          .replaceFirst('%s', entry.file)
          .replaceFirst('%d', '${entry.instancesOrEmpty.length}')
          .replaceFirst('%d', '${entry.total}'),
      confirmLabel: SSettings.backupRestore,
    );
    if (!ok) return;
    await state.backups.restore(entry.file);
  }
}

class _BackupRow extends StatelessWidget {
  const _BackupRow({required this.entry, required this.onRestore});

  final BackupEntry entry;
  final VoidCallback onRestore;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: Tokens.gap2),
      child: Row(
        children: <Widget>[
          SizedBox(
            width: 260,
            child: CodeText(entry.file, style: theme.textTheme.bodySmall),
          ),
          SizedBox(
            width: 160,
            child: CodeText(
              entry.createdAt?.toLocal().toString().substring(0, 19) ??
                  S.unspecified,
              style: theme.textTheme.bodySmall,
            ),
          ),
          SizedBox(
            width: 96,
            child: NumText(
              '${entry.instancesOrEmpty.length} 实例',
              style: theme.textTheme.bodySmall,
            ),
          ),
          SizedBox(
            width: 80,
            child: NumText(
              '${entry.total} 条',
              style: theme.textTheme.bodySmall,
            ),
          ),
          const Spacer(),
          OutlinedButton(
            onPressed: onRestore,
            child: const Text(SSettings.backupRestore),
          ),
        ],
      ),
    );
  }
}
