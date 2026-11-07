# 设计：竞品详情查看与一键搬运

## 协议层（internal/browser）
- 新文件 internal/browser/competitor_detail.go：
  CompetitorItemDetail(ctx, cookieID, cookieStr, itemID) (CompetitorDetail, error)
  - 打开详情页 https://www.goofish.com/item?id=<id>（登录态注入，复用 newPage）
  - 监听 mtop.taobao.idle.pc.detail 响应，解析 data.itemDO（desc/imageInfos/title/price）
    与 data.sellerDO（sellerId/nickName）
  - 轮询等待响应（复用 sourcing 轮询模式），风控/会话失效即停（复用 sourcingRetError）
- CompetitorDetail：Title/Price/Desc/Images[]/SellerNickname/SellerID/WantCount

## 数据层（迁移 00063，三方言）
- market_intel_item_details：
  item_id PK、title、price、desc_text、images_json、seller_nickname、seller_id、
  want_count_text、fetched_at（7 天缓存判断）、created_at
- db 方法：GetItemDetail（含 7 天内有效判断）/ SaveItemDetail

## 应用层（internal/application/marketintel）
- DetailPort：FetchDetail(ctx, itemID) (CompetitorDetail, error)
- DetailStore：GetCachedDetail / SaveDetail
- GetCompetitorDetail：缓存命中（7 天内）直接返回；未命中调 DetailPort 并落库
- RewriteCopy(原标题/描述) → AI 改写（复用 ChatClient 模式，市场情报模型配置）
- AnalyzeSellingPoints(商品信息) → AI 卖点分析
- 图片下载复用 adapter CopyHTTPImageFetcher（搬运时触发，下载入资源库）

## HTTP（/api/v1/market-intel）
- GET /items/{item_id}/detail（缓存 7 天；未命中实时抓，可能耗时 10-20s）
- POST /items/{item_id}/rewrite（AI 改写文案）
- POST /items/{item_id}/analyze（AI 卖点分析）
- POST /items/{item_id}/carry（搬运：下载图片入资源库 → 返回资源库图片 ID 列表 + 预填数据）
- OpenAPI + 契约场景

## 前端（marketintel feature）
- KeywordAggregatesTable 行展开：该词当天商品列表（标题/价格/想要/卖家）
- 商品行「查看详情」→ CompetitorDetailModal（描述/图片网格/卖家/价格）
- 弹窗按钮：「AI 改写搬运」（默认改写，可切原文）、「AI 卖点分析」
- 搬运 → 调 carry → 预填现有单品发布弹窗（复用 ItemList 的 publish 流程 props）
- 图片相同提示条

## 发布对接
- carry 返回：title/desc/price + 资源库图片 ID 列表
- 前端把资源库图片转 File（现有 LibraryPicker 已有此能力）填入发布表单
