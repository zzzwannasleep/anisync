import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';

import '../core/backend.dart';
import '../core/core_event.dart';
import '../core/errors.dart';
import '../core/ffi_backend.dart';
import '../core/node_backend.dart';
import '../core/ops.dart';
import '../model/backup.dart';
import '../model/config.dart';
import '../model/instance_view.dart';
import '../model/job.dart';
import '../model/op_info.dart';
import '../model/plan.dart';
import '../model/profile_view.dart';
import '../model/run_entry.dart';
import '../model/season_item.dart';
import '../model/status.dart';
import '../model/subscription.dart';
import 'async_state.dart';
import 'prefs.dart';

/// 后端类型：本机（进程内 FFI）/ 远端节点（HTTP）。
enum BackendKind { ffi, node }

/// 桌面上偏好文件放可执行文件同目录（与 `config.yaml` 一处，删目录即卸载干净）。
/// Android / iOS 走只读目录，真正的目录由 `main.dart` 用 path_provider 解析后传进来。
String defaultPrefsDirectory() {
  if (Platform.isAndroid || Platform.isIOS) return Directory.systemTemp.path;
  return File(Platform.resolvedExecutable).parent.path;
}

/// 事件环形缓冲：上限 500 行，给「事件流断了」兜底用的界面视图。
class EventLog {
  final List<CoreEvent> _events = <CoreEvent>[];

  static const int capacity = 500;

  List<CoreEvent> get events => List<CoreEvent>.unmodifiable(_events);

  bool get isEmpty => _events.isEmpty;

  void add(CoreEvent event) {
    _events.add(event);
    if (_events.length > capacity) {
      _events.removeRange(0, _events.length - capacity);
    }
  }

  /// `log.recent` 的兜底内容（SSE 重连、App 切前台、isolate 重启之后）。
  ///
  /// **必须走 [add]**：环形上限的裁剪逻辑只该有一处。之前这里直接往 `_events`
  /// 里塞，于是「上限 500 行」这条不变量在兜底路径上被绕过了 ——
  /// 兜底来 800 行就真的存 800 行（由 `EventLog.capacity` 的测试抓到）。
  void seed(Iterable<LogLine> lines) {
    _events.clear();
    for (final line in lines) {
      add(CoreEvent(
        type: CoreEventTypes.log,
        ts: line.ts,
        data: <String, Object?>{'level': line.level, 'text': line.text},
      ));
    }
  }

  void clear() => _events.clear();
}

/// 应用根状态。单一 [AppState] + 若干子 state（不引 provider / riverpod / bloc）。
class AppState extends ChangeNotifier {
  AppState({PrefsStore? prefs})
      : prefs = prefs ?? PrefsStore(directory: defaultPrefsDirectory());

  final PrefsStore prefs;

  Backend? backend;
  BackendKind backendKind = BackendKind.ffi;

  /// 事件总线（FFI 回调 / SSE 都汇到这里）。
  final EventLog eventLog = EventLog();

  StreamSubscription<CoreEvent>? _eventSubscription;

  /// 界面上的瞬时提示（一次性消费）。
  String? banner;

  late final CoreState core = CoreState(this);
  late final ConfigState config = ConfigState(this);
  late final InstancesState instances = InstancesState(this);
  late final SubscriptionsState subscriptions = SubscriptionsState(this);
  late final ProfilesState profiles = ProfilesState(this);
  late final DiscoverState discover = DiscoverState(this);
  late final JournalState journal = JournalState(this);
  late final JobsState jobs = JobsState(this);
  late final StatusState status = StatusState(this);
  late final BackupState backups = BackupState(this);

  bool get hasBackend => backend != null;

  /// 后端标签（顶栏显示）。
  String get backendLabel => backend?.label ?? '未连接';

  /// 当前工作区（引擎报的 `configPath`；没连上时用偏好里的最近工作区）。
  String get workspacePath {
    final info = core.info.valueOrNull;
    if (info != null && info.configPath.isNotEmpty) return info.configPath;
    return prefs.recentWorkspaces.isEmpty ? '' : prefs.recentWorkspaces.first;
  }

  String get dataDir => core.info.valueOrNull?.dataDir ?? '';

  void touch() => notifyListeners();

  /// 接上一个后端。`open` 为 false 时只绑定（测试用，不碰真实库 / 网络）。
  Future<void> attach(Backend next, {bool open = true}) async {
    await _eventSubscription?.cancel();
    await backend?.dispose();
    backend = next;
    backendKind = next is NodeBackend ? BackendKind.node : BackendKind.ffi;
    _eventSubscription = next.events.listen(_onEvent);
    if (open) {
      try {
        await next.open();
        await core.bootstrap();
      } on Object catch (error) {
        // FFI 动态库找不到时，错误里已经列全了「找过哪些路径」，原样展示给用户。
        core.info = AsyncFailed<CoreInfo>(
          CoreError.transport('后端打开失败', detail: '$error'),
        );
      }
    }
    touch();
  }

  void _onEvent(CoreEvent event) {
    eventLog.add(event);
    jobs.ingest(event);
    if (event.type == CoreEventTypes.leaseLost) {
      banner = '租约已失去：另一个进程接管了写入权，本机已停止写入。';
    }
    touch();
  }

  /// 本机后端：加载动态库失败会抛出列全了候选路径的错误，**不静默降级**。
  Future<void> useLocalEngine() async {
    await attach(FfiBackend(), open: true);
    prefs.lastBackend = PrefsStore.backendFfi;
    await prefs.save();
  }

  Future<void> useNode(String url, String token) async {
    prefs.nodeUrl = url;
    prefs.nodeToken = token;
    prefs.lastBackend = PrefsStore.backendNode;
    await prefs.save();
    await attach(NodeBackend(baseUrl: url, token: token), open: true);
  }

  /// 换工作区：`core.init` 可以重复调用（换一份 config.yaml = 换一个工作区）。
  Future<bool> switchWorkspace(String configPath, {String? dataDir}) async {
    final ok = await core.initWorkspace(configPath, dataDir: dataDir);
    if (ok) {
      prefs.rememberWorkspace(configPath);
      await prefs.save();
      await reloadAll();
    }
    return ok;
  }

  /// 把界面需要的数据一次拉齐。每一支都独立失败，互不拖累。
  Future<void> reloadAll() async {
    await config.load();
    await status.load();
    await instances.load();
    await journal.load();
    await jobs.refresh();
  }

  @override
  void dispose() {
    _eventSubscription?.cancel();
    backend?.dispose();
    super.dispose();
  }
}

/// core：版本、op 表、工作区。
class CoreState {
  CoreState(this._app);

  final AppState _app;

  AsyncState<CoreInfo> info = const AsyncIdle<CoreInfo>();
  AsyncState<OpsCatalogue> catalogue = const AsyncIdle<OpsCatalogue>();

  /// 动态库来源路径（加载成功时才有）。
  String libraryPath = '';

  CoreInfo? get current => info.valueOrNull;

  bool get ready => info is AsyncData<CoreInfo>;

  Future<void> bootstrap() async {
    final controller = AsyncController<CoreInfo>(_app.touch, initial: info);
    controller.loading();
    info = controller.state;
    try {
      final version = await _app.backend!.call(Ops.coreVersion, <String, Object?>{});
      var parsed = CoreInfo.fromJson(version);
      // 已经指定过工作区时顺手把它钉上（`core.init` 可重复调用）。
      final workspace = _app.prefs.recentWorkspaces.isEmpty
          ? ''
          : _app.prefs.recentWorkspaces.first;
      if (workspace.isNotEmpty) {
        final data = await _app.backend!.call(Ops.coreInit, <String, Object?>{
          'configPath': workspace,
        });
        parsed = CoreInfo.fromJson(data);
      }
      info = AsyncData<CoreInfo>(parsed);
      unawaited(_loadCatalogue());
    } on CoreException catch (error) {
      info = AsyncFailed<CoreInfo>(error.error);
    } on Object catch (error) {
      info = AsyncFailed<CoreInfo>(
        CoreError.transport('初始化核心失败', detail: '$error'),
      );
    }
    _app.touch();
  }

  Future<void> _loadCatalogue() async {
    try {
      final data = await _app.backend!.call(Ops.coreOps, <String, Object?>{});
      catalogue = AsyncData<OpsCatalogue>(OpsCatalogue.fromJson(data));
    } on Object catch (error) {
      catalogue = AsyncFailed<OpsCatalogue>(
        CoreError.transport('读取 op 表失败', detail: '$error'),
      );
    }
    _app.touch();
  }

  Future<bool> initWorkspace(String configPath, {String? dataDir}) async {
    if (configPath.trim().isEmpty) return false;
    try {
      final payload = <String, Object?>{'configPath': configPath.trim()};
      if (dataDir != null && dataDir.trim().isNotEmpty) {
        payload['dataDir'] = dataDir.trim();
      }
      final data = await _app.backend!.call(Ops.coreInit, payload);
      info = AsyncData<CoreInfo>(CoreInfo.fromJson(data));
      _app.touch();
      return true;
    } on CoreException catch (error) {
      info = AsyncFailed<CoreInfo>(error.error, previous: info.valueOrNull);
      _app.touch();
      return false;
    }
  }
}

/// config：整份配置的读 / 改 / 存。**只有它需要逐字保真的 toJson。**
class ConfigState {
  ConfigState(this._app);

  final AppState _app;

  AsyncState<Config> config = const AsyncIdle<Config>();
  ConfigViews? views;
  List<RuleFileInfo> ruleFiles = <RuleFileInfo>[];
  List<String> warnings = <String>[];
  String path = '';

  /// 有未保存的编辑。
  bool dirty = false;

  /// 当前配置能不能真的跑同步。
  ///
  /// - `null` = 还没校验过（刚启动、`config.get` 之后还没跑 `config.validate`）
  /// - `false` = **已保存，但不能同步**：文件真写下去了，`errors` 摆在这里。
  ///   `check` / `plan` / `sync` 仍会拦 —— 界面必须把这件事说出来，
  ///   不能让用户在 `valid:false` 的状态下点了「立即同步」才发现。
  bool? valid;
  List<ConfigIssue> issues = <ConfigIssue>[];

  /// 最近一次保存的**硬错误**：文件**没有**落盘，原因在这里。
  ///
  /// 与 [issues] 是两类东西：`issues` 是「不完整」（用户看得见、同步会被拦），
  /// 硬错误是「引用的外置规则文件读不出来」—— 放行会让后续 `plan` / `sync`
  /// **少一套规则地跑**而配置文本看上去毫无问题，正是要防的静默回落。
  String? hardError;
  String? hardErrorDetail;

  bool get hasSavedButUnusable => valid == false && issues.isNotEmpty;

  Config? get value => config.valueOrNull;

  Config get editable => config.valueOrNull ?? Config.empty();

  List<InstanceView> get instanceViews => views?.instancesOrEmpty ?? const <InstanceView>[];
  List<ProfileView> get profileViews => views?.profilesOrEmpty ?? const <ProfileView>[];

  Future<void> load() async {
    final controller = AsyncController<Config>(_app.touch, initial: config);
    controller.loading();
    config = controller.state;
    try {
      final data = await _app.backend!.call(Ops.configGet, <String, Object?>{});
      final parsed = Config.fromJson(
        (data['config'] as Map?)?.cast<String, Object?>() ?? <String, Object?>{},
      );
      path = (data['path'] as String?) ?? '';
      warnings = ((data['warnings'] as List?) ?? const <Object?>[])
          .whereType<String>()
          .toList();
      ruleFiles = ((data['ruleFiles'] as List?) ?? const <Object?>[])
          .whereType<Map>()
          .map((e) => RuleFileInfo.fromJson(e.cast<String, Object?>()))
          .toList();
      views = data['views'] == null
          ? null
          : ConfigViews.fromJson(
              (data['views']! as Map).cast<String, Object?>(),
            );
      config = AsyncData<Config>(parsed);
      dirty = false;
    } on CoreException catch (error) {
      config = AsyncFailed<Config>(error.error, previous: config.valueOrNull);
    } on Object catch (error) {
      config = AsyncFailed<Config>(
        CoreError.transport('读取配置失败', detail: '$error'),
      );
    }
    _app.touch();
    // 读完之后顺手问一次「这份配置能用吗」：`config.validate` 不发网络请求，
    // 便宜到可以跟着 load 一起跑。有了它，「已保存但不能同步」这条状态
    // 在**没有点过保存**的情况下也能显示出来（例如刚打开一个空工作区）。
    unawaited(refreshValidity());
  }

  /// 跑一次 `config.validate`（不发网络请求），刷新 [valid] / [issues]。
  Future<void> refreshValidity() async {
    final current = config.valueOrNull;
    if (current == null) return;
    try {
      final data = await _app.backend!.call(Ops.configValidate, <String, Object?>{
        'config': current.toJson(),
      });
      final result = ConfigValidateResult.fromJson(data);
      valid = result.ok;
      issues = result.errorsOrEmpty;
      _app.touch();
    } on CoreException {
      // 校验本身失败（例如版本偏斜）不影响配置读取，交给各自的错误路径显示。
    }
  }

  /// 标记正在编辑（界面改完字段调一次）。
  void markDirty() {
    dirty = true;
    _app.touch();
  }

  /// 丢掉本地编辑，回到上次读到的配置。
  Future<void> revert() async {
    await load();
  }

  /// 保存。**走 `Config.toJson()`**，null 保持 null，只读派生不写回。
  ///
  /// 两种失败要分开（§7.2）：
  /// - **校验类问题** → 落盘 + `valid:false` + `errors`，**保存仍然成功**（返回 true）；
  /// - **外置规则文件读不出来** → 硬错误、**不落盘**，以 `CoreException` 回来（返回 false），
  ///   界面必须红色指出「没保存」以及是哪个文件。
  Future<bool> save() async {
    final current = config.valueOrNull;
    if (current == null) return false;
    try {
      final data = await _app.backend!.call(Ops.configSave, <String, Object?>{
        'config': current.toJson(),
      });
      final saved = ConfigWriteResult.fromJson(data);
      warnings = saved.warningsOrEmpty;
      if (saved.path.isNotEmpty) path = saved.path;
      dirty = false;
      hardError = null;
      hardErrorDetail = null;
      // valid:false 也算保存成功：文件真写下去了。
      valid = saved.valid;
      issues = saved.errorsOrEmpty;
      _app.touch();
      await load();
      return true;
    } on CoreException catch (error) {
      // 硬错误：文件没落盘。
      hardError = error.error.message;
      hardErrorDetail = error.error.detail ??
          (error.error.versionSkew ? 'version-skew' : error.error.code);
      _app.touch();
      return false;
    }
  }

  /// 丢掉硬错误提示（用户已看过 / 已处理）。
  void clearHardError() {
    hardError = null;
    hardErrorDetail = null;
    _app.touch();
  }

  /// 保存前预检（`title` 与 `bgmId` 不能同时为空、方案必须存在……）。
  Future<ConfigValidateResult?> validate() async {
    final current = config.valueOrNull;
    if (current == null) return null;
    try {
      final data = await _app.backend!.call(Ops.configValidate, <String, Object?>{
        'config': current.toJson(),
      });
      return ConfigValidateResult.fromJson(data);
    } on CoreException catch (error) {
      return ConfigValidateResult(
        ok: false,
        errors: <ConfigIssue>[
          ConfigIssue(field: '', message: error.error.message),
        ],
      );
    }
  }

  /// 服务端回来的新配置（`profile.upsert` / `discover.apply` 这类会带 `config`）。
  void applyServerConfig(Object? rawConfig, {Object? rawViews}) {
    if (rawConfig is Map) {
      config = AsyncData<Config>(
        Config.fromJson(rawConfig.cast<String, Object?>()),
      );
      dirty = false;
    }
    if (rawViews is Map) {
      views = ConfigViews.fromJson(rawViews.cast<String, Object?>());
    }
    _app.touch();
  }

  Future<ConfigSampleResult?> sample() async {
    try {
      final data = await _app.backend!.call(Ops.configSample, <String, Object?>{});
      return ConfigSampleResult.fromJson(data);
    } on CoreException {
      return null;
    }
  }
}

/// instance：实例列表与连接测试。
class InstancesState {
  InstancesState(this._app);

  final AppState _app;

  AsyncState<InstanceListResult> list = const AsyncIdle<InstanceListResult>();

  /// 每个实例最近一次「测试连接」的结果。
  final Map<String, InstanceTestResult> tests = <String, InstanceTestResult>{};

  /// 正在测试中的实例名。
  final Set<String> testing = <String>{};

  List<InstanceView> get items => list.valueOrNull?.instancesOrEmpty ?? const <InstanceView>[];

  Future<void> load() async {
    final controller = AsyncController<InstanceListResult>(_app.touch, initial: list);
    controller.loading();
    list = controller.state;
    try {
      final data = await _app.backend!.call(Ops.instanceList, <String, Object?>{});
      list = AsyncData<InstanceListResult>(InstanceListResult.fromJson(data));
    } on CoreException catch (error) {
      list = AsyncFailed<InstanceListResult>(error.error, previous: list.valueOrNull);
    } on Object catch (error) {
      list = AsyncFailed<InstanceListResult>(
        CoreError.transport('读取实例列表失败', detail: '$error'),
      );
    }
    _app.touch();
  }

  Future<void> test(String name) async {
    testing.add(name);
    _app.touch();
    try {
      final data = await _app.backend!.call(Ops.instanceTest, <String, Object?>{
        'name': name,
      });
      final result = InstanceTestResult.fromJson(data);
      // §1：`instance.test` 自带 `error?` 字段，取消会以 `ok:true` + `error:"context canceled"`
      // 回来。**两种表现都算取消成功** —— 不认它，用户会看到一条红色的「连接失败」，
      // 而它其实是他自己（或上层）取消的。
      tests[name] = looksCanceled(result.error)
          ? InstanceTestResult(
              name: name,
              ok: false,
              latencyMs: result.latencyMs,
              error: null,
            )
          : result;
    } on CoreException catch (error) {
      if (!error.error.isCanceled) {
        tests[name] = InstanceTestResult(
          name: name,
          ok: false,
          error: error.error.fullText,
        );
      }
    } finally {
      testing.remove(name);
      _app.touch();
    }
  }
}

/// subscription：一个实例下的订阅列表与写操作。
enum SubscriptionFilter { all, withStandby, noMatch, disabled }

class SubscriptionsState {
  SubscriptionsState(this._app);

  final AppState _app;

  AsyncState<SubscriptionListResult> list =
      const AsyncIdle<SubscriptionListResult>();

  String instanceName = '';
  String query = '';
  SubscriptionFilter filter = SubscriptionFilter.all;
  final Set<String> selected = <String>{};

  /// 上一次批量操作的结果（用来回报「改了几条」）。
  SubscriptionSetResult? lastResult;

  List<SubscriptionView> get all => list.valueOrNull?.itemsOrEmpty ?? const <SubscriptionView>[];

  /// 搜索 + 过滤之后要显示的条目。
  List<SubscriptionView> get visible {
    final needle = query.trim().toLowerCase();
    return all.where((item) {
      if (needle.isNotEmpty) {
        final haystack = <String>[
          item.title,
          item.key,
          item.bgmId,
          item.profile,
          item.main?.group ?? '',
          ...item.standbyOrEmpty.map((e) => e.group),
        ].join(' ').toLowerCase();
        if (!haystack.contains(needle)) return false;
      }
      return switch (filter) {
        SubscriptionFilter.all => true,
        SubscriptionFilter.withStandby => item.hasStandby,
        SubscriptionFilter.noMatch => item.hasNoMatch,
        SubscriptionFilter.disabled => !item.enable,
      };
    }).toList(growable: false);
  }

  bool get isFilteredEmpty => all.isNotEmpty && visible.isEmpty;

  Future<void> load(String name, {bool refresh = false}) async {
    instanceName = name;
    final controller = AsyncController<SubscriptionListResult>(_app.touch, initial: list);
    controller.loading();
    list = controller.state;
    selected.clear();
    try {
      final data = await _app.backend!.call(Ops.instanceSubscriptions, <String, Object?>{
        'name': name,
        if (refresh) 'refresh': true,
      });
      list = AsyncData<SubscriptionListResult>(SubscriptionListResult.fromJson(data));
    } on CoreException catch (error) {
      list = AsyncFailed<SubscriptionListResult>(error.error, previous: list.valueOrNull);
    } on Object catch (error) {
      list = AsyncFailed<SubscriptionListResult>(
        CoreError.transport('读取订阅失败', detail: '$error'),
      );
    }
    _app.touch();
  }

  void toggle(String key) {
    if (!selected.remove(key)) selected.add(key);
    _app.touch();
  }

  void selectAll() {
    selected
      ..clear()
      ..addAll(visible.map((e) => e.key));
    _app.touch();
  }

  void clearSelection() {
    selected.clear();
    _app.touch();
  }

  /// 批量改字段。`patch` 里没写的字段一律不动；`dryRun` 只算 diff。
  Future<SubscriptionSetResult?> applyPatch(
    SubscriptionPatch patch, {
    bool dryRun = false,
  }) async {
    if (selected.isEmpty) return null;
    final keys = selected.toList(growable: false);
    final items = <Map<String, Object?>>[];
    for (final key in keys) {
      final view = all.firstWhere((e) => e.key == key, orElse: () => SubscriptionView(key: key));
      items.add(SubscriptionSetItem(key: key, raw: view.raw, patch: patch).toJson());
    }
    try {
      final data = await _app.backend!.call(Ops.subscriptionSet, <String, Object?>{
        'name': instanceName,
        'items': items,
        if (dryRun) 'dryRun': true,
      });
      lastResult = SubscriptionSetResult.fromJson(data);
      if (!dryRun) await load(instanceName, refresh: true);
      return lastResult;
    } on CoreException catch (error) {
      list = AsyncFailed<SubscriptionListResult>(error.error, previous: list.valueOrNull);
      _app.touch();
      return null;
    }
  }

  /// 删除订阅。**危险操作**：没有 `confirm:true` 后端会直接 `bad_request`。
  Future<SubscriptionDeleteResult?> delete(
    List<String> keys, {
    required bool confirm,
    bool deleteFiles = false,
  }) async {
    if (keys.isEmpty) return null;
    try {
      final data = await _app.backend!.call(Ops.subscriptionDelete, <String, Object?>{
        'name': instanceName,
        'keys': keys,
        'confirm': confirm,
        'deleteFiles': deleteFiles,
      });
      final result = SubscriptionDeleteResult.fromJson(data);
      await load(instanceName, refresh: true);
      return result;
    } on CoreException catch (error) {
      list = AsyncFailed<SubscriptionListResult>(error.error, previous: list.valueOrNull);
      _app.touch();
      return null;
    }
  }

  /// 新增订阅（「添加番剧」弹窗的第二条路）。
  ///
  /// `instance` 显式给定时用它；否则用当前打开的实例。
  Future<SubscriptionView?> add(
    Map<String, Object?> payload, {
    String? instance,
  }) async {
    final name = instance ?? instanceName;
    if (name.isEmpty) return null;
    try {
      final data = await _app.backend!.call(Ops.subscriptionAdd, <String, Object?>{
        'name': name,
        ...payload,
      });
      final ani = data['ani'];
      if (name == instanceName) await load(name, refresh: true);
      return ani is Map
          ? SubscriptionView.fromJson(ani.cast<String, Object?>())
          : null;
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return null;
    }
  }
}

/// profile：方案的读写。方案住在 `config.profiles` 里，所以真正的落地在 [ConfigState]。
class ProfilesState {
  ProfilesState(this._app);

  final AppState _app;

  AsyncState<RuleFileDetail> ruleFile = const AsyncIdle<RuleFileDetail>();

  /// 选中的方案名；空串 = 「默认（顶层规则）」。
  String selectedName = '';

  bool get isDefaultSelected => selectedName.isEmpty;

  /// 顶层规则的保存路径是 `config.save`；方案走 `profile.upsert`。
  Future<bool> upsert(Profile profile) async {
    try {
      final data = await _app.backend!.call(Ops.profileUpsert, <String, Object?>{
        'profile': profile.toJson(),
      });
      _app.config.applyServerConfig(data['config'], rawViews: data['views']);
      return true;
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return false;
    }
  }

  Future<List<String>> delete(String name, {required bool confirm}) async {
    if (!confirm) return const <String>[];
    try {
      final data = await _app.backend!.call(Ops.profileDelete, <String, Object?>{
        'name': name,
        'confirm': true,
      });
      final result = ProfileDeleteResult.fromJson(data);
      _app.config.applyServerConfig(data['config'], rawViews: data['views']);
      if (selectedName == name) selectedName = '';
      return result.affectedOrEmpty;
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return const <String>[];
    }
  }

  /// 外置规则文件的内容（**只读**：内容仍然只能在文件里改）。
  Future<void> loadRuleFile(String owner, String path) async {
    final controller = AsyncController<RuleFileDetail>(_app.touch, initial: ruleFile);
    controller.loading();
    ruleFile = controller.state;
    try {
      final data = await _app.backend!.call(Ops.profileRuleFile, <String, Object?>{
        'owner': owner,
        'path': path,
      });
      ruleFile = AsyncData<RuleFileDetail>(RuleFileDetail.fromJson(data));
    } on CoreException catch (error) {
      ruleFile = AsyncFailed<RuleFileDetail>(error.error, previous: ruleFile.valueOrNull);
    }
    _app.touch();
  }
}

/// discover：本季新番 + 手工「添加番剧」。
class DiscoverState {
  DiscoverState(this._app);

  final AppState _app;

  AsyncState<DiscoverListResult> list = const AsyncIdle<DiscoverListResult>();
  AsyncState<BgmSearchResult> search = const AsyncIdle<BgmSearchResult>();

  /// 未保存的 `japanOnly` 预览开关；null = 用配置里的值。
  bool? japanOnlyOverride;
  final Set<String> selected = <String>{};

  DiscoverListResult? get value => list.valueOrNull;

  List<SeasonItem> get items => value?.itemsOrEmpty ?? const <SeasonItem>[];

  bool get japanOnly => japanOnlyOverride ?? _app.config.editable.discover.japanOnlyEffective;

  Future<void> load({bool force = false, bool? japanOnly}) async {
    final controller = AsyncController<DiscoverListResult>(_app.touch, initial: list);
    controller.loading();
    list = controller.state;
    if (japanOnly != null) japanOnlyOverride = japanOnly;
    try {
      final data = await _app.backend!.call(Ops.discoverList, <String, Object?>{
        'japanOnly': japanOnlyOverride ?? _app.config.editable.discover.japanOnlyEffective,
        if (force) 'force': true,
      });
      list = AsyncData<DiscoverListResult>(DiscoverListResult.fromJson(data));
      selected.clear();
    } on CoreException catch (error) {
      list = AsyncFailed<DiscoverListResult>(error.error, previous: list.valueOrNull);
    } on Object catch (error) {
      list = AsyncFailed<DiscoverListResult>(
        CoreError.transport('拉取本季新番失败', detail: '$error'),
      );
    }
    _app.touch();
  }

  void toggle(String bgmId) {
    if (!selected.remove(bgmId)) selected.add(bgmId);
    _app.touch();
  }

  /// 勾选后「加入 targets」（批量版）。
  Future<int> applySelected() async {
    if (selected.isEmpty) return 0;
    try {
      final data = await _app.backend!.call(Ops.discoverApply, <String, Object?>{
        'bgmIds': selected.toList(growable: false),
        'asTargets': true,
      });
      _app.config.applyServerConfig(data['config']);
      final added = data['added'];
      selected.clear();
      await load();
      return added is int ? added : 0;
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return 0;
    }
  }

  Future<void> autoAdd({bool dryRun = false}) async {
    final jobId = await _app.jobs.start(Ops.discoverAutoAdd, <String, Object?>{
      if (dryRun) 'dryRun': true,
    });
    if (jobId == null) return;
  }

  Future<void> runSearch(String keyword, {int? limit}) async {
    final q = keyword.trim();
    if (q.isEmpty) {
      search = const AsyncEmpty<BgmSearchResult>(why: '输入关键字后才会搜索');
      _app.touch();
      return;
    }
    final controller = AsyncController<BgmSearchResult>(_app.touch, initial: search);
    controller.loading();
    search = controller.state;
    try {
      final data = await _app.backend!.call(Ops.bgmSearch, <String, Object?>{
        'q': q,
        'limit': ?limit,
      });
      search = AsyncData<BgmSearchResult>(BgmSearchResult.fromJson(data));
    } on CoreException catch (error) {
      search = AsyncFailed<BgmSearchResult>(error.error, previous: search.valueOrNull);
    }
    _app.touch();
  }

  /// 「加进番剧配置」那条路：往 `Config.targets` 追加，然后保存。
  Future<bool> addToTargets(BgmSubject subject) async {
    final config = _app.config.editable;
    final targets = (config.targets ?? <Target>[]).toList()
      ..add(Target(title: subject.displayName, jpTitle: subject.name, bgmId: subject.id));
    config.targets = targets;
    _app.config.markDirty();
    return _app.config.save();
  }
}

/// journal：运行日志。
class JournalState {
  JournalState(this._app);

  final AppState _app;

  AsyncState<JournalTailResult> journal = const AsyncIdle<JournalTailResult>();
  AsyncState<LogRecentResult> logs = const AsyncIdle<LogRecentResult>();

  bool onlyFailures = false;
  int limit = 100;

  List<RunEntry> get entries {
    final all = journal.valueOrNull?.entriesOrEmpty ?? const <RunEntry>[];
    return onlyFailures ? all.where((e) => e.isFailure).toList() : all;
  }

  bool get isFilteredEmpty =>
      (journal.valueOrNull?.entriesOrEmpty.isNotEmpty ?? false) && entries.isEmpty;

  Future<void> load() async {
    final controller = AsyncController<JournalTailResult>(_app.touch, initial: journal);
    controller.loading();
    journal = controller.state;
    try {
      final data = await _app.backend!.call(Ops.journalTail, <String, Object?>{
        'limit': limit,
      });
      journal = AsyncData<JournalTailResult>(JournalTailResult.fromJson(data));
    } on CoreException catch (error) {
      journal = AsyncFailed<JournalTailResult>(error.error, previous: journal.valueOrNull);
    }
    _app.touch();
  }

  Future<void> loadLogs() async {
    try {
      final data = await _app.backend!.call(Ops.logRecent, <String, Object?>{});
      final parsed = LogRecentResult.fromJson(data);
      logs = AsyncData<LogRecentResult>(parsed);
      _app.eventLog.seed(parsed.linesOrEmpty);
    } on CoreException catch (error) {
      logs = AsyncFailed<LogRecentResult>(error.error, previous: logs.valueOrNull);
    }
    _app.touch();
  }

  /// 清空运行记录。**危险操作**：必须 `confirm:true`，界面要二次确认。
  Future<JournalClearResult?> clear({required bool confirm}) async {
    if (!confirm) return null;
    try {
      final data = await _app.backend!.call(Ops.journalClear, <String, Object?>{
        'confirm': true,
      });
      final result = JournalClearResult.fromJson(data);
      await load();
      return result;
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return null;
    }
  }
}

/// job：异步 op 的进度追踪。
class JobsState {
  JobsState(this._app);

  final AppState _app;

  final List<JobView> jobs = <JobView>[];
  final Map<String, String> messages = <String, String>{};
  final Map<String, double> fraction = <String, double>{};

  /// 最近一次异步 op 是否因为「写权被别的进程占着」而失败。
  ///
  /// 这个标记存在的理由：`lease_held` 只显示「失败」是不够的 ——
  /// 用户要知道是谁挡着（pid / note / 多久没心跳）以及能不能接管。
  bool leaseConflict = false;

  List<JobView> get running =>
      jobs.where((e) => e.isRunning).toList(growable: false);

  bool get hasRunning => running.isNotEmpty;

  void ingest(CoreEvent event) {
    final jobId = event.jobId;
    if (jobId == null) return;
    switch (event.type) {
      case CoreEventTypes.jobStart:
        jobs.insert(
          0,
          JobView(
            id: jobId,
            kind: event.kind ?? '',
            state: JobState.running,
            startedAt: event.ts ?? DateTime.now(),
            message: '已开始',
          ),
        );
      case CoreEventTypes.progress:
      case CoreEventTypes.instanceProgress:
        final done = event.done ?? 0;
        final total = event.total ?? 0;
        messages[jobId] = event.label ??
            (total > 0 ? '进行中 $done/$total' : '进行中');
        if (total > 0) fraction[jobId] = done / total;
      case CoreEventTypes.jobDone:
        _finish(jobId, JobState.done, event.result);
      case CoreEventTypes.jobFailed:
        final error = event.error;
        _finish(jobId, JobState.failed, null,
            message: error == null ? '失败' : '${error['message'] ?? '失败'}');
      case CoreEventTypes.log:
        final text = event.text;
        if (text != null) messages[jobId] = text;
      default:
        break;
    }
  }

  void _finish(String jobId, String state, Map<String, Object?>? result,
      {String message = ''}) {
    final index = jobs.indexWhere((e) => e.id == jobId);
    final finished = DateTime.now();
    if (index >= 0) {
      final current = jobs[index];
      jobs[index] = JobView(
        id: current.id,
        kind: current.kind,
        state: state,
        startedAt: current.startedAt,
        finishedAt: finished,
        message: message.isEmpty ? current.message : message,
        result: result ?? current.result,
      );
    } else {
      jobs.insert(
        0,
        JobView(
          id: jobId,
          kind: '',
          state: state,
          finishedAt: finished,
          message: message,
          result: result,
        ),
      );
    }
    fraction.remove(jobId);
  }

  Future<void> refresh() async {
    try {
      final data = await _app.backend!.call(Ops.jobList, <String, Object?>{});
      final parsed = JobListResult.fromJson(data);
      jobs
        ..clear()
        ..addAll(parsed.jobsOrEmpty);
    } on CoreException {
      // job 列表拿不到不影响主流程，界面上「最近任务」为空即可。
    }
    _app.touch();
  }

  /// 起一个异步 op，返回 `jobId`。
  Future<String?> start(String op, Map<String, Object?> payload) async {
    try {
      final data = await _app.backend!.call(op, payload);
      final id = data['jobId'];
      if (id is! String) return null;
      leaseConflict = false;
      jobs.insert(
        0,
        JobView(
          id: id,
          kind: op,
          state: JobState.running,
          startedAt: DateTime.now(),
          message: '已提交',
        ),
      );
      _app.touch();
      return id;
    } on CoreException catch (error) {
      // `lease_held` 不能只显示「同步失败」：用户需要知道是**哪个进程**挡着、
      // 要不要接管。所以翻一个标记，由概览页把租约持有者与「接管」一起显示出来。
      leaseConflict = error.code == ErrorCodes.leaseHeld;
      _app.banner = error.error.message;
      if (leaseConflict) unawaited(_app.status.load());
      _app.touch();
      return null;
    }
  }

  Future<void> cancel(String id) async {
    try {
      await _app.backend!.call(Ops.jobCancel, <String, Object?>{'id': id});
      _finish(id, JobState.canceled, null, message: '已取消');
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
    }
    _app.touch();
  }
}

/// status：概览页与「不发任何网络请求」的状态汇总。
class StatusState {
  StatusState(this._app);

  final AppState _app;

  AsyncState<StatusResult> status = const AsyncIdle<StatusResult>();

  StatusResult? get value => status.valueOrNull;

  Future<void> load({int limit = 20}) async {
    final controller = AsyncController<StatusResult>(_app.touch, initial: status);
    controller.loading();
    status = controller.state;
    try {
      final data = await _app.backend!.call(Ops.statusGet, <String, Object?>{
        'limit': limit,
      });
      status = AsyncData<StatusResult>(StatusResult.fromJson(data));
    } on CoreException catch (error) {
      status = AsyncFailed<StatusResult>(error.error, previous: status.valueOrNull);
    } on Object catch (error) {
      status = AsyncFailed<StatusResult>(
        CoreError.transport('读取状态失败', detail: '$error'),
      );
    }
    _app.touch();
  }

  /// 立即同步（异步 op，返回 jobId）。
  Future<String?> runSync({bool discover = false, bool preview = false}) =>
      _app.jobs.start(Ops.syncRun, <String, Object?>{
        if (discover) 'discover': true,
        if (preview) 'preview': true,
      });

  /// 接管租约。**危险操作**：`force` + `confirm` 都要给。
  Future<bool> takeover() async {
    try {
      await _app.backend!.call(Ops.leaseTakeover, <String, Object?>{
        'force': true,
        'confirm': true,
      });
      await load();
      return true;
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return false;
    }
  }
}

/// backup：存档的建 / 列 / 还原。
class BackupState {
  BackupState(this._app);

  final AppState _app;

  AsyncState<BackupListResult> backups = const AsyncIdle<BackupListResult>();

  List<BackupEntry> get items => backups.valueOrNull?.backupsOrEmpty ?? const <BackupEntry>[];

  Future<void> load() async {
    final controller = AsyncController<BackupListResult>(_app.touch, initial: backups);
    controller.loading();
    backups = controller.state;
    try {
      final data = await _app.backend!.call(Ops.backupList, <String, Object?>{});
      backups = AsyncData<BackupListResult>(BackupListResult.fromJson(data));
    } on CoreException catch (error) {
      backups = AsyncFailed<BackupListResult>(error.error, previous: backups.valueOrNull);
    }
    _app.touch();
  }

  Future<BackupCreateResult?> create() async {
    try {
      final data = await _app.backend!.call(Ops.backupCreate, <String, Object?>{});
      await load();
      return BackupCreateResult.fromJson(data);
    } on CoreException catch (error) {
      _app.banner = error.error.fullText;
      _app.touch();
      return null;
    }
  }

  Future<String?> restore(String file, {bool prune = false}) =>
      _app.jobs.start(Ops.backupRestore, <String, Object?>{
        'file': file,
        if (prune) 'prune': true,
      });
}

/// `plan.build` 的一次调用结果（订阅页与番剧配置页共用）。
Future<PlanView?> buildPlan(AppState app, {String? instance, int? limit}) async {
  try {
    final data = await app.backend!.call(Ops.planBuild, <String, Object?>{
      if (instance != null && instance.isNotEmpty) 'instance': instance,
      if (limit != null && limit > 0) 'limit': limit,
    });
    return PlanBuildResult.fromJson(data).planOrEmpty;
  } on CoreException catch (error) {
    app.banner = error.error.fullText;
    app.touch();
    return null;
  }
}

/// `plan.apply`：把计划写进实例（异步 op）。
Future<String?> applyPlan(AppState app, {bool discover = false, bool force = false}) =>
    app.jobs.start(Ops.planApply, <String, Object?>{
      if (discover) 'discover': true,
      if (force) 'force': true,
    });
