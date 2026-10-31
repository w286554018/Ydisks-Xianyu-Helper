# 设计：跨账号复制发布

## 核心策略：零新链路，全复用
复制发布 = 从本地商品快照生成一份「批量铺货预检批次」（行 = 目标账号 × 商品内容），后续完全复用
既有批量 worker（发布、进度、取消、失败重试、账号级节流）。

- 无新迁移：复用 item_publish_batches / item_publish_batch_rows
- 无新平台接口：快照含 imageInfos URL 列表；发布期图片由 LoadBatchPublishImages 的 downloadRemote 下载
- 多规格源商品：直接拒绝并提示（本期范围外；当前库中无多规格商品）

## 应用层（internal/application/items/copy_publish.go）
- CopyPreviewService：
  - PreviewCopy(userID, cookieID, itemID, targetCookieIDs)：读 item_info 快照 → CopyPreview（标题/描述/价格/图片URL/类目/包邮 + 目标账号归属校验与状态）→ 不发任何平台请求
  - CreateCopyTask(userID, source, targets, edits)：多规格拒绝 → 快照行展开（每目标一行，可编辑字段覆盖）→ 复用 PreviewRowsValidator + PreviewPersister 落 preview 批次 → 返回 batchID

## 端点（/api/v1，OpenAPI + 契约测试同步）
- POST /api/v1/items/copy-preview：源商品快照预览 + 目标账号预检
- POST /api/v1/items/copy-tasks：创建复制批次（返回 batchId，前端用既有批次进度接口轮询）

## 前端（features/items）
- ItemList 多选模式新增「复制到其他账号」操作
- CopyDialog：目标账号多选（预检异常标黄）+ 预填可编辑字段 + 确认
- 确认后复用现有批次进度展示
