#!/usr/bin/env bash
set -euo pipefail

SCRIPT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/miniray.sh"
export MINIRAY_TEST_VERSION
MINIRAY_TEST_VERSION=$(cat "$(dirname "$SCRIPT")/internal/config/version")
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
  status) test -f "$state/$2.running";;
  start|restart) touch "$state/$2.running"; printf '%s %s\n' "$1" "$2" >> "$state/services";;
  stop) rm -f "$state/$2.running"; printf 'stop %s\n' "$2" >> "$state/services";;
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
  stop)
    if [[ ! -f "$state/$1.service.running" ]]; then
      printf '* WARNING: %s is already stopped\n' "$1" >&2
      exit 1
    fi
    rm -f "$state/$1.service.running";;
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

cat > "$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MINIRAY_TEST_STATE/public-ip-calls"
printf '203.0.113.42\n'
MOCK

cat > "$work/root/miniray" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MINIRAY_TEST_STATE/commands"
case "$1" in
  -v) echo "$MINIRAY_TEST_VERSION";;
  panel-info)
    path=/panel/
    [[ ! -f "$MINIRAY_TEST_STATE/path" ]] || read -r path < "$MINIRAY_TEST_STATE/path"
    printf 'http://%s:2053%s\nadmin\n' "$2" "$path";;
  reset-credentials)
    read -r username
    read -r password
    printf 'reset=%s;%s\n' "$username" "$password" >> "$MINIRAY_TEST_STATE/commands";;
  setting)
    if [[ "$2" == -webBasePath ]]; then
      printf '%s\n' "$3" > "$MINIRAY_TEST_STATE/path"
      printf 'Base URI path set successfully\n'
    else
      printf 'Port set successfully: 2054\n'
    fi;;
esac
MOCK

cat > "$work/root/bin/xray" <<'MOCK'
#!/usr/bin/env bash
printf 'Xray 26.6.27 (Xray)\n'
MOCK

chmod +x "$work/bin/"* "$work/root/miniray" "$work/root/bin/xray"
export MINIRAY_TEST_STATE="$work/state" MINIRAY_SERVICE_MANAGER=systemd
export MINIRAY_MAIN_FOLDER="$work/root" MINIRAY_LANGUAGE_FILE="$work/state/lang"
export MINIRAY_CORE_STOP_FILE="$work/state/manual-core-stop"
export PATH="$work/bin:$PATH" TERM=dumb

printf '0\n' | bash "$SCRIPT" > "$work/default"
grep -q '面板管理' "$work/default"
grep -q 'Xray 核心' "$work/default"
grep -q '系统工具' "$work/default"
grep -q 'Xray v26.6.27' "$work/default"
grep -q "M I N I R A Y   v${MINIRAY_TEST_VERSION}" "$work/default"
head -n 1 "$work/default" | grep -q "M I N I R A Y   v${MINIRAY_TEST_VERSION}"
grep -q '完整卸载 Miniray' "$work/default"
grep -q 'Web面板  已停止' "$work/default"
grep -q '修改 Web Path' "$work/default"
grep -qE '12[[:space:]]+English' "$work/default"
! grep -q '切换至 English' "$work/default"
grep -q '启动面板' "$work/default"
! grep -q '查看状态' "$work/default"

printf '1\n\n2\n\n12\n7\n\n0\n' | bash "$SCRIPT" > "$work/english"
grep -qE '12[[:space:]]+中文' "$work/english"
! grep -q 'Switch to 中文' "$work/english"
grep -q 'Web panel  Running' "$work/english"
grep -q "M I N I R A Y   v${MINIRAY_TEST_VERSION}" "$work/english"
grep -q 'http://203.0.113.42:2053/panel/' "$work/english"
grep -q 'Username: admin' "$work/english"
grep -Fq -- '--noproxy *' "$work/state/public-ip-calls"
MINIRAY_PUBLIC_IPV4=198.51.100.24 bash "$SCRIPT" web-info > "$work/override-ip"
grep -q 'http://198.51.100.24:2053/panel/' "$work/override-ip"
test -f "$work/state/miniray.service.running"
test -f "$work/state/miniray.service.enabled"
test "$(cat "$work/state/lang")" = en

printf '5\nadmin-area\n\n7\n\n0\n' | bash "$SCRIPT" > "$work/path"
grep -q 'http://203.0.113.42:2053/admin-area/' "$work/path"
grep -q 'setting -webBasePath /admin-area/' "$work/state/commands"
grep -q 'restart miniray.service' "$work/state/services"
! grep -q 'restart miniray-core.service' "$work/state/services"

printf '/dashboard/\n' | bash "$SCRIPT" change-path > "$work/cli-path"
grep -q 'http://203.0.113.42:2053/dashboard/' "$work/cli-path"
grep -q 'setting -webBasePath /dashboard/' "$work/state/commands"

printf '5\nbad?query\n\n0\n' | bash "$SCRIPT" > "$work/invalid-path"
grep -q 'Enter a valid web path' "$work/invalid-path"
test "$(cat "$work/state/path")" = '/dashboard/'

printf '4\n70000\n\n4\n2054\n\n6\ntester\nsecret-key\nsecret-key\n\n0\n' | bash "$SCRIPT" > "$work/credentials"
grep -q 'Port must be between' "$work/credentials"
grep -q 'Port set successfully' "$work/credentials"
grep -q 'reset=tester;secret-key' "$work/state/commands"

touch "$work/state/miniray-core.service.running"
rm -f "$work/state/miniray-agent.service.running"
bash "$SCRIPT" stop-core > "$work/systemd-core-stop"
test ! -e "$work/state/miniray-core.service.running"
test -f "$MINIRAY_CORE_STOP_FILE"
grep -q 'stop miniray-core.service' "$work/state/services"
bash "$SCRIPT" start-core > "$work/systemd-core-start"
test -f "$work/state/miniray-core.service.running"
test -f "$work/state/miniray-agent.service.running"
test ! -e "$MINIRAY_CORE_STOP_FILE"

export MINIRAY_SERVICE_MANAGER=openrc
rm -f "$work/state/miniray-core.service.running" "$work/state/miniray-agent.service.running"
printf '1\n\n2\n\n8\n\n9\n\n0\n' | bash "$SCRIPT" > "$work/openrc"
test ! -e "$work/state/miniray.service.running"
test ! -e "$work/state/miniray.service.enabled"
test -f "$work/state/miniray-core.service.running"
test -f "$work/state/miniray-agent.service.running"
test -f "$work/state/miniray-core.service.enabled"
test -f "$work/state/miniray-agent.service.enabled"
grep -q 'Start panel' "$work/openrc"
rm -f "$work/state/miniray-agent.service.running"
bash "$SCRIPT" stop-core > "$work/openrc-core-stop"
test ! -e "$work/state/miniray-core.service.running"
test -f "$MINIRAY_CORE_STOP_FILE"
bash "$SCRIPT" restart-core > "$work/openrc-core-restart"
test -f "$work/state/miniray-core.service.running"
test -f "$work/state/miniray-agent.service.running"
test ! -e "$MINIRAY_CORE_STOP_FILE"

python3 - "$work/default" "$work/english" <<'PY'
from pathlib import Path
from unicodedata import east_asian_width
import sys

def width(text):
    return sum(2 if east_asian_width(char) in ("F", "W") else 1 for char in text)

expected = None
for filename in sys.argv[1:]:
    lines = Path(filename).read_text().splitlines()
    borders = [line for line in lines if line.startswith(("  ┌", "  │", "  └"))]
    assert borders
    for line in borders:
        assert line.endswith(("┐", "│", "┘")), (filename, line)
        if expected is None:
            expected = width(line)
        assert width(line) == expected, (filename, width(line), expected, line)
    separators = [line for line in lines if line.startswith("  ─")]
    assert separators and all(width(line) == expected for line in separators)
    assert sum(line.startswith("  ┌") for line in borders) == sum(line.startswith("  └") for line in borders)
PY

python3 - "$SCRIPT" "$work/state/lang" <<'PY'
import errno
import os
import pty
import select
import signal
import sys
import time
from pathlib import Path

for language in ("zh", "en"):
    Path(sys.argv[2]).write_text(language + "\n", encoding="utf-8")
    pid, fd = pty.fork()
    if pid == 0:
        os.environ["TERM"] = "xterm"
        os.environ["MINIRAY_SERVICE_MANAGER"] = "systemd"
        os.execvp("bash", ["bash", sys.argv[1]])
    os.write(fd, b"0\n")
    output = bytearray()
    deadline = time.monotonic() + 12
    try:
        while time.monotonic() < deadline:
            ready, _, _ = select.select([fd], [], [], 0.5)
            if ready:
                try:
                    data = os.read(fd, 65536)
                except OSError as exc:
                    if exc.errno == errno.EIO:
                        break
                    raise
                if not data:
                    break
                output.extend(data)
        else:
            os.kill(pid, signal.SIGKILL)
            raise AssertionError("terminal menu timed out")
        _, status = os.waitpid(pid, 0)
        assert os.waitstatus_to_exitcode(status) == 0, output.decode(errors="replace")
    finally:
        os.close(fd)

    rendered = output.decode(errors="replace")
    assert rendered.count("\x1b[58G│") == 15, (language, rendered)
    assert rendered.count("\x1b[5G") == 3, (language, rendered)
    assert rendered.count("┐") == 3 and rendered.count("┘") == 3, (language, rendered)
PY
cat > "$work/bin/curl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MINIRAY_TEST_STATE/curl-calls"
printf '{"tag_name":"v%s"}\n' "$MINIRAY_TEST_VERSION"
MOCK
cat > "$work/root/install.sh" <<'MOCK'
#!/usr/bin/env bash
printf 'Installed mock tag: %s\n' "$MINIRAY_UPDATE_TAG"
MOCK
chmod +x "$work/bin/curl" "$work/root/install.sh"
unset MINIRAY_UPDATE_TAG
bash "$(dirname "$SCRIPT")/update.sh" > "$work/update-output"
head -n 2 "$work/update-output" | grep -Fx "Current Miniray version: $MINIRAY_TEST_VERSION"
sed -n '2p' "$work/update-output" | grep -Fx "Latest Miniray version: v$MINIRAY_TEST_VERSION"
grep -Fx "Installed mock tag: v$MINIRAY_TEST_VERSION" "$work/update-output"
grep -Fx "Updated Miniray version: $MINIRAY_TEST_VERSION" "$work/update-output"
test "$(wc -l < "$work/state/curl-calls")" -eq 1
MINIRAY_UPDATE_TAG="v$MINIRAY_TEST_VERSION" bash "$(dirname "$SCRIPT")/update.sh" > "$work/pinned-output"
sed -n '2p' "$work/pinned-output" | grep -Fx "Target Miniray version: v$MINIRAY_TEST_VERSION"
test "$(wc -l < "$work/state/curl-calls")" -eq 1

printf 'Menu and updater acceptance: systemd, OpenRC, terminal layout, version output passed\n'
