#!/usr/bin/env bash
set -euo pipefail
PANEL=miniray.service
CORE=miniray-core.service
AGENT=miniray-agent.service
ROOT=/usr/local/miniray

require_root() {
  if (( EUID != 0 )); then echo 'Run as root' >&2; exit 1; fi
}
show_status() {
  for unit in "$PANEL" "$CORE" "$AGENT"; do
    printf '%s: running=%s boot=%s\n' "$unit" \
      "$(systemctl is-active "$unit" 2>/dev/null || :)" \
      "$(systemctl is-enabled "$unit" 2>/dev/null || :)"
  done
}
action() {
  case "$1" in
    start-panel) systemctl start "$PANEL";;
    stop-panel) systemctl stop "$PANEL";;
    restart-panel) systemctl restart "$PANEL";;
    start-core) systemctl start "$CORE" "$AGENT";;
    stop-core) systemctl stop "$AGENT" "$CORE";;
    restart-core) systemctl restart "$CORE" "$AGENT";;
    start) systemctl start "$CORE" "$AGENT" "$PANEL";;
    stop) systemctl stop "$PANEL" "$AGENT" "$CORE";;
    restart) systemctl restart "$CORE" "$AGENT" "$PANEL";;
    status) show_status;;
    enable-panel|enable) systemctl enable "$PANEL";;
    disable-panel|disable) systemctl disable "$PANEL";;
    enable-core) systemctl enable "$CORE" "$AGENT";;
    disable-core) systemctl disable "$CORE" "$AGENT";;
    logs) journalctl -u "$PANEL" -u "$CORE" -u "$AGENT" --no-pager -n 100;;
    panel-logs) journalctl -u "$PANEL" --no-pager -n 100;;
    core-logs) journalctl -u "$CORE" -u "$AGENT" --no-pager -n 100;;
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
command -v systemctl >/dev/null || { echo 'systemd is required' >&2; exit 1; }
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
