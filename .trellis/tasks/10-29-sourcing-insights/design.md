# 选品参考 技术设计

## 1. 架构落位

按 `docs/architecture/dependency-rules.md` 的分层归属：

| 关注点 | 落位 |
| --- | --- |
| HTTP 传输与 DTO | `internal/server/sourcing_handlers.go` |
| 用例编排、授权、事务边界 | `internal/application/sourcing/` |
| 快照与会话结果的持久化 | `internal/db/sourcing_repository.go` |
| 浏览器态热词采集与关键词搜索 | `internal/browser/sourcing.go` |
| AI 文案调用 | `internal/application/sourcing/copywriter.go` |
| 前端页面与 API 适配 | `frontend/app/features/sourcing/` |

`internal/server` 不得直接调用 `browser` 或 `Store.DB`；所有跨层调用经由 `internal/application/sourcing`。

## 2. 数据模型

新增迁移 `internal/db/migrations/{sqlite,mysql,postgres}/00059_sourcing.sql`（三种方言编号与最终结构保持对齐）。

```sql
CREATE TABLE sourcing_snapshots (
    id            TEXT PRIMARY KEY,
    -- kind 区分快照类别：hot_keywords（平台热词）或 shop_ranking（本店畅销榜）
    kind          TEXT NOT NULL,
    -- snapshot_date 是快照归属日，格式 YYYY-MM-DD，按北京时间划分
    snapshot_date TEXT NOT NULL,
    -- payload_json 保存该日快照的结构化结果，不含任何凭证
    payload_json  TEXT NOT NULL,
    -- status 表示采集结果：ok 或 failed
    status        TEXT NOT NULL DEFAULT 'ok',
    -- error_message 保存失败原因的脱敏摘要，成功时为空
    error_message TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_sourcing_snapshots_kind_date ON sourcing_snapshots (kind, snapshot_date);
```

设计取舍：

- 用单表 `kind` 区分而非多表，因为两类快照的读取模式一致（按日取一条），
  且都只保留 30 天，清理逻辑可复用。
- 保留失败快照并记录 `error_message`，使界面能明确显示"本次未取到"及其原因，
  避免把采集失败静默成"当天没有热词"。
- 30 天清理在写入后按 `snapshot_date` 执行一次 `DELETE`，不做后台常驻清理器。

## 3. 浏览器能力（`internal/browser/sourcing.go`）

### 3.1 热词采集（游客态）

```go
// FetchHotKeywords 以游客身份打开闲鱼搜索页，拦截搜索热词接口响应并返回词条。
// 不注入任何账号 cookie；单次调用最多产生 2 次接口请求。
func (m *Manager) FetchHotKeywords(ctx context.Context) ([]string, error)
```

实现要点：

- 使用**不注入账号 cookie** 的全新 browser context，`headless=true`。
- 监听 `mtop.taobao.idlemtopsearch.pc.item.search.activate` 与
  `mtop.taobao.idlemtopsearch.pc.search.shade` 的响应体，从中提取词条。
- 探测已证实这两个接口在游客态返回 `SUCCESS`，不触发 `RGV587`。
- 单次调用内不重试：一次失败即返回错误，由上层记录为失败快照。
- 整个调用受 `ctx` 控制，超时时间独立于账号运行时的浏览器预算。

### 3.2 关键词搜索（账号登录态）

```go
// SearchItem 是搜索结果中的单条商品摘要，不含任何凭证。
type SearchItem struct {
    ItemID     string
    Title      string
    Price      string
    WantCount  string
    Location   string
    DetailURL  string
}

// SearchItems 注入账号登录态后打开搜索页，拦截搜索接口响应并返回商品列表。
// 单次调用最多产生 1 次搜索请求，返回上限 30 条，不做翻页。
func (m *Manager) SearchItems(ctx context.Context, cookieID, cookieStr, keyword string) ([]SearchItem, error)
```

实现要点：

- 复用现有 context 创建与 `AddCookies` 路径（参考 `internal/browser/lifecycle_pool.go:633`）。
- 打开 `https://www.goofish.com/search?q=<keyword>`，拦截
  `mtop.taobao.idlemtopsearch.pc.search` 响应，从 `data.resultList` 提取字段；
  接口无结构化数据时回退到 DOM 抓取 `a[href*="item?id="]`。
- 响应 `ret` 含 `RGV587` 时返回专用错误 `ErrSourcingRiskControl`，**不重试**。
- 响应 `ret` 含 `SESSION_EXPIRED` / `TOKEN` 时返回 `ErrSourcingSessionExpired`。
- cookie 只作为入参在内存中使用，禁止写入日志。错误信息中不得包含 cookie 片段。
- 搜索结果**不落库**，仅在响应中返回，避免把他人商品数据当成本地事实。

## 4. 应用层（`internal/application/sourcing/`）

```go
// Service 编排选品参考的用例：热词采集、关键词搜索、本店畅销榜与 AI 文案。
type Service struct { ... }
```

职责：

- `CollectHotKeywords(ctx)`：调用浏览器采集 → 写快照 → 清理 30 天前数据。
- `CollectShopRanking(ctx)`：本地聚合 → 写快照（无平台请求）。
- `SearchItems(ctx, cookieID, keyword)`：校验账号归属 → 调用浏览器 → 返回列表。
- `GenerateCopywriting(ctx, req)`：读取 AI 配置 → 调用 chat completions → 返回文案。
- 所有方法都不直接返回凭证或异常原始响应。

### 4.1 本店畅销榜计算

基于 `orders` 表聚合，字段见现有结构：`item_id`、`quantity`、`amount`、`order_status`、
`cookie_id`、`created_at`、`deleted_at`。

口径：

- 只统计 `deleted_at IS NULL` 且 `order_status` 属于已付款之后的订单。
- `quantity` 为文本列，聚合前在 SQL 侧做安全转换（非数字按 0 计）。
- 时间窗口用 `created_at`，支持 7 天与 30 天。
- 输出字段：商品 ID、标题（关联 `item_info.item_title`，缺失时为空）、销量、营收、订单数。
- 该路径**不调用任何平台接口**，需有测试断言无出站请求。

### 4.2 AI 文案

- 复用现有设置键：`ai_api_key`（敏感，经 `ReadSensitiveSettingForAccount`）、
  `ai_api_url` / `ai_base_url`、`ai_model`，与 `internal/engine/ai.go:244` 的读取口径一致。
- 新增 `copywriter.go`，只做 `chat/completions` 调用与提示词拼装，不复制引擎回复逻辑。
- 两种模式：
  - `listing`：输出可直接粘贴的标题（≤30 字）与描述正文。
  - `analysis`：输出卖点结构、定价区间建议、注意事项。
- 失败（无配置、HTTP 错误、空内容）必须返回明确错误，不得用占位文案冒充成功。

## 5. HTTP API

新增 `/api/v1` 路由，同步更新 `api/openapi.yaml` 与生成的 `schema.ts`：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/sourcing/hot-keywords` | 读取当日热词快照（含失败状态） |
| POST | `/api/v1/sourcing/search` | 关键词单次查询，body 需 `cookie_id` + `keyword` |
| GET | `/api/v1/sourcing/shop-ranking` | 本店畅销榜，query 支持 `window=7d\|30d` |
| POST | `/api/v1/sourcing/copywriting` | AI 文案，body 需 `mode` + `topic`（可选 `item_id`） |
| POST | `/api/v1/sourcing/hot-keywords/refresh` | 手动触发一次热词采集（可选，用于首次回填） |

约束：

- 全部使用命名 DTO，不返回 DB 模型或 `map[string]any`。
- 统一错误信封，正确的 HTTP 状态码。
- `search` 接口在应用层做**同账号同关键词冷却**（建议 10 秒），冷却中返回 429，
  防止界面连点造成高频搜索。

## 6. 定时任务

- 新增 `internal/application/sourcing/scheduler.go`，每日北京时间固定时刻执行一次：
  - 先采集热词（游客态，≤2 请求）
  - 再计算本店畅销榜（0 请求）
- 生命周期：owner 为组合根，`ctx` 来自 `cmd/server` 的根上下文，
  `Start` 不发布半成品，`Stop` 幂等，并纳入现有退出 Wait 流程。
- 单日执行失败只记录失败快照并写一条系统事件，不影响其它自动化与账号运行时。

## 7. 前端结构

```
frontend/app/features/sourcing/
  api.ts            # 契约 DTO -> UI 模型的适配
  models.ts         # UI 模型
  hooks.ts          # 状态与请求编排（含取消与代次控制）
  pages/SourcingInsights.tsx
  components/HotKeywordPanel.tsx
  components/ItemSearchPanel.tsx
  components/ShopRankingPanel.tsx
  components/CopywritingDialog.tsx
```

要点：

- 组件不直接 fetch，统一走 `api.ts`。
- 搜索请求必须有取消与最新代次保护，旧响应不得覆盖新结果。
- 查询按钮在请求期间禁用，冷却提示由服务端 429 驱动。
- 导航新增「选品参考」入口，与现有菜单风格一致。

## 8. 风险与降级

| 风险 | 处理 |
| --- | --- |
| 热词接口字段或接口名变更 | 提取失败记录失败快照，界面显示"本次未取到"，不影响其它功能 |
| 搜索触发 `RGV587` | 立即中止，提示需人工在闲鱼处理风控，不重试 |
| 登录态过期 | 提示重新登录账号，不自动重登 |
| 浏览器不可用（`-no-browser`） | 热词与搜索入口显示不可用原因；本店畅销榜与 AI 文案仍可用 |
| AI 未配置 | 文案按钮提示"请先在设置中配置 AI"，不做占位输出 |

## 9. 测试策略

确定性测试优先，不依赖真实平台：

- `internal/browser/sourcing_test.go`：用本地 `httptest` 页面与伪造接口响应验证
  字段提取、`RGV587` 中止、会话过期分支、游客态不携带 cookie。
- `internal/application/sourcing/*_test.go`：用内存 SQLite 验证快照写入、30 天清理、
  畅销榜口径、搜索冷却、AI 成功与失败分支。
- `internal/server/sourcing_handlers_test.go`：契约、鉴权、错误信封、状态码。
- 前端：`hooks.test.tsx` 覆盖成功、失败、取消、切换关键词与陈旧响应；
  `components/*.test.tsx` 覆盖渲染与禁用态。
- 需要真实账号或真实平台的分支标记为环境依赖，在覆盖率分类中如实登记。

## 10. 与既有规则的兼容性

- 不修改 `docs/slider-captcha-frozen-spec.md` 覆盖的任何文件与行为。
- 不修改 `internal/xianyu/mtop/pacer.go` 的任何间隔参数。
- 不新增绕过 `mtop.NewClient()` 的客户端构造。
- 搜索请求不由 `mtop` 客户端发出，而是经浏览器上下文发出，因此不进入账号级节流器；
  其频率控制由"手动单次触发 + 服务端冷却 + 单次上限 30 条"共同保证，并在代码注释中写明该归属。
