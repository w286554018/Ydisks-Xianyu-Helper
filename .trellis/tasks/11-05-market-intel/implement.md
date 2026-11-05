# 实施计划：市场情报（AI 相关产品需求扫描与趋势）

## 阶段 0：热词快照地基 ✅（快照表已有 sourcing_snapshots 复用，无需新表）
- 热词涨幅计算服务 momentum.go（读 14 天快照 → 新冒头/持续/上升/回落）+ GET /sourcing/hot-keywords/momentum 端点
- 测试：涨幅分类/失败快照跳过/空视图 3 用例

## 阶段 1：词表 + 数据层 ✅
- 迁移 00062 之 market_intel_keywords / snapshots / verdicts / aggregates 表（三方言）
- db 层 CRUD + 唯一约束 + 90 天清理方法
- application/marketintel：模型 + Store 端口 + 词表用例（上限 30 校验、内置种子词初始化）
- 词表 CRUD 端点 + OpenAPI + 契约场景
- 迁移版本断言测试更新（migrate_test / keyword_expressions_test → 62）

## 阶段 2：扫描编排 ✅
- SearchPort adapter（包装现有 browser.SearchItems）+ Store adapter
- RunDailyScan 编排：幂等检查 → 搜索落快照 → 聚合（先不含 AI 判定，want 总数直接聚合）
- 风控即停 + 词间抖动 + 90 天清理
- 手动触发端点（POST /scan，幂等）+ 每日调度挂载
- 测试：编排全覆盖（fake 端口）

## 阶段 3：AI 判定 ✅
- 市场情报模型配置（settings 独立键 + GET/PUT settings 端点 + 连接测试端点）
- VerdictPort adapter（OpenAI 兼容、批量 30 条、JSON 解析失败降级不重试）
- RunDailyScan 接入判定步骤：判定成功才写聚合行（含类目分布）；失败只落快照
- 补判定逻辑（手动扫描时对无判定快照补跑）
- 测试：JSON 解析用例、降级路径、补判定幂等

## 阶段 4：前端 ✅
- 设置页：市场情报模型配置区块（参照 AI 客服模型配置形态）+ 关注词管理（增删改/启停/上限提示）
- 选品参考页「市场情报」区块：热词涨幅榜、词需求趋势（曲线，复用现有图表依赖）、竞品卖家榜
- 行为测试：词表管理、趋势渲染、扫描按钮幂等态

## 阶段 5：验证收尾 ✅
- go test ./... 全过；vet/commentlint/architecturecheck/api-check 通过；make check 除 golangci-lint（既有缺失）外全过
- Go 覆盖 89.6%（基线 90.1%；唯一 0% 是 adapter SearchKeyword——依赖真实浏览器的平台路径，按 PRD 例外条款记录）
- 前端 842 测试全过，语句覆盖 90.2%；MarketIntelSection 独立 chunk 17.78 kB（预算内）
- featureArchitecture 门禁：市场情报作为独立 feature，由 AuthenticatedShell 在 sourcing 路由下 lazy 组合

## 阶段 5：验证收尾
- go test ./... 全过；make check（除 golangci-lint 既有缺失）；make cover ≥90%；cover-frontend
- 前端 build 重编 Go 二进制；bundle 预算检查（选品参考页若超预算按事实更新）
- implement.md 完成标记 + 一次中文提交

## 验证门禁
- 每阶段：go vet + commentlint + architecturecheck + 焦点测试
- 最终：make check + make cover + cover-frontend + 全量测试
