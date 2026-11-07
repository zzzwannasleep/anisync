import 'dart:convert';

import 'package:anisync_app/src/model/config.dart';
import 'package:flutter_test/flutter_test.dart';

/// 三态字段与「null / 省略」的保真往返。
///
/// 这是全篇最容易翻车的一处：把 `null` 读成 `false` / `0`，
/// 一次 `config.save` 就会把所有订阅的偏移洗成 0。
void main() {
  /// 模拟 Go 侧从 `samples/05-双方案.yaml` 读出来（并 `Normalize()` 过）之后
  /// `config.get` 会回的那份 JSON：
  /// - `instances[]` 没写 `enabled` → 键缺席
  /// - `targets[]` 只写了 `season: 2`，`offset` / `standbyOffset` / `enable` 缺席
  /// - `sync` 三个三态全缺席
  /// - `discover` 没写 `japanOnly` / `enable`（指针 omitempty 生效）
  /// - 方案的 `rules` 没写 `deny` → `null`（不带 omitempty 的 nil slice）
  Map<String, Object?> sample05AsGoEmitsIt() => <String, Object?>{
        'listen': '127.0.0.1:51837',
        'proxy': '',
        'sources': <String, Object?>{
          'instance': '',
          'order': <String>['anibt', 'nyaa'],
          'stopOnFirstMatch': false,
          'timeoutSeconds': 25,
          'cacheMinutes': 720,
        },
        'rules': <String, Object?>{
          'primary': null,
          'deny': <Map<String, Object?>>[
            <String, Object?>{'match': '合集|全集|(?i)\\bbatch\\b', 'why': '不要合集'},
          ],
          'primaryFiles': <String>['raws.yaml', 'subs.yaml'],
          'denyFiles': <String>['deny.yaml'],
          'maxStandby': 1,
        },
        'instances': <Map<String, Object?>>[
          <String, Object?>{
            'name': '追番库',
            'baseUrl': 'https://ass-a.example.com/',
            'apiKey': '\${ANISYNC_KEY_A}',
            'profile': '追番',
          },
          <String, Object?>{
            'name': '收藏库',
            'baseUrl': 'https://ass-b.example.com/',
            'apiKey': '\${ANISYNC_KEY_B}',
            'profile': '收藏',
          },
        ],
        'profiles': <Map<String, Object?>>[
          <String, Object?>{
            'name': '追番',
            'rules': <String, Object?>{
              'primary': null,
              'deny': <Map<String, Object?>>[
                <String, Object?>{'match': '(?i)bdrip', 'why': '追番不等 BDRip，完结后再单独收'},
              ],
              'primaryFiles': <String>['raws.yaml', 'subs.yaml'],
              'denyFiles': <String>['deny.yaml'],
              'maxStandby': 1,
            },
            'sources': <String, Object?>{
              'order': <String>['anibt', 'animegarden'],
            },
            'fileRules': <Map<String, Object?>>[
              <String, Object?>{'path': 'raws.yaml', 'kind': 'primary', 'count': 4},
            ],
          },
          <String, Object?>{
            'name': '收藏',
            'rules': <String, Object?>{
              'primary': null,
              'deny': null,
              'primaryFiles': <String>['raws.yaml', 'subs.yaml', 'all.yaml'],
              'denyFiles': <String>['deny.yaml'],
              'maxStandby': 12,
            },
            'sources': <String, Object?>{
              'order': <String>['anibt', 'animegarden', 'mikan', 'nyaa'],
            },
          },
        ],
        'targets': <Map<String, Object?>>[
          <String, Object?>{
            'title': '冰之城墙',
            'bgmId': '665390',
            'profile': '收藏',
            'season': 2,
          },
        ],
        'discover': <String, Object?>{
          'enabled': true,
          'sources': <String>['yuc'],
          'days': 180,
          'limit': 50,
        },
        'sync': <String, Object?>{
          'intervalMinutes': 0,
          'preview': false,
          'addMissing': false,
        },
        'fileRules': <Map<String, Object?>>[
          <String, Object?>{'path': 'raws.yaml', 'kind': 'primary', 'count': 4},
          <String, Object?>{'path': 'subs.yaml', 'kind': 'primary', 'count': 7},
          <String, Object?>{'path': 'deny.yaml', 'kind': 'deny', 'count': 8},
        ],
      };

  test('null 与「写了 0 / false」是两件事：三态字段不许被填默认值', () {
    final source = sample05AsGoEmitsIt();
    final config = Config.fromJson(source);

    // 读到的是 null，不是 false。
    expect(config.instancesOrEmpty.every((e) => e.enabled == null), isTrue);
    expect(config.targetsOrEmpty.first.enable, isNull);
    expect(config.targetsOrEmpty.first.offset, isNull);
    expect(config.targetsOrEmpty.first.standbyOffset, isNull);
    expect(config.sync.season, isNull);
    expect(config.sync.offset, isNull);
    expect(config.sync.standbyOffset, isNull);
    expect(config.discover.enable, isNull);
    expect(config.discover.japanOnly, isNull);

    final out = config.toJson();

    // 写回去时这些键必须**整个不存在**，绝不能是 false / 0。
    for (final instance in (out['instances']! as List)) {
      expect((instance as Map).containsKey('enabled'), isFalse);
    }
    final target = (out['targets']! as List).first as Map;
    expect(target.containsKey('enable'), isFalse);
    expect(target.containsKey('offset'), isFalse);
    expect(target.containsKey('standbyOffset'), isFalse);
    expect(target['season'], 2, reason: '写了 2 就要原样带回去');

    final sync = out['sync']! as Map;
    expect(sync.containsKey('season'), isFalse);
    expect(sync.containsKey('offset'), isFalse);
    expect(sync.containsKey('standbyOffset'), isFalse);
    expect(sync['intervalMinutes'], 0, reason: '没有 omitempty 的 0 必须保留');
    expect(sync['preview'], false);
    expect(sync['addMissing'], false);

    final discover = out['discover']! as Map;
    expect(discover.containsKey('enable'), isFalse);
    expect(discover.containsKey('japanOnly'), isFalse);
    expect(discover['enabled'], true);
    expect(discover['days'], 180);
  });

  test('NullPreservingLists：null 列表往返仍是 null，[] 仍是 []', () {
    final source = sample05AsGoEmitsIt();
    final out = Config.fromJson(source).toJson();

    // 不带 omitempty 的列表：null 就是 null（Go 的 nil slice 序列化成 null）。
    expect(out.containsKey('instances'), isTrue);
    expect(out.containsKey('targets'), isTrue);
    expect((out['rules']! as Map)['primary'], isNull);
    expect((out['rules']! as Map)['deny'], isNotNull);

    final favorite = (out['profiles']! as List)[1] as Map;
    expect(
      (favorite['rules']! as Map)['deny'],
      isNull,
      reason: '「收藏」方案没写 deny → Go 回 null → 必须原样回传 null（不是 []）',
    );

    // [] 也不能被写成 null。
    final empty = Config.fromJson(<String, Object?>{
      'listen': '127.0.0.1:51837',
      'rules': <String, Object?>{'primary': <Object?>[], 'deny': <Object?>[]},
      'instances': <Object?>[],
      'targets': <Object?>[],
    });
    final emptyOut = empty.toJson();
    expect((emptyOut['rules']! as Map)['primary'], <Object?>[]);
    expect((emptyOut['rules']! as Map)['deny'], <Object?>[]);
    expect(emptyOut['instances'], <Object?>[]);
    expect(emptyOut['targets'], <Object?>[]);
  });

  test('无条件输出的三个键：listen / proxy / sources.instance', () {
    final config = Config.fromJson(<String, Object?>{
      'listen': '',
      'proxy': '',
      'sources': <String, Object?>{'instance': ''},
    });
    final out = config.toJson();
    expect(out.containsKey('listen'), isTrue, reason: 'listen 必须无条件输出（Go 侧无 omitempty）');
    expect(out.containsKey('proxy'), isTrue);
    expect(out['listen'], '');
    expect(out['proxy'], '');
    expect((out['sources']! as Map).containsKey('instance'), isTrue);
    expect((out['sources']! as Map)['instance'], '');
  });

  test('只读派生不写回：fileRules 不落回 JSON', () {
    final config = Config.fromJson(sample05AsGoEmitsIt());
    expect(config.fileRulesOrEmpty.length, 3, reason: '读要读到，界面要显示');
    expect(config.profilesOrEmpty.first.ruleFilesOrEmpty.length, 1);

    final out = config.toJson();
    expect(out.containsKey('fileRules'), isFalse, reason: 'Config.fileRules 是 yaml:"-"，不许写回');
    for (final profile in (out['profiles']! as List)) {
      expect(
        (profile as Map).containsKey('fileRules'),
        isFalse,
        reason: 'profiles[].fileRules 同样是只读派生，不许写回',
      );
    }
  });

  test('omitempty 的可选键：空串 / 空列表 / null 都不写键', () {
    final config = Config.fromJson(sample05AsGoEmitsIt());
    final out = config.toJson();

    for (final instance in (out['instances']! as List)) {
      expect((instance as Map).containsKey('enabled'), isFalse);
    }
    final target = (out['targets']! as List).first as Map;
    expect(target.containsKey('jpTitle'), isFalse);
    expect(target.containsKey('mikanId'), isFalse);
    expect(target.containsKey('instances'), isFalse);

    final discover = out['discover']! as Map;
    expect(discover.containsKey('bgmUser'), isFalse);
    expect(discover.containsKey('exclude'), isFalse);
    expect(discover.containsKey('enable'), isFalse);
  });

  test('profiles 空时省略整个键（Go 侧 omitempty）', () {
    final config = Config.fromJson(<String, Object?>{
      'listen': '127.0.0.1:51837',
      'instances': <Object?>[],
      'targets': <Object?>[],
      'profiles': <Object?>[],
    });
    expect(config.profiles, isEmpty);
    expect(config.toJson().containsKey('profiles'), isFalse);
  });

  test('往返幂等：fromJson → toJson → fromJson → toJson 逐字相同', () {
    final source = sample05AsGoEmitsIt();
    final first = Config.fromJson(source).toJson();
    final second = Config.fromJson(first).toJson();
    expect(
      jsonEncode(second),
      jsonEncode(first),
      reason: '走一圈之后语义必须一字不变，否则 config.save 会漂移',
    );
  });

  test('clone() 深拷，改副本不影响原件（编辑后取消用）', () {
    final config = Config.fromJson(sample05AsGoEmitsIt());
    final copy = config.clone();
    copy.instancesOrEmpty.first.name = '改过的';
    copy.sync.offset = 0;
    expect(config.instancesOrEmpty.first.name, isNot('改过的'));
    expect(config.sync.offset, isNull, reason: '副本写 0 不该影响原件');
  });

  test('Rule / DenyRule 的可选键与 priority 保真', () {
    final rule = Rule.fromJson(<String, Object?>{
      'name': '放流组优先',
      'match': '(?i)^ani\\b',
      'priority': 0,
      'scope': 'main',
      'in': 'group',
    });
    expect(rule.priority, 0);
    final out = rule.toJson();
    expect(out['priority'], 0, reason: 'priority 不带 omitempty，0 也要写出来');
    expect(out['scope'], 'main');
    expect(out['in'], 'group');
    expect(out.containsKey('site'), isFalse);

    final plain = Rule.fromJson(<String, Object?>{
      'name': 'x',
      'match': 'y',
      'priority': 5,
    });
    expect(plain.toJson().containsKey('in'), isFalse);
    expect(plain.toJson().containsKey('scope'), isFalse);
  });
}
