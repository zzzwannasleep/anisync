/// 界面文案集中处。页面里**不写裸中文字符串** —— 改口径只改这一个文件。
library;

import '../core/errors.dart';

class S {
  S._();

  static const String appTitle = 'AniSync';
  static const String appSubtitle = '多 ANI-RSS 实例 · 主副 RSS 自动打点';
  static const String loading = '加载中…';
  static const String unspecified = '—';

  static const List<String> navLabels = <String>[
    '概览',
    '实例',
    '规则',
    '番剧',
    '新番',
    '日志',
    '设置',
  ];
}

/// 通用动作与状态词。
class SC {
  SC._();

  static const String loading = '加载中…';
  static const String connected = '已连接';
  static const String retry = '重试';
  static const String refresh = '刷新';
  static const String copy = '复制';
  static const String copied = '已复制到剪贴板';
  static const String cancel = '取消';
  static const String confirm = '确认';
  static const String save = '保存';
  static const String saving = '保存中…';
  static const String saved = '已保存';
  static const String close = '关闭';
  static const String delete = '删除';
  static const String add = '添加';
  static const String edit = '编辑';
  static const String search = '搜索';
  static const String clearFilter = '清空筛选';
  static const String showDetail = '展开';
  static const String hideDetail = '收起';
  static const String copyError = '复制错误原文';
  static const String reload = '重新加载';
  static const String revert = '放弃修改';
  static const String unsaved = '有未保存的修改';
  static const String loadingFailed = '加载失败';
  static const String emptyResult = '没有符合条件的内容';
  static const String offlineHint = '连不上引擎：改动会在连上之后再同步。';
  static const String silentCanceled = '（已取消，无需处理）';

  /// 取消有两种合法表现（§1）：`canceled` 信封，或自带 `error?` 字段的 op 回
  /// `ok:true` + `data.error`。后者**不是失败** —— 不认它就会报一条红色的
  /// 「连接失败」，而它其实是被取消的。
  static const String canceledInData =
      '这次读取被取消了，没有拿到数据。这不代表实例有问题，重新读一次即可。';
}

/// 错误码 → 人话（`docs/ops-api.md` §3 那张表）。
class SErr {
  SErr._();

  static const Map<String, String> byCode = <String, String>{
    'bad_request': '请求字段缺失或非法',
    'not_found': '目标不存在，数据可能已经变了',
    'invalid_config': '配置不合法',
    'network': '连不上实例或上游',
    'busy': '已经有一个写操作在进行中',
    'lease_held': '写入权被另一个进程持有',
    'canceled': '调用已取消',
    'internal': '引擎内部错误',
    'transport': '与本机引擎/节点的连接失败',
    'unauthorized': '节点拒绝了请求：token 不对或没带',
  };

  /// code 级文案。**注意它看不到 op**，所以给不出「版本偏斜」的区分 ——
  /// 那个区分只能由调用点做（那里才知道自己发了哪个 op）。用 [describe] 取完整描述。
  static String human(String code) => byCode[code] ?? '未知错误（$code）';

  /// 版本偏斜（`not_found` 且 `detail == 刚发出的 op 名`）的文案。
  static const String skewTitle = '本机引擎比这个 App 旧，不认识这个操作';
  static const String skewWhy =
      '这是**版本不匹配**，不是配置写错了 —— 重试与刷新都不会让它变好。'
      '升级引擎（或换用与它配套的客户端）之后这个操作才会被认识。';
  static const String skewOp = '不认识的 op：%s';
  static const String skewWhere = '后端在「设置」页可以切换与查看';

  /// 资源不存在（`not_found` 但 `detail` 不是 op 名）：**把资源名显示出来**。
  static const String resourceName = '不存在的名字：%s';
  static const String resourceHint = '数据可能已经被别处改过，刷新一次就能对齐。';

  /// 带上下文的完整描述：版本偏斜走它自己那条路，其余走 code 表。
  static String describe(CoreError error) {
    if (error.versionSkew) return skewTitle;
    final base = human(error.code);
    if (error.code == ErrorCodes.notFound &&
        error.detail != null &&
        error.detail!.isNotEmpty) {
      return '$base（${resourceName.replaceFirst('%s', error.detail!)}）';
    }
    return base;
  }
}

/// `config.save` 的两种失败 + 客户端诊断。
class SSave {
  SSave._();

  static const String savedOk = '已保存';
  static const String savedButUnusableTitle = '已保存，但还不能同步';
  static const String savedButUnusableBody =
      '文件已经写下去了。下面这些问题会让 check / plan / sync 拦住它 —— 修好之后才跑得动。';
  static const String fixNow = '去修正';
  static const String hardErrorTitle = '没有保存：外置规则文件读不出来';
  static const String hardErrorBody =
      '配置**没有**落盘。这一类不能放行 —— 放行会让 plan / sync 少一套规则地跑，'
      '而配置文本看上去毫无问题。';
  static const String hardErrorFile = '读不出来的文件：%s';
  static const String dismiss = '我知道了';

  static const String diagnostics = '客户端诊断';
  static const String eventFrames = '事件帧 读成功 / 释放成功';
  static const String eventFrameMismatch =
      '两个数字不等：读成功的事件没有全部被释放，或存在重复释放（契约 §1.1）。';
  static const String eventReadFailures = '事件读失败次数';
  static const String eventDropped = '被限速丢掉的日志行数';
  static const String designExternal =
      '这是设计外状态（契约 §1.1 规定这条路径不该到达），请上报并附诊断信息。';
  static const String backpressure =
      '不是错误：引擎对客户端日志限速 20 行/秒，超出即丢弃（背压）。';
  static const String zero = '0';
}

/// 三态字段的统一文案。**这是全篇最容易被误解的一处，文案必须说清「不碰」。**
class STri {
  STri._();

  static const String unset = '未设置';
  static const String unsetNumber = '未设置（不碰订阅里已有的值）';
  static const String setOverride = '覆盖';
  static const String backToUnset = '回到未设置';
  static const String whySeason = '留空 = 交给 ANI-RSS 按每个 RSS 的实际集数自己推断。';
  static const String enableUnset = '未设置 = 新订阅不启用（默认）';
  static const String enabledUnset = '未设置 = 启用（默认）';
  static const String japanOnlyUnset = '未设置 = 只列日番（默认）';
  static const String standbyOffsetHint = '副 RSS 的集偏移，与主 RSS 分开。';
}

/// 概览页。
class SOverview {
  SOverview._();

  static const String workspace = '当前工作区';
  static const String noWorkspace = '还没有选工作区：先确认引擎在哪个目录读 config.yaml。';
  static const String instances = '实例与方案';
  static const String noInstances = '还没有配置任何 ANI-RSS 实例。先在「实例」页添加一个，规则才有地方生效。';
  static const String goInstances = '去添加实例';
  static const String lease = '写入权（单飞租约）';
  static const String leaseHeld = '被占用';
  static const String leaseFree = '空闲';
  static const String leaseHolder = '持有者';
  static const String leaseAge = '距上次心跳';
  static const String leaseExpired = '已过期（可以接管）';
  static const String takeover = '接管租约';
  static const String recentRuns = '最近几轮';
  static const String noRuns = '还没跑过同步。点「立即同步」跑第一轮，结果会出现在这里。';
  static const String syncNow = '立即同步';
  static const String syncStarted = '已提交同步任务，进度见下方「进行中的任务」。';
  static const String running = '进行中的任务';
  static const String noRunning = '没有正在跑的任务。';
  static const String cancel = '取消任务';
  static const String failureBanner = '有失败的运行记录';
  static const String expandFailures = '展开逐条原因';
  static const String nextRun = '下次自动运行';
  static const String platform = '平台';
  static const String version = '版本';
  static const String refreshAll = '刷新全部';
  static const String leaseConflictTitle = '写权被另一个进程占着，这一轮没能开始';
  static const String leaseConflictBody = '另一个进程正在写这份配置。可以点「接管租约」把它抢过来，'
      '或者等它这一轮跑完再来。';
  static const String jobPlanDetail = '展开改了哪几条';
  static const String jobErrors = '逐条失败原因';
  static const String jobElapsed = '耗时';
}

/// 实例页。
class SInstances {
  SInstances._();

  static const String title = '实例';
  static const String addInstance = '添加实例';
  static const String editInstance = '编辑实例';
  static const String name = '名称';
  static const String baseUrl = 'baseUrl';
  static const String apiKey = 'apiKey';
  static const String profile = '挂的方案';
  static const String enabled = '启用';
  static const String test = '测试连接';
  static const String testing = '测试中…';
  static const String openSubscriptions = '订阅列表';
  static const String noInstances = '还没有实例。ANI-RSS 实例是「谁在用」，方案是「怎么用」，先加实例。';
  static const String apiKeyHint = '支持 \${ENV_VAR} 写法，避免明文落盘。留空会沿用原值。';
  static const String apiKeyUnchanged = '（不改动：留空即保留原来的密钥）';
  static const String profileHint = '留空 = 用顶层规则（绝大多数老配置就是这样）。';
  static const String deleteInstance = '删除这个实例';
  static const String deleteInstanceConfirm = '将从这个工作区的配置里删掉实例「%s」。配置里引用它的地方（站点数据来源 / 番剧配置的实例限定）会失去目标。';
  static const String maskedKey = '遮蔽显示';
  static const String showKey = '显示原文';
  static const String hideKey = '隐藏';
  static const String scheme = '生效方案';
  static const String subscriptions = '订阅数';
  static const String latency = '延迟';
  static const String testOk = '连接正常';
  static const String testFailed = '连接失败';
}

/// 订阅页。
class SSubs {
  SSubs._();

  static const String title = '订阅';
  static const String refreshFromInstance = '从实例重读';
  static const String filterAll = '全部';
  static const String filterStandby = '有副 RSS';
  static const String filterNoMatch = '未匹配规则';
  static const String filterDisabled = '已停用';
  static const String columnTitle = '标题';
  static const String columnKey = '标识';
  static const String columnRss = '主 / 副 RSS';
  static const String columnNumbers = '季度 / 主偏移 / 副偏移';
  static const String columnState = '状态';
  static const String selected = '已选 %d 条';
  static const String selectAllVisible = '全选当前';
  static const String clearSelection = '清空选择';
  static const String editSelected = '批量改字段';
  static const String planAndApply = '规划并应用';
  static const String deleteSelected = '删除所选';
  static const String deleteConfirm = '将从实例「%s」删除 %d 条订阅%s。\n这个动作无法撤销。';
  static const String deleteConfirmFiles = '（并删除已经下载的文件）';
  static const String deleteFiles = '同时删除已下载的文件';
  static const String noSubscriptions = '这个实例里还没有订阅。可以直接在这里加，或去「新番」页勾选后批量加入。';
  static const String emptyNext = '下一步：在「新番」页勾选要追的番剧后批量加入，或去「番剧」页加一条番剧级配置再跑一轮同步。';
  static const String filteredEmpty = '当前筛选/搜索没有命中任何订阅。清掉条件就能看到全部。';
  static const String instanceDisabledHint = '这个实例当前是停用状态，同步不会处理它。先在「实例」页启用。';
  static const String issues = '读取时有 N 条降级说明';
  static const String mainGroup = '主';
  static const String standbyGroup = '副';
  static const String noMain = '没有主 RSS';
  static const String patchTitle = '批量修改';
  static const String patchHint = '没勾的字段一律不动。profile 选「清除覆盖」会把该订阅的覆盖去掉。';
  static const String dryRun = '只算差异（不写入）';
  static const String apply = '写入';
  static const String changed = '将改动的字段';
  static const String updatedCount = '写入 %d 条，跳过 %d 条';
  static const String planTitle = '打点计划';
  static const String planEmpty = '计划为空：没有需要改动的订阅。';
  static const String planWritable = '可写入 %d 条';
  static const String applyPlan = '应用计划';
  static const String applyPlanConfirm = '将按这份计划写入实例「%s」，共 %d 条改动。写入是整份覆盖（setAni），请确认计划无误。';
  static const String profileClear = '清除覆盖';
  static const String profileInherit = '跟随实例';
}

/// 规则页（顶层 + 方案，两级）。
class SRules {
  SRules._();

  static const String title = '规则';
  static const String defaultEntry = '默认（顶层规则）';
  static const String defaultEntryWhy = '没挂方案的实例用的就是这套';
  static const String usedBy = '被这些实例挂着：%s';
  static const String usedByNone = '当前没有实例挂这套方案';
  static const String tabRules = '规则';
  static const String tabDeny = '排除';
  static const String tabFiles = '外置规则文件';
  static const String tabSites = '站点顺序';
  static const String addRule = '添加规则';
  static const String addDeny = '添加排除';
  static const String maxStandby = '副 RSS 上限';
  static const String maxStandbyHint = '每条订阅保留几条副 RSS。N 条 = 每条订阅每轮多花 N 秒。';
  static const String colName = '名称';
  static const String colMatch = '正则';
  static const String colPriority = '优先级';
  static const String colScope = '作用域';
  static const String colIn = '作用位置';
  static const String colWhy = '原因';
  static const String scopeMain = '主 RSS';
  static const String scopeStandby = '副 RSS';
  static const String scopeBoth = '两者';
  static const String inAny = '组名+标题';
  static const String inGroup = '只看组名';
  static const String inTitle = '只看标题';
  static const String filesHint = '外置文件的内容只能在文件里改（界面只读展示）。一个文件一行，不聚合。';
  static const String filesEmpty = '这套没用外置规则文件：规则全部写在内联列表里。';
  static const String fileContent = '查看内容';
  static const String sitesHint = '顺序即优先级：排在前面的站点先被取用。';
  static const String sitesInherit = '这套方案没写站点顺序 = 继承顶层顺序。';
  static const String sitesInheritAction = '回到继承顶层';
  static const String addProfile = '新建方案';
  static const String newProfileName = '方案名';
  static const String deleteProfile = '删除方案';
  static const String deleteProfileConfirm = '将删除方案「%s」，它当前被 %d 个实例挂着：%s。删除后这些实例会回落到顶层规则。';
  static const String defaultCannotDelete = '顶层规则不可删除';
  static const String topLevel = '顶层';
  static const String ruleFilesCount = '%d 个文件';
  static const String priorityHint = '数字越小越优先';
}

/// 番剧配置页（targets 是一等公民）。
class STargets {
  STargets._();

  static const String title = '番剧配置';
  static const String subtitle = '番剧级配置：规定季度与集偏移、决定新订阅是否启用、可以按番剧覆盖方案。'
      '它和「实例里真实存在的订阅」不是一回事。';
  static const String addTarget = '添加番剧配置';
  static const String editTarget = '编辑番剧配置';
  static const String colTitle = '标题';
  static const String colBgm = 'bgmId';
  static const String colSeasonOffset = '季度 / 主偏移 / 副偏移';
  static const String colProfile = '方案覆盖';
  static const String colInstances = '限定实例';
  static const String colEnable = '新订阅启用';
  static const String colHits = '命中订阅';
  static const String hitsUnknown = '无法计算：%s';
  static const String hitsHint = '来自 plan.build 的 actions[].target，只反映当前这次计划的结果。';
  static const String empty = '还没有番剧级配置。不写它也能跑 —— 这里用来给个别番剧单独定季度与集偏移。';
  static const String emptyAction = '去「新番」页挑一部';
  static const String allInstances = '全部启用实例';
  static const String jpTitleHint = '日文原名。Nyaa 这类站点只认日文/罗马音。';
  static const String bgmIdHint = '可以直接贴 https://bgm.tv/subject/665390 这样的链接。';
  static const String titleOrBgmRequired = '标题与 bgmId 至少要有一个，否则匹配不到任何番剧';
  static const String deleteConfirm = '将从配置里删掉番剧配置「%s」。它只是配置，不会去动实例里的订阅。';
}

/// 新番页。
class SDiscover {
  SDiscover._();

  static const String title = '本季新番';
  static const String refresh = '刷新列表';
  static const String onlyJapan = '只列日番';
  static const String japanOnlyChanged = '筛选已切换（还没保存到配置）';
  static const String added = '已添加';
  static const String notAdded = '未添加';
  static const String selectedCount = '已选 %d 部';
  static const String addToTargets = '加入 targets';
  static const String addDialog = '添加番剧';
  static const String addDialogHint = '搜 Bangumi，挑一部，然后选它加到哪边。';
  static const String searchHint = '番剧名（中文或日文）';
  static const String targetChoice = '加进番剧配置';
  static const String targetChoiceWhy = '写进 config.yaml 的 targets，下次同步才会进实例';
  static const String instanceChoice = '直接加进实例';
  static const String instanceChoiceWhy = '立刻写进实例，不经过 targets';
  static const String bothChoice = '两边都加';
  static const String bothChoiceWhy = '先写配置再建订阅，两处都会有一条';
  static const String resultTargets = '已写入 targets：%d 条';
  static const String resultInstance = '已在实例「%s」新建订阅（%s）';
  static const String resultBoth = '两处都已写入';
  static const String filtered = '被「只列日番」过滤掉 %d 条';
  static const String unmatched = '有 %d 条在 Bangumi 上认不出条目，认不出来就进不了列表';
  static const String instanceError = '读实例失败：%s（此时的「已添加」判定不可信）';
  static const String warnings = '来源降级：%s';
  static const String sources = '来源：%s';
  static const String empty = '没有拉到任何番剧。来源可能暂时不可用，或者筛选条件把结果都排除了。';
  static const String noSearchResult = '没有搜到。换个关键词试试。';
  static const String searchEmpty = '输入关键字后才会搜索。';
}

/// 运行日志页。
class SJournal {
  SJournal._();

  static const String title = '运行日志';
  static const String onlyFailures = '只看失败';
  static const String clear = '清空运行记录';
  static const String clearConfirm = '将清空 %d 条运行记录（文件：%s）。清空后无法恢复，「最近几轮」会变成空的。';
  static const String cleared = '已清空 %d 条';
  static const String empty = '还没跑过同步，所以没有运行记录。跑一轮之后这里会出现每次的耗时、写入条数与逐条失败原因。';
  static const String emptyNext = '下一步：回「概览」页点「立即同步」，跑完就会出现在这里。';
  static const String filteredEmpty = '当前这几轮都成功了（没有失败记录）。关掉「只看失败」看全部。';
  static const String busy = '这轮让开了（上一轮还没跑完）';
  static const String eventStream = '事件流（实时）';
  static const String useFallback = '从 log.recent 补回最近的日志';
  static const String path = '日志文件';
  static const String colWhen = '时间';
  static const String colMode = '触发';
  static const String colResult = '结果';
  static const String colCounts = '可写 / 更新 / 新增 / 跳过';
  static const String colMs = '耗时';
}

/// 设置页。
class SSettings {
  SSettings._();

  static const String title = '设置';
  static const String sectionWorkspace = '工作区';
  static const String workspaceHint = '桌面端默认与可执行文件同目录（GUI 与 CLI 放一起就共用同一份 config.yaml）。';
  static const String workspacePath = 'config.yaml 路径';
  static const String dataDir = '数据目录';
  static const String applyWorkspace = '切换工作区';
  static const String recentWorkspaces = '最近工作区';
  static const String workspaceEmpty = '路径为空。填一个含 config.yaml 的目录。';
  static const String workspaceApplied = '工作区已切换';
  static const String sectionBackend = '后端';
  static const String backendLocal = '本机引擎（进程内 FFI）';
  static const String backendNode = '远端节点（HTTP）';
  static const String nodeUrl = '节点地址';
  static const String nodeToken = '访问 token';
  static const String nodeTokenHint = '节点绑非回环地址时必须带 token，否则它会拒绝启动。';
  static const String connect = '连接';
  static const String connected = '已连接';
  static const String sectionSync = '同步与后台';
  static const String interval = '自动同步间隔（分钟）';
  static const String intervalHint = '0 = 不自动。';
  static const String preview = '预览模式（preview）';
  static const String addMissing = '实例里没有的番剧自动新增（addMissing）';
  static const String sectionNetwork = '网络与代理';
  static const String proxy = '出站代理';
  static const String proxyHint = '留空 = 用环境变量 ANISYNC_PROXY，其次 HTTPS_PROXY / HTTP_PROXY。作用于站点数据、Bangumi 搜索与封面。';
  static const String listen = '监听地址';
  static const String listenHint = '只影响 `AniSync serve` 节点模式；默认只绑 127.0.0.1。';
  static const String sectionDiscover = '自动发现';
  static const String discoverSources = '发现来源';
  static const String discoverDays = '时间窗口（天）';
  static const String discoverLimit = '单次新增上限';
  static const String discoverEnable = '发现出来的新订阅直接启用';
  static const String discoverJapanOnly = '只列日本 TV 番剧与剧场版';
  static const String bgmUser = 'Bangumi 用户名 / uid（bangumi-collections 需要）';
  static const String excludeHint = '标题正则黑名单，命中就不发现（一行一条）。';
  static const String sectionOffsets = '季度与集偏移（全局兜底）';
  static const String offsetsHint = '这三个是「番剧没命中 targets 时的统一覆盖」。留空 = 不碰订阅里已有的值。';
  static const String sectionBackup = '备份';
  static const String backupCreate = '创建备份';
  static const String backupEmpty = '还没有备份。备份会把各实例的订阅导出一份，还原时按文件写回。';
  static const String backupRestore = '还原';
  static const String backupRestoreConfirm = '将用备份「%s」还原 %d 个实例的订阅（共 %d 条）。这会覆盖实例里现有的订阅数据。';
  static const String backupPrune = '还原时清掉备份里没有的（prune）';
  static const String sectionInfo = '版本与平台';
  static const String libraryPath = '引擎动态库';
  static const String libraryPathMissing = '尚未加载';
  static const String opsCatalogue = 'op 表';
  static const String goVersion = 'Go 版本';
  static const String schemaVersion = '契约版本';
  static const String sectionSample = '配置样例';
  static const String showSample = '查看 config.example.yaml';
  static const String jsonRoundTrip = '把当前配置按 save 的形状导出（对拍用）';
}

/// 危险操作确认框。
class SConfirm {
  SConfirm._();

  static const String title = '确认操作';
  static const String irreversible = '这个动作无法撤销。';
  static const String typeToConfirm = '输入「%s」以确认';
}
