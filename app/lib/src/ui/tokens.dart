import 'dart:ui';

/// 设计 token。数据密集的管理工具：4/8px 网格、语义化配色、密度优先、数字等宽。
///
/// 锁定的旋钮（`ui-craft-dense-dashboard`）：CRAFT_LEVEL=7 / MOTION_INTENSITY=3 /
/// VISUAL_DENSITY=9，DESIGN_VARIANCE=4（管理工具，安全布局）。
class Tokens {
  Tokens._();

  // ---- 间距：只用 4 / 8 / 12 / 16 / 24，绝不出现 5 / 7 / 9 / 10 ----
  static const double gap1 = 4;
  static const double gap2 = 8;
  static const double gap3 = 12;
  static const double gap4 = 16;
  static const double gap6 = 24;

  /// 表格单元格内边距（密，但不挤）。
  static const double cellX = 12;
  static const double cellY = 8;

  // ---- 圆角：分级，不统一（统一圆角一眼就是模板） ----
  static const double radiusInput = 6;
  static const double radiusCard = 8;
  static const double radiusOverlay = 14;

  // ---- 字号 ----
  static const double fontCaption = 11;
  static const double fontTable = 12;
  static const double fontBody = 13;
  static const double fontBodyLarge = 14;
  static const double fontSection = 15;
  static const double fontTitle = 19;

  // ---- 颜色：一个主色 + subdued 语义色，禁高饱和 ----
  /// 主色：深青。不是默认蓝 —— 默认蓝是最明显的「模板味」信号。
  static const Color accent = Color(0xFF2F6F6B);
  static const Color accentDark = Color(0xFF6FBDB6);

  static const Color success = Color(0xFF3F8F5B);
  static const Color warning = Color(0xFFB07C2A);
  static const Color danger = Color(0xFFB0473C);
  static const Color info = Color(0xFF3E6E9E);

  /// 行 hover 底色（亮 / 暗）。
  static const Color rowHoverLight = Color(0xFFF5F6F8);
  static const Color rowHoverDark = Color(0xFF23272E);

  /// 选中行：主色 6% 底 + 2px 主色左边框。
  static Color rowSelectedLight = accent.withValues(alpha: 0.06);
  static Color rowSelectedDark = accentDark.withValues(alpha: 0.10);

  /// 等宽数字。集数、偏移、条数、耗时全部走它。
  static const List<FontFeature> tabular = <FontFeature>[
    FontFeature.tabularFigures(),
  ];

  /// ID / 版本 / 时间戳：等宽 + 斜杠零。
  static const List<FontFeature> codeFigures = <FontFeature>[
    FontFeature.tabularFigures(),
    FontFeature.slashedZero(),
  ];

  /// 系统字体栈（不联网拉字体）。
  static const String? fontFamily = null;

  /// 面板/侧栏用的浅色底（**不是**全暗侧栏）。
  static const Color railLight = Color(0xFFF7F8FA);
  static const Color railDark = Color(0xFF1C2026);

  static const Duration hoverDuration = Duration(milliseconds: 80);
  static const Duration inlineSaveDuration = Duration(milliseconds: 150);
}
