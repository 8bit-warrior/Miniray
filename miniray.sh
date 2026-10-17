#!/usr/bin/env bash
set -euo pipefail

PANEL=miniray.service
CORE=miniray-core.service
AGENT=miniray-agent.service
ROOT="${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}"
LANG_FILE="${MINIRAY_LANGUAGE_FILE:-/etc/miniray/menu-language}"

if [[ "${MINIRAY_SERVICE_MANAGER:-}" == systemd || "${MINIRAY_SERVICE_MANAGER:-}" == openrc ]]; then
  MANAGER="$MINIRAY_SERVICE_MANAGER"
elif command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
  MANAGER=systemd
else
  MANAGER=openrc
fi

svc() {
  local operation="$1" unit="$2"
  if [[ "$MANAGER" == openrc ]]; then
    rc-service "${unit%.service}" "$operation"
  else
    systemctl "$operation" "$unit"
  fi
}

enabled() {
  if [[ "$MANAGER" == openrc ]]; then
    rc-update show default | grep -qE "^[[:space:]]*${1%.service}[[:space:]]"
  else
    systemctl is-enabled --quiet "$1"
  fi
}

toggle() {
  local operation="$1" unit="$2"
  if [[ "$MANAGER" == openrc ]]; then
    rc-update "$operation" "${unit%.service}" default
  elif [[ "$operation" == add ]]; then
    systemctl enable "$unit"
  else
    systemctl disable "$unit"
  fi
}

language() {
  local current=zh
  [[ ! -f "$LANG_FILE" ]] || read -r current < "$LANG_FILE"
  [[ "$current" == en ]] && printf 'en' || printf 'zh'
}

tr_text() {
  if [[ "$MENU_LANG" == en ]]; then printf '%s' "$2"; else printf '%s' "$1"; fi
}

set_language() {
  local next="$1"
  install -d -m 0700 "$(dirname "$LANG_FILE")"
  printf '%s\n' "$next" > "$LANG_FILE"
  chmod 0600 "$LANG_FILE"
  MENU_LANG="$next"
}

color() {
  local code="$1" text="$2"
  if [[ -t 1 && "${TERM:-dumb}" != dumb && -z "${NO_COLOR:-}" ]]; then
    printf '\033[%sm%s\033[0m' "$code" "$text"
  else
    printf '%s' "$text"
  fi
}

running() { svc status "$1" >/dev/null 2>&1; }

state_text() {
  if running "$1"; then
    color 32 "$(tr_text '运行中' 'Running')"
  else
    color 31 "$(tr_text '已停止' 'Stopped')"
  fi
}

boot_text() {
  if enabled "$1"; then
    color 32 "$(tr_text '已启用' 'Enabled')"
  else
    color 33 "$(tr_text '已禁用' 'Disabled')"
  fi
}

core_version() {
  local output first
  [[ -x "$ROOT/bin/xray" ]] || { printf '%s' '--'; return; }
  output=$("$ROOT/bin/xray" version 2>/dev/null) || { printf '%s' '--'; return; }
  first=${output%%$'\n'*}
  if [[ "$first" =~ ^Xray[[:space:]]+([^[:space:]]+) ]]; then
    printf '%s' "${BASH_REMATCH[1]}"
  else
    printf '%s' '--'
  fi
}

status_header() {
  printf '\n'
  color '1;36' '  ╭──────────────────────────────────────────────────╮'
  printf '\n'
  color '1;36' '  │                 M I N I R A Y                   │'
  printf '\n'
  color '1;36' '  ╰──────────────────────────────────────────────────╯'
  printf '\n'
  printf '  %s  %s   %s %s\n' "$(tr_text '面板' 'Panel')" "$(state_text "$PANEL")" "$(tr_text '开机自启:' 'Autostart:')" "$(boot_text "$PANEL")"
  printf '  Xray v%s  %s   %s %s\n' "$(core_version)" "$(state_text "$CORE")" "$(tr_text '开机自启:' 'Autostart:')" "$(boot_text "$CORE")"
}

menu_item() {
  printf '  │  %2s   %s\n' "$1" "$2"
}

menu_group() {
  printf '\n'
  color '36' "  ┌─ $1 ─────────────────────────────────────────────"
  printf '\n'
}

menu_end_group() {
  color '36' '  └──────────────────────────────────────────────────'
  printf '\n'
}

render_menu() {
  [[ ! -t 1 || "${TERM:-dumb}" == dumb ]] || printf '\033[H\033[2J'
  status_header

  menu_group "$(tr_text '面板管理' 'Web panel')"
  if running "$PANEL"; then menu_item 1 "$(tr_text '停止面板' 'Stop panel')"; else menu_item 1 "$(tr_text '启动面板' 'Start panel')"; fi
  if enabled "$PANEL"; then menu_item 2 "$(tr_text '禁用面板开机自启' 'Disable panel autostart')"; else menu_item 2 "$(tr_text '启用面板开机自启' 'Enable panel autostart')"; fi
  menu_item 3 "$(tr_text '重启面板' 'Restart panel')"
  menu_item 4 "$(tr_text '修改 Web 端口' 'Change web port')"
  menu_item 5 "$(tr_text '重置用户名和密码' 'Reset username and password')"
  menu_item 6 "$(tr_text '查看 Web 面板信息' 'Show web panel information')"
  menu_end_group

  menu_group "$(tr_text 'Xray 核心' 'Xray core')"
  if running "$CORE"; then menu_item 7 "$(tr_text '停止代理核心' 'Stop proxy core')"; else menu_item 7 "$(tr_text '启动代理核心' 'Start proxy core')"; fi
  if enabled "$CORE"; then menu_item 8 "$(tr_text '禁用核心开机自启' 'Disable core autostart')"; else menu_item 8 "$(tr_text '启用核心开机自启' 'Enable core autostart')"; fi
  menu_item 9 "$(tr_text '重启代理核心' 'Restart proxy core')"
  menu_end_group

  menu_group "$(tr_text '系统工具' 'System tools')"
  menu_item 10 "$(tr_text '查看日志' 'View logs')"
  menu_item 11 "$(tr_text '更新 Miniray' 'Update Miniray')"
  if [[ "$MENU_LANG" == zh ]]; then menu_item 12 '切换至 English'; else menu_item 12 'Switch to 中文'; fi
  menu_item 0 "$(tr_text '退出' 'Exit')"
  menu_end_group
  printf '\n'
}

panel_ip() {
  local value=''
  if command -v ip >/dev/null 2>&1; then
    value=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')
  fi
  if [[ -z "$value" ]] && command -v hostname >/dev/null 2>&1; then
    value=$(hostname -I 2>/dev/null | awk '{print $1}')
  fi
  printf '%s' "${value:-127.0.0.1}"
}

web_info() {
  local output url username
  output=$("$ROOT/miniray" panel-info "$(panel_ip)") || return
  url=${output%%$'\n'*}
  username=${output#*$'\n'}
  printf '%s\n' "$url"
  printf '%s: %s\n' "$(tr_text '用户名' 'Username')" "$username"
}

change_port() {
  local value
  read -r -p "$(tr_text '新的 Web 端口 (1-65535): ' 'New web port (1-65535): ')" value || return
  if [[ ! "$value" =~ ^[1-9][0-9]{0,4}$ ]] || (( 10#$value > 65535 )); then
    printf '%s\n' "$(tr_text '端口必须在 1 至 65535 之间。' 'Port must be between 1 and 65535.')"
    return 1
  fi
  "$ROOT/miniray" setting -port "$value" || return
  if running "$PANEL"; then svc restart "$PANEL" || return; fi
}

reset_credentials() {
  local username password confirm
  read -r -p "$(tr_text '新用户名: ' 'New username: ')" username || return
  read -r -s -p "$(tr_text '新密码: ' 'New password: ')" password || return
  printf '\n'
  read -r -s -p "$(tr_text '再次输入密码: ' 'Confirm password: ')" confirm || return
  printf '\n'
  if [[ -z "$username" || -z "$password" || "$password" != "$confirm" ]]; then
    printf '%s\n' "$(tr_text '用户名或密码无效，或两次密码不一致。' 'Invalid credentials or passwords do not match.')"
    return 1
  fi
  printf '%s\n%s\n' "$username" "$password" | "$ROOT/miniray" reset-credentials || return
}

action() {
  case "$1" in
    start-panel) svc start "$PANEL";;
    stop-panel) svc stop "$PANEL";;
    restart-panel) svc restart "$PANEL";;
    start-core) svc start "$CORE" && svc start "$AGENT";;
    stop-core) svc stop "$AGENT" && svc stop "$CORE";;
    restart-core) svc restart "$CORE" && svc restart "$AGENT";;
    start) svc start "$CORE"; svc start "$AGENT"; svc start "$PANEL";;
    stop) svc stop "$PANEL"; svc stop "$AGENT"; svc stop "$CORE";;
    restart) svc restart "$CORE"; svc restart "$AGENT"; svc restart "$PANEL";;
    status) printf 'Panel: '; svc status "$PANEL"; printf 'Xray: '; svc status "$CORE";;
    enable-panel|enable) toggle add "$PANEL";;
    disable-panel|disable) toggle del "$PANEL";;
    enable-core) toggle add "$CORE" && toggle add "$AGENT";;
    disable-core) toggle del "$CORE" && toggle del "$AGENT";;
    logs|panel-logs|core-logs)
      if [[ "$MANAGER" == openrc ]]; then
        case "$1" in
          panel-logs) tail -n 100 /var/log/miniray/panel.log;;
          core-logs) tail -n 100 /var/log/miniray/core.log /var/log/miniray/agent.log;;
          *) tail -n 100 /var/log/miniray/panel.log /var/log/miniray/core.log /var/log/miniray/agent.log;;
        esac
      else
        case "$1" in
          panel-logs) journalctl -u "$PANEL" --no-pager -n 100;;
          core-logs) journalctl -u "$CORE" -u "$AGENT" --no-pager -n 100;;
          *) journalctl -u "$PANEL" -u "$CORE" -u "$AGENT" --no-pager -n 100;;
        esac
      fi;;
    web-info) web_info;;
    change-port) change_port;;
    reset-credentials) reset_credentials;;
    initial-password) "$ROOT/miniray" initial-password;;
    settings) "$ROOT/miniray" setting -show;;
    reset-settings) "$ROOT/miniray" setting -reset;;
    version) "$ROOT/miniray" -v;;
    update|install) bash "$ROOT/update.sh";;
    *) return 2;;
  esac
}

usage() {
  cat <<'HELP'
Miniray / mr
  miniray                            Open the interactive menu
  miniray start-panel|stop-panel|restart-panel
  miniray enable-panel|disable-panel
  miniray start-core|stop-core|restart-core
  miniray enable-core|disable-core
  miniray web-info|change-port|reset-credentials
  miniray logs|panel-logs|core-logs
  miniray update
HELP
}

case "${1:-}" in
  -h|--help|help) usage; exit 0;;
esac
(( EUID == 0 )) || { echo 'Run as root' >&2; exit 1; }
if [[ "$MANAGER" == openrc ]]; then command -v rc-service >/dev/null 2>&1; fi
case "${1:-}" in
  '') ;;
  *) action "$1" || { usage; exit 2; }; exit;;
esac

MENU_LANG=$(language)
while true; do
  render_menu
  read -r -p "$(tr_text '选择功能 [0-12]: ' 'Select an option [0-12]: ')" choice || break
  if ! case "$choice" in
    1) if running "$PANEL"; then action stop-panel; else action start-panel; fi;;
    2) if enabled "$PANEL"; then action disable-panel; else action enable-panel; fi;;
    3) action restart-panel;;
    4) change_port;;
    5) reset_credentials;;
    6) web_info;;
    7) if running "$CORE"; then action stop-core; else action start-core; fi;;
    8) if enabled "$CORE"; then action disable-core; else action enable-core; fi;;
    9) action restart-core;;
    10) action logs;;
    11) action update;;
    12) if [[ "$MENU_LANG" == zh ]]; then set_language en; else set_language zh; fi;;
    0) exit 0;;
    *) printf '%s\n' "$(tr_text '无效选项。' 'Invalid option.')";;
  esac; then
    printf '%s\n' "$(tr_text '操作失败。' 'Operation failed.')"
  fi
  if [[ "$choice" != 12 ]]; then
    read -r -p "$(tr_text '按回车键返回菜单...' 'Press Enter to return...')" _ || break
  fi
done
