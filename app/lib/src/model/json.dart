/// JSON 取值助手。
///
/// 两条纪律贯穿整个 model 层：
/// 1. **读要宽容**：Go 侧的可选字段可能缺席，缺席时给 `null` / 空集合，不许抛。
/// 2. **写要保真**：`null` 与「写了 0 / false」是两件事，所以写的时候区分
///    `put`（无条件）、`putIfNotNull`（null 就不写键）、`putIfNotEmpty`（空集合不写键）。
library;

/// 读：宽容取值。
class Json {
  Json._();

  static String str(Object? value, [String fallback = '']) =>
      value is String ? value : fallback;

  static String? strOrNull(Object? value) {
    if (value is String) return value;
    return null;
  }

  static int intOf(Object? value, [int fallback = 0]) =>
      intOrNull(value) ?? fallback;

  static int? intOrNull(Object? value) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    if (value is String) return int.tryParse(value);
    return null;
  }

  static double doubleOf(Object? value, [double fallback = 0]) {
    if (value is num) return value.toDouble();
    if (value is String) return double.tryParse(value) ?? fallback;
    return fallback;
  }

  static double? doubleOrNull(Object? value) {
    if (value is num) return value.toDouble();
    if (value is String) return double.tryParse(value);
    return null;
  }

  static bool boolOf(Object? value, [bool fallback = false]) =>
      value is bool ? value : fallback;

  /// 三态布尔：只有真的是 bool 才给值，其余（含缺席）给 `null`。
  static bool? boolOrNull(Object? value) => value is bool ? value : null;

  /// 字符串列表；`null` 与 `[]` 都读出 `[]`（展示用）。
  static List<String> strList(Object? value) {
    if (value is List) {
      return value.whereType<String>().toList(growable: false);
    }
    return const <String>[];
  }

  /// 字符串列表，**保留 null**：给需要区分「键缺席」与「空列表」的场景。
  static List<String>? strListOrNull(Object? value) {
    if (value is List) {
      return value.whereType<String>().toList();
    }
    return null;
  }

  static Map<String, Object?> map(Object? value) {
    if (value is Map) return value.cast<String, Object?>();
    return <String, Object?>{};
  }

  static Map<String, Object?>? mapOrNull(Object? value) {
    if (value is Map) return value.cast<String, Object?>();
    return null;
  }

  static List<Map<String, Object?>> mapList(Object? value) {
    if (value is List) {
      return value
          .whereType<Map>()
          .map((e) => e.cast<String, Object?>())
          .toList(growable: false);
    }
    return const <Map<String, Object?>>[];
  }

  static List<Object?>? listOrNull(Object? value) =>
      value is List ? value.toList() : null;

  static List<Object?> list(Object? value) =>
      value is List ? value.toList() : const <Object?>[];

  static DateTime? time(Object? value) {
    if (value is String && value.isNotEmpty) {
      return DateTime.tryParse(value);
    }
    return null;
  }
}

/// 写：`key` 一律输出（对应 Go 侧**不带** omitempty 的字段）。
void put(Map<String, Object?> out, String key, Object? value) {
  out[key] = value;
}

/// 写：值为 `null` 时**整个键省略**（对应 Go 侧的指针 / omitempty）。
///
/// 三态字段（`enabled` / `enable` / `season` / `offset` / `standbyOffset` /
/// `japanOnly`）必须走这个函数：把 `null` 落成 `false` / `0` 会改变语义
/// ——「不碰订阅里已有的偏移」会变成「把所有订阅的偏移洗成 0」。
void putIfNotNull(Map<String, Object?> out, String key, Object? value) {
  if (value != null) out[key] = value;
}

/// 写：`null` 与空集合都不写键（对应 Go 侧 slice/map 上的 omitempty）。
void putIfNotEmpty(Map<String, Object?> out, String key, List<Object?>? value) {
  if (value != null && value.isNotEmpty) out[key] = value;
}

/// 写：空字符串不写键（对应 Go 侧 string 上的 omitempty）。
void putIfNotEmptyString(
  Map<String, Object?> out,
  String key,
  String? value,
) {
  if (value != null && value.isNotEmpty) out[key] = value;
}

/// 写：`0` 不写键（对应 Go 侧 int 上的 omitempty，例如 `discover.days`）。
void putIfNotZero(Map<String, Object?> out, String key, int? value) {
  if (value != null && value != 0) out[key] = value;
}
