#!/bin/bash
# 安装本机抓取助手与 SSH 反向隧道的 LaunchAgent 自启服务。
#
# 做四件事：构建并安装助手可执行文件到用户主目录下的稳定路径、准备令牌与日志目录、
# 渲染并加载两个 LaunchAgent、等待助手健康检查通过。
# 助手只监听回环地址；脚本不修改任何服务器配置，只建立到服务器的反向隧道。
set -euo pipefail

# 默认参数：与本仓库当前部署一致，可用命令行参数覆盖。
DEFAULT_SERVER_HOST="69.63.212.5"
DEFAULT_SERVER_PORT="19371"
DEFAULT_REMOTE_PORT="17901"
# AGENT_LABEL 与 TUNNEL_LABEL 是两个服务的标识，需与 plist 中的 Label 一致。
AGENT_LABEL="com.ydisks.local-fetch-agent"
TUNNEL_LABEL="com.ydisks.local-fetch-tunnel"

# 脚本自身位置与仓库根目录，用于定位模板并构建助手。
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# HOME_DIR 是当前用户主目录；LAUNCH_AGENTS_DIR 是当前用户的 LaunchAgent 目录。
HOME_DIR="${HOME}"
LAUNCH_AGENTS_DIR="${HOME_DIR}/Library/LaunchAgents"
# FETCH_DIR 保存助手二进制、令牌与日志；这些内容都不进入版本库。
FETCH_DIR="${HOME_DIR}/.ydisks-fetch"
BIN_DIR="${FETCH_DIR}/bin"
LOG_DIR="${FETCH_DIR}/logs"
BIN_PATH="${BIN_DIR}/local-fetch-agent"
TOKEN_PATH="${FETCH_DIR}/token"
# DOMAIN 是当前用户的 launchd 域。
DOMAIN="gui/$(id -u)"

# 运行期参数。
SERVER_HOST="${DEFAULT_SERVER_HOST}"
SERVER_PORT="${DEFAULT_SERVER_PORT}"
REMOTE_PORT="${DEFAULT_REMOTE_PORT}"
SSH_KEY="${HOME_DIR}/.ssh/ydisks_${DEFAULT_SERVER_HOST//./_}_${DEFAULT_SERVER_PORT}"
# TAKEOVER 表示允许停掉手工启动的同名助手或隧道（它们会占用端口，导致 launchd 服务反复重启）。
TAKEOVER=0

# usage 打印用法。
usage() {
    cat <<'USAGE'
用法: install-autostart.sh [选项]

选项:
  -s <host>       服务器地址（默认 69.63.212.5）
  -p <port>       SSH 端口（默认 19371）
  -k <keyfile>    SSH 私钥路径（默认 ~/.ssh/ydisks_<host>_<port>，点号替换为下划线）
  -r <port>       反向隧道端口（默认 17901）
  -t              接管：停掉手工启动的助手/隧道进程后继续（默认拒绝并提示）
  -h              显示本帮助
USAGE
}

# log 输出带前缀的进度信息。
log() {
    printf '[autostart] %s\n' "$*"
}

# fail 输出错误并退出。
fail() {
    printf '[autostart] 错误: %s\n' "$*" >&2
    exit 1
}

# 解析参数。
while getopts ":s:p:k:r:th" opt; do
    case "${opt}" in
        s) SERVER_HOST="${OPTARG}" ;;
        p) SERVER_PORT="${OPTARG}" ;;
        k) SSH_KEY="${OPTARG}" ;;
        r) REMOTE_PORT="${OPTARG}" ;;
        t) TAKEOVER=1 ;;
        h) usage; exit 0 ;;
        *) usage; fail "未知选项 -${OPTARG}" ;;
    esac
done

# 前置检查：必需命令、私钥、模板与仓库结构。
command -v go >/dev/null 2>&1 || fail "未找到 go，无法构建助手"
command -v openssl >/dev/null 2>&1 || fail "未找到 openssl，无法生成共享令牌"
command -v lsof >/dev/null 2>&1 || fail "未找到 lsof，无法检查端口占用"
[ -f "${SSH_KEY}" ] || fail "找不到 SSH 私钥: ${SSH_KEY}"
[ -f "${SCRIPT_DIR}/com.ydisks.local-fetch-agent.plist.tmpl" ] || fail "缺少助手 plist 模板"
[ -f "${SCRIPT_DIR}/com.ydisks.local-fetch-tunnel.plist.tmpl" ] || fail "缺少隧道 plist 模板"
[ -f "${REPO_ROOT}/cmd/local-fetch-agent/main.go" ] || fail "仓库结构异常，未找到 cmd/local-fetch-agent"

# 接管检查：按「谁在监听回环端口」判断，避免把本脚本自身或 launchd 托管的助手误判为手工进程。
# LISTEN_PIDS 是当前监听助手端口的进程集合（可能为空）。
LISTEN_PIDS="$(lsof -nP -iTCP:"${REMOTE_PORT}" -sTCP:LISTEN -t 2>/dev/null | sort -u || true)"
# OUR_PID 是已加载的 launchd 助手服务当前 PID（服务未加载时为空）。
OUR_PID="$(launchctl print "${DOMAIN}/${AGENT_LABEL}" 2>/dev/null | awk '/^[[:space:]]*pid = [0-9]+/{print $3}' || true)"
# FOREIGN_PIDS 是监听该端口但不属于本 launchd 服务的进程。
FOREIGN_PIDS=""
for pid in ${LISTEN_PIDS}; do
    [ "${pid}" = "${OUR_PID}" ] && continue
    FOREIGN_PIDS="${FOREIGN_PIDS} ${pid}"
done
FOREIGN_PIDS="$(printf '%s' "${FOREIGN_PIDS}" | tr -s ' ' | sed -e 's/^ //' -e 's/ $//')"
# MANUAL_TUNNEL 是手工启动的反向隧道进程（方括号写法避免匹配到本脚本自身）。
MANUAL_TUNNEL="$(pgrep -f "[-]R ${REMOTE_PORT}:127.0.0.1:${REMOTE_PORT}" 2>/dev/null || true)"

if { [ -n "${FOREIGN_PIDS}" ] || [ -n "${MANUAL_TUNNEL}" ]; } && [ "${TAKEOVER}" -ne 1 ]; then
    log "检测到手工启动的助手或隧道，它们会占用 ${REMOTE_PORT} 端口并让 launchd 服务反复重启："
    if [ -n "${FOREIGN_PIDS}" ]; then
        log "  占用端口的进程："
        ps -o pid=,command= -p ${FOREIGN_PIDS} 2>/dev/null | sed 's/^/    /' || true
    fi
    if [ -n "${MANUAL_TUNNEL}" ]; then
        log "  手工隧道 PID: $(printf '%s' "${MANUAL_TUNNEL}" | tr '\n' ' ')"
    fi
    fail "请先停止它们，或使用 -t 让本脚本接管"
fi
if [ "${TAKEOVER}" -eq 1 ]; then
    if [ -n "${FOREIGN_PIDS}" ]; then
        log "接管：停止占用端口的进程 ${FOREIGN_PIDS}"
        kill ${FOREIGN_PIDS} 2>/dev/null || true
    fi
    if [ -n "${MANUAL_TUNNEL}" ]; then
        log "接管：停止手工隧道 $(printf '%s' "${MANUAL_TUNNEL}" | tr '\n' ' ')"
        kill ${MANUAL_TUNNEL} 2>/dev/null || true
    fi
    # 停掉占用端口的进程后短暂等待，确保端口真正释放。
    sleep 2
fi

# 准备目录与令牌：令牌不存在时生成 32 字节十六进制串，权限 0600。
mkdir -p "${BIN_DIR}" "${LOG_DIR}" "${LAUNCH_AGENTS_DIR}"
chmod 700 "${FETCH_DIR}" "${BIN_DIR}"
if [ ! -s "${TOKEN_PATH}" ]; then
    log "生成共享令牌: ${TOKEN_PATH}"
    openssl rand -hex 32 > "${TOKEN_PATH}"
fi
chmod 600 "${TOKEN_PATH}"

# 构建助手：明确 darwin 与当前架构，避免交叉编译产物不可执行。
# GOARCH_RAW 是 uname -m 的原始值；GOARCH_TARGET 是 Go 使用的架构名。
GOARCH_RAW="$(uname -m)"
case "${GOARCH_RAW}" in
    arm64|aarch64) GOARCH_TARGET="arm64" ;;
    x86_64|amd64) GOARCH_TARGET="amd64" ;;
    *) fail "不支持的架构: ${GOARCH_RAW}" ;;
esac
log "构建助手 (darwin/${GOARCH_TARGET}) → ${BIN_PATH}"
(cd "${REPO_ROOT}" && GOOS=darwin GOARCH="${GOARCH_TARGET}" go build -trimpath -o "${BIN_PATH}" ./cmd/local-fetch-agent)
chmod 755 "${BIN_PATH}"

# render 渲染 plist：把模板占位符替换为实际路径与参数。
render() {
    # template 是模板文件路径；dest 是渲染后的落盘路径。
    local template="$1"
    local dest="$2"
    sed \
        -e "s|__HOME__|${HOME_DIR}|g" \
        -e "s|__SSH_KEY__|${SSH_KEY}|g" \
        -e "s|__SERVER_HOST__|${SERVER_HOST}|g" \
        -e "s|__SERVER_PORT__|${SERVER_PORT}|g" \
        -e "s|__REMOTE_PORT__|${REMOTE_PORT}|g" \
        "${template}" > "${dest}"
    chmod 644 "${dest}"
}

# AGENT_PLIST 与 TUNNEL_PLIST 是渲染后的服务定义路径。
AGENT_PLIST="${LAUNCH_AGENTS_DIR}/${AGENT_LABEL}.plist"
TUNNEL_PLIST="${LAUNCH_AGENTS_DIR}/${TUNNEL_LABEL}.plist"
log "渲染 LaunchAgent: ${AGENT_PLIST} 与 ${TUNNEL_PLIST}"
render "${SCRIPT_DIR}/com.ydisks.local-fetch-agent.plist.tmpl" "${AGENT_PLIST}"
render "${SCRIPT_DIR}/com.ydisks.local-fetch-tunnel.plist.tmpl" "${TUNNEL_PLIST}"

# 校验 plist 语法，避免加载时才报错。
plutil -lint "${AGENT_PLIST}" >/dev/null || fail "助手 plist 语法错误"
plutil -lint "${TUNNEL_PLIST}" >/dev/null || fail "隧道 plist 语法错误"

# 加载服务：先卸载同名旧服务，再用现代 bootstrap 加载并显式启用。
for label in "${AGENT_LABEL}" "${TUNNEL_LABEL}"; do
    launchctl bootout "${DOMAIN}/${label}" >/dev/null 2>&1 || true
done
log "加载服务到 ${DOMAIN}"
launchctl bootstrap "${DOMAIN}" "${AGENT_PLIST}"
launchctl bootstrap "${DOMAIN}" "${TUNNEL_PLIST}"
launchctl enable "${DOMAIN}/${AGENT_LABEL}"
launchctl enable "${DOMAIN}/${TUNNEL_LABEL}"

# 等待助手健康检查：最多 20 秒，每 1 秒探测一次回环健康端点。
log "等待助手健康检查..."
# HEALTH_OK 表示助手是否在超时前就绪。
HEALTH_OK=0
for _ in $(seq 1 20); do
    if curl -fsS --max-time 2 "http://127.0.0.1:${REMOTE_PORT}/health" >/dev/null 2>&1; then
        HEALTH_OK=1
        break
    fi
    sleep 1
done
if [ "${HEALTH_OK}" -ne 1 ]; then
    log "助手未在 20 秒内就绪，请查看日志：${LOG_DIR}/agent.err.log"
    log "常见原因：${REMOTE_PORT} 端口被占用，或专用浏览器 profile 被其他进程锁定。"
    exit 1
fi

# 等待隧道在服务器侧生效：服务器回环端口出现监听即视为成功。
log "等待隧道在服务器侧生效（最多 15 秒）..."
# TUNNEL_OK 表示服务器是否已监听反向端口。
TUNNEL_OK=0
for _ in $(seq 1 5); do
    if ssh -o BatchMode=yes -o ConnectTimeout=5 -i "${SSH_KEY}" -p "${SERVER_PORT}" "root@${SERVER_HOST}" \
        "ss -lnt | grep -q '127.0.0.1:${REMOTE_PORT}'" >/dev/null 2>&1; then
        TUNNEL_OK=1
        break
    fi
    sleep 3
done

log "安装完成"
log "  助手: ${BIN_PATH}（监听 127.0.0.1:${REMOTE_PORT}）"
log "  令牌: ${TOKEN_PATH}"
log "  日志: ${LOG_DIR}"
log "  服务: ${DOMAIN}/${AGENT_LABEL}、${DOMAIN}/${TUNNEL_LABEL}"
if [ "${TUNNEL_OK}" -eq 1 ]; then
    log "  隧道: 已在服务器 ${SERVER_HOST}:${SERVER_PORT} 的 127.0.0.1:${REMOTE_PORT} 生效"
else
    log "  隧道: 尚未确认生效，请手动检查（见 ${LOG_DIR}/tunnel.err.log）"
fi
log "注意：服务器 .env 的 LOCAL_FETCH_AGENT_TOKEN 必须与 ${TOKEN_PATH} 一致。"
