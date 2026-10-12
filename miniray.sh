#!/usr/bin/env bash
set -euo pipefail
PANEL=miniray.service
CORE=miniray-core.service
ROOT=/usr/local/miniray
root(){ if ((EUID!=0));then echo 'Run as root' >&2;exit 1;fi; }
action(){
 case "$1" in
  start-panel) systemctl start "$PANEL";;
  stop-panel) systemctl stop "$PANEL";;
  restart-panel) systemctl restart "$PANEL";;
  start-core) systemctl start "$CORE";;
  stop-core) systemctl stop "$CORE";;
  restart-core) systemctl restart "$CORE";;
  start) systemctl start "$CORE" || true;systemctl start "$PANEL";;
  stop) systemctl stop "$PANEL";systemctl stop "$CORE";;
  restart) systemctl restart "$CORE";systemctl restart "$PANEL";;
  status) echo 'Panel:';systemctl status --no-pager "$PANEL" || true; echo 'Proxy core:';systemctl status --no-pager "$CORE" || true;;
  enable) systemctl enable "$PANEL" "$CORE";;
  disable) systemctl disable "$PANEL" "$CORE";;
  logs) journalctl -u "$PANEL" -u "$CORE" --no-pager -n 100;;
  panel-logs) journalctl -u "$PANEL" --no-pager -n 100;;
  core-logs) journalctl -u "$CORE" --no-pager -n 100;;
  settings) "$ROOT/miniray" setting -show;;
  reset-settings) "$ROOT/miniray" setting -reset;;
  version) "$ROOT/miniray" -v;;
  update|install) bash "$ROOT/install.sh";;
  *) return 2;;
 esac
}
usage(){
 cat <<'EOF'
Miniray panel and independent Xray manager
  miniray                  Interactive menu
  miniray start-panel      Start only web panel
  miniray stop-panel       Stop only panel, keep proxy core running
  miniray restart-panel    Restart panel, preserve core
  miniray start-core       Start proxy core
  miniray stop-core        Stop proxy core
  miniray restart-core     Restart proxy core
  miniray start|stop       Start/stop both services
  miniray status           Separate core/panel status
  miniray enable|disable   Change boot enablement
  miniray logs             Recent logs
  miniray version          Panel version
  miniray update           Update from Miniray releases
EOF
}
root
if ! command -v systemctl >/dev/null;then echo 'systemd is required' >&2;exit 1;fi
case "${1:-}" in
  -h|--help|help) usage;exit 0;;
  '') ;;
  *) action "$1" || {usage;exit 2;};exit;;
esac
while true;do
 echo 'Miniray panel and core:'
 echo '1) Start panel         2) Stop panel (retain proxy)'
 echo '3) Restart panel       4) Status'
 echo '5) Start all           6) Stop all'
 echo '7) Restart core        8) Logs'
 echo '9) Enable at boot     10) Disable at boot'
 echo '11) Update             0) Exit'
 read -rp 'Select: ' choice
 case "$choice" in
  1) action start-panel;;2) action stop-panel;;3) action restart-panel;;
  4) action status;;5) action start;;6) action stop;;7) action restart-core;;
  8) action logs;;9) action enable;;10) action disable;;11) action update;;
  0) break;;*) echo 'Invalid selection';;
 esac
done
