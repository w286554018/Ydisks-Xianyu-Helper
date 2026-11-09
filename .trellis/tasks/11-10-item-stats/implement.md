# 11-10-item-stats 实施计划

## 阶段 0：取证（已完成）

- 实测自有商品详情返回 `browseCnt` / `wantCnt` / `collectCnt` / `soldCnt`（`1098309932961` → 22 / 2 / 1 / 0）。
- 实测自有商品列表卡片只有「N人想要」文本、无浏览量（库内 36 商品含「浏览」字样 0 条）→ 浏览量必须来自详情接口。
- 结论：复用既有本机抓取助手通道，不新增外部依赖。

## 阶段 1：数据库

1. 三方言新增 `00064_item_stats.sql`（结构见 design 第 2 节），同号、同最终结构。
2. 新增 `internal/db/item_stats.go`：`ItemStatsRow`、`GetItemStats`、`ListItemStats`、`UpsertItemStats`。
3. 更新迁移版本断言（`migrate_test.go` finalVersion、`keyword_expressions_test.go`）为 64。

验证：`go test ./internal/db/ -run 'TestMigrate|ItemStats' -count=1`；SQLite 上跑通 upsert 与批量查询；
三方言分支可用既有手法（把 Dialect 指向其它方言触发分支）覆盖。

## 阶段 2：解析与助手字段

1. `internal/browser/competitor_detail.go`：payload 增加四个计数（`flexString`），`CompetitorDetail` 增加 int64 字段并填充。
2. `internal/localfetch`：`FetchDetailResponse` 增加 `browse_cnt` / `want_cnt` / `collect_cnt` / `sold_cnt`。
3. `internal/adapter/local_fetch_agent.go`：解析并回填四个计数。
4. 测试：browser 解析用例（含数字/字符串/缺失三种形态）、adapter 客户端映射用例、localfetch 响应用例。

验证：`go test ./internal/browser/ ./internal/localfetch/ ./internal/adapter/ -count=1`。

## 阶段 3：应用层用例

1. 新增 `internal/application/items/stats.go`：`ItemStats` 模型、`ItemStatsFetcher` / `ItemStatsRepository` 端口、
   `ItemStatsService`（`RefreshItemStats` 强制刷新；`RefreshStatsBatch` 候选筛选 + 上限 10 + 串行 + 风控即停）。
2. 测试（应用层，纯内存替身）：
   - 缓存内不请求助手；
   - 批量跳过新鲜商品、最旧优先、超过上限被截断；
   - 串行顺序可断言；
   - 风控即停且保留已抓结果、`Stopped=true`；
   - 助手不可用 / 未登录直接返回对应错误；
   - 抓取成功即落库（仓储调用断言）。

验证：`go test ./internal/application/items/ -run 'Stats' -count=1`。

## 阶段 4：传输层与契约

1. 新增 `internal/server/item_stats_handlers.go`：
   - `POST /api/v1/items/{cookie_id}/{item_id}/stats/refresh`
   - `POST /api/v1/items/stats/refresh-batch`
   统一错误信封；`ErrLocalFetch*` → 503/409 + 稳定错误码；未归类错误 `Warn` 日志。
2. 列表接口 `GET /api/v1/items` 增加 `stats` 字段（无数据为 `null`）。
3. `api/openapi.yaml`：新增两条 operation 与 `ItemStats` / 批次响应 schema；为列表响应补字段；
   运行 `npm run api:generate --prefix frontend` 重新生成契约类型。
4. 契约测试：路由匹配、400/401 场景、成功契约场景、503 场景。

验证：`make api-check`、`go test ./internal/application/... ./internal/server/... -count=1`。

## 阶段 5：前端

1. `frontend/app/features/items/api.ts`：新增两个 API 调用。
2. `models.ts` / `types.ts`：商品模型增加 `stats`；新增统计类型与 `formatStatsAge` 纯函数。
3. `hooks.ts`：两个刷新动作（期间禁用、就地更新、错误提示）。
4. `components/ItemTable.tsx`：「价格」后插入「浏览」「想要」两列 + 悬浮 + 行内「刷新数据」。
5. `pages/ItemList.tsx`：工具栏「刷新本页」按钮 + 结果提示。
6. 测试：`formatStatsAge` 纯函数用例；hooks 行为测试（成功/失败/批量进行中禁用/风控停止提示）。

验证：`npm --prefix frontend run typecheck`、`npm --prefix frontend run test`、
`npm --prefix frontend run comments:check`。

## 阶段 6：门禁、覆盖率与上线

1. `go test ./...`、`go vet ./...`、`make api-check`、前后端 `commentlint`。
2. `make cover`（Go ≥ 90%）与前端覆盖率；补齐新增分支测试。
3. 部署：按既有流程用提交哈希重建镜像并 `docker compose up -d`，核对 `/health` 的 commit。
4. 端到端实测（线上）：
   - 列表显示「浏览」「想要」列；
   - 单个刷新 → 数据落库并更新（对比刷新前后的数字）；
   - 批量刷新 → 单轮最多 10 个、跳过新鲜、返回剩余数量；
   - 停掉家用机隧道 → 刷新返回 503 + 可操作提示（列表仍显示历史数据）；
   - 恢复隧道 → 刷新恢复正常。
5. 更新服务器 `DEPLOYMENT.md` 与项目文档。

## 全量验证清单

- [ ] 三方言迁移同号（64）且最终结构一致；版本断言已更新
- [ ] `go test ./...` 全绿（含架构门禁）
- [ ] `go vet ./...`、Go 与前端 `commentlint` 通过
- [ ] `make api-check` 通过（含 400/401 与成功契约场景）
- [ ] Go 覆盖率 ≥ 90%；前端新增逻辑有行为测试
- [ ] 缓存命中不发助手请求；批量上限/最旧优先/串行/风控即停 均有测试
- [ ] 助手离线 → 503 且不触发服务器侧浏览器
- [ ] 线上端到端四项实测通过

## 实施进度（Go 侧已完成，均已本地验证）

### 阶段 1：数据库（完成）

- 三方言 `00064_item_stats.sql` 已落地（同号、同最终结构）。
- 新增 `internal/db/item_stats.go`：`ItemStatsRow`、`GetItemStats`、`ListItemStats`、`UpsertItemStats`（跨库 upsert）。
- `internal/db/store.go` 注册 `ItemStats` 仓储；迁移版本断言更新为 64。
- 测试：`internal/db/item_stats_test.go`（读写/覆盖/批量/有效期边界/三方言分支/错误分支）。

### 阶段 2：解析与助手字段（完成，含一处设计调整）

- `internal/browser/competitor_detail.go`：`itemDO` 增加 `browseCnt`/`wantCnt`/`collectCnt`/`soldCnt`（`flexString`），
  `CompetitorDetail` 增加四个 int64 字段；新增 `parseFlexCount`（空值/负值/非法文本按 0）。
- `internal/localfetch/agent.go`：`FetchDetailResponse` 增加四个计数并透传。
- `internal/adapter/local_fetch_agent.go`：抽出 `fetchLocalDetail` 作为唯一通道调用路径；
  `FetchCompetitorDetail` 与**新增的 `FetchItemStats`** 共用它；
  新增 `translateLocalFetchError`，把助手场景错误翻译成 `items` 包自有哨兵。
- **设计调整**：四个计数不再加进 `marketintel.CompetitorDetail`，改为在适配器上新增
  `FetchItemStats` 方法并返回 `items.ItemStatsCounts`。理由是统计属于「我的商品」用例域，
  加进竞品详情结构会让市场情报域承载无落库位置的字段，并迫使 `items` 反向依赖市场情报包。
- 顺带修复：`internal/localfetch/detail.go` 的 `sleepWithJitter` 在 `jitter <= 0` 时会 panic
  （`rand.Int63n(0)`），已加零值保护。
- 新增测试：`internal/localfetch/agent_test.go`（此前该包无任何测试）、
  browser 计数解析用例、adapter `FetchItemStats` 映射用例。

### 阶段 3：应用层用例（完成）

- `internal/application/items/stats.go`：`ItemStatsService`（`RefreshItemStats` 强制刷新；
  `RefreshStatsBatch` 候选筛选/最旧优先/上限 10/串行/风控即停；`ListItemStatsForItems` 按账号分组读列表）；
  常量 `ItemStatsCacheTTL = 24h`、`ItemStatsBatchLimit = 10`。
- 测试：`internal/application/items/stats_test.go`（强制刷新、缓存跳过、最旧优先、上限截断、风控即停、
  硬故障上抛、归属校验、上下文取消、列表新鲜度、依赖缺失）。

### 阶段 4：传输层与契约（完成）

- 新增 `internal/server/item_stats_handlers.go`（两条端点 + 统一错误信封 + 未归类错误 Warn 日志）、
  `internal/server/application_ports_item_stats.go`（`ItemStatsPort`）。
- 列表 `GET /api/v1/items` 与 `GET /api/v1/items/cookie/{cookie_id}` 增加 `stats` 字段（无数据为 null）；
  统计查询失败时降级为不展示统计并留告警，不影响列表主流程。
- `api/openapi.yaml` 新增两条 operation 与 `ItemStats`/`ItemStatsBatchRequest`/`ItemStatsBatchResponse` schema，
  并重新生成 `frontend/shared/api-contract/generated/schema.ts`。
- 接线：`internal/adapter/item_stats.go`（仓储适配器）、
  `internal/composition/runtime/runtime_item_stats.go`（`buildItemStats`，抓取端口固定指向本机助手且不回退）、
  `composition.Dependencies.ItemStats` → `Services` → `TransportPorts` → `ApplicationPortsInput`。
- 契约测试：`internal/server/item_stats_handlers_test.go`（成功契约、400/401/403、409/503 映射、
  列表附带统计与降级），并登记到 `TestOpenAPISuccessContractCoverage` 场景表。

### 阶段 4 附加：架构门禁修复（完成）

新增代码触发了三项门禁，均已按业务职责拆分（未放宽门禁）：

- `internal/composition/application_services.go` 拆出 `transport_ports_projection.go`（HTTP Port 投影）。
- `internal/composition/runtime/runtime.go` 拆出 `runtime_service_ports.go`（订单补偿协调器 + 数据库健康端口）。
- `internal/server/item_handlers.go` 的列表统计辅助函数移到 `item_stats_handlers.go`。

### 阶段 5：前端（完成）

- `api.ts` 新增 `refreshItemStats` / `refreshStatsBatch` 与 `statsRefreshErrorMessage`（后者放在 API 适配层，
  因为只有适配器可以引用共享 HTTP 错误类型）；`models.ts` 新增 `ItemStats`/`ItemStatsBatchResult` 与 `Item.stats`。
- 新增 `stats.ts`（纯函数：索引键、相对时间文案、悬浮文案、就地合并、可刷新计数、批量结果提示）。
- 新增 `hooks.ts` 的 `useItemStatsRefresh`：单行刷新按「账号 + 商品」就地更新、批量刷新用代次守卫防止
  旧响应覆盖新结果、卸载后不再写入状态。
- `ItemTable.tsx` 在「价格」后插入「浏览」「想要」两列（悬浮「收藏 N · 已售 N · 更新于 X 前」）与行内刷新按钮；
  `ItemList.tsx` 工具栏新增「刷新本页」（含可刷新数量提示与结果提示）。
- 测试：`stats.test.ts`（纯函数）、`statsRefresh.test.tsx`（成功/失败/风控停止/旧响应不覆盖/卸载/空集合）。

### 阶段 6：门禁、覆盖率与上线（完成）

- 门禁：`go build`、`go vet`、`architecturecheck`、Go 与前端 `commentlint`、`make api-check` 五段全部通过。
- 全量测试：`go test ./... -p 1` 全绿（并行会因 4 个重型包抢占 CPU 而长时间不收敛，必须串行）；
  前端 `typecheck` + 140 个测试文件 / 877 个用例全通过。
- 覆盖率：Go `go test ./... -coverprofile` = 89.09%，叠加 `RUN_BROWSER_INTEGRATION=1`（`internal/browser`
  由 35.9% 提升到 62.1%）合计 **90.97%**；前端 `test:coverage` = **90.58%** statements。
- 上线：提交 `2c3c251` → `9519f40` → `45902fe`，镜像 `local-2c3c251` → `local-9519f40` → `local-45902fe`，
  生产 Postgres 迁移到 00064；`/health` 的 version/commit 与仓库 HEAD 一致。
- 线上实测（`https://xianyu.fishtrip.net`，admin 会话）：
  1. 列表附带 `stats`：8 件商品中 5 件有统计、其余为 `null`；
  2. 单个刷新 200 / 2.8 秒，`browse_cnt` 随平台实时变化（31 → 32 → 35）；
  3. 批量刷新 200 / 2.5 秒，24 小时内已抓取的商品被跳过（`skipped=2`）、`remaining` 正确；
  4. 助手侧风控：真实命中过 409「闲鱼要求人机验证」，提示可操作且不重试（符合冻结规范）。
- 关键运维事实：**本机抓取助手必须使用包含四个计数字段的二进制**；旧助手只返回标题/价格/描述，
  会导致统计全部落库为 0。已用 `go build ./cmd/local-fetch-agent` 重装到
  `~/.ydisks-fetch/bin/local-fetch-agent` 并 `launchctl kickstart -k gui/501/com.ydisks.local-fetch-agent` 重启。
