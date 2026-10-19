#!/usr/bin/env bash
set -euo pipefail

PANEL=miniray.service
CORE=miniray-core.service
AGENT=miniray-agent.service
ROOT="${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}"
LANG_FILE="${MINIRAY_LANGUAGE_FILE:-/etc/miniray/menu-language}"
CORE_STOP_FILE="${MINIRAY_CORE_STOP_FILE:-/run/miniray-core.manual-stop}"

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
  local version rule
  version=$("$ROOT/miniray" -v)
  if [[ "$version" != v* && "$version" != dev+* ]]; then version="v$version"; fi
  color '1;36' "  M I N I R A Y   $version"
  printf '\n'
  printf -v rule '%*s' "$((MENU_WIDTH + 2))" ''
  color '36' "  ${rule// /─}"
  printf '\n'
  printf '  %s  %s   %s %s\n' "$(tr_text 'Web面板' 'Web panel')" "$(state_text "$PANEL")" "$(tr_text '开机自启:' 'Autostart:')" "$(boot_text "$PANEL")"
  printf '  Xray v%s  %s   %s %s\n' "$(core_version)" "$(state_text "$CORE")" "$(tr_text '开机自启:' 'Autostart:')" "$(boot_text "$CORE")"
}

MENU_WIDTH=54

menu_text_width() {
  local LC_ALL=C value="$1" byte i width=0
  for ((i=0; i<${#value}; i++)); do
    printf -v byte '%d' "'${value:i:1}"
    if ((byte < 128 || (byte >= 192 && byte <= 223))); then
      ((width += 1))
    elif ((byte >= 224 && byte <= 244)); then
      ((width += 2))
    fi
  done
  printf '%d' "$width"
}

menu_item() {
  local content spaces
  printf -v content '  %2s   %s' "$1" "$2"
  printf '  │%s' "$content"
  if [[ -t 1 && "${TERM:-dumb}" != dumb ]]; then
    printf '\033[%dG│\n' "$((MENU_WIDTH + 4))"
  else
    spaces=$((MENU_WIDTH - $(menu_text_width "$content")))
    printf '%*s│\n' "$spaces" ''
  fi
}

menu_group() {
  local title="$1" spaces fill
  printf '\n'
  if [[ -t 1 && "${TERM:-dumb}" != dumb ]]; then
    printf -v fill '%*s' "$MENU_WIDTH" ''
    color '36' "  ┌${fill// /─}┐"
    printf '\033[5G'
    color '36' " $title "
    printf '\n'
  else
    spaces=$((MENU_WIDTH - 3 - $(menu_text_width "$title")))
    printf -v fill '%*s' "$spaces" ''
    color '36' "  ┌─ $title ${fill// /─}┐"
    printf '\n'
  fi
}

menu_end_group() {
  local fill
  printf -v fill '%*s' "$MENU_WIDTH" ''
  color '36' "  └${fill// /─}┘"
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
  menu_item 5 "$(tr_text '修改 Web Path' 'Change web path')"
  menu_item 6 "$(tr_text '重置用户名和密码' 'Reset username and password')"
  menu_item 7 "$(tr_text '查看 Web 面板信息' 'Show web panel information')"
  menu_end_group

  menu_group "$(tr_text 'Xray 核心' 'Xray core')"
  if running "$CORE"; then menu_item 8 "$(tr_text '停止代理核心' 'Stop proxy core')"; else menu_item 8 "$(tr_text '启动代理核心' 'Start proxy core')"; fi
  if enabled "$CORE"; then menu_item 9 "$(tr_text '禁用核心开机自启' 'Disable core autostart')"; else menu_item 9 "$(tr_text '启用核心开机自启' 'Enable core autostart')"; fi
  menu_item 10 "$(tr_text '重启代理核心' 'Restart proxy core')"
  menu_end_group

  menu_group "$(tr_text '系统工具' 'System tools')"
  menu_item 11 "$(tr_text '查看日志' 'View logs')"
  if [[ "$MENU_LANG" == zh ]]; then menu_item 12 'English'; else menu_item 12 '中文'; fi
  menu_item 13 "$(tr_text '更新 Miniray' 'Update Miniray')"
  menu_item 14 "$(tr_text '完整卸载 Miniray' 'Uninstall Miniray')"
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

change_path() {
  local value
  read -r -p "$(tr_text '新的 Web Path (例如 /panel/): ' 'New web path (e.g. /panel/): ')" value || return
  if [[ -z "$value" || "$value" =~ [[:space:]?#] ]]; then
    printf '%s\n' "$(tr_text '请输入有效的 Web Path。' 'Enter a valid web path.')"
    return 1
  fi
  [[ "$value" == /* ]] || value="/$value"
  [[ "$value" == */ ]] || value="$value/"
  "$ROOT/miniray" setting -webBasePath "$value" || return
  if running "$PANEL"; then svc restart "$PANEL" || return; fi
  web_info
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

database_folder() {
  local folder="${MINIRAY_DB_FOLDER:-}"
  local saved
  if [[ -z "$folder" && -f /etc/default/miniray ]]; then
    saved=$(sed -n 's/^MINIRAY_DB_FOLDER=//p' /etc/default/miniray | tail -n 1)
    folder="${saved%\"}"
    folder="${folder#\"}"
  fi
  if [[ -z "$folder" && -f /etc/conf.d/miniray ]]; then
    saved=$(sed -n 's/^MINIRAY_DB_FOLDER=//p' /etc/conf.d/miniray | tail -n 1)
    folder="${saved%\"}"
    folder="${folder#\"}"
  fi
  printf '%s' "${folder:-/etc/miniray}"
}

UNINSTALLED=0
uninstall_miniray() {
  local reply remove_data=0 folder unit suffix
  read -r -p "$(tr_text '确定卸载 Miniray？[y/N]: ' 'Uninstall Miniray? [y/N]: ')" reply || return 1
  if [[ "${reply,,}" != y && "${reply,,}" != yes ]]; then
    printf '%s\n' "$(tr_text '已取消卸载。' 'Uninstallation cancelled.')"
    return 0
  fi
  read -r -p "$(tr_text '同时清除 Miniray 数据库？[y/N]: ' 'Delete the Miniray database too? [y/N]: ')" reply || return 1
  if [[ "${reply,,}" == y || "${reply,,}" == yes ]]; then remove_data=1; fi
  folder=$(database_folder)
  if [[ "$ROOT" != /* || "$ROOT" == / || ! -f "$ROOT/miniray.sh" || "$folder" != /* || "$folder" == / ]]; then
    printf '%s\n' "$(tr_text '安装目录或数据库目录无效，已取消卸载。' 'Invalid installation or database directory; uninstallation aborted.')" >&2
    return 1
  fi

  for unit in "$PANEL" "$AGENT" "$CORE"; do
    if running "$unit"; then svc stop "$unit" || return 1; fi
    if enabled "$unit"; then toggle del "$unit" || return 1; fi
  done
  if [[ "$MANAGER" == openrc ]]; then
    rm -f /etc/init.d/miniray /etc/init.d/miniray-agent /etc/init.d/miniray-core
  else
    rm -f /etc/systemd/system/miniray.service /etc/systemd/system/miniray-agent.service /etc/systemd/system/miniray-core.service
    systemctl daemon-reload
    systemctl reset-failed miniray.service miniray-agent.service miniray-core.service >/dev/null 2>&1 || :
  fi

  rm -f /usr/local/bin/miniray /usr/local/bin/mr
  rm -rf -- "$ROOT" /var/log/miniray
  rm -f /run/miniray.pid /run/miniray-agent.pid /run/miniray-core.pid "$CORE_STOP_FILE"
  rm -f /etc/conf.d/miniray /etc/default/miniray

  if (( remove_data == 1 )); then
    if [[ "$folder" == /etc/miniray ]]; then
      rm -rf -- "$folder"
    else
      for suffix in '' -wal -shm -journal; do rm -f -- "$folder/miniray.db$suffix"; done
      rm -f -- "$folder/initial-admin-password" "$folder/initial-admin-credentials" "$folder/menu-language" "$folder/update-status.json"
      rm -rf -- "$folder/imported-legacy-files"
      rmdir "$folder" 2>/dev/null || :
    fi
    printf '%s\n' "$(tr_text 'Miniray 已卸载，数据库已清除。' 'Miniray uninstalled; database deleted.')"
  else
    rm -f -- "$folder/menu-language" "$folder/update-status.json"
    if [[ "$folder" != /etc/miniray ]]; then
      install -d -m 0755 /etc/default
      printf 'MINIRAY_DB_FOLDER="%s"\n' "$folder" > /etc/default/miniray
      chmod 0600 /etc/default/miniray
    fi
    printf '%s\n' "$(tr_text 'Miniray 已卸载，数据库保留在：' 'Miniray uninstalled; database kept at:') $folder/miniray.db"
  fi
  UNINSTALLED=1
}

action() {
  case "$1" in
    start-panel) svc start "$PANEL";;
    stop-panel) svc stop "$PANEL";;
    restart-panel) svc restart "$PANEL";;
    start-core)
      rm -f "$CORE_STOP_FILE"
      if ! running "$CORE"; then svc start "$CORE" || return; fi
      if ! running "$AGENT"; then svc start "$AGENT" || return; fi;;
    stop-core)
      printf '1\n' > "$CORE_STOP_FILE"
      local failed=0
      if running "$AGENT"; then svc stop "$AGENT" || failed=1; fi
      if running "$CORE"; then svc stop "$CORE" || failed=1; fi
      (( failed == 0 ));;
    restart-core)
      rm -f "$CORE_STOP_FILE"
      if running "$CORE"; then svc restart "$CORE" || return; else svc start "$CORE" || return; fi
      if running "$AGENT"; then svc restart "$AGENT"; else svc start "$AGENT"; fi;;
    start) action start-core || return; if ! running "$PANEL"; then svc start "$PANEL"; fi;;
    stop) if running "$PANEL"; then svc stop "$PANEL" || return; fi; action stop-core;;
    restart) action restart-core || return; if running "$PANEL"; then svc restart "$PANEL"; else svc start "$PANEL"; fi;;
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
    change-path) change_path;;
    reset-credentials) reset_credentials;;
    settings) "$ROOT/miniray" setting -show;;
    reset-settings) "$ROOT/miniray" setting -reset;;
    version) "$ROOT/miniray" -v;;
    update|install) MINIRAY_MENU_LANG="$MENU_LANG" bash "$ROOT/update.sh";;
    uninstall) uninstall_miniray;;
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
  miniray web-info|change-port|change-path|reset-credentials
  miniray logs|panel-logs|core-logs
  miniray update
  miniray uninstall
HELP
}

case "${1:-}" in
  -h|--help|help) usage; exit 0;;
esac
(( EUID == 0 )) || { echo 'Run as root' >&2; exit 1; }
if [[ "$MANAGER" == openrc ]]; then command -v rc-service >/dev/null 2>&1; fi
MENU_LANG=$(language)
case "${1:-}" in
  '') ;;
  *) action "$1" || { usage; exit 2; }; exit;;
esac

while true; do
  render_menu
  read -r -p "$(tr_text '选择功能 [0-14]: ' 'Select an option [0-14]: ')" choice || break
  if ! case "$choice" in
    1) if running "$PANEL"; then action stop-panel; else action start-panel; fi;;
    2) if enabled "$PANEL"; then action disable-panel; else action enable-panel; fi;;
    3) action restart-panel;;
    4) change_port;;
    5) change_path;;
    6) reset_credentials;;
    7) web_info;;
    8) if running "$CORE"; then action stop-core; else action start-core; fi;;
    9) if enabled "$CORE"; then action disable-core; else action enable-core; fi;;
    10) action restart-core;;
    11) action logs;;
    12) if [[ "$MENU_LANG" == zh ]]; then set_language en; else set_language zh; fi;;
    13) action update;;
    14) uninstall_miniray; if (( UNINSTALLED == 1 )); then exit 0; fi;;
    0) exit 0;;
    *) printf '%s\n' "$(tr_text '无效选项。' 'Invalid option.')";;
  esac; then
    printf '%s\n' "$(tr_text '操作失败。' 'Operation failed.')"
  fi
  if [[ "$choice" != 12 ]]; then
    read -r -p "$(tr_text '按回车键返回菜单...' 'Press Enter to return...')" _ || break
  fi
done
