import 'package:flutter/material.dart' hide DataRow;

import '../../model/job.dart';
import '../../model/plan.dart';
import '../../model/run_entry.dart';
import '../../model/status.dart';
import '../../state/app_state.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';
import '../widgets/config_strip.dart';
import '../widgets/run_row.dart';

/// 概览：当前工作区、各实例健康、租约、最近几轮、一个大按钮「立即同步」。
class OverviewPage extends StatelessWidget {
  const OverviewPage({
    super.key,
    required this.state,
    required this.onOpenInstances,
  });

  final AppState state;
  final VoidCallback onOpenInstances;

  @override
  Widget build(BuildContext context) {
    // 状态条放在 AsyncView **外面**：它必须在「状态还在加载 / 加载失败」时也看得见。
    // 挂在里面就等于「同步加载不出来时，配置有没有问题也不告诉你了」。
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(
            Tokens.gap4,
            Tokens.gap3,
            Tokens.gap4,
            0,
          ),
          // 「保存成功 ≠ 配置可用」：常驻、非 modal，且**概览页也看得见** ——
          // 不能在用户点了「立即同步」之后才告诉他。
          child: ConfigStatusStrip(state: state, onFix: onOpenInstances),
        ),
        Expanded(
          child: AsyncView<StatusResult>(
            state: state.status.status,
            onRetry: () => state.status.load(),
            idleBuilder: (context) => const EmptyState(
              why: '引擎还没连上。先在「设置」页确认工作区与后端。',
              icon: Icons.power_settings_new_outlined,
            ),
            emptyBuilder: (context, why) => EmptyState(
              why: why.isEmpty ? SOverview.noInstances : why,
              actionLabel: SOverview.goInstances,
              onAction: onOpenInstances,
            ),
            builder: (context, status) => ListView(
              padding: const EdgeInsets.all(Tokens.gap4),
              children: <Widget>[
                _Header(state: state),
                const SizedBox(height: Tokens.gap4),
                _WorkspaceCard(state: state, status: status),
                const SizedBox(height: Tokens.gap4),
                _InstanceHealth(
                  state: state,
                  status: status,
                  onOpenInstances: onOpenInstances,
                ),
                const SizedBox(height: Tokens.gap4),
                _LeaseCard(state: state, status: status),
                const SizedBox(height: Tokens.gap4),
                _RunningJobs(state: state),
                const SizedBox(height: Tokens.gap4),
                _RecentRuns(
                  entries: status.runsOrEmpty,
                  nextRunAt: status.nextRunAt,
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: <Widget>[
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(
                S.navLabels.first,
                style: theme.textTheme.titleLarge,
              ),
              const SizedBox(height: Tokens.gap1),
              Text(
                SOverview.instances,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ],
          ),
        ),
        OutlinedButton.icon(
          onPressed: () => state.status.load(),
          icon: const Icon(Icons.refresh, size: 16),
          label: const Text(SC.refresh),
        ),
        const SizedBox(width: Tokens.gap2),
        FilledButton.icon(
          onPressed: state.backend == null
              ? null
              : () async {
                  final jobId = await state.status.runSync();
                  if (!context.mounted) return;
                  ScaffoldMessenger.of(context).showSnackBar(
                    SnackBar(
                      content: Text(
                        jobId == null ? SC.loadingFailed : SOverview.syncStarted,
                      ),
                    ),
                  );
                },
          icon: const Icon(Icons.play_arrow, size: 18),
          label: const Text(SOverview.syncNow),
        ),
      ],
    );
  }
}

class _WorkspaceCard extends StatelessWidget {
  const _WorkspaceCard({required this.state, required this.status});

  final AppState state;
  final StatusResult status;

  @override
  Widget build(BuildContext context) {
    return SectionCard(
      title: SOverview.workspace,
      child: Column(
        children: <Widget>[
          KeyValueRow(
            label: 'config.yaml',
            value: status.configPath.isEmpty ? S.unspecified : status.configPath,
            mono: true,
          ),
          KeyValueRow(
            label: SSettings.dataDir,
            value: status.dataDir.isEmpty ? S.unspecified : status.dataDir,
            mono: true,
          ),
          KeyValueRow(label: SOverview.version, value: status.version),
          KeyValueRow(label: SOverview.platform, value: status.platform),
          KeyValueRow(
            label: SSettings.goVersion,
            value: state.core.current?.goVersion ?? S.unspecified,
            mono: true,
          ),
        ],
      ),
    );
  }
}

class _InstanceHealth extends StatelessWidget {
  const _InstanceHealth({
    required this.state,
    required this.status,
    required this.onOpenInstances,
  });

  final AppState state;
  final StatusResult status;
  final VoidCallback onOpenInstances;

  @override
  Widget build(BuildContext context) {
    final instances = status.instancesOrEmpty;
    if (instances.isEmpty) {
      return SectionCard(
        title: SOverview.instances,
        child: EmptyState(
          why: SOverview.noInstances,
          actionLabel: SOverview.goInstances,
          onAction: onOpenInstances,
        ),
      );
    }
    return SectionCard(
      title: SOverview.instances,
      subtitle: 'status.get 不发任何网络请求；订阅数要在「实例」页点一次「测试连接」才会读到。',
      padding: EdgeInsets.zero,
      child: Column(
        children: <Widget>[
          Padding(
            padding: const EdgeInsets.symmetric(
              horizontal: Tokens.cellX,
              vertical: Tokens.cellY,
            ),
            child: Row(
              children: const <Widget>[
                Th('实例', flex: 2),
                Th('方案', flex: 2),
                Th('站点顺序', flex: 3),
                Th('规则', align: TextAlign.right),
                Th('排除', align: TextAlign.right),
                Th('副 RSS', align: TextAlign.right),
                Th(SInstances.subscriptions, align: TextAlign.right),
              ],
            ),
          ),
          const HairLine(),
          for (final scheme in instances)
            _InstanceHealthRow(scheme: scheme, state: state),
        ],
      ),
    );
  }
}

class _InstanceHealthRow extends StatelessWidget {
  const _InstanceHealthRow({required this.scheme, required this.state});

  final SchemeSummary scheme;
  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final test = state.instances.tests[scheme.instance];
    final label = scheme.profile.isEmpty
        ? SRules.defaultEntry
        : scheme.profile;
    return DataRow(
      cells: <Widget>[
        Cell(
          flex: 2,
          child: Text(scheme.instance, style: theme.textTheme.bodyMedium),
        ),
        Cell(
          flex: 2,
          child: Text(
            label,
            style: theme.textTheme.bodySmall?.copyWith(
              color: scheme.profile.isEmpty
                  ? theme.colorScheme.onSurfaceVariant
                  : theme.colorScheme.primary,
            ),
          ),
        ),
        Cell(
          flex: 3,
          child: Text(
            scheme.siteOrderOrEmpty.isEmpty
                ? S.unspecified
                : scheme.siteOrderOrEmpty.join(' → '),
            style: theme.textTheme.bodySmall,
          ),
        ),
        Cell(
          align: MainAxisAlignment.end,
          child: NumText('${scheme.ruleCount}', style: theme.textTheme.bodySmall),
        ),
        Cell(
          align: MainAxisAlignment.end,
          child: NumText('${scheme.denyCount}', style: theme.textTheme.bodySmall),
        ),
        Cell(
          align: MainAxisAlignment.end,
          child: NumText('${scheme.maxStandby}', style: theme.textTheme.bodySmall),
        ),
        Cell(
          align: MainAxisAlignment.end,
          child: test == null
              ? Text(
                  S.unspecified,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                )
              : NumText(
                  '${test.subscriptionCount}',
                  style: theme.textTheme.bodySmall,
                ),
        ),
      ],
    );
  }
}

class _LeaseCard extends StatelessWidget {
  const _LeaseCard({required this.state, required this.status});

  final AppState state;
  final StatusResult status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final lease = status.lease;
    return SectionCard(
      title: SOverview.lease,
      trailing: Row(
        children: <Widget>[
          StatusDot(
            color: lease.held ? Tokens.warning : Tokens.success,
          ),
          const SizedBox(width: Tokens.gap2),
          Text(
            lease.held ? SOverview.leaseHeld : SOverview.leaseFree,
            style: theme.textTheme.bodySmall,
          ),
        ],
      ),
      child: lease.held
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                KeyValueRow(
                  label: SOverview.leaseHolder,
                  value: 'pid ${lease.pid}  ${lease.note}',
                  mono: true,
                ),
                KeyValueRow(
                  label: SOverview.leaseAge,
                  value: '${lease.ageSeconds.toStringAsFixed(1)} 秒',
                ),
                if (lease.expired)
                  Padding(
                    padding: const EdgeInsets.only(bottom: Tokens.gap2),
                    child: Text(
                      SOverview.leaseExpired,
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: Tokens.warning),
                    ),
                  ),
                Align(
                  alignment: Alignment.centerLeft,
                  child: OutlinedButton(
                    onPressed: () async {
                      final ok = await ConfirmDialog.show(
                        context,
                        title: SOverview.takeover,
                        message: '将抢占 pid ${lease.pid}（${lease.note}）持有的写入权。'
                            '原持有者会立刻停止写入，它的这一轮同步可能中途作废。',
                        confirmLabel: SOverview.takeover,
                      );
                      if (ok) await state.status.takeover();
                    },
                    child: const Text(SOverview.takeover),
                  ),
                ),
              ],
            )
          : Text(
              '没有进程在写这份配置。跑同步时会自动抢一个租约。',
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
    );
  }
}

class _RunningJobs extends StatelessWidget {
  const _RunningJobs({required this.state});

  final AppState state;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final jobs = state.jobs.jobs
        .where((job) => job.isRunning || job.isFailed || job.result != null)
        .take(5)
        .toList(growable: false);
    return SectionCard(
      title: SOverview.running,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          if (state.jobs.leaseConflict) ...<Widget>[
            Container(
              width: double.infinity,
              margin: const EdgeInsets.only(bottom: Tokens.gap3),
              padding: const EdgeInsets.all(Tokens.gap3),
              decoration: BoxDecoration(
                color: Tokens.warning.withValues(alpha: 0.08),
                borderRadius: BorderRadius.circular(Tokens.radiusCard),
                border: Border.all(color: Tokens.warning.withValues(alpha: 0.4)),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      const Icon(Icons.lock_outline, size: 16, color: Tokens.warning),
                      const SizedBox(width: Tokens.gap2),
                      Expanded(
                        child: Text(
                          SOverview.leaseConflictTitle,
                          style: theme.textTheme.bodyMedium?.copyWith(
                            fontWeight: FontWeight.w500,
                          ),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: Tokens.gap1),
                  Text(
                    SOverview.leaseConflictBody,
                    style: theme.textTheme.bodySmall,
                  ),
                  const SizedBox(height: Tokens.gap2),
                  OutlinedButton(
                    onPressed: () async {
                      final lease = state.status.value?.lease;
                      final ok = await ConfirmDialog.show(
                        context,
                        title: SOverview.takeover,
                        message: lease == null || !lease.held
                            ? '将强制接管写入权。'
                            : '将抢占 pid ${lease.pid}（${lease.note}）持有的写入权。'
                                '原持有者会立刻停止写入。',
                        confirmLabel: SOverview.takeover,
                      );
                      if (!ok) return;
                      final taken = await state.status.takeover();
                      if (taken) state.jobs.leaseConflict = false;
                    },
                    child: const Text(SOverview.takeover),
                  ),
                ],
              ),
            ),
          ],
          if (jobs.isEmpty)
            Text(
              SOverview.noRunning,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            )
          else
            for (final job in jobs) _JobRow(job: job, state: state),
        ],
      ),
    );
  }
}

class _JobRow extends StatefulWidget {
  const _JobRow({required this.job, required this.state});

  final JobView job;
  final AppState state;

  @override
  State<_JobRow> createState() => _JobRowState();
}

class _JobRowState extends State<_JobRow> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final job = widget.job;
    final fraction = widget.state.jobs.fraction[job.id];
    final message = widget.state.jobs.messages[job.id] ?? job.message;
    final summary = job.summary();
    final errors = job.resultErrors;
    final plan = job.resultPlan;
    final elapsed = job.elapsed(DateTime.now());

    return Padding(
      padding: const EdgeInsets.only(bottom: Tokens.gap2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              StatusDot(
                color: switch (job.state) {
                  JobState.running => Tokens.info,
                  JobState.done => Tokens.success,
                  JobState.failed => Tokens.danger,
                  _ => theme.colorScheme.onSurfaceVariant,
                },
              ),
              const SizedBox(width: Tokens.gap2),
              CodeText(job.kind, style: theme.textTheme.bodySmall),
              const SizedBox(width: Tokens.gap2),
              Expanded(
                child: Text(message, style: theme.textTheme.bodySmall),
              ),
              if (summary.isNotEmpty)
                NumText(summary, style: theme.textTheme.bodySmall),
              if (elapsed != null) ...<Widget>[
                const SizedBox(width: Tokens.gap3),
                NumText(
                  '${elapsed.inSeconds} s',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                  ),
                ),
              ],
              if (job.isRunning)
                TextButton(
                  onPressed: () => widget.state.jobs.cancel(job.id),
                  child: const Text(SOverview.cancel),
                )
              else if (plan != null && plan.actionsOrEmpty.isNotEmpty)
                TextButton(
                  onPressed: () => setState(() => _expanded = !_expanded),
                  child: Text(SOverview.jobPlanDetail),
                ),
            ],
          ),
          if (fraction != null)
            Padding(
              padding: const EdgeInsets.only(top: Tokens.gap1),
              child: LinearProgressIndicator(value: fraction),
            ),
          if (errors.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(left: Tokens.gap4, top: Tokens.gap1),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    SOverview.jobErrors,
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: Tokens.danger),
                  ),
                  for (final line in errors)
                    SelectableText('· $line', style: theme.textTheme.bodySmall),
                ],
              ),
            ),
          if (_expanded && plan != null)
            Container(
              width: double.infinity,
              margin: const EdgeInsets.only(
                left: Tokens.gap4,
                top: Tokens.gap2,
              ),
              padding: const EdgeInsets.all(Tokens.gap2),
              decoration: BoxDecoration(
                color: theme.colorScheme.surfaceContainerHighest,
                borderRadius: BorderRadius.circular(Tokens.radiusInput),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  for (final action in plan.actionsOrEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: Tokens.gap1),
                      child: Text(
                        '${action.kind} · ${action.title}'
                        '${action.changesOrEmpty.isEmpty ? '' : '（${action.changesOrEmpty.map((c) => c.field).join('、')}）'}',
                        style: theme.textTheme.bodySmall,
                      ),
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

class _RecentRuns extends StatefulWidget {
  const _RecentRuns({required this.entries, this.nextRunAt});

  final List<RunEntry> entries;
  final DateTime? nextRunAt;

  @override
  State<_RecentRuns> createState() => _RecentRunsState();
}

class _RecentRunsState extends State<_RecentRuns> {
  bool _onlyFailures = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final entries = _onlyFailures
        ? widget.entries.where((e) => e.isFailure).toList()
        : widget.entries;
    final hasFailures = widget.entries.any((e) => e.isFailure);

    return SectionCard(
      title: SOverview.recentRuns,
      subtitle: widget.nextRunAt == null
          ? null
          : '${SOverview.nextRun}：${widget.nextRunAt}',
      trailing: hasFailures
          ? FilterChip(
              label: const Text(SJournal.onlyFailures),
              selected: _onlyFailures,
              showCheckmark: false,
              onSelected: (value) => setState(() => _onlyFailures = value),
            )
          : null,
      child: entries.isEmpty
          ? Text(
              widget.entries.isEmpty ? SOverview.noRuns : SJournal.filteredEmpty,
              style: theme.textTheme.bodySmall?.copyWith(
                color: theme.colorScheme.onSurfaceVariant,
              ),
            )
          : Column(
              children: <Widget>[
                for (final entry in entries) RunEntryRow(entry: entry),
              ],
            ),
    );
  }
}
