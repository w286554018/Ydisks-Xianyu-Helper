# 实施计划（11-07 竞品详情查看 + AI 改写 + 一键搬运）

## 阶段 0：详情协议拦截 ✅ 完成
- `browser.CompetitorItemDetail`（打开详情页 + 拦截页面自发 `mtop.taobao.idle.pc.detail`）
- 纯解析函数 `parseCompetitorDetailPayload` / `buildCompetitorDetail` / `stripHTMLTags`
- 焦点测试：真实响应结构样本、非 JSON 降级、图片去空白、价格分段拼接、参数守卫、浏览器不可用

## 阶段 1：数据层 ✅ 完成
- 迁移 00063 `market_intel_item_details`（SQLite/MySQL/Postgres 三方言）+ 版本断言 63
- `db.MarketIntel` 详情缓存读写（GetItemDetail / SaveItemDetail / IsFresh，7 天有效期）
- 焦点测试：读写往返、覆盖不新增、有效期边界、三方言分支、数据库不可用与零值仓储

## 阶段 2：应用层 ✅ 完成
- `GetCompetitorDetail`（7 天缓存命中即返回，未命中抓取后回写）、`RewriteCompetitorCopy`、`AnalyzeCompetitorSellingPoints`
- 搬运用例 `CarryService`：详情 → AI 改写（失败降级原文）→ 图片下载入库 → 预填数据；
  单张图片失败只跳过，入库失败整次报错；图片数量上限 20
- 焦点测试：缓存命中/过期/损坏/未命中/无浏览器/写缓存失败、AI 未配置、搬运全部分支

## 阶段 3：HTTP ✅ 完成
- 4 端点（detail/rewrite/analyze/carry）+ 词商品列表端点 + OpenAPI + 契约场景
- 搬运用例改由应用层承担，Server 只消费应用 Port（阶段二架构门禁）
- 详情平台错误归类：风控/会话失效 → 详情专属应用错误 → 409 可操作提示（原为通用 500）
- 失败可见：未归类错误写 Warn 诊断日志（logsafe，不含凭证）

## 阶段 4：前端 ✅ 完成
- `marketintel` feature：`api.ts` 5 个新 API、`hooks.ts` 的 `useCompetitorDetail`、详情弹窗组件
- 市场情报区块「查看商品」展开词商品列表（最新快照日 30 条）+「查看详情」弹窗
- 前端测试：`api.test.ts`（9 用例）、`hooks.test.tsx`（6 用例，含取消语义）

## 阶段 5：验证 ✅ 完成（线上详情链路受平台风控阻塞）
- `go test ./...` 全绿；architecturecheck / api-check / vet / comments（Go 与前端）全通过
- Go statement 90.17%（未设 `RUN_BROWSER_INTEGRATION=1`）；前端 statement 90.56%
- 部署 `local-11-07-carry4`：schema 62 → 63，核心表计数与部署前一致，其他容器未变化
- 线上实测通过：登录、词表、词商品列表（kiro 30 条）、概览、热词涨幅、
  前端市场情报区块与「查看商品」展开（30 条含「查看详情」）
- 线上未通过（外部阻塞）：真实竞品详情抓取返回平台风控，已按铁律停止重试；
  详情弹窗 → AI 改写/分析（还需配置市场情报模型 API Key）→ 搬运图片入库
  待风控解除后复验
