import 'package:flutter/material.dart';

import '../core/core_event.dart';
import '../state/app_state.dart';
import 'pages/discover_page.dart';
import 'pages/instances_page.dart';
import 'pages/journal_page.dart';
import 'pages/overview_page.dart';
import 'pages/profiles_page.dart';
import 'pages/settings_page.dart';
import 'pages/targets_page.dart';
import 'strings.dart';
import 'tokens.dart';
import 'widgets/common.dart';

/// 导航图标：与 `S.navLabels` 一一对应。
const List<IconData> _navIcons = <IconData>[
  Icons.dashboard_outlined,
  Icons.dns_outlined,
  Icons.rule_outlined,
  Icons.movie_filter_outlined,
  Icons.auto_awesome_motion_outlined,
  Icons.receipt_long_outlined,
  Icons.settings_outlined,
];

/// 自适应导航壳：窄屏 NavigationBar，宽屏 NavigationRail，断点 900dp。
class AppShell extends StatefulWidget {
  const AppShell({super.key, required this.state});

  final AppState state;

  @override
  State<AppShell> createState() => _AppShellState();
}

class _AppShellState extends State<AppShell> {
  int _index = 0;

  /// 从「实例」页跳到某个实例的订阅列表时用。
  String? _subscriptionInstance;

  void _openSubscriptions(String instance) {
    setState(() {
      _subscriptionInstance = instance;
      _index = 1;
    });
  }

  Widget _page() {
    final state = widget.state;
    switch (_index) {
      case 0:
        return OverviewPage(
          state: state,
          onOpenInstances: () => setState(() => _index = 1),
        );
      case 1:
        return InstancesPage(
          state: state,
          onOpenSubscriptions: _openSubscriptions,
          initialInstance: _subscriptionInstance,
        );
      case 2:
        return ProfilesPage(state: state);
      case 3:
        return TargetsPage(state: state, onOpenDiscover: () => setState(() => _index = 4));
      case 4:
        return DiscoverPage(state: state, onOpenTargets: () => setState(() => _index = 3));
      case 5:
        return JournalPage(state: state);
      default:
        return SettingsPage(
          state: state,
          onWorkspaceChanged: () => setState(() => _index = 0),
        );
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = widget.state;
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 900;
        final body = Column(
          children: <Widget>[
            _TopBar(state: state),
            if (state.banner != null) _BannerBar(state: state),
            Expanded(child: _page()),
          ],
        );
        if (!wide) {
          return Scaffold(
            body: body,
            bottomNavigationBar: NavigationBar(
              selectedIndex: _index,
              height: 60,
              labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
              onDestinationSelected: (index) => setState(() => _index = index),
              destinations: <NavigationDestination>[
                for (var i = 0; i < S.navLabels.length; i++)
                  NavigationDestination(
                    icon: Icon(_navIcons[i], size: 20),
                    label: S.navLabels[i],
                  ),
              ],
            ),
          );
        }
        return Scaffold(
          body: Row(
            children: <Widget>[
              _Rail(
                selected: _index,
                onSelect: (index) => setState(() => _index = index),
              ),
              const VerticalDivider(width: 1),
              Expanded(child: body),
            ],
          ),
        );
      },
    );
  }
}

/// 窄侧栏（200–240px，浅色底，**不做全暗**）。
class _Rail extends StatelessWidget {
  const _Rail({required this.selected, required this.onSelect});

  final int selected;
  final ValueChanged<int> onSelect;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final dark = theme.brightness == Brightness.dark;
    return Container(
      width: 208,
      color: dark ? Tokens.railDark : Tokens.railLight,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Padding(
            padding: const EdgeInsets.fromLTRB(
              Tokens.gap3,
              Tokens.gap4,
              Tokens.gap3,
              Tokens.gap2,
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  S.appTitle,
                  style: theme.textTheme.titleLarge?.copyWith(
                    fontWeight: FontWeight.w700,
                    letterSpacing: -0.4,
                  ),
                ),
                const SizedBox(height: Tokens.gap1),
                Text(
                  S.appSubtitle,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: Tokens.gap2),
          for (var i = 0; i < S.navLabels.length; i++)
            Padding(
              padding: const EdgeInsets.symmetric(
                horizontal: Tokens.gap2,
                vertical: 1,
              ),
              child: _RailItem(
                icon: _navIcons[i],
                label: S.navLabels[i],
                selected: i == selected,
                onTap: () => onSelect(i),
              ),
            ),
        ],
      ),
    );
  }
}

class _RailItem extends StatelessWidget {
  const _RailItem({
    required this.icon,
    required this.label,
    required this.selected,
    required this.onTap,
  });

  final IconData icon;
  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(Tokens.radiusInput),
      child: Container(
        padding: const EdgeInsets.symmetric(
          horizontal: Tokens.gap3,
          vertical: Tokens.gap2,
        ),
        decoration: BoxDecoration(
          color: selected
              ? theme.colorScheme.primary.withValues(alpha: 0.08)
              : Colors.transparent,
          borderRadius: BorderRadius.circular(Tokens.radiusInput),
          border: Border(
            left: BorderSide(
              color: selected ? theme.colorScheme.primary : Colors.transparent,
              width: 2,
            ),
          ),
        ),
        child: Row(
          children: <Widget>[
            Icon(
              icon,
              size: 18,
              color: selected
                  ? theme.colorScheme.primary
                  : theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(width: Tokens.gap3),
            Text(
              label,
              style: theme.textTheme.bodyMedium?.copyWith(
                fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                color: selected ? theme.colorScheme.primary : null,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// 顶栏：工作区、后端、进行中的任务数、全局操作。
class _TopBar extends StatelessWidget {
  const _TopBar({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final running = state.jobs.running.length;
    return Container(
      height: 46,
      decoration: BoxDecoration(
        color: theme.colorScheme.surface,
        border: Border(
          bottom: BorderSide(color: theme.colorScheme.outlineVariant),
        ),
      ),
      padding: const EdgeInsets.symmetric(horizontal: Tokens.gap3),
      child: Row(
        children: <Widget>[
          StatusDot(
            color: state.core.ready ? Tokens.success : Tokens.warning,
            tooltip: state.core.ready ? SC.connected : SC.loadingFailed,
          ),
          const SizedBox(width: Tokens.gap2),
          Flexible(
            child: CodeText(
              state.workspacePath.isEmpty
                  ? SOverview.noWorkspace
                  : state.workspacePath,
              style: theme.textTheme.bodySmall,
            ),
          ),
          const SizedBox(width: Tokens.gap4),
          Text(
            state.backendLabel,
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
          const Spacer(),
          if (running > 0) ...<Widget>[
            NumText(
              '$running',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(width: Tokens.gap1),
            Text(
              SOverview.running,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.primary,
              ),
            ),
            const SizedBox(width: Tokens.gap3),
          ],
          IconButton(
            tooltip: SOverview.refreshAll,
            visualDensity: VisualDensity.compact,
            onPressed: state.core.ready ? () => state.reloadAll() : null,
            icon: const Icon(Icons.refresh, size: 18),
          ),
          IconButton(
            tooltip: '主题：${state.prefs.theme}',
            visualDensity: VisualDensity.compact,
            onPressed: () async {
              state.prefs.theme = switch (state.prefs.theme) {
                'system' => 'light',
                'light' => 'dark',
                _ => 'system',
              };
              await state.prefs.save();
              state.touch();
            },
            icon: const Icon(Icons.contrast, size: 18),
          ),
          IconButton(
            tooltip: SJournal.eventStream,
            visualDensity: VisualDensity.compact,
            onPressed: () => _showEvents(context, state),
            icon: const Icon(Icons.bolt_outlined, size: 18),
          ),
        ],
      ),
    );
  }

  void _showEvents(BuildContext context, AppState state) {
    showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (context) => AnimatedBuilder(
        animation: state,
        builder: (context, _) => SizedBox(
          height: 360,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: Tokens.gap4,
                  vertical: Tokens.gap2,
                ),
                child: Row(
                  children: <Widget>[
                    Text(
                      SJournal.eventStream,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const Spacer(),
                    TextButton(
                      onPressed: state.journal.loadLogs,
                      child: const Text(SJournal.useFallback),
                    ),
                  ],
                ),
              ),
              Expanded(
                child: state.eventLog.isEmpty
                    ? const EmptyState(
                        why: '还没有事件。跑一次同步就能看到进度与日志。',
                        icon: Icons.bolt_outlined,
                      )
                    : ListView.builder(
                        itemCount: state.eventLog.events.length,
                        itemBuilder: (context, index) {
                          final events = state.eventLog.events;
                          final event = events[events.length - 1 - index];
                          return _EventRow(event: event);
                        },
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _EventRow extends StatelessWidget {
  const _EventRow({required this.event});

  final CoreEvent event;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // 上色按 type；`log` 行再按 level 细分 —— 否则一条 error 日志与一条 info 日志
    // 在界面上长得一模一样，而 `log.recent` 兜底视图正是断线后唯一的回看入口。
    //
    // level 比较一律走 `LogLevels` 的小写常量：引擎写进缓冲的是小写（§7.12），
    // 大写比较会永远不命中，而且是静默的。
    final Color color;
    switch (event.type) {
      case CoreEventTypes.jobFailed:
        color = Tokens.danger;
      case CoreEventTypes.jobDone:
        color = Tokens.success;
      case CoreEventTypes.leaseLost:
        color = Tokens.warning;
      default:
        if (event.isError) {
          color = Tokens.danger;
        } else if (event.isWarning) {
          color = Tokens.warning;
        } else {
          color = theme.colorScheme.onSurfaceVariant;
        }
    }
    final text = event.text ??
        switch (event.type) {
          CoreEventTypes.progress =>
            '${event.label ?? '进度'} ${event.done ?? 0}/${event.total ?? 0}',
          CoreEventTypes.instanceProgress =>
            '${event.instanceName ?? ''} ${event.done ?? 0}/${event.total ?? 0}',
          CoreEventTypes.jobStart => '开始 ${event.kind ?? ''}',
          CoreEventTypes.jobDone => '完成 ${event.kind ?? event.jobId ?? ''}',
          CoreEventTypes.jobFailed => '失败 ${event.error?['message'] ?? ''}',
          CoreEventTypes.leaseLost =>
            '租约被接管（pid ${event.holder?['pid'] ?? '?'}）',
          _ => event.type,
        };
    return Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: Tokens.gap4,
        vertical: Tokens.gap1,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          SizedBox(
            width: 96,
            child: CodeText(
              event.type,
              style: theme.textTheme.bodySmall?.copyWith(color: color),
            ),
          ),
          const SizedBox(width: Tokens.gap2),
          Expanded(
            child: Text(text, style: theme.textTheme.bodySmall),
          ),
        ],
      ),
    );
  }
}

/// 一条横幅（租约丢失 / 保存失败这类需要立刻看见的信息）。
class _BannerBar extends StatelessWidget {
  const _BannerBar({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      color: Tokens.warning.withValues(alpha: 0.10),
      padding: const EdgeInsets.symmetric(
        horizontal: Tokens.gap3,
        vertical: Tokens.gap2,
      ),
      child: Row(
        children: <Widget>[
          const Icon(Icons.info_outline, size: 16, color: Tokens.warning),
          const SizedBox(width: Tokens.gap2),
          Expanded(
            child: SelectableText(
              state.banner ?? '',
              maxLines: 3,
              style: theme.textTheme.bodySmall,
            ),
          ),
          TextButton(
            onPressed: () {
              state.banner = null;
              state.touch();
            },
            child: const Text(SC.close),
          ),
        ],
      ),
    );
  }
}
