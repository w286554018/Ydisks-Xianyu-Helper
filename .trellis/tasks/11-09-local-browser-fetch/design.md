# 11-09-local-browser-fetch 设计

## 1. 架构与数据流

```
                 用户 Mac（住宅网络）                          服务器 69.63.212.5
┌──────────────────────────────────────────┐        ┌────────────────────────────────┐
│  真实 Google Chrome（专用 user-data-dir） │        │  xianyu-server（应用容器）      │
│        ▲ CDP 127.0.0.1:9222               │        │                                │
│        │                                  │        │  ① 详情接口 → 7 天缓存命中？    │
│  local-fetch-agent（HTTP 127.0.0.1:17901）│        │     命中 → 直接返回            │
│        ▲ 共享令牌                          │        │  ② 未命中 → 调用本机抓取助手    │
│        │                                  │        │        │                       │
└────────┼──────────────────────────────────┘        │        ▼                       │
         │  ssh -R 17901:127.0.0.1:17901（Mac 主动）  │  LocalFetchAgentClient         │
         └───────────────────────────────────────────▶│  http://127.0.0.1:17901        │
                                                      │        │                       │
                                                      │  ③ 详情 JSON → 既有缓存表       │
                                                      │  ④ AI 改写 / 图片下载入库 / 搬运 │
                                                      └────────────────────────────────┘
```

要点：只有「取详情 JSON」这一步离开服务器；缓存、AI 改写、图片 CDN 下载、搬运预填全部保持现状。

## 2. Mac 侧：local-fetch-agent

- 位置：同仓库 `cmd/local-fetch-agent/`（复用既有 google/uuid、playwright-go 等依赖，版本与主程序对齐）。
- 监听：`127.0.0.1:17901`（可配 `-listen`）；**只绑回环**，不监听 `0.0.0.0`。
- 配置（参数或环境变量）：`-chrome-path`（默认 `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`）、
  `-chrome-profile-dir`（默认 `~/Library/Application Support/ydisks-fetch-chrome`）、
  `-debug-port`（默认 9222）、`-token-file`（默认 `~/.ydisks-fetch/token`，0600）。
- 启动流程：调试端口已就绪则直接 `ConnectOverCDP`；否则以专用 profile 拉起 Chrome
  （`--user-data-dir=<dir> --remote-debugging-port=<port> --no-first-run --no-default-browser-check
  `--no-default-browser-check`，打开 `https://www.goofish.com/`），轮询 CDP 就绪后再服务。
- 接口（`X-Auth-Token` 必须匹配令牌文件内容）：
  - `GET /health` → `{status, chrome_connected}`（不需要令牌，便于本机自检）
  - `POST /fetch-detail`，body `{"item_id":"<纯数字>"}`
    - `item_id` 校验：仅允许 1–32 位数字；**不接受 URL / host / 路径**，由助手自己拼
      `https://www.goofish.com/item?id=<item_id>`（避免成为通用代理）。
    - 通过 CDP 复用/新建标签页导航到该地址，监听页面**自发**请求
      `mtop.taobao.idle.pc.detail` 的响应，解析为详情结构后返回。
    - 未登录（页面跳登录 / cookie 无 `unb`）→ `409 not_logged_in`；命中平台人机验证
      （punish / `FAIL_SYS_USER_VALIDATE`）→ `409 risk_control`，消息里提示"请在助手浏览器中完成验证"。
- 复用既有解析：`internal/browser` 已把详情解析抽成纯函数
  （`parseCompetitorDetailPayload` / `buildCompetitorDetail` / `stripHTMLTags`），
  助手通过**新增的导出包装**复用它们，避免复制解析逻辑。
- 日志：不打印令牌、不打印完整 Cookie；只记 `item_id`、结果状态与耗时。

## 3. 服务器侧

- 应用层 `internal/application/marketintel/detail.go`：`detailFetcher` 端口签名不变；
  新增场景专属错误：`ErrLocalFetchUnavailable` / `ErrLocalFetchUnauthorized` /
  `ErrLocalFetchNotLoggedIn` / `ErrLocalFetchRiskControl`（与既有
  `ErrCompetitorDetailRiskControl` 区分，避免文案歧义）。
- 新适配器 `internal/adapter/local_fetch_agent.go`：
  - `LocalFetchAgentClient`（`http.Client` 超时 90s；`X-Auth-Token` 来自配置）；
  - 把助手的 HTTP 状态与错误码映射为上述应用错误；
  - 连接被拒 / 超时 / DNS 失败 → `ErrLocalFetchUnavailable`；
  - 任何路径都不得把令牌写入错误文本或日志（错误文本用固定中文文案 + 状态码）。
- 组合根（`internal/composition`）：新增配置 `LOCAL_FETCH_AGENT_URL`、
  `LOCAL_FETCH_AGENT_TOKEN`（从 env/`.env` 读取，0600）。
  - 已配置 → 详情端口注入 `LocalFetchAgentClient`；
  - 未配置 → **保留既有服务器侧浏览器实现作为可配置兜底（默认关闭）**，
    以便无 Mac 的开发环境仍可回归既有测试；不允许在已配置助手的生产环境自动回退。
- transport（`internal/server/market_intel_detail_handlers.go`）：
  - `ErrLocalFetchUnavailable` → **503** + 错误码 `local_fetch_unavailable`（含可操作提示）；
  - `ErrLocalFetchUnauthorized` → **503** + `local_fetch_unauthorized`；
  - `ErrLocalFetchNotLoggedIn` → **503** + `local_fetch_not_logged_in`；
  - `ErrLocalFetchRiskControl` → **409** + `competitor_detail_risk_control`（保持既有语义）；
  - 新增未归类错误必须留 `Warn` 诊断日志。
- 契约：`api/openapi.yaml` 详情接口补 503 响应；`versioned_*` 契约测试补场景；
  前端 `marketintel` 按错误码显示可操作文案（最小改动，不新增页面）。
- 部署：服务器侧不新增任何公网监听；隧道端口只在服务器回环可见；
  `.env` 增加两个键并由 `docker compose` 注入应用容器。

## 4. 关键取舍

- **为什么不回退服务器侧浏览器**：实测该路径必然 55s+409，且每次回退都会再触发 3+3 次滑块，
  加重平台风控（项目铁律要求风控即停）。助手不可用时应立刻明确失败。
- **为什么助手只回 JSON**：把不可控面压缩到单点；图片走阿里 CDN 直链，服务器下载不受详情接口风控影响。
- **为什么用专用 profile**：Chrome 136+ 禁止在默认 profile 上开调试端口；
  且用户日常闲鱼登录态在火狐中，Chrome 日常 profile 无法复用。
- **为什么用 CDP 附着而不是让助手自己开无头浏览器**：本方案的全部价值就是"真人浏览器 + 真人登录态"，
  必须附着在用户可见的真实 Chrome 上。

## 5. 风险与回退

- **主要未知**：附着 CDP 后平台是否仍判罚。若仍判罚，退一步的方案是
  「用户在助手浏览器里手动打开商品页，助手只读取已加载页面的响应」——更接近纯人工，但需要用户点一下。
  实施阶段第 1 步就先做这个可行性的最小实测，再写服务器侧代码。
- **可用性**：Mac 休眠 / 隧道断开时助手不可达 → 503 语义；可选提供 launchd plist 让隧道与助手自启自恢复。
- **登录态失效**：闲鱼登录失效时助手返回 `409 not_logged_in`，用户需在助手浏览器中重新登录（可能需人脸验证）。

## 6. 需要用户确认的开放项

1. 助手代码放在本仓库 `cmd/local-fetch-agent/`（推荐）还是独立目录/仓库。
2. 是否接受「未配置助手时保留服务器侧浏览器为可配置兜底」，以便本机开发回归既有测试。
