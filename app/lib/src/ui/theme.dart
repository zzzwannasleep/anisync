import 'package:flutter/material.dart';

import 'tokens.dart';

/// 亮 / 暗两套主题。不用 `ColorScheme.fromSeed` 的默认结果 —— 它会给出模板味的紫/蓝。
ThemeData buildAppTheme(Brightness brightness) {
  final isLight = brightness == Brightness.light;
  final accent = isLight ? Tokens.accent : Tokens.accentDark;
  final surface = isLight ? const Color(0xFFFFFFFF) : const Color(0xFF16191E);
  final background = isLight ? const Color(0xFFFBFBFC) : const Color(0xFF111418);
  final onSurface = isLight ? const Color(0xFF1B1F24) : const Color(0xFFE6E8EB);
  final outline = isLight ? const Color(0xFFDDE0E5) : const Color(0xFF31363E);

  final base = ColorScheme.fromSeed(
    seedColor: accent,
    brightness: brightness,
  );

  final scheme = base.copyWith(
    primary: accent,
    onPrimary: Colors.white,
    surface: surface,
    onSurface: onSurface,
    surfaceContainerHighest: isLight
        ? const Color(0xFFF2F3F6)
        : const Color(0xFF20242B),
    outline: outline,
    outlineVariant: outline,
    error: Tokens.danger,
    onError: Colors.white,
  );

  final textTheme = Typography.material2021(platform: TargetPlatform.windows)
      .black
      .apply(bodyColor: onSurface, displayColor: onSurface);

  return ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor: background,
    canvasColor: background,
    fontFamily: Tokens.fontFamily,
    visualDensity: VisualDensity.compact,
    textTheme: textTheme.copyWith(
      titleLarge: textTheme.titleLarge?.copyWith(
        fontSize: Tokens.fontTitle,
        fontWeight: FontWeight.w600,
      ),
      titleMedium: textTheme.titleMedium?.copyWith(
        fontSize: Tokens.fontSection,
        fontWeight: FontWeight.w500,
      ),
      bodyMedium: textTheme.bodyMedium?.copyWith(fontSize: Tokens.fontBody),
      bodySmall: textTheme.bodySmall?.copyWith(fontSize: Tokens.fontTable),
    ),
    dividerTheme: DividerThemeData(
      color: outline,
      thickness: 1,
      space: 1,
    ),
    cardTheme: CardThemeData(
      elevation: 0,
      color: surface,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(Tokens.radiusCard),
        side: BorderSide(color: outline),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      isDense: true,
      filled: true,
      fillColor: surface,
      contentPadding: const EdgeInsets.symmetric(
        horizontal: Tokens.gap3,
        vertical: Tokens.gap2 + Tokens.gap1,
      ),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(Tokens.radiusInput),
        borderSide: BorderSide(color: outline),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(Tokens.radiusInput),
        borderSide: BorderSide(color: outline),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(Tokens.radiusInput),
        borderSide: BorderSide(color: accent, width: 2),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        visualDensity: VisualDensity.compact,
        padding: const EdgeInsets.symmetric(
          horizontal: Tokens.gap4,
          vertical: Tokens.gap3,
        ),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(Tokens.radiusInput),
        ),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        visualDensity: VisualDensity.compact,
        padding: const EdgeInsets.symmetric(
          horizontal: Tokens.gap3,
          vertical: Tokens.gap3,
        ),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(Tokens.radiusInput),
        ),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(visualDensity: VisualDensity.compact),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: Colors.transparent,
      side: BorderSide(color: outline),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(Tokens.radiusInput),
      ),
      labelStyle: TextStyle(fontSize: Tokens.fontTable),
      padding: const EdgeInsets.symmetric(horizontal: Tokens.gap2),
    ),
    listTileTheme: const ListTileThemeData(
      dense: true,
      minVerticalPadding: Tokens.gap2,
    ),
    dialogTheme: DialogThemeData(
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(Tokens.radiusOverlay),
      ),
    ),
    snackBarTheme: const SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      elevation: 2,
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(
      color: accent,
      linearMinHeight: 2,
    ),
    scrollbarTheme: const ScrollbarThemeData(
      thickness: WidgetStatePropertyAll<double>(8),
    ),
  );
}
