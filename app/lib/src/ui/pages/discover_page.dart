import 'package:flutter/material.dart';

import '../../model/season_item.dart';
import '../../state/app_state.dart';
import '../dialogs/bgm_search_dialog.dart';
import '../strings.dart';
import '../tokens.dart';
import '../widgets/common.dart';

/// 本季新番：封面网格 + 已添加标记 + 勾选后加入 targets + 手工「添加番剧」闭环。
class DiscoverPage extends StatelessWidget {
  const DiscoverPage({
    super.key,
    required this.state,
    required this.onOpenTargets,
  });

  final AppState state;
  final VoidCallback onOpenTargets;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final discover = state.discover;

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
              Text(SDiscover.title, style: theme.textTheme.titleLarge),
              FilterChip(
                label: const Text(SDiscover.onlyJapan),
                selected: discover.japanOnly,
                showCheckmark: false,
                onSelected: (value) => discover.load(japanOnly: value),
              ),
              if (discover.japanOnlyOverride != null)
                Text(
                  SDiscover.japanOnlyChanged,
                  style: theme.textTheme.bodySmall?.copyWith(color: Tokens.warning),
                ),
              OutlinedButton.icon(
                onPressed: () => BgmSearchDialog.show(context, state),
                icon: const Icon(Icons.search, size: 16),
                label: const Text(SDiscover.addDialog),
              ),
              OutlinedButton.icon(
                onPressed: () => discover.load(force: true),
                icon: const Icon(Icons.refresh, size: 16),
                label: const Text(SDiscover.refresh),
              ),
              FilledButton.icon(
                onPressed: discover.selected.isEmpty
                    ? null
                    : () async {
                        final added = await discover.applySelected();
                        if (!context.mounted) return;
                        ScaffoldMessenger.of(context).showSnackBar(
                          SnackBar(
                            content: Text(
                              SDiscover.resultTargets
                                  .replaceFirst('%d', '$added'),
                            ),
                          ),
                        );
                      },
                icon: const Icon(Icons.playlist_add, size: 18),
                label: Text(
                  discover.selected.isEmpty
                      ? SDiscover.addToTargets
                      : SDiscover.selectedCount
                          .replaceFirst('%d', '${discover.selected.length}'),
                ),
              ),
            ],
          ),
        ),
        const HairLine(),
        Expanded(
          child: AsyncView<DiscoverListResult>(
            state: discover.list,
            onRetry: () => discover.load(),
            skeleton: const _GridSkeleton(),
            emptyBuilder: (context, why) => EmptyState(
              why: SDiscover.empty,
              icon: Icons.auto_awesome_motion_outlined,
              actionLabel: SDiscover.refresh,
              onAction: () => discover.load(force: true),
            ),
            builder: (context, result) => Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                _InfoStrip(result: result),
                const HairLine(),
                Expanded(
                  child: GridView.builder(
                    padding: const EdgeInsets.all(Tokens.gap4),
                    gridDelegate:
                        const SliverGridDelegateWithMaxCrossAxisExtent(
                      maxCrossAxisExtent: 168,
                      mainAxisSpacing: Tokens.gap3,
                      crossAxisSpacing: Tokens.gap3,
                      childAspectRatio: 0.58,
                    ),
                    itemCount: result.itemsOrEmpty.length,
                    itemBuilder: (context, index) {
                      final item = result.itemsOrEmpty[index];
                      return _SeasonCard(
                        item: item,
                        selected: discover.selected.contains(item.bgmId),
                        onToggle: () => discover.toggle(item.bgmId),
                        onOpenTargets: onOpenTargets,
                      );
                    },
                  ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// 边角信息条：filtered / unmatched / instanceError / warnings / sources。
class _InfoStrip extends StatelessWidget {
  const _InfoStrip({required this.result});

  final DiscoverListResult result;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final lines = <Widget>[];

    if (result.filtered > 0) {
      lines.add(_InfoItem(
        icon: Icons.filter_alt_outlined,
        color: Tokens.info,
        text: SDiscover.filtered.replaceFirst('%d', '${result.filtered}'),
      ));
    }
    if ((result.unmatched ?? 0) > 0) {
      lines.add(_InfoItem(
        icon: Icons.help_outline,
        color: Tokens.warning,
        text: SDiscover.unmatched.replaceFirst('%d', '${result.unmatched}'),
      ));
    }
    if (result.instanceError != null && result.instanceError!.isNotEmpty) {
      lines.add(_InfoItem(
        icon: Icons.cloud_off_outlined,
        color: Tokens.danger,
        text: SDiscover.instanceError
            .replaceFirst('%s', result.instanceError!),
      ));
    }
    for (final warning in result.warningsOrEmpty) {
      lines.add(_InfoItem(
        icon: Icons.warning_amber_outlined,
        color: Tokens.warning,
        text: SDiscover.warnings.replaceFirst('%s', warning),
      ));
    }
    lines.add(_InfoItem(
      icon: Icons.source_outlined,
      color: theme.colorScheme.onSurfaceVariant,
      text: SDiscover.sources.replaceFirst(
        '%s',
        result.source.isEmpty ? S.unspecified : result.source,
      ),
    ));

    return Padding(
      padding: const EdgeInsets.symmetric(
        horizontal: Tokens.gap4,
        vertical: Tokens.gap2,
      ),
      child: Wrap(
        spacing: Tokens.gap4,
        runSpacing: Tokens.gap1,
        children: lines,
      ),
    );
  }
}

class _InfoItem extends StatelessWidget {
  const _InfoItem({
    required this.icon,
    required this.color,
    required this.text,
  });

  final IconData icon;
  final Color color;
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Icon(icon, size: 14, color: color),
        const SizedBox(width: Tokens.gap1),
        Text(text, style: theme.textTheme.bodySmall),
      ],
    );
  }
}

class _SeasonCard extends StatefulWidget {
  const _SeasonCard({
    required this.item,
    required this.selected,
    required this.onToggle,
    required this.onOpenTargets,
  });

  final SeasonItem item;
  final bool selected;
  final VoidCallback onToggle;
  final VoidCallback onOpenTargets;

  @override
  State<_SeasonCard> createState() => _SeasonCardState();
}

class _SeasonCardState extends State<_SeasonCard> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final item = widget.item;
    return MouseRegion(
      onEnter: (_) => setState(() => _hovered = true),
      onExit: (_) => setState(() => _hovered = false),
      child: InkWell(
        onTap: widget.item.added ? widget.onOpenTargets : widget.onToggle,
        borderRadius: BorderRadius.circular(Tokens.radiusCard),
        child: AnimatedContainer(
          duration: Tokens.hoverDuration,
          decoration: BoxDecoration(
            color: theme.colorScheme.surface,
            borderRadius: BorderRadius.circular(Tokens.radiusCard),
            border: Border.all(
              color: widget.selected
                  ? theme.colorScheme.primary
                  : theme.colorScheme.outline,
              width: widget.selected ? 2 : 1,
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Expanded(
                child: ClipRRect(
                  borderRadius: const BorderRadius.vertical(
                    top: Radius.circular(Tokens.radiusCard),
                  ),
                  child: SizedBox(
                    width: double.infinity,
                    child: item.cover.isEmpty
                        ? Container(
                            color: theme.colorScheme.surfaceContainerHighest,
                            child: Icon(
                              Icons.image_not_supported_outlined,
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          )
                        : Image.network(
                            item.cover,
                            fit: BoxFit.cover,
                            errorBuilder: (context, error, stack) => Container(
                              color: theme.colorScheme.surfaceContainerHighest,
                              child: const Icon(Icons.broken_image_outlined),
                            ),
                          ),
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.all(Tokens.gap2),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      item.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: theme.textTheme.bodySmall?.copyWith(
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: Tokens.gap1),
                    Row(
                      children: <Widget>[
                        if (item.added)
                          Text(
                            SDiscover.added,
                            style: theme.textTheme.bodySmall
                                ?.copyWith(color: Tokens.success),
                          )
                        else if (widget.selected)
                          Text(
                            '已选',
                            style: theme.textTheme.bodySmall
                                ?.copyWith(color: theme.colorScheme.primary),
                          )
                        else if (_hovered)
                          Text(
                            SDiscover.notAdded,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: theme.colorScheme.onSurfaceVariant,
                            ),
                          ),
                        const Spacer(),
                        if (item.score > 0)
                          NumText(
                            item.score.toStringAsFixed(1),
                            style: theme.textTheme.bodySmall,
                          ),
                      ],
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// 骨架网格：**同尺寸占位**，防 CLS。
class _GridSkeleton extends StatelessWidget {
  const _GridSkeleton();

  @override
  Widget build(BuildContext context) {
    final color = Theme.of(context).colorScheme.surfaceContainerHighest;
    return GridView.builder(
      padding: const EdgeInsets.all(Tokens.gap4),
      gridDelegate: const SliverGridDelegateWithMaxCrossAxisExtent(
        maxCrossAxisExtent: 168,
        mainAxisSpacing: Tokens.gap3,
        crossAxisSpacing: Tokens.gap3,
        childAspectRatio: 0.58,
      ),
      itemCount: 12,
      itemBuilder: (context, index) => Container(
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(Tokens.radiusCard),
        ),
      ),
    );
  }
}
