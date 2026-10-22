# 开发

## 常用命令

```bash
go vet ./...
go test ./...
go build ./...
```

## 测试

单元测试集中在两处：

- `internal/rules`：规则编译、主/副 RSS 排序、去重、`scope` 语义
- `internal/source`：关键词清洗、Nyaa 搜索地址拼接、mikan 番剧页推导、站点到 `Ani.type` 的映射、缓存裁剪

涉及网络的适配器不做单元测试，改用 `probe` 打真实站点验证：

```bash
AniSync probe --site anibt  --bgm 665390
AniSync probe --site mikan  --mikan 3887
AniSync probe --site nyaa   --jp "White Cat Legend S03"
```

`probe` 输出每个候选的字幕组名、热度与标题样本数，用来判断「为什么这个番剧没匹配到」。

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
