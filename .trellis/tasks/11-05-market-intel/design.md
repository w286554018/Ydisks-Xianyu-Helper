# 设计：市场情报（AI 相关产品需求扫描与趋势）

对应 PRD：本目录 prd.md。遵循主计划架构（应用层自有模型、窄端口、transport 编排）。

## 架构总览

```
每日调度器（复用现有 sourcing 每日调度模式）
  └─ MarketIntelService.RunDailyScan(ctx)
       ├─ 1. 热词快照：游客态热词采集（已有能力，改为每日快照落库）
       ├─ 2. 逐词扫描（串行 + 5-10s 抖动，风控即停）：
       │     a. 幂等检查：当日该词已有成功快照 → 跳过
       │     b. SearchItems(word) → 30 条原始快照落库
       │     c. AI 批量判定（独立模型配置）→ 判定结果落库
       │     d. 聚合当日统计行落库
       └─ 3. 清理：删除 90 天前的原始快照
```

## 分层与端口（遵循 dependency-rules.md）

- **internal/application/marketintel**（新包）：
  - 自有模型：WatchedKeyword / ItemSnapshot / SnapshotVerdict / DailyAggregate / HotKeywordSnapshot / KeywordMomentum
  - 端口（消费方定义、窄接口）：
    - `SearchPort`：`SearchKeyword(ctx, keyword) ([]SearchResultItem, error)` —— adapter 包装现有 browser sourcing 搜索
    - `VerdictPort`：`JudgeBatch(ctx, keyword string, items []VerdictInput) ([]VerdictResult, error)` —— adapter 包装市场情报模型调用（OpenAI 兼容 chat/completions，JSON 输出）
    - `HotKeywordPort`：`CollectHotKeywords(ctx) ([]string, error)` —— 复用现有游客态热词采集
    - `Store`：快照/判定/聚合/词表/热词快照的持久化窄接口
  - `RunDailyScan`：编排上述步骤；错误分类（风控/会话失效→中止；AI 失败→跳过判定继续下一词）
- **internal/adapter**（新文件 market_intel.go）：
  - SearchPort 实现：包装 browser.SearchItems（不新增平台请求形态）
  - VerdictPort 实现：从 Settings 读市场情报模型配置（api_key/base_url/model 独立键），OpenAI 兼容接口，单次批量 30 条，提示词要求 JSON 数组输出，解析失败一次性降级为「全部未知」不重试
  - Store 实现：db 转换层（应用模型 ↔ 行模型）
- **internal/db**（迁移 00062，三方言）：
  - `market_intel_keywords`：id, keyword, enabled, sort_order, created_at（keyword 唯一）
  - `market_intel_snapshots`：id, keyword, snapshot_date, item_id, title, price, want_count, seller_nickname, publish_time, raw_json, created_at；索引 (keyword, snapshot_date)；保留 90 天
  - `market_intel_verdicts`：snapshot_id PK/FK, is_ai_related, category, reason, created_at
  - `market_intel_aggregates`：id, keyword, snapshot_date, ai_count, total_want, category_json, top_sellers_json, created_at；唯一 (keyword, snapshot_date)
  - `market_intel_hot_keywords`：id, snapshot_date, position, keyword, created_at；唯一 (snapshot_date, position)
- **internal/server**（market_intel_handlers.go，/api/v1 前缀）：
  - GET/POST/PUT/DELETE `/api/v1/market-intel/keywords`（词表 CRUD，上限 30 校验→400）
  - GET `/api/v1/market-intel/overview`（热词涨幅榜 + 各词最新聚合摘要）
  - GET `/api/v1/market-intel/keywords/{id}/trend?days=30`（单词趋势曲线）
  - GET `/api/v1/market-intel/sellers?days=30`（竞品卖家榜）
  - POST `/api/v1/market-intel/scan`（手动触发，幂等）
  - GET/PUT `/api/v1/market-intel/settings`（市场情报模型配置；GET 不回明文 key，PUT 支持留空保持原值）
  - 全部 operation：OpenAPI + 400/401 统一错误响应 + 契约场景注册
- **internal/composition**：Services 注册 marketintel 服务；TransportPorts 暴露；server_dependencies 接线

## 关键决策

1. **AI 判定失败不阻断快照落库**：原始快照先落库，判定失败时该词当天无聚合行；
   支持事后补判定（对无判定结果的快照重跑 VerdictPort），补判定入口与手动扫描合并（幂等：已有判定跳过）。
2. **JSON 输出不重试**：AI 返回非法 JSON 时不重试（防成本失控），当天降级为无判定；
   连续失败由用户通过连接测试与日志发现。模型配置错误在测试连接时暴露。
3. **热词涨幅计算在应用层内存中完成**：读最近 14 天热词快照，对比出「新冒头/持续/上升/回落」，
   不需要额外表或物化视图。
4. **卖家榜从快照聚合**：按 seller_nickname 分组统计出现次数与均价，零额外平台请求。
5. **want_count 解析容错**：沿用现有 SearchItem.WantCount 文本解析（"120人想要"/"1.2万人想要"），
   解析失败存 0 并在 raw_json 保留原文。
6. **调度复用**：挂到现有每日调度框架（同擦亮/热词的调度模式），执行时段沿用现有低峰配置；
   遵循「服务重启后先查当日成功记录再执行」的既有幂等模式（TIP-20261030-339ddaf7）。

## 风控与成本护栏（不可放宽）

- 搜索：每词每天最多 1 次请求（幂等保护下手动触发也不超）；风控(RGV587/punish/captcha)或会话失效 → 立即中止当天批次，不重试，次日自动恢复
- AI：每词每天最多 1 次批量判定（30 条一次请求）；失败不重试；独立模型配置便于用便宜模型
- 词表上限 30：硬校验在应用层 Create/Update 与前端同时生效
- 快照清理：90 天，每日扫描末尾顺带执行（DELETE WHERE snapshot_date < cutoff）

## 测试策略

- application/marketintel：编排逻辑（幂等跳过、风控即停、AI 失败降级、聚合正确性、90 天清理）用 fake 端口全覆盖
- adapter：VerdictPort 的 JSON 解析/降级、Store 转换往返
- db：迁移三方言 + CRUD + 唯一约束
- server：契约测试（新 operation 全部注册场景）+ 词表上限 400 路径
- 前端：词表管理、模型配置表单、三视图渲染的行为测试
