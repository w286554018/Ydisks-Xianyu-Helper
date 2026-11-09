# 11-09-local-browser-fetch 实施计划

## 阶段 0：可行性实测（go / no-go 关卡，先做）

目的：在写任何服务器侧代码前，确认「附着 CDP 的真实 Chrome」打开闲鱼商品页时**不被判罚**。

1. 用专用 profile 启动本机 Chrome 并开调试端口：
   `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome --user-data-dir="$HOME/Library/Application Support/ydisks-fetch-chrome" --remote-debugging-port=9222 https://www.goofish.com/`
2. 用户在该窗口中**手动登录闲鱼**（已登录则跳过）。
3. 一次性实测脚本：经 CDP 打开 `https://www.goofish.com/item?id=<真实商品ID>`，
   监听 `mtop.taobao.idle.pc.detail` 响应 → 打印 `ret` 与关键字段。
   - 判定：拿到正常 `ret` 与商品字段 = **通过**，进入阶段 1；
   - 若仍返回 `FAIL_SYS_USER_VALIDATE` / punish = **不通过**，退到
     「用户手动打开商品页、助手只读取已加载页面响应」的形态，回来重新确认设计。

验证：脚本输出（脱敏后）记录到本任务目录。

## 阶段 1：Mac 侧助手 `cmd/local-fetch-agent`

1. HTTP 服务：`GET /health`（免令牌）、`POST /fetch-detail`（`X-Auth-Token`）。
2. `item_id` 校验：仅 1–32 位数字，否则 400 且不发起任何出站请求。
3. Chrome 管理：调试端口就绪则 `ConnectOverCDP`，否则按专用 profile 拉起并等待就绪。
4. 抓取：复用 `internal/browser` 的解析纯函数（新增导出包装），返回详情 JSON。
5. 错误语义：`unavailable` / `unauthorized` / `not_logged_in` / `risk_control` / `bad_item_id`。
6. 令牌：从 0600 文件读取；缺失时启动即失败并给出生成命令；任何日志与响应都不含令牌。

验证：`go test ./cmd/local-fetch-agent/...` 覆盖校验、令牌、错误映射、无令牌泄漏；
本机 `curl` 实测 `health` 与真实 `item_id`。

## 阶段 2：服务器侧适配器与配置

1. `internal/application/marketintel/detail.go`：新增 4 个场景错误。
2. `internal/adapter/local_fetch_agent.go`：`LocalFetchAgentClient`（90s 超时、令牌头、错误映射）。
3. `internal/composition`：读取 `LOCAL_FETCH_AGENT_URL` / `LOCAL_FETCH_AGENT_TOKEN`，
   已配置则注入；未配置保留既有浏览器实现（可配置兜底，默认关闭）。
4. 组合根新增单测（已配置 / 未配置 / 令牌缺失 / URL 非法 四分支）。

验证：`go test ./internal/application/marketintel/... ./internal/adapter/... ./internal/composition/...`。

## 阶段 3：传输层、契约与前端

1. `internal/server/market_intel_detail_handlers.go`：503/409 映射 + 未归类错误 `Warn`。
2. `api/openapi.yaml`：详情接口补 503；`make api-check`；补契约成功与失败场景。
3. 前端 `marketintel`：按错误码显示可操作文案（不新增页面、不改数据流）。

验证：`go test ./internal/server/...`、`make api-check`、前端 `npm run typecheck` 与既有测试。

## 阶段 4：接线、部署与端到端实测

1. 服务器 `.env` 增加两个键（0600），compose 注入；不新增公网监听。
2. Mac 侧启动脚本：`ssh -R` 隧道 + 助手（可选 launchd plist 自启自恢复）。
3. 端到端实测：
   - 助手在线 → 详情 200（与缓存/AI 改写/搬运打通）；
   - 停掉助手 → 503 `local_fetch_unavailable` 且 1 秒内返回、**不触发**服务器侧浏览器；
   - 令牌错误 → 503 `local_fetch_unauthorized`；
   - 缓存命中 → 不发助手请求。
4. 更新服务器 `DEPLOYMENT.md` 与项目文档（含回退步骤）。

## 全量验证清单

- [ ] `go test ./...`（含架构门禁）
- [ ] `go vet ./...`、`commentlint`（Go + 前端）
- [ ] `make api-check`
- [ ] Go 覆盖率 ≥ 90%（新增确定性函数与错误分支全部有测试）
- [ ] OpenAPI 503 契约 + 契约测试
- [ ] 端到端四条路径实测（在线 / 离线 / 令牌错 / 缓存命中）
- [ ] 敏感信息检查：令牌不出现在日志、响应、错误消息、提交内容中
