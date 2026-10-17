#!/usr/bin/env bash
set -euo pipefail
PANEL=miniray.service
CORE=miniray-core.service
AGENT=miniray-agent.service
ROOT=/usr/local/miniray

require_root() {
  if (( EUID != 0 )); then echo 'Run as root' >&2; exit 1; fi
}
if command -v systemctl >/dev/null && [[ -d /run/systemd/system ]]; then
  MANAGER=systemd
else
  MANAGER=openrc
fi
svc() {
  local command="$1" unit="$2"
  if [[ "$MANAGER" == openrc ]]; then rc-service "${unit%.service}" "$command"; else systemctl "$command" "$unit"; fi
}
enabled() {
  if [[ "$MANAGER" == openrc ]]; then rc-update show default | grep -qE "^[[:space:]]*${1%.service}[[:space:]]"; else systemctl is-enabled --quiet "$1"; fi
}
toggle() {
  if [[ "$MANAGER" == openrc ]]; then
    rc-update "$1" "${2%.service}" default
  else
    systemctl "$([[ "$1" == add ]] && echo enable || echo disable)" "$2"
  fi
}
show_status() {
  for unit in "$PANEL" "$CORE" "$AGENT"; do
    printf '%s: running=%s boot=%s\n' "$unit" "$(svc status "$unit" >/dev/null 2>&1 && echo active || echo inactive)" "$(enabled "$unit" && echo enabled || echo disabled)"
  done
}
action() {
  case "$1" in
    start-panel) svc start "$PANEL";;
    stop-panel) svc stop "$PANEL";;
    restart-panel) svc restart "$PANEL";;
    start-core) svc start "$CORE"; svc start "$AGENT";;
    stop-core) svc stop "$AGENT"; svc stop "$CORE";;
    restart-core) svc restart "$CORE"; svc restart "$AGENT";;
    start) svc start "$CORE"; svc start "$AGENT"; svc start "$PANEL";;
    stop) svc stop "$PANEL"; svc stop "$AGENT"; svc stop "$CORE";;
    restart) svc restart "$CORE"; svc restart "$AGENT"; svc restart "$PANEL";;
    status) show_status;;
    enable-panel|enable) toggle add "$PANEL";;
    disable-panel|disable) toggle del "$PANEL";;
    enable-core) toggle add "$CORE"; toggle add "$AGENT";;
    disable-core) toggle del "$CORE"; toggle del "$AGENT";;
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
    initial-password) "$ROOT/miniray" initial-password;;
    settings) "$ROOT/miniray" setting -show;;
    reset-settings) "$ROOT/miniray" setting -reset;;
    version) "$ROOT/miniray" -v;;
    update|install) bash "$ROOT/update.sh";;
    *) return 2;;
  esac
}
usage() {
  cat <<'EOF'
Miniray independent panel and proxy services
  miniray start-panel|stop-panel|restart-panel  Control web panel only
  miniray enable-panel|disable-panel            Panel boot autostart only
  miniray start-core|stop-core|restart-core     Control Xray and protocol agent
  miniray enable-core|disable-core              Proxy boot autostart only
  miniray start|stop|restart                    Control all services explicitly
  miniray status                                Service and boot states
  miniray logs|panel-logs|core-logs             View logs
  miniray initial-password                      Read one-time initial administrator password
  miniray update                                Update installed version
EOF
}
require_root
if [[ "$MANAGER" == openrc ]]; then command -v rc-service >/dev/null || exit 1; fi
case "${1:-}" in
  -h|--help|help) usage; exit 0;;
  '') ;;
  *) action "$1" || { usage; exit 2; }; exit;;
esac

while true; do
  echo 'Miniray independent services:'
  echo '1) Start panel          2) Stop panel'
  echo '3) Restart panel        4) Status'
  echo '5) Enable panel boot    6) Disable panel boot'
  echo '7) Start proxy services 8) Stop proxy services'
  echo '9) Enable proxy boot   10) Disable proxy boot'
  echo '11) Logs               12) Update'
  echo '0) Exit'
  read -rp 'Select: ' choice
  case "$choice" in
    1) action start-panel;; 2) action stop-panel;; 3) action restart-panel;;
    4) action status;; 5) action enable-panel;; 6) action disable-panel;;
    7) action start-core;; 8) action stop-core;;
    9) action enable-core;; 10) action disable-core;;
    11) action logs;; 12) action update;;
    0) break;; *) echo 'Invalid selection';;
  esac
done
