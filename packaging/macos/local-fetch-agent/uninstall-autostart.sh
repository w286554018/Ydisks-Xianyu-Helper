#!/bin/bash
# 卸载本机抓取助手与隧道自启服务。
#
# 默认只停止并移除两个 LaunchAgent（保留助手二进制、令牌与日志，便于重新安装）。
# 加 -p 时连同助手目录一并删除（会删除共享令牌与日志，需自行确认服务器侧不再使用）。
set -euo pipefail

# AGENT_LABEL 与 TUNNEL_LABEL 是要卸载的服务标识。
AGENT_LABEL="com.ydisks.local-fetch-agent"
TUNNEL_LABEL="com.ydisks.local-fetch-tunnel"
# LAUNCH_AGENTS_DIR 是当前用户的 LaunchAgent 目录。
LAUNCH_AGENTS_DIR="${HOME}/Library/LaunchAgents"
# FETCH_DIR 保存助手二进制、令牌与日志。
FETCH_DIR="${HOME}/.ydisks-fetch"
# PURGE 表示是否同时删除助手目录。
PURGE=0

# log 输出带前缀的进度信息。
log() {
    printf '[autostart] %s\n' "$*"
}

# 解析参数。
while getopts ":ph" opt; do
    case "${opt}" in
        p) PURGE=1 ;;
        h) printf '用法: uninstall-autostart.sh [-p]\n  -p  同时删除 %s（令牌、日志与二进制）\n' "${FETCH_DIR}"; exit 0 ;;
        *) printf '未知选项 -%s\n' "${OPTARG}" >&2; exit 1 ;;
    esac
done

# DOMAIN 是当前用户的 launchd 域。
DOMAIN="gui/$(id -u)"
# 先卸载服务，再删除 plist，避免 launchd 继续按已删除的配置拉起进程。
for label in "${AGENT_LABEL}" "${TUNNEL_LABEL}"; do
    if launchctl print "${DOMAIN}/${label}" >/dev/null 2>&1; then
        log "卸载服务 ${label}"
        launchctl bootout "${DOMAIN}/${label}" >/dev/null 2>&1 || true
    else
        log "服务未加载，跳过 ${label}"
    fi
    rm -f "${LAUNCH_AGENTS_DIR}/${label}.plist"
done

if [ "${PURGE}" -eq 1 ]; then
    log "删除助手目录 ${FETCH_DIR}"
    rm -rf "${FETCH_DIR}"
fi

log "卸载完成"
[ "${PURGE}" -eq 0 ] && log "已保留助手二进制/令牌/日志：${FETCH_DIR}（如需清除请加 -p）"
