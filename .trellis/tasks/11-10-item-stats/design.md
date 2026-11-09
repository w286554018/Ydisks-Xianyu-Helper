# 11-10-item-stats 设计

## 1. 数据流

```
前端「商品管理」列表
   │  ① GET /api/v1/items（列表接口，附 stats 字段：浏览/想要/收藏/已售/fetched_at）
   │  ② 行内「刷新数据」 → POST /api/v1/items/{cookie_id}/{item_id}/stats/refresh
   │  ③ 工具栏「刷新本页」 → POST /api/v1/items/stats/refresh-batch {cookie_id, item_ids, limit}
   ▼
应用层 ItemStatsService（internal/application/items/stats.go）
   │  · 缓存判定（24h）与强制刷新
   │  · 批量候选筛选（缺数据/过期 → 最旧优先 → 上限 10）
   │  · 串行执行；遇风控立即停止并保留已抓结果
   ▼
ItemStatsFetcher 端口 → adapter.LocalFetchAgentClient
   ▼  POST /fetch-detail（本机助手）
本机真实 Chrome → 闲鱼商品详情 → itemDO.browseCnt / wantCnt / collectCnt / soldCnt
   ▼
internal/db/item_stats.go（按 (cookie_id, item_id) upsert 落库）
```

## 2. 数据模型

新增迁移 **00064_item_stats.sql**（sqlite / mysql / postgres 同号同结构）：

```
item_stats(
  id           自增主键,
  cookie_id    TEXT NOT NULL REFERENCES cookies(id) ON DELETE CASCADE,  -- 所属账号
  item_id      TEXT NOT NULL,                                          -- 平台商品标识
  browse_cnt   BIGINT NOT NULL DEFAULT 0,                              -- 浏览量
  want_cnt     BIGINT NOT NULL DEFAULT 0,                              -- 想要量
  collect_cnt  BIGINT NOT NULL DEFAULT 0,                              -- 收藏量
  sold_cnt     BIGINT NOT NULL DEFAULT 0,                              -- 已售量
  fetched_at   TIMESTAMP NOT NULL,                                     -- 抓取时间（数据新鲜度）
  created_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(cookie_id, item_id)
)
```

- 独立表而不是往 `item_info` 加列：统计是**高频变动的派生数据**，与商品主体的写入路径、生命周期
  （发布、改价、下架、软删除）完全不同；分表可避免污染商品主表并便于单独过期。
- 迁移版本断言：`internal/db/migrate_test.go` 的 finalVersion 与 `keyword_expressions_test.go` 同步更新为 64。

## 3. 各层改动

### 3.1 internal/browser（解析，复用既有详情通道）

- `competitorDetailPayload.Data.ItemDO` 增加 `browseCnt` / `wantCnt` / `collectCnt` / `soldCnt`
  （用既有 `flexString` 兼容字符串/数字/null，避免平台类型变化导致整段解析失败——这是本项目已踩过的坑）。
- `CompetitorDetail` 增加同名 int64 字段，`buildCompetitorDetail` 填充。
- `ParseCompetitorDetailResponse` 不变（仍返回详情 + rets + 可用性）。

### 3.2 internal/localfetch（本机助手）

- `FetchDetailResponse` 增加 `browse_cnt` / `want_cnt` / `collect_cnt` / `sold_cnt`；成功响应一并返回。
- 其余行为不变（限流重试、未登录、Chrome 不可用等既有语义保持）。

### 3.3 internal/db

- 新增 `internal/db/item_stats.go`：
  - `ItemStatsRow`（持久化模型，含 ItemID/CookieID/四项计数/FetchedAt）；
  - `GetItemStats(ctx, cookieID, itemID)`、`ListItemStats(ctx, cookieID, itemIDs)`（一次查一批，供列表与批量筛选）、
    `UpsertItemStats(ctx, row)`。跨库 upsert 使用 `dialectUpsert`；写入用自增主键时遵循既有 `insertReturningID` 约定
    （本表无自增主键返回需求，按唯一键 upsert 即可）。

### 3.4 internal/adapter

- `LocalFetchAgentClient`：解析新字段并填充 `marketintel.CompetitorDetail`（该结构新增四个计数）。
  错误映射（unavailable / unauthorized / not_logged_in / risk_control）与令牌保密要求保持现状。
- 新增 `adapter.ItemStatsStore`：把 `db.ItemStats` 适配为应用层端口（读写分离，不暴露 `db.*` 类型给应用层）。

### 3.5 internal/application/items（用例）

新增 `internal/application/items/stats.go`：

- 端口：
  - `ItemStatsFetcher`（单商品抓取，返回四项计数）；由本机助手适配器实现；
  - `ItemStatsRepository`（`Get` / `List` / `Upsert`）。
- 服务 `ItemStatsService`：
  - `RefreshItemStats(ctx, cookieID, itemID) (ItemStats, error)`：**强制**抓取一次并落库（忽略缓存）。
  - `RefreshStatsBatch(ctx, cookieID string, itemIDs []string, limit int) (StatsBatchResult, error)`：
    1. `limit` 上限硬编码为 10（无论入参多大都截断）；
    2. 一次查出候选的现有 stats，筛出「未抓过 或 `fetched_at` 早于 24 小时」的；
    3. 按 `fetched_at` 升序（未抓过的排最前）取前 `limit` 个；
    4. **串行**逐个抓取；成功即落库；返回 `{Refreshed []ItemStats, Remaining int, Stopped bool, StopReason string}`；
    5. 遇 `ErrLocalFetchRiskControl` → **立即停止**，保留已抓结果，`Stopped=true`；
       遇 `ErrLocalFetchUnavailable` / `ErrLocalFetchNotLoggedIn` → 直接返回该错误（前端提示可操作信息）。
- 常量：`ItemStatsCacheTTL = 24 * time.Hour`、`ItemStatsBatchLimit = 10`。
- 应用层自有模型 `ItemStats`（含 `Fresh bool` 供前端判断是否需要提示刷新）。

### 3.6 internal/server（传输层）

- 列表接口 `GET /api/v1/items`：商品项新增 `stats` 对象（可为 `null`）：
  `{browse_cnt, want_cnt, collect_cnt, sold_cnt, fetched_at}`。
- 新增两条端点（`/api/v1` 前缀、命名 DTO、统一错误信封）：
  - `POST /api/v1/items/{cookie_id}/{item_id}/stats/refresh` → 200 单商品 stats；
  - `POST /api/v1/items/stats/refresh-batch` → 200 批次结果
    `{refreshed:[...], remaining, stopped, stop_reason}`。
- 统一错误映射（复用现有：`ErrLocalFetch*` → 503/409 + 稳定错误码）；
  未归类错误保留 `Warn` 诊断日志（`logsafe.Error`）。
- 鉴权：登录会话 + 商品归属校验（`cookie_id` 属于当前用户），沿用既有归属检查手段。

### 3.7 前端（frontend/app/features/items）

- `api.ts`：新增 `refreshItemStats(cookieID, itemID)`、`refreshStatsBatch(cookieID, itemIDs, limit)`。
- `models.ts` / `types.ts`：商品模型增加可选 `stats`；新增统计类型与「新鲜度文案」纯函数
  （`formatStatsAge(fetchedAt)` → 「刚刚 / N 分钟前 / N 小时前 / N 天前」）。
- `hooks.ts`：`refreshItemStats`、`refreshStatsBatch` 两个动作；**批量刷新期间禁用按钮**、
  完成后用返回值就地更新行数据（不整页重载）；错误信息展示可操作提示。
- `components/ItemTable.tsx`：在「价格」后插入「浏览」「想要」两列；`title` 悬浮显示
  「收藏 N · 已售 N · 更新于 X 前」；无数据显示「—」；操作列新增「刷新数据」按钮。
- `pages/ItemList.tsx`：工具栏新增「刷新本页」按钮（与「同步商品」并列），
  提交当前页商品 ID 列表；结果提示「已刷新 N 个，剩余 M 个待刷新」。

## 4. 关键取舍

- **为什么不做服务器侧浏览器回退**：商品详情接口对服务器侧浏览器确定性判罚（已实测），
  回退只会多花 50 秒以上并加重风控；助手不可用时立即 503。
- **为什么批量上限写死在服务端**：真正的约束是平台请求次数；把「上限、候选筛选、风控即停」
  放在应用层可以由测试证明，前端只负责触发与展示。
- **为什么强制刷新忽略缓存**：用户点「刷新」就是明确要求最新值；而批量刷新按新鲜度跳过，
  避免把 10 个名额浪费在刚抓过的商品上。
- **为什么统计独立成表**：见 2 的说明；同时避免商品列表查询变宽影响既有页面性能。
- **复用既有详情通道**：本机助手、错误语义、隧道与自启都已上线，本次只扩展字段，不新增外部依赖。

## 5. 风险与回退

- **平台详情接口限流**：批量刷新可能触发 `RGV587` 限流。设计上遇风控即停、保留已抓数据，
  用户可稍后再点；单轮上限 10 保证一次点击的最坏耗时可控（约 30–60 秒）。
- **助手离线**（家用机关机/隧道断）：所有刷新类请求返回 503；列表仍能显示已缓存的历史数据。
- **字段缺失**：若某商品详情不含计数（平台变体差异），按 0 处理且仍记录 `fetched_at`，
  避免反复重试同一商品；列表显示 0 而不是「—」（表示已抓过但为 0）。
- **回退**：本功能为纯增量（新表 + 新端点 + 新列），回退只需不调用新端点；
  数据库保留 `item_stats` 不影响既有功能。
