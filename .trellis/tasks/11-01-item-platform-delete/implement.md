# 实施计划

## 阶段 0：协议 ground truth（先于一切平台代码）✅ 已完成
- 启动有头浏览器加载登录 profile（复用 browser.Manager 交互页签）
- 实际采用步进模式（agent 逐步驱动 + 用户监场防人机验证），商品 1097378253052 已真实下架+删除
- 捕获页面自发请求：API 名、参数、签名头
- 产出：ground truth 已记录本目录 protocol-notes.md（downshelf 2.0 与 delete 1.1，请求体同构 data={"itemId":"<id>"}；响应体未捕获，阶段 2 首次真实调用时验证）

## 阶段 1：数据层 ✅ 已完成
- 迁移 00060：item_info.status（三方言，默认 `active`；Down 为真 DROP COLUMN 以支持升级测试 DownTo 回滚）
- Items 状态读写方法 + 焦点测试：
  - ItemStatusActive/ItemStatusDown 常量；ItemInfoRow.Status 字段
  - AllForCookie/GetByCookieItem/ListForUser 三查询返回 status（COALESCE 兜底 active）
  - UpdateStatus：非法值报错、0 行更新返回 ErrNotFound；Upsert/SyncFromRemote 不重置 status（防同步复活）
  - 测试：item_status_test.go（默认值/切换/非法值/NotFound/同步保留）
  - 迁移版本断言测试同步更新 59→60（migrate_test.go、keyword_expressions_test.go）
- 验证：internal/db 全测试通过；vet/commentlint/architecturecheck/全仓 build 通过；全量 go test 后台确认中

## 阶段 2：协议层 ✅ 已完成
- internal/xianyu/mtop/item_lifecycle.go：DownshelfItemContext / DeleteItemContext
  - 请求体同构共用 once/retry/query；URL 查询与表单体严格按抓包 ground truth
  - 节流：走 httpClient()（pacingTransport 账号级节流自动生效）
  - 失败分类：token 过期→重试（≤4 次含主动刷新）；风控 FAIL_SYS_USER_VALIDATE/RGV587→立即停止不重试；业务 FAIL_BIZ_→静默 ok=false；非 2xx→保留 ret 返回错误
  - ClientImpl 新增 DownshelfItemURL/DeleteItemURL 测试覆盖字段（生产空值用官方端点）
- 测试 item_lifecycle_test.go：wire 形态对照 ground truth、风控即停 requests==1、业务拒绝不重试、HTTP 错误保留 ret
- 验证：mtop 包全测试通过、commentlint/architecturecheck 通过

## 阶段 3：应用层 + 批次 ✅ 已完成
- 迁移 00061（三方言）：item_lifecycle_batches + item_lifecycle_batch_rows（Down 真 DROP TABLE）
- internal/db/item_lifecycle_batches.go：批次与行仓储（创建/查询/列表/租约/行抢占按 status==pending 可重抢占/计数收口不降级 partially_failed/失败重试/取消）
- internal/application/items/lifecycle.go：应用自有模型 + 窄端口（LifecyclePlatformPort/LifecycleStore/LifecycleOwnershipPort）+ RunLifecycleBatch（串行 + 5-10s 抖动可取消 + 风控即停）；platform/session/platform_failed 分类经构造注入 classifyErr
- internal/adapter/item_lifecycle.go：平台端口（凭证快照 + persistBatchCookieSession 会话回写）、归属适配器、仓储适配器、错误哨兵翻译（db→应用，保持 server 不依赖 db）

## 阶段 4：HTTP + 契约 ✅ 已完成
- 端点：POST /api/v1/items/lifecycle（201）、GET /api/v1/items/lifecycle-batches、GET/POST .../{batch_id}、.../retry-failed、.../cancel；DTO 具名、错误信封统一（Busy→409、NotFound→404）
- 商品列表 DTO 加 status（active/down），OpenAPI ItemListResponse 同步 enum
- OpenAPI 5 路径 + 8 schema（每 operation 补齐 400/401）；schema.ts 再生成；契约场景 item-lifecycle 注册
- 后台执行由 transport（itemLifecycleTransport，进程根 Context）负责，handler 不启动 goroutine

## 阶段 5：前端 ✅ 已完成
- api.ts 生命周期五个封装 + models.ts Item.status 与生命周期 UI 模型
- lifecycle.ts：useItemLifecycle（确认弹窗状态、批次创建、进度轮询 3s、取消、失败重试、结束后 loadItems）；requiresDeletePhrase 批量删除防呆
- components/ItemLifecycleModals.tsx：确认弹窗（下架普通确认/删除警告/批量删除输入「删除」）与批次进度弹窗（状态卡 + 行明细 + 取消/重试）
- ItemTable：下架按钮、已下架徽标、已下架隐藏编辑/下架并禁用复制、多选框（selectedKeys/onToggleSelect）
- ItemList：行删除改走平台删除（1 行批次）、批量操作栏（批量下架/删除/清除选择）、卡片模式下架按钮与徽标
- 测试：lifecycle.test.tsx（6 用例）+ ItemTable.test.tsx（4 用例）；ItemList 分片预算更新为 90 KiB（旧 66 KiB 预算在功能合入前已过期，实际 88.27 KiB）

## 验证门禁 ✅ 已完成
- go test ./... 全过；vet/architecturecheck/commentlint 通过；make check 除 golangci-lint 本机未安装（既有状态）外全过
- make cover：Go 90.1%（≥90%；补 adapter/item_lifecycle_test.go 后达标，RUN_BROWSER_INTEGRATION=0）
- cover-frontend：92.74% 语句覆盖；异常仅真实平台账号与平台行为路径
- 全部文件一次中文提交
