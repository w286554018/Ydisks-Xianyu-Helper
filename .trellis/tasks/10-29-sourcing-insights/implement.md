# 选品参考 实施计划

本任务按单一任务、单次提交推进，不拆分交付。以下步骤只是执行顺序，不是独立的里程碑。

## 阶段 0 前置确认

- [ ] 确认 `docs/architecture/refactoring-master-plan.md` 当前阶段状态表，选品参考不得引入新的依赖违规。
- [ ] 确认迁移编号：现有最大编号为 `00058`，本任务使用 `00059`。

## 阶段 1 数据层

- [ ] 新增 `internal/db/migrations/sqlite/00059_sourcing.sql`、`mysql/00059_sourcing.sql`、
      `postgres/00059_sourcing.sql`，三方言编号与最终结构一致。
- [ ] 新增 `internal/db/sourcing_repository.go`：快照 upsert、按日读取、30 天清理、本店畅销榜聚合查询。
- [ ] 新增 `internal/db/sourcing_repository_test.go`：SQLite 内存库覆盖 upsert 幂等、失败快照、
      30 天边界清理、畅销榜聚合口径（含非数字 `quantity`、软删除订单、状态过滤）。

验证：`go test ./internal/db -run Sourcing -v`

## 阶段 2 浏览器能力

- [ ] 新增 `internal/browser/sourcing.go`：
      - `FetchHotKeywords(ctx)`：游客 context，拦截热词接口。
      - `SearchItems(ctx, cookieID, cookieStr, keyword)`：账号 cookie，拦截搜索接口，DOM 兜底。
      - 错误变量 `ErrSourcingRiskControl`、`ErrSourcingSessionExpired`。
- [ ] 新增 `internal/browser/sourcing_test.go`：
      - 游客态请求不携带 cookie（断言请求头与 context cookie 为空）。
      - `RGV587` 立即中止且不重试（断言请求次数为 1）。
      - 会话过期分支映射到正确错误。
      - `resultList` 提取与 DOM 兜底两条路径。
      - 结果条数上限 30。

验证：`go test ./internal/browser -run Sourcing -v`

## 阶段 3 应用层

- [ ] 新增 `internal/application/sourcing/service.go`：用例编排与授权校验。
- [ ] 新增 `internal/application/sourcing/ranking.go`：本店畅销榜口径与模型。
- [ ] 新增 `internal/application/sourcing/copywriter.go`：AI `chat/completions` 调用与两套提示词。
- [ ] 新增 `internal/application/sourcing/cooldown.go`：同账号同关键词 10 秒冷却。
- [ ] 新增 `internal/application/sourcing/scheduler.go`：每日北京时间执行热词采集 + 畅销榜快照，
      具备 owner、ctx、Stop 幂等与 Wait 收敛。
- [ ] 新增对应测试：快照写入与清理、畅销榜无出站请求断言、冷却触发 429、
      AI 成功/无配置/HTTP 失败/空内容分支、scheduler 启停与幂等。

验证：`go test ./internal/application/sourcing -v`

## 阶段 4 传输层与契约

- [ ] 新增 `internal/server/sourcing_handlers.go` 与命名 DTO。
- [ ] 新增 `internal/server/versioned_sourcing_routes.go` 并在现有 `/api/v1` 挂载点注册。
- [ ] 更新 `api/openapi.yaml`：5 个操作、请求/响应 schema、错误信封。
- [ ] 重新生成 `frontend/shared/api-contract/generated/schema.ts`
      （`npm run api:generate --prefix frontend`）。
- [ ] 新增 `internal/server/sourcing_handlers_test.go`：鉴权、所有权、状态码、错误信封、
      契约场景覆盖。

验证：`make api-check`、`go test ./internal/server -run Sourcing -v`

## 阶段 5 组合根接线

- [ ] 在 `internal/composition` 装配 sourcing 服务与 scheduler，保持依赖由消费者定义。
- [ ] 在 `cmd/server` 启动与退出流程中登记 scheduler 的 Start / Stop / Wait，
      遵守生命周期与并发约束。
- [ ] 浏览器不可用（`-no-browser`）时降级为仅本店畅销榜与 AI 文案可用。

验证：`go test ./internal/composition ./cmd/server -v`

## 阶段 6 前端

- [ ] 新增 `frontend/app/features/sourcing/`：`models.ts`、`api.ts`、`hooks.ts`、
      `pages/SourcingInsights.tsx`、四个组件。
- [ ] 导航新增「选品参考」入口。
- [ ] 测试：`hooks.test.tsx` 覆盖成功、失败、取消、切换关键词、陈旧响应；
      组件测试覆盖渲染、禁用态、冷却提示、AI 失败提示。
- [ ] 所有新增函数与变量补齐中文语义注释，通过 `comments:check`。

验证：`npm --prefix frontend run test`、`npm --prefix frontend run comments:check`

## 阶段 7 全量验证

- [ ] `make fmt`
- [ ] `go run ./tools/architecturecheck`
- [ ] `make api-check`
- [ ] `make vet`
- [ ] `make lint`（0 issues）
- [ ] `make test`
- [ ] `make comments`
- [ ] `make cover` 并记录 Go 语句覆盖率与例外分类
- [ ] `npm --prefix frontend run test` 与 `npm --prefix frontend run comments:check`
- [ ] `npm --prefix frontend run build` 重新生成 `internal/webui/static`
- [ ] 重启服务，实际访问新页面，验证热词、手动查询、畅销榜与 AI 文案四条路径

## 阶段 8 提交

- [ ] 单次中文提交，提交信息说明功能范围、请求频率控制方式与验证结果。
- [ ] 不在提交中包含 `dist/`、`cover*.out`、`frontend/coverage/` 等生成物。

## 明确不做

- 不新增自动翻页、多关键词轮询、并发抓取。
- 不修改 `pacer.go` 间隔参数与滑块验证相关文件。
- 不为搜索结果建立持久化商品表。

## 实施记录与设计调整（2026-10-30）

### 相对 design.md 的调整
1. **本店畅销榜改为按用户实时计算，不再写每日快照。** 原设计的 `sourcing_snapshots` 以 (kind, snapshot_date) 唯一，不区分用户，多用户时会互相覆盖并可能读到他人数据。现改为接口请求时只读当前用户名下账号的本地订单实时聚合，同时支持 7/30 天切换；该路径无任何平台请求。快照表只保存平台热词（全局、游客态、与用户无关）。
2. **调度器只负责热词**：每日北京时间 8 点后执行一次；若当天已有成功快照则跳过，避免服务重启触发平台请求；失败当天不自动重试。
3. **错误码**：查货无结果返回 200 + 空列表（不使用 200 + 错误信封）；热词采集无结果 503；账号不属于当前用户 403；空关键词 400；冷却 429；风控/会话失效/AI 未配置 409；浏览器未启用 503。
4. **选品参考为可选能力**：`-no-browser` 时不登记调度器，热词与查货返回 503，畅销榜与 AI 文案仍可用；Server 未装配时端点返回 503。
5. 为满足架构门禁（函数 ≤180 行、文件 ≤800 行），选品装配抽到 `internal/composition/runtime/runtime_sourcing.go`，`SourcingPort` 独立到 `internal/server/sourcing_ports.go`。
6. 迁移版本推进到 59，`internal/db/migrate_test.go` 与 `keyword_expressions_test.go` 中"最新版本"断言同步改为 59，并在回滚重升检查中加入 `sourcing_snapshots.payload_json`。

### 验证结果
- `go run ./tools/architecturecheck`：通过
- `go run ./tools/apicheck` 与 `npm run api:check`：通过
- `go vet ./...`：通过；`golangci-lint run ./...`（v2.15.0，CI 指定 v2.12.2）：0 issues
- `go test ./... -count=1`：全部通过；`go test -race`（sourcing 应用层与 adapter）：通过
- Go 注释门禁与前端注释门禁：通过
- 前端 `tsc --noEmit`：通过；`vitest run`：134 个文件 / 829 项通过
- 前端构建并嵌入 `internal/webui/static`：成功
- 实机：迁移到 59；5 个端点已注册且未登录返回 401；`/app/sourcing` 返回 200
- 实机平台验证：游客态热词采集取到 10 个词条（一次性临时测试，已删除）
- 尚未实机验证：登录态关键词查货（需用户在页面点击，消耗 1 次搜索）、AI 文案（需系统已配置 AI）
- 覆盖率命令 `make cover` / `make cover-frontend` 未执行，未提供覆盖率百分比。
