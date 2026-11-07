import 'json.dart';

/// 规则作用域：决定这条规则参与挑选主 RSS 还是副 RSS。
class RuleScope {
  RuleScope._();

  static const String main = 'main';
  static const String standby = 'standby';
  static const String both = 'both';
  static const List<String> values = <String>[main, standby, both];
}

/// 匹配位置：决定 `match` 正则作用在哪个字符串上。
class RuleIn {
  RuleIn._();

  static const String any = 'any';
  static const String group = 'group';
  static const String title = 'title';
  static const List<String> values = <String>[any, group, title];
}

/// 全部合法站点标识（与 `config.KnownSiteNames` 一致）。
const List<String> knownSites = <String>['anibt', 'nyaa', 'animegarden', 'mikan'];

/// 站点显示名，只用于界面。标识本身不改。
const Map<String, String> siteLabels = <String, String>{
  'anibt': 'AniBT',
  'nyaa': 'Nyaa',
  'animegarden': 'AnimeGarden',
  'mikan': 'Mikan',
};

/// 自动发现来源（与 `config.DiscoverSourceNames` 一致）。
const List<String> discoverSources = <String>[
  'yuc',
  'animegarden',
  'bangumi-calendar',
  'bangumi-collections',
];

const Map<String, String> discoverSourceLabels = <String, String>{
  'yuc': 'Yuc 当季番剧表',
  'animegarden': 'AnimeGarden 字幕组在做',
  'bangumi-calendar': 'Bangumi 每日放送表',
  'bangumi-collections': 'Bangumi 用户在看',
};

/// 一条匹配规则。`priority` 数字越小越优先。
class Rule {
  Rule({
    this.name = '',
    this.match = '',
    this.priority = 0,
    this.site = '',
    this.scope = '',
    this.inValue = '',
  });

  factory Rule.fromJson(Map<String, Object?> json) => Rule(
        name: Json.str(json['name']),
        match: Json.str(json['match']),
        priority: Json.intOf(json['priority']),
        site: Json.str(json['site']),
        scope: Json.str(json['scope']),
        inValue: Json.str(json['in']),
      );

  String name;
  String match;
  int priority;

  /// 限定只在该来源生效，空表示不限。
  String site;

  /// `main` / `standby` / `both`；空表示 main。
  String scope;

  /// `any` / `group` / `title`；空表示 any。字段名 `in` 是 Dart 关键字，故为 `inValue`。
  String inValue;

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'match': match,
        'priority': priority,
        if (site.isNotEmpty) 'site': site,
        if (scope.isNotEmpty) 'scope': scope,
        if (inValue.isNotEmpty) 'in': inValue,
      };
}

/// 命中即剔除该候选。
class DenyRule {
  DenyRule({this.match = '', this.why = ''});

  factory DenyRule.fromJson(Map<String, Object?> json) => DenyRule(
        match: Json.str(json['match']),
        why: Json.str(json['why']),
      );

  String match;
  String why;

  Map<String, Object?> toJson() => <String, Object?>{
        'match': match,
        if (why.isNotEmpty) 'why': why,
      };
}

/// 一个外置规则文件的加载结果，只用于展示。**一个文件一行，不聚合。**
class RuleFileInfo {
  RuleFileInfo({this.path = '', this.kind = '', this.count = 0});

  factory RuleFileInfo.fromJson(Map<String, Object?> json) => RuleFileInfo(
        path: Json.str(json['path']),
        kind: Json.str(json['kind']),
        count: Json.intOf(json['count']),
      );

  String path;

  /// `primary` / `deny`。
  String kind;
  int count;

  Map<String, Object?> toJson() => <String, Object?>{
        'path': path,
        'kind': kind,
        'count': count,
      };
}

/// 站点数据来源。
class Sources {
  Sources({
    this.instance = '',
    this.order,
    this.stopOnFirstMatch = false,
    this.timeoutSeconds = 25,
    this.cacheMinutes = 720,
  });

  factory Sources.fromJson(Map<String, Object?> json) => Sources(
        instance: Json.str(json['instance']),
        order: Json.strListOrNull(json['order']),
        stopOnFirstMatch: Json.boolOf(json['stopOnFirstMatch']),
        timeoutSeconds: Json.intOf(json['timeoutSeconds'], 25),
        cacheMinutes: Json.intOf(json['cacheMinutes'], 720),
      );

  /// 借用哪个实例的站点数据；留空 = 第一个启用的实例。
  ///
  /// 刻意**无条件输出**：Go 侧没有 omitempty，空值也要写出来，
  /// 否则保存一次这个键就没了（挂在它上面的注释也跟着消失）。
  String instance;

  /// 站点顺序；Go 侧 nil 会序列化成 `null`，所以这里保留可空。
  List<String>? order;

  bool stopOnFirstMatch;
  int timeoutSeconds;
  int cacheMinutes;

  List<String> get orderOrEmpty => order ?? const <String>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'instance': instance,
        'order': order,
        'stopOnFirstMatch': stopOnFirstMatch,
        'timeoutSeconds': timeoutSeconds,
        'cacheMinutes': cacheMinutes,
      };
}

/// 全局匹配规则（顶层）与方案里的规则是同一个结构。
class Rules {
  Rules({
    this.primary,
    this.deny,
    this.primaryFiles,
    this.denyFiles,
    this.maxStandby = 1,
  });

  factory Rules.fromJson(Map<String, Object?> json) => Rules(
        // primary / deny 在 Go 侧**不带** omitempty：nil slice 会序列化成 `null`。
        // 所以这里保留 null 与 [] 的区别 —— 写回时也照原样写。
        primary: json['primary'] == null
            ? null
            : Json.mapList(json['primary']).map(Rule.fromJson).toList(),
        deny: json['deny'] == null
            ? null
            : Json.mapList(json['deny']).map(DenyRule.fromJson).toList(),
        primaryFiles: Json.strListOrNull(json['primaryFiles']),
        denyFiles: Json.strListOrNull(json['denyFiles']),
        maxStandby: Json.intOf(json['maxStandby'], 1),
      );

  /// 白名单；Go 侧 `primary` 不带 omitempty，nil → `null`。
  List<Rule>? primary;

  /// 黑名单；同上，`null` 与 `[]` 都要保真（05 样例的「收藏」方案就没写 deny）。
  List<DenyRule>? deny;

  /// 外置规则文件（白名单侧）。
  List<String>? primaryFiles;

  /// 外置规则文件（黑名单侧）。
  List<String>? denyFiles;

  /// 每条订阅保留几条副 RSS。
  int maxStandby;

  List<Rule> get primaryOrEmpty => primary ?? const <Rule>[];
  List<DenyRule> get denyOrEmpty => deny ?? const <DenyRule>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'primary': primary?.map((r) => r.toJson()).toList(),
        'deny': deny?.map((r) => r.toJson()).toList(),
        if (primaryFiles != null && primaryFiles!.isNotEmpty)
          'primaryFiles': primaryFiles,
        if (denyFiles != null && denyFiles!.isNotEmpty) 'denyFiles': denyFiles,
        'maxStandby': maxStandby,
      };
}

/// 一个 ANI-RSS 实例。
class Instance {
  Instance({
    this.name = '',
    this.baseUrl = '',
    this.apiKey = '',
    this.profile = '',
    this.enabled,
  });

  factory Instance.fromJson(Map<String, Object?> json) => Instance(
        name: Json.str(json['name']),
        baseUrl: Json.str(json['baseUrl']),
        apiKey: Json.str(json['apiKey']),
        profile: Json.str(json['profile']),
        enabled: Json.boolOrNull(json['enabled']),
      );

  String name;
  String baseUrl;

  /// **原文**：可能是 `${ANISYNC_KEY_A}` 这种占位符。
  /// 界面编辑写的是这里，不是 `InstanceView.apiKey`（那是遮蔽值，写回去会毁密钥）。
  String apiKey;

  /// 挂的方案名；空 = 用顶层 rules / sources。
  String profile;

  /// **三态**：`null` = 启用（默认）。
  bool? enabled;

  /// 生效的启用状态（展示用，不回写）。
  bool get isEnabled => enabled ?? true;

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'baseUrl': baseUrl,
        'apiKey': apiKey,
        if (profile.isNotEmpty) 'profile': profile,
        if (enabled != null) 'enabled': enabled,
      };
}

/// 方案自己的站点顺序；留空 = 用顶层 `sources.order`。
class ProfileSources {
  ProfileSources({this.order});

  factory ProfileSources.fromJson(Map<String, Object?> json) =>
      ProfileSources(order: Json.strListOrNull(json['order']));

  List<String>? order;

  Map<String, Object?> toJson() => <String, Object?>{
        if (order != null && order!.isNotEmpty) 'order': order,
      };
}

/// 一套命名的订阅方案。
class Profile {
  Profile({
    this.name = '',
    Rules? rules,
    this.sources,
    this.fileRules,
  }) : rules = rules ?? Rules();

  factory Profile.fromJson(Map<String, Object?> json) => Profile(
        name: Json.str(json['name']),
        rules: Rules.fromJson(Json.map(json['rules'])),
        sources: json['sources'] == null
            ? null
            : ProfileSources.fromJson(Json.map(json['sources'])),
        fileRules: Json
            .mapList(json['fileRules'])
            .map(RuleFileInfo.fromJson)
            .toList(),
      );

  String name;
  Rules rules;

  /// `null` = 这套方案没写站点顺序，用顶层 order。
  ProfileSources? sources;

  /// **只读派生**（Go 侧 `yaml:"-"`）：读进来展示，`toJson` 不写回。
  List<RuleFileInfo>? fileRules;

  List<RuleFileInfo> get ruleFilesOrEmpty =>
      fileRules ?? const <RuleFileInfo>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'name': name,
        'rules': rules.toJson(),
        if (sources != null) 'sources': sources!.toJson(),
        // fileRules 刻意不写回：它是 config.get 的只读派生，写回去语义就是错的。
      };
}

/// 一条番剧级配置。
///
/// 与「实例里真实存在的订阅」**不是一回事**：实例里手动加的订阅不在 targets 里，
/// 一条 target 也可能一条订阅都没命中。界面必须分开管理。
class Target {
  Target({
    this.title = '',
    this.jpTitle = '',
    this.bgmId = '',
    this.mikanId = '',
    this.instances,
    this.profile = '',
    this.enable,
    this.season,
    this.offset,
    this.standbyOffset,
  });

  factory Target.fromJson(Map<String, Object?> json) => Target(
        title: Json.str(json['title']),
        jpTitle: Json.str(json['jpTitle']),
        bgmId: Json.str(json['bgmId']),
        mikanId: Json.str(json['mikanId']),
        instances: Json.strListOrNull(json['instances']),
        profile: Json.str(json['profile']),
        enable: Json.boolOrNull(json['enable']),
        season: Json.intOrNull(json['season']),
        offset: Json.intOrNull(json['offset']),
        standbyOffset: Json.intOrNull(json['standbyOffset']),
      );

  String title;

  /// 日文原名。Nyaa 这类站点只认日文/罗马音。
  String jpTitle;

  /// 接受 bgm 链接（`https://bgm.tv/subject/123`），保存时后端会归一成数字。
  String bgmId;
  String mikanId;

  /// 限定生效实例；留空 = 全部启用实例。
  List<String>? instances;

  /// 按番剧覆盖方案；空 = 不覆盖。
  String profile;

  /// **三态**：`null` = 新订阅不启用（默认）。
  bool? enable;

  /// **三态**：`null` = 沿用 `sync` 的全局值；全局也为 `null` 就不碰订阅里已有的值。
  int? season;
  int? offset;
  int? standbyOffset;

  Map<String, Object?> toJson() => <String, Object?>{
        'title': title,
        if (jpTitle.isNotEmpty) 'jpTitle': jpTitle,
        'bgmId': bgmId,
        if (mikanId.isNotEmpty) 'mikanId': mikanId,
        if (instances != null && instances!.isNotEmpty)
          'instances': instances,
        if (profile.isNotEmpty) 'profile': profile,
        if (enable != null) 'enable': enable,
        if (season != null) 'season': season,
        if (offset != null) 'offset': offset,
        if (standbyOffset != null) 'standbyOffset': standbyOffset,
      };
}

/// 自动发现。
class Discover {
  Discover({
    this.enabled = false,
    this.sources,
    this.bgmUser = '',
    this.days = 180,
    this.limit = 50,
    this.enable,
    this.exclude,
    this.japanOnly,
  });

  factory Discover.fromJson(Map<String, Object?> json) => Discover(
        enabled: Json.boolOf(json['enabled']),
        sources: Json.strListOrNull(json['sources']),
        bgmUser: Json.str(json['bgmUser']),
        days: Json.intOf(json['days'], 180),
        limit: Json.intOf(json['limit'], 50),
        enable: Json.boolOrNull(json['enable']),
        exclude: Json.strListOrNull(json['exclude']),
        japanOnly: Json.boolOrNull(json['japanOnly']),
      );

  bool enabled;
  List<String>? sources;
  String bgmUser;

  /// `0` 会被 Go 省略（`omitempty`），`Normalize` 会补成 180。
  int days;
  int limit;

  /// **三态**：`null` = 发现出来的新订阅不启用（默认）。
  bool? enable;

  List<String>? exclude;

  /// **三态**：`null` = 只列日番（默认）。
  bool? japanOnly;

  bool get japanOnlyEffective => japanOnly ?? true;
  List<String> get sourcesOrEmpty => sources ?? const <String>[];

  Map<String, Object?> toJson() => <String, Object?>{
        'enabled': enabled,
        if (sources != null && sources!.isNotEmpty) 'sources': sources,
        if (bgmUser.isNotEmpty) 'bgmUser': bgmUser,
        if (days != 0) 'days': days,
        if (limit != 0) 'limit': limit,
        if (enable != null) 'enable': enable,
        if (exclude != null && exclude!.isNotEmpty) 'exclude': exclude,
        if (japanOnly != null) 'japanOnly': japanOnly,
      };
}

/// 同步行为。
class Sync {
  Sync({
    this.intervalMinutes = 0,
    this.preview = false,
    this.addMissing = false,
    this.season,
    this.offset,
    this.standbyOffset,
  });

  factory Sync.fromJson(Map<String, Object?> json) => Sync(
        intervalMinutes: Json.intOf(json['intervalMinutes']),
        preview: Json.boolOf(json['preview']),
        addMissing: Json.boolOf(json['addMissing']),
        season: Json.intOrNull(json['season']),
        offset: Json.intOrNull(json['offset']),
        standbyOffset: Json.intOrNull(json['standbyOffset']),
      );

  /// 0 = 不自动。
  int intervalMinutes;
  bool preview;
  bool addMissing;

  /// **三态**：`null` = **不碰订阅里已有的值**；写了（哪怕 `0`）= 覆盖。
  ///
  /// 这是全篇最容易翻车的一处：把 `null` 落成 `0` 会让一次保存把全部订阅的偏移洗成 0。
  int? season;
  int? offset;
  int? standbyOffset;

  Map<String, Object?> toJson() => <String, Object?>{
        'intervalMinutes': intervalMinutes,
        'preview': preview,
        'addMissing': addMissing,
        if (season != null) 'season': season,
        if (offset != null) 'offset': offset,
        if (standbyOffset != null) 'standbyOffset': standbyOffset,
      };
}

/// AniSync 的完整配置。字段名与 Go 的 json 标签**逐字一致**，键顺序也照 Go 结构体顺序。
///
/// 只有这个家族需要「逐字保真」的 `toJson` —— 因为只有它会被 `config.save` 整份回传。
class Config {
  Config({
    this.listen = '127.0.0.1:51837',
    this.proxy = '',
    Sources? sources,
    Rules? rules,
    this.instances,
    this.profiles,
    this.targets,
    Discover? discover,
    Sync? sync,
    this.fileRules,
  })  : sources = sources ?? Sources(),
        rules = rules ?? Rules(),
        discover = discover ?? Discover(),
        sync = sync ?? Sync();

  factory Config.fromJson(Map<String, Object?> json) => Config(
        listen: Json.str(json['listen'], '127.0.0.1:51837'),
        proxy: Json.str(json['proxy']),
        sources: Sources.fromJson(Json.map(json['sources'])),
        rules: Rules.fromJson(Json.map(json['rules'])),
        // 不带 omitempty 的列表：**保留 null 与 [] 的区别**（Go 的 nil slice → `null`）。
        instances: json['instances'] == null
            ? null
            : Json.mapList(json['instances']).map(Instance.fromJson).toList(),
        profiles: json['profiles'] == null
            ? null
            : Json.mapList(json['profiles']).map(Profile.fromJson).toList(),
        targets: json['targets'] == null
            ? null
            : Json.mapList(json['targets']).map(Target.fromJson).toList(),
        discover: Discover.fromJson(Json.map(json['discover'])),
        sync: Sync.fromJson(Json.map(json['sync'])),
        fileRules: Json
            .mapList(json['fileRules'])
            .map(RuleFileInfo.fromJson)
            .toList(),
      );

  /// 空配置（`config.default` 之外的本地面板初值；不承担默认值语义）。
  factory Config.empty() => Config();

  /// 监听地址。**无条件输出**（Go 侧无 omitempty）。
  String listen;

  /// 出站代理。**无条件输出**（同上，空串也要写）。
  String proxy;

  Sources sources;
  Rules rules;

  /// nil → `null`（Go 侧不带 omitempty）。
  List<Instance>? instances;

  /// 空时省略键（Go 侧 `omitempty`）。
  List<Profile>? profiles;

  /// nil → `null`。
  List<Target>? targets;

  /// 永远出现（结构体上的 omitempty 在 encoding/json 下无效 —— 已实测）。
  Discover discover;

  Sync sync;

  /// **只读派生**（Go 侧 `yaml:"-"`）：读进来展示，**`toJson` 不写回**。
  List<RuleFileInfo>? fileRules;

  List<Instance> get instancesOrEmpty => instances ?? const <Instance>[];
  List<Profile> get profilesOrEmpty => profiles ?? const <Profile>[];
  List<Target> get targetsOrEmpty => targets ?? const <Target>[];
  List<RuleFileInfo> get fileRulesOrEmpty =>
      fileRules ?? const <RuleFileInfo>[];

  /// 按名字取实例。
  Instance? instanceByName(String name) {
    for (final item in instancesOrEmpty) {
      if (item.name == name) return item;
    }
    return null;
  }

  /// 按名字取方案。
  Profile? profileByName(String name) {
    for (final item in profilesOrEmpty) {
      if (item.name == name) return item;
    }
    return null;
  }

  /// 深拷一份，供「编辑后取消」用。
  Config clone() => Config.fromJson(toJson());

  Map<String, Object?> toJson() => <String, Object?>{
        'listen': listen,
        'proxy': proxy,
        'sources': sources.toJson(),
        'rules': rules.toJson(),
        'instances': instances?.map((e) => e.toJson()).toList(),
        // profiles 上是 omitempty：空 / null 都不写键。
        if (profiles != null && profiles!.isNotEmpty)
          'profiles': profiles!.map((e) => e.toJson()).toList(),
        'targets': targets?.map((e) => e.toJson()).toList(),
        'discover': discover.toJson(),
        'sync': sync.toJson(),
        // fileRules 刻意不写回：yaml:"-"，只读派生。
      };
}
