import 'dart:io';

import 'package:flutter/material.dart';
import 'package:path_provider/path_provider.dart';

import 'src/state/app_state.dart';
import 'src/state/prefs.dart';
import 'src/ui/app_shell.dart';
import 'src/ui/theme.dart';

/// 宽屏 / 窄屏断点（dp）：≥900 用 NavigationRail，窄屏用 NavigationBar。
const double kNavBreakpoint = 900;

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // 应用偏好（最近工作区 / 上次后端 / 主题）落 app-prefs.json。
  // Android 上它与 config.yaml 一样放应用私有的 support 目录 —— 不申请存储权限，
  // 也不放外部存储（那里别的应用能读，而 config.yaml 里有各实例的 apiKey）。
  var prefsDirectory = defaultPrefsDirectory();
  if (Platform.isAndroid || Platform.isIOS) {
    try {
      prefsDirectory = (await getApplicationSupportDirectory()).path;
    } on Object {
      // 拿不到平台目录就退回默认；偏好丢了不影响引擎。
    }
  }

  final prefs = PrefsStore(directory: prefsDirectory);
  await prefs.load();

  final state = AppState(prefs: prefs);
  runApp(AniSyncApp(state: state));
}

class AniSyncApp extends StatefulWidget {
  const AniSyncApp({super.key, required this.state});

  final AppState state;

  @override
  State<AniSyncApp> createState() => _AniSyncAppState();
}

class _AniSyncAppState extends State<AniSyncApp> {
  @override
  void initState() {
    super.initState();
    // 首帧之后再连后端：连不上是「界面状态」，不是启动异常。
    WidgetsBinding.instance.addPostFrameCallback((_) => _connect());
  }

  Future<void> _connect() async {
    final prefs = widget.state.prefs;
    if (prefs.lastBackend == PrefsStore.backendNode) {
      await widget.state.useNode(prefs.nodeUrl, prefs.nodeToken);
    } else {
      await widget.state.useLocalEngine();
    }
    if (widget.state.core.ready) {
      await widget.state.reloadAll();
    }
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: widget.state,
      builder: (context, _) => MaterialApp(
        title: 'AniSync',
        debugShowCheckedModeBanner: false,
        theme: buildAppTheme(Brightness.light),
        darkTheme: buildAppTheme(Brightness.dark),
        themeMode: switch (widget.state.prefs.theme) {
          'light' => ThemeMode.light,
          'dark' => ThemeMode.dark,
          _ => ThemeMode.system,
        },
        home: AppShell(state: widget.state),
      ),
    );
  }
}
