# 本机抓取助手：macOS 自启（launchd）

竞品详情依赖家用机上的抓取助手与到服务器的 SSH 反向隧道常驻。本目录把两者做成
用户级 LaunchAgent，登录即启动、异常退出自动重启，避免每次重启 Mac 后手动拉起。

## 安装

```bash
packaging/macos/local-fetch-agent/install-autostart.sh -t
```

- `-t`（接管）会先停掉手动启动的同名助手/隧道进程；不加该参数时，若检测到手动进程会直接报错退出，
  因为它们占用的端口会让 launchd 服务反复重启。
- 可选参数：`-s <服务器>`、`-p <SSH端口>`、`-k <私钥>`、`-r <隧道端口>`，
  默认值为 `69.63.212.5`、`19371`、`~/.ssh/ydisks_69_63_212_5_19371`、`17901`。

脚本会：构建并安装助手到 `~/.ydisks-fetch/bin/local-fetch-agent`、准备令牌（不存在时生成 32 字节十六进制串，
权限 0600）、渲染并加载两个 plist、等待回环健康检查通过。

## 卸载

```bash
packaging/macos/local-fetch-agent/uninstall-autostart.sh      # 仅移除服务，保留令牌与日志
packaging/macos/local-fetch-agent/uninstall-autostart.sh -p   # 连同 ~/.ydisks-fetch 一并删除
```

## 文件与路径

| 内容 | 路径 |
| --- | --- |
| 助手二进制 | `~/.ydisks-fetch/bin/local-fetch-agent` |
| 共享令牌（0600） | `~/.ydisks-fetch/token` |
| 日志 | `~/.ydisks-fetch/logs/{agent,tunnel}.{out,err}.log` |
| 专用浏览器 profile | `~/Library/Application Support/ydisks-fetch-chrome` |
| 服务定义 | `~/Library/LaunchAgents/com.ydisks.{local-fetch-agent,local-fetch-tunnel}.plist` |

服务器侧 `.env` 的 `LOCAL_FETCH_AGENT_TOKEN` 必须与 `~/.ydisks-fetch/token` 一致；
不一致时详情接口会返回 `503 local_fetch_unauthorized`。

## 常用运维命令

```bash
# 查看服务状态（第一列 PID，第二列最近退出码）
launchctl list | grep ydisks

# 查看渲染后的服务定义
plutil -p ~/Library/LaunchAgents/com.ydisks.local-fetch-agent.plist

# 手动重启（改完配置或想立即重连时）
launchctl kickstart -k gui/$(id -u)/com.ydisks.local-fetch-agent
launchctl kickstart -k gui/$(id -u)/com.ydisks.local-fetch-tunnel

# 本机自检
curl -s http://127.0.0.1:17901/health

# 服务器侧确认隧道（应看到 127.0.0.1:17901 监听）
ssh -p <SSH端口> root@<服务器> 'ss -lnt | grep 17901'
```

## 已知行为

- **隧道断开**时详情接口会立即返回 `503 local_fetch_unavailable`，这是设计行为
  （服务器侧浏览器抓取已被平台判罚，不做回退）。KeepAlive 会在数秒内自动重连。
- **首次使用需人工登录**：专用 profile 的浏览器需登录一次闲鱼；
  登录态保存在该 profile 中，之后长期复用。登录失效时详情返回
  `503 local_fetch_not_logged_in`，在助手浏览器窗口中重新登录即可。
- **占用 17901 端口的手动进程**会让 launchd 服务反复重启；用 `-t` 安装或在安装前手动停止。
- 日志为追加写，长期运行后可按需清理：`rm -f ~/.ydisks-fetch/logs/*.log`（服务会重新创建）。
