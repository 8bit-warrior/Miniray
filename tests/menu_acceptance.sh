#!/usr/bin/env bash
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/miniray.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/bin" "$work/root/bin" "$work/state"

cat > "$work/bin/systemctl" <<'MOCK'
#!/usr/bin/env bash
state="$MINIRAY_TEST_STATE"
case "$1" in
  is-active|is-enabled)
    action="$1"; shift
    [[ "${1:-}" != --quiet ]] || shift
    if [[ "$action" == is-active ]]; then test -f "$state/$1.running"; else test -f "$state/$1.enabled"; fi;;
  start|restart) touch "$state/$2.running";;
  stop) rm -f "$state/$2.running";;
  enable) touch "$state/$2.enabled";;
  disable) rm -f "$state/$2.enabled";;
  *) exit 1;;
esac
MOCK

cat > "$work/bin/rc-service" <<'MOCK'
#!/usr/bin/env bash
state="$MINIRAY_TEST_STATE"
case "$2" in
  status) test -f "$state/$1.service.running";;
  start|restart) touch "$state/$1.service.running";;
  stop) rm -f "$state/$1.service.running";;
  *) exit 1;;
esac
MOCK

cat > "$work/bin/rc-update" <<'MOCK'
#!/usr/bin/env bash
state="$MINIRAY_TEST_STATE"
if [[ "$1" == show ]]; then
  for file in "$state"/*.enabled; do
    [[ -e "$file" ]] || continue
    unit="${file##*/}"
    printf '  %s | default\n' "${unit%.service.enabled}"
  done
elif [[ "$1" == add ]]; then
  touch "$state/$2.service.enabled"
elif [[ "$1" == del ]]; then
  rm -f "$state/$2.service.enabled"
fi
MOCK

cat > "$work/bin/ip" <<'MOCK'
#!/usr/bin/env bash
printf '1.1.1.1 via 192.0.2.1 dev eth0 src 192.0.2.42 uid 0\n'
MOCK

cat > "$work/root/miniray" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MINIRAY_TEST_STATE/commands"
case "$1" in
  -v) echo 0.0.2;;
  panel-info) printf 'http://%s:2053/panel/\nadmin\n' "$2";;
  reset-credentials)
    read -r username
    read -r password
    printf 'reset=%s;%s\n' "$username" "$password" >> "$MINIRAY_TEST_STATE/commands";;
  setting) printf 'Port set successfully: 2054\n';;
esac
MOCK

cat > "$work/root/bin/xray" <<'MOCK'
#!/usr/bin/env bash
printf 'Xray 26.6.27 (Xray)\n'
MOCK

chmod +x "$work/bin/"* "$work/root/miniray" "$work/root/bin/xray"
export MINIRAY_TEST_STATE="$work/state" MINIRAY_SERVICE_MANAGER=systemd
export MINIRAY_MAIN_FOLDER="$work/root" MINIRAY_LANGUAGE_FILE="$work/state/lang"
export PATH="$work/bin:$PATH" TERM=dumb

printf '0\n' | bash "$SCRIPT" > "$work/default"
grep -q '面板管理' "$work/default"
grep -q 'Xray 核心' "$work/default"
grep -q '系统工具' "$work/default"
grep -q 'Xray v26.6.27' "$work/default"
grep -q '启动面板' "$work/default"
! grep -q '查看状态' "$work/default"

printf '1\n\n2\n\n12\n6\n\n0\n' | bash "$SCRIPT" > "$work/english"
grep -q 'Switch to 中文' "$work/english"
grep -q 'http://192.0.2.42:2053/panel/' "$work/english"
grep -q 'Username: admin' "$work/english"
test -f "$work/state/miniray.service.running"
test -f "$work/state/miniray.service.enabled"
test "$(cat "$work/state/lang")" = en

printf '4\n70000\n\n4\n2054\n\n5\ntester\nsecret-key\nsecret-key\n\n0\n' | bash "$SCRIPT" > "$work/credentials"
grep -q 'Port must be between' "$work/credentials"
grep -q 'Port set successfully' "$work/credentials"
grep -q 'reset=tester;secret-key' "$work/state/commands"

export MINIRAY_SERVICE_MANAGER=openrc
printf '1\n\n2\n\n7\n\n8\n\n0\n' | bash "$SCRIPT" > "$work/openrc"
test ! -e "$work/state/miniray.service.running"
test ! -e "$work/state/miniray.service.enabled"
test -f "$work/state/miniray-core.service.running"
test -f "$work/state/miniray-agent.service.running"
test -f "$work/state/miniray-core.service.enabled"
test -f "$work/state/miniray-agent.service.enabled"
grep -q 'Start panel' "$work/openrc"
printf 'Menu acceptance: systemd, OpenRC, bilingual, credentials, port and address passed\n'
