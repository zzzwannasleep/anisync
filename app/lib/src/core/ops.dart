/// op 名常量表 —— 与 `internal/bridge` dump 出的 `app/test/fixtures/ops.json` **双向**一致。
///
/// 规则（见 `docs/ops-api.md` §7 / §9）：
/// - op 清单**只从 §7 抄**，不从 §8 反推。§8 只是给 §7 里已有的危险条目加 `confirm` 闸门。
/// - 分组顺序即实现顺序。
/// - 多一个少一个都会在 `test/contract/ops_contract_test.dart` 里红。
library;

/// 全部 op 名。新增 op 必须同时改这里与 Go 侧注册表，否则契约测试当场红。
class Ops {
  Ops._();

  // 7.1 core
  static const String coreInit = 'core.init';
  static const String coreVersion = 'core.version';
  static const String coreOps = 'core.ops';

  // 7.2 config
  static const String configGet = 'config.get';
  static const String configSave = 'config.save';
  static const String configValidate = 'config.validate';
  static const String configDefault = 'config.default';
  static const String configSample = 'config.sample';

  // 7.3 instance
  static const String instanceList = 'instance.list';
  static const String instanceTest = 'instance.test';
  static const String instanceSubscriptions = 'instance.subscriptions';

  // 7.4 subscription
  static const String subscriptionSet = 'subscription.set';
  static const String subscriptionAdd = 'subscription.add';
  static const String subscriptionDelete = 'subscription.delete';
  static const String subscriptionResolve = 'subscription.resolve';

  // 7.5 profile
  static const String profileUpsert = 'profile.upsert';
  static const String profileDelete = 'profile.delete';
  static const String profileRuleFile = 'profile.ruleFile';

  // 7.6 discover
  static const String discoverList = 'discover.list';
  static const String discoverApply = 'discover.apply';
  static const String discoverAutoAdd = 'discover.autoAdd';

  // 7.7 plan
  static const String planBuild = 'plan.build';
  static const String planApply = 'plan.apply';

  // 7.8 sync / job
  static const String syncRun = 'sync.run';
  static const String jobList = 'job.list';
  static const String jobGet = 'job.get';
  static const String jobCancel = 'job.cancel';

  // 7.9 status / journal / lease
  static const String statusGet = 'status.get';
  static const String journalTail = 'journal.tail';
  static const String journalClear = 'journal.clear';
  static const String leaseTakeover = 'lease.takeover';

  // 7.10 backup
  static const String backupCreate = 'backup.create';
  static const String backupList = 'backup.list';
  static const String backupRestore = 'backup.restore';

  // 7.11 probe / bgm
  static const String probeRun = 'probe.run';
  static const String bgmSearch = 'bgm.search';

  // 7.12 log / events
  static const String logRecent = 'log.recent';

  /// 唯一一个**由客户端写入引擎状态**的 op（§7.12）。
  ///
  /// 读失败这种「设计外状态」专用通道：`log.recent` 会在断线/切前台时当兜底视图用，
  /// 而事件是旁路、会丢，写进环形缓冲的那一行不会丢。
  static const String logWrite = 'log.write';

  /// 全量清单，对拍用。顺序 = §7 的分组顺序。
  static const List<String> all = <String>[
    coreInit,
    coreVersion,
    coreOps,
    configGet,
    configSave,
    configValidate,
    configDefault,
    configSample,
    instanceList,
    instanceTest,
    instanceSubscriptions,
    subscriptionSet,
    subscriptionAdd,
    subscriptionDelete,
    subscriptionResolve,
    profileUpsert,
    profileDelete,
    profileRuleFile,
    discoverList,
    discoverApply,
    discoverAutoAdd,
    planBuild,
    planApply,
    syncRun,
    jobList,
    jobGet,
    jobCancel,
    statusGet,
    journalTail,
    journalClear,
    leaseTakeover,
    backupCreate,
    backupList,
    backupRestore,
    probeRun,
    bgmSearch,
    logRecent,
    logWrite,
  ];

  /// §4：只有「会写实例、且可能很久」的 op 是异步的 —— 这四个。
  static const Set<String> asyncOps = <String>{
    syncRun,
    planApply,
    backupRestore,
    discoverAutoAdd,
  };

  /// 分组 → op 列表。分组的键与 `ops.json` 里 `group` 字段一致（按名字前缀分的），
  /// 不是 §7 的小节标题 —— 后者会把 job 并进 sync、把 journal/lease 并进 status。
  static const Map<String, List<String>> groups = <String, List<String>>{
    'backup': <String>[backupCreate, backupList, backupRestore],
    'bgm': <String>[bgmSearch],
    'config': <String>[
      configGet,
      configSave,
      configValidate,
      configDefault,
      configSample,
    ],
    'core': <String>[coreInit, coreVersion, coreOps],
    'discover': <String>[discoverList, discoverApply, discoverAutoAdd],
    'instance': <String>[instanceList, instanceTest, instanceSubscriptions],
    'job': <String>[jobList, jobGet, jobCancel],
    'journal': <String>[journalTail, journalClear],
    'lease': <String>[leaseTakeover],
    'log': <String>[logRecent, logWrite],
    'plan': <String>[planBuild, planApply],
    'probe': <String>[probeRun],
    'profile': <String>[profileUpsert, profileDelete, profileRuleFile],
    'status': <String>[statusGet],
    'subscription': <String>[
      subscriptionSet,
      subscriptionAdd,
      subscriptionDelete,
      subscriptionResolve,
    ],
    'sync': <String>[syncRun],
  };

  /// §8：没有 `confirm:true` 一律 `bad_request` 的危险 op。
  static const Set<String> dangerOps = <String>{
    subscriptionDelete,
    journalClear,
    leaseTakeover,
  };
}
