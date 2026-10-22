# 开发

## 常用命令

```bash
go vet ./...
go test ./...
go build ./...
```

## 测试

单元测试集中在四处：

- `internal/rules`：规则编译、主/副 RSS 排序、去重、`scope` 语义
- `internal/source`：关键词清洗、Nyaa 搜索地址拼接、mikan 番剧页推导、站点到 `Ani.type` 的映射、缓存裁剪
- `internal/planner`：备用 RSS 的偏移继承、偏移变化的差异识别、季度/偏移的取值优先级、番剧级配置的匹配（bgmId / 标题包含 / 限定实例）
- `internal/config`：番剧级配置的校验与规整

涉及网络的适配器不做单元测试，改用 `probe` 打真实站点验证：

```bash
AniSync probe --site anibt  --bgm 665390
AniSync probe --site mikan  --mikan 3887
AniSync probe --site nyaa   --jp "White Cat Legend S03"
```

`probe` 输出每个候选的字幕组名、热度与标题样本数，用来判断「为什么这个番剧没匹配到」。

## 设置页（WebUI）

`internal/webui/assets/index.html` 是单文件内嵌资源，没有前端构建步骤：改完 `go build` 即生效。样式与脚本都写在这一个文件里，改完跑一遍静态检查，避免把「AI 味」的样式写回去：

```bash
npx ui-craft-detect internal/webui/assets/index.html
```

判定口径是 0 error / 0 warning：占位符不能当 label、`:hover` 必须配 `:focus-visible`、表格要有横向溢出与 sticky 表头、可见文案里的破折号不超过两条、字号层级不超过 6 档。

## 改动站点适配的注意事项

AniBT / AnimeGarden / Mikan 的接口契约以 ANI-RSS 源码为准（`controller/{AniBT,AnimeGarden,Mikan}Controller.java` 与对应 Service），不要凭页面结构猜。字段名三家不同（mikan 用 `label`，另两家用 `name`），统一在 `assapi.Group` 收口。

Nyaa 的搜索地址必须与站点搜索页「RSS」按钮给出的形式一致，改动后用真实站点复核。

## 新增一个站点

1. 在 `internal/source` 实现 `Source` 接口；
2. 在 `NewRegistry` 注册，并在 `SiteType` 里补上对应的 `Ani.type`；
3. 在 `internal/webui` 的 `/api/sources` 里补上说明，供设置页展示；
4. 补上能离线跑的单元测试（地址拼接、字段映射），网络部分交给 `probe`。

## 提交前

```bash
go vet ./... && go test ./... && go build ./...
```

CI 覆盖 Windows / macOS / Linux 六个目标，并校验 macOS 二进制已链接 Cocoa。推送 `v*` 标签会触发发布；推送到 `main` 会自动递增补丁号并发版。
