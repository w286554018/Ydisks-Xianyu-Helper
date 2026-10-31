
## 实施记录（2026-11-01）

### 已交付
- 应用层 `internal/application/items/copy_publish.go`：CopyPublishService（PreviewCopy/CreateCopyTask）+ 哨兵错误（ErrCopySourceMissing/ErrCopyNotOwned/ErrCopyNoTargets/ErrCopyTooManyTargets/ErrCopyMultiSpecUnsupported/ErrCopyNoImages）+ 应用自有模型 CopySourceItem（不暴露 db.ItemInfoRow，通过 adapter 转换）。
- 适配层 `internal/adapter/item_copy.go`：CopyHTTPImageFetcher / CopyAccountsAdapter / CopyItemSourceAdapter；`LibraryFileStore.SaveImage` 支持批次图片落盘。
- 组合层：buildItemCatalogServices 增加 copyPublish；`application_services.go` 的 New 拆出 `buildAccountServiceBundle` 以满足体量门禁；TransportPorts/`server.NewApplicationPorts` 增加_ITEM_COPY_PUBLISH_字段。
- HTTP 层：`application_ports_item_copy.go`（ItemCopyPublishPort 拆分文件避免超 800 行）+ `item_copy_handlers.go`（copy-preview 200 / copy-tasks 201，统一错误映射 400/403/404/409/500）+ 路由注册 `POST /api/v1/items/copy-preview`、`POST /api/v1/items/copy-tasks`。
- 契约：openapi.yaml 新增 2 路径 + 4 schema；schema.ts 再生成；契约覆盖场景 item-copy-publish 注册。
- 前端：api.ts/models.ts 增加 previewItemCopy/createItemCopyTask 与 UI 模型；`copyPublish.ts` Hook（预览加载/目标勾选/任务创建/代次取消）；`components/ItemCopyModal.tsx`（预填可编辑 + 目标账号预检标黄 + 间隔设置）；ItemList 表格与卡片视图均提供「复制到其他账号」入口；创建成功经 `openBatchResultByID` 跳既有批次进度。
- 零新增平台请求：图片走 CDN 下载（CopyHTTPImageFetcher），发布复用既有批量 worker 与节流。

### 验证证据
- `make check`（fmt/architecturecheck/apicheck/路由契约/操作契约覆盖/vet/golangci-lint 0 issues/全量 go test）全部通过。
- `make cover`：Go 语句覆盖 90.7%；`make cover-frontend`：语句 93.23%、分支 79.35%、函数 93.55%（未用 RUN_BROWSER_INTEGRATION）。
- 复制相关新文件测试：应用层 copy_publish_test.go、adapter/item_copy_test.go、server/item_copy_handlers_test.go、前端 copyPublish.test.tsx。
- 无外部平台/真实账号依赖的未覆盖例外。
