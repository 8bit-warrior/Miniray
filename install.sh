#!/usr/bin/env bash
set -euo pipefail
REPO=https://github.com/8bit-warrior/Miniray
VERSION="${MINIRAY_UPDATE_TAG:-latest}"
ROOT="${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}"
DB_FOLDER="${MINIRAY_DB_FOLDER:-/etc/miniray}"
if [[ -z "${MINIRAY_DB_FOLDER:-}" && -f /etc/default/miniray ]]; then
  saved_db_folder=$(sed -n 's/^MINIRAY_DB_FOLDER=//p' /etc/default/miniray | tail -n 1)
  saved_db_folder=${saved_db_folder%\"}; saved_db_folder=${saved_db_folder#\"}
  if [[ -n "$saved_db_folder" ]]; then DB_FOLDER="$saved_db_folder"; fi
fi
if [[ -f /etc/conf.d/miniray && -z "${MINIRAY_DB_FOLDER:-}" ]]; then
  source /etc/conf.d/miniray
  DB_FOLDER="${MINIRAY_DB_FOLDER:-$DB_FOLDER}"
fi
DB_PATH="$DB_FOLDER/miniray.db"
LEGACY_DB_FOLDER="${XUI_DB_FOLDER:-/etc/x-ui}"
LEGACY_DB_TYPE="sqlite"
if [[ -f /etc/default/x-ui ]]; then
  legacy_folder=$(sed -n 's/^XUI_DB_FOLDER=//p' /etc/default/x-ui | tail -n 1)
  legacy_type=$(sed -n 's/^XUI_DB_TYPE=//p' /etc/default/x-ui | tail -n 1)
  legacy_folder=${legacy_folder%\"}; legacy_folder=${legacy_folder#\"}
  legacy_type=${legacy_type%\"}; legacy_type=${legacy_type#\"}
  if [[ -n "$legacy_folder" && -z "${XUI_DB_FOLDER:-}" ]]; then LEGACY_DB_FOLDER="$legacy_folder"; fi
  if [[ -n "$legacy_type" ]]; then LEGACY_DB_TYPE="$legacy_type"; fi
fi
LEGACY_DB="$LEGACY_DB_FOLDER/x-ui.db"

(( EUID == 0 )) || { echo 'Run as root' >&2; exit 1; }
if command -v systemctl >/dev/null 2>&1 && [[ -d /run/systemd/system ]]; then
  SERVICE_MANAGER=systemd
elif command -v rc-service >/dev/null 2>&1 && command -v rc-update >/dev/null 2>&1; then
  SERVICE_MANAGER=openrc
else
  echo 'Requires systemd or OpenRC' >&2; exit 1
fi
service_action() {
  local action="$1" unit="$2"
  if [[ "$SERVICE_MANAGER" == openrc ]]; then
    rc-service "${unit%.service}" "$action"
  else
    systemctl "$action" "$unit"
  fi
}
service_active() {
  if [[ "$SERVICE_MANAGER" == openrc ]]; then
    rc-service "${1%.service}" status >/dev/null 2>&1
  else
    systemctl is-active --quiet "$1"
  fi
}
service_enabled() {
  if [[ "$SERVICE_MANAGER" == openrc ]]; then
    rc-update show default | grep -qE "^[[:space:]]*${1%.service}[[:space:]]"
  else
    systemctl is-enabled --quiet "$1"
  fi
}
service_enable() {
  if [[ "$SERVICE_MANAGER" == openrc ]]; then rc-update add "${1%.service}" default; else systemctl enable "$1"; fi
}
service_disable() {
  if [[ "$SERVICE_MANAGER" == openrc ]]; then rc-update del "${1%.service}" default; else systemctl disable "$1"; fi
}
legacy_service_exists() {
  if [[ "$SERVICE_MANAGER" == openrc ]]; then
    [[ -e /etc/init.d/x-ui ]]
  else
    systemctl list-unit-files x-ui.service --no-legend 2>/dev/null | grep -q '^x-ui.service'
  fi
}
for cmd in curl tar sha256sum install cp sed; do
  command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2; exit 1; }
done
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64;;
  aarch64|arm64) ARCH=arm64;;
  *) echo 'Only Linux amd64 and arm64 supported' >&2; exit 1;;
esac
case "$ROOT" in
  /*) ;;
  *) echo 'MINIRAY_MAIN_FOLDER must be absolute' >&2; exit 1;;
esac
case "$DB_FOLDER" in
  /*) ;;
  *) echo 'MINIRAY_DB_FOLDER must be absolute' >&2; exit 1;;
esac

LEGACY_FOUND=0
REMOVE_LEGACY=0
IMPORT_LEGACY=0
COEXIST=0
EXISTING_MINIRAY=0
if [[ -f "$ROOT/miniray" ]]; then EXISTING_MINIRAY=1; fi
if (( EXISTING_MINIRAY == 0 )) && [[ -f "$DB_PATH" ]]; then
  [[ -r /dev/tty ]] || { echo 'An existing Miniray database requires an interactive terminal.' >&2; exit 1; }
  printf 'Existing Miniray database: %s\n' "$DB_PATH"
  echo '1) Import and reuse existing database'
  echo '2) Clear old database and install fresh'
  echo '3) Cancel installation'
  while true; do
    read -r -p 'Select [1/2/3]: ' choice </dev/tty || exit 1
    case "$choice" in
      1) break;;
      2) CLEAR_EXISTING_DB=1; break;;
      3) echo 'Installation cancelled.'; exit 0;;
      *) echo 'Invalid selection.';;
    esac
  done
fi
CLEAR_EXISTING_DB="${CLEAR_EXISTING_DB:-0}"
if legacy_service_exists ||
   [[ -e /usr/local/x-ui/x-ui || -e "$LEGACY_DB" || -e /usr/bin/x-ui || -e /usr/local/bin/x-ui ]]; then
  LEGACY_FOUND=1
  [[ -r /dev/tty ]] || { echo 'Select a 3x-ui installation strategy in an interactive terminal.' >&2; exit 1; }
  echo 'Existing 3x-ui installation detected:'
  echo '1) Coexist: install and run Miniray alongside 3x-ui'
  echo '2) Migrate: uninstall 3x-ui and import supported data into Miniray'
  echo '3) Cancel this installation'
  while true; do
    read -r -p 'Select [1/2/3]: ' choice </dev/tty || exit 1
    case "$choice" in
      1) COEXIST=1; break;;
      2) REMOVE_LEGACY=1; IMPORT_LEGACY=1; break;;
      3) echo 'Installation cancelled.'; exit 0;;
      *) echo 'Invalid selection. Enter 1, 2 or 3.';;
    esac
  done
  if (( IMPORT_LEGACY == 1 )); then
    if (( EXISTING_MINIRAY == 1 )) || { (( CLEAR_EXISTING_DB == 0 )) && [[ -f "$DB_PATH" ]]; }; then
      echo 'Migration requires a fresh Miniray database. Existing Miniray data was not changed.' >&2
      exit 1
    fi
    if [[ "$LEGACY_DB_TYPE" != sqlite && "$LEGACY_DB_TYPE" != SQLite ]]; then
      echo "Legacy $LEGACY_DB_TYPE databases cannot be imported; SQLite is required." >&2
      exit 1
    fi
    if [[ ! -f "$LEGACY_DB" ]]; then
      echo "Legacy SQLite database not found at $LEGACY_DB." >&2
      exit 1
    fi
  fi
  if (( COEXIST == 1 && EXISTING_MINIRAY == 0 )); then
    command -v python3 >/dev/null || { echo 'Python 3 is required to isolate coexistence ports.' >&2; exit 1; }
  fi
fi

TMP=$(mktemp -d)
LEGACY_STOPPED=0
LEGACY_REMOVED=0
LEGACY_WAS_ACTIVE=0
STAGED_INSTALL=0
UNIT_STAGED=0
WRAPPER_STAGED=0
CREATED_DB=0
ROOT_WAS_ABSENT=0
[[ -e "$ROOT" ]] || ROOT_WAS_ABSENT=1
finish_install() {
  local status=$?
  if (( status != 0 && LEGACY_REMOVED == 0 && EXISTING_MINIRAY == 0 && STAGED_INSTALL == 1 )); then
    service_action stop miniray.service 2>/dev/null || :
    service_action stop miniray-agent.service 2>/dev/null || :
    service_action stop miniray-core.service 2>/dev/null || :
  fi
  if (( status != 0 && LEGACY_STOPPED == 1 && LEGACY_REMOVED == 0 && LEGACY_WAS_ACTIVE == 1 )); then
    service_action start x-ui.service || :
  fi
  if (( status != 0 && LEGACY_REMOVED == 0 && EXISTING_MINIRAY == 0 )); then
    if (( CREATED_DB == 1 )); then
      rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm" "$DB_FOLDER/initial-admin-password"
    fi
    if (( UNIT_STAGED == 1 )); then
      for unit in miniray.service miniray-agent.service miniray-core.service; do service_disable "$unit" 2>/dev/null || :; done
      if [[ "$SERVICE_MANAGER" == openrc ]]; then
        rm -f /etc/init.d/miniray /etc/init.d/miniray-agent /etc/init.d/miniray-core
      else
        rm -f /etc/systemd/system/miniray.service /etc/systemd/system/miniray-core.service /etc/systemd/system/miniray-agent.service
        systemctl daemon-reload || :
      fi
    fi
    if (( WRAPPER_STAGED == 1 )); then rm -f /usr/local/bin/miniray /usr/local/bin/mr; fi
    if (( STAGED_INSTALL == 1 )); then
      if (( ROOT_WAS_ABSENT == 1 )); then
        rm -rf "$ROOT"
      else
        rm -f "$ROOT/miniray" "$ROOT/.miniray.next"
      fi
    fi
  fi
  if (( status != 0 && LEGACY_REMOVED == 1 )); then
    echo "Installation failed after legacy removal; restore data from $BACKUP_DIR if needed." >&2
  fi
  if [[ -n "${DB_BACKUP_DIR:-}" ]]; then
    if (( status != 0 )); then
      for suffix in '' -wal -shm -journal; do
        rm -f "$DB_PATH$suffix"
        if [[ -f "$DB_BACKUP_DIR/miniray.db$suffix" ]]; then mv "$DB_BACKUP_DIR/miniray.db$suffix" "$DB_PATH$suffix"; fi
      done
      if [[ -f "$DB_BACKUP_DIR/initial-admin-password" ]]; then
        mv "$DB_BACKUP_DIR/initial-admin-password" "$DB_FOLDER/initial-admin-password"
      fi
    fi
    rm -rf "$DB_BACKUP_DIR"
  fi
  rm -rf "$TMP"
}
trap finish_install EXIT
ASSET="miniray-linux-$ARCH.tar.gz"
ACTION_RUN_ID="${MINIRAY_ACTION_RUN_ID:-}"
if [[ -n "$ACTION_RUN_ID" ]]; then
  [[ "$ACTION_RUN_ID" =~ ^[1-9][0-9]*$ ]] || { echo 'Invalid Actions run ID' >&2; exit 1; }
  for cmd in python3 unzip; do command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2; exit 1; }; done
  API="https://api.github.com/repos/8bit-warrior/Miniray"
  curl -fsSL --retry 4 -o "$TMP/action-run.json" "$API/actions/runs/$ACTION_RUN_ID"
  curl -fsSL --retry 4 -o "$TMP/action-artifacts.json" "$API/actions/runs/$ACTION_RUN_ID/artifacts?per_page=100"
  ACTION_META=$(python3 - "$TMP/action-run.json" "$TMP/action-artifacts.json" "$ARCH" "$ACTION_RUN_ID" <<'PY'
import json
import re
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    run = json.load(stream)
with open(sys.argv[2], encoding="utf-8") as stream:
    artifacts = json.load(stream)["artifacts"]
arch, run_id = sys.argv[3:]
sha = run.get("head_sha", "")
suite = run.get("check_suite_id")
if (
    not isinstance(suite, int)
    or suite <= 0
    or str(run.get("id")) != run_id
    or run.get("head_branch") != "main"
    or run.get("status") != "completed"
    or run.get("conclusion") != "success"
    or run.get("path", "").split("@")[0] != ".github/workflows/release.yml"
    or not re.fullmatch(r"[0-9a-fA-F]{40}", sha)
):
    sys.exit("Selected Actions run is not a successful Miniray main build")
version = "dev+" + sha[:8].lower()
release = re.fullmatch(r"chore\(release\): (v[0-9]+\.[0-9]+\.[0-9]+)", run.get("display_title", ""))
if release:
    version = release.group(1)[1:]
for artifact in artifacts:
    digest = artifact.get("digest", "")
    workflow = artifact.get("workflow_run") or {}
    if (
        artifact.get("name") == f"miniray-linux-{arch}"
        and not artifact.get("expired")
        and re.fullmatch(r"sha256:[0-9a-f]{64}", digest)
        and workflow.get("id") == run["id"]
        and workflow.get("head_sha", "").lower() == sha.lower()
    ):
        print(artifact["id"], digest.split(":", 1)[1], version, sha.lower(), suite)
        break
else:
    sys.exit("Selected Actions build has no valid, unexpired artifact for this architecture")
PY
  ) || exit 1
  read -r ARTIFACT_ID EXPECTED_SHA EXPECTED_VERSION ACTION_COMMIT CHECK_SUITE_ID <<< "$ACTION_META"
  printf 'Downloading Actions run %s, artifact %s...\n' "$ACTION_RUN_ID" "$ARTIFACT_ID"
  curl -fL --retry 4 --connect-timeout 15 --max-filesize 268435456 \
    -o "$TMP/action.zip" \
    "https://nightly.link/8bit-warrior/Miniray/suites/$CHECK_SUITE_ID/artifacts/$ARTIFACT_ID"
  printf '%s  %s\n' "$EXPECTED_SHA" "$TMP/action.zip" | sha256sum -c -
  unzip -p "$TMP/action.zip" "$ASSET" > "$TMP/$ASSET"
  test -s "$TMP/$ASSET"
  VERSION="$EXPECTED_VERSION"
else
  if [[ "$VERSION" == latest ]]; then
    curl -fsSL --retry 4 -o "$TMP/release.json" "https://api.github.com/repos/8bit-warrior/Miniray/releases/latest"
    VERSION=$(sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' "$TMP/release.json" | head -n 1)
    [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid latest stable Release tag' >&2; exit 1; }
  fi
  BASE="$REPO/releases/download/$VERSION"
  printf "Downloading %s from %s...\n" "$ASSET" "$VERSION"
  curl -fsSL --retry 4 -o "$TMP/$ASSET" "$BASE/$ASSET"
  curl -fsSL --retry 4 -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
  (cd "$TMP" && grep -F "  $ASSET" SHA256SUMS | sha256sum -c -)
fi
tar -tzf "$TMP/$ASSET" | grep -vE '(^miniray/|^miniray$)' && {
  echo 'Unexpected archive paths' >&2
  exit 1
} || :
tar -xzf "$TMP/$ASSET" -C "$TMP"
test -x "$TMP/miniray/miniray"
test -x "$TMP/miniray/bin/xray-linux-$ARCH"
PACKAGE_VERSION=$("$TMP/miniray/miniray" -v)
if [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && "$PACKAGE_VERSION" != "${VERSION#v}" ]]; then
  echo "Release $VERSION contains Miniray $PACKAGE_VERSION; installation aborted" >&2
  exit 1
fi
if (( COEXIST == 1 && EXISTING_MINIRAY == 0 )); then
  test -s "$TMP/miniray/bin/template.json"
  test -s "$TMP/miniray/coexist_ports.py"
fi

PANEL_BOOT=$(service_enabled miniray.service && echo enabled || echo disabled)
CORE_BOOT=$(service_enabled miniray-core.service && echo enabled || echo disabled)
AGENT_BOOT=$(service_enabled miniray-agent.service && echo enabled || echo disabled)
PANEL_ACTIVE=1
AGENT_ACTIVE=1
if (( EXISTING_MINIRAY == 1 )); then
  PANEL_ACTIVE=0
  AGENT_ACTIVE=0
  if service_active miniray.service; then PANEL_ACTIVE=1; fi
  if service_active miniray-agent.service; then AGENT_ACTIVE=1; fi
fi

LEGACY_ASSETS=''
if (( REMOVE_LEGACY == 1 )); then
  if legacy_service_exists; then
    if service_active x-ui.service; then LEGACY_WAS_ACTIVE=1; fi
    service_action stop x-ui.service
    LEGACY_STOPPED=1
    if service_active x-ui.service; then
      echo 'Cannot stop the old panel. No files were removed.' >&2
      exit 1
    fi
  fi
  "$TMP/miniray/miniray" snapshot-legacy --source "$LEGACY_DB" --out "$TMP/legacy.db"
  test -s "$TMP/legacy.db"
  install -d -m 0700 /var/backups/miniray
  BACKUP_DIR=$(mktemp -d /var/backups/miniray/legacy-XXXXXXXX)
  chmod 700 "$BACKUP_DIR"
  install -m 0600 "$TMP/legacy.db" "$BACKUP_DIR/x-ui.db"
  test -s "$BACKUP_DIR/x-ui.db"
  if [[ -d /etc/x-ui ]]; then
    cp -a /etc/x-ui "$BACKUP_DIR/config"
    LEGACY_ASSETS="$DB_FOLDER/imported-legacy-files"
    install -d -m 0700 "$LEGACY_ASSETS"
    cp -a /etc/x-ui/. "$LEGACY_ASSETS/"
    rm -f "$LEGACY_ASSETS/x-ui.db" "$LEGACY_ASSETS/x-ui.db-wal" "$LEGACY_ASSETS/x-ui.db-shm" "$LEGACY_ASSETS/install-result.env"
  fi
  if [[ -f /etc/default/x-ui ]]; then
    install -m 0600 /etc/default/x-ui "$BACKUP_DIR/service-env"
  fi
fi

service_action stop miniray.service 2>/dev/null || :
STAGED_INSTALL=1
install -d -m 0700 "$DB_FOLDER"
mkdir -p "$ROOT/bin" /var/log/miniray
for binary in "$TMP/miniray"/bin/xray-linux-*; do
  [[ -f "$binary" ]] || continue
  destination="$ROOT/bin/$(basename "$binary")"
  if [[ ! -e "$destination" ]]; then install -m 0755 "$binary" "$destination"; fi
  rm -f "$binary"
done
for binary in "$TMP/miniray"/bin/mtg-*; do
  [[ -f "$binary" ]] || continue
  destination="$ROOT/bin/$(basename "$binary")"
  install -m 0755 "$binary" "$destination.next"
  mv -f "$destination.next" "$destination"
  rm -f "$binary"
done
install -m 0755 "$TMP/miniray/miniray" "$ROOT/.miniray.next"
mv -f "$ROOT/.miniray.next" "$ROOT/miniray"
rm -f "$TMP/miniray/miniray"
for script in install.sh update.sh miniray.sh; do
  install -m 0755 "$TMP/miniray/$script" "$ROOT/.$script.next"
  mv -f "$ROOT/.$script.next" "$ROOT/$script"
  rm -f "$TMP/miniray/$script"
done
cp -a "$TMP/miniray/." "$ROOT/"
if [[ -n "$ACTION_RUN_ID" ]]; then
  printf '%s\n' "$ACTION_COMMIT" > "$ROOT/.action-commit"
  chmod 0644 "$ROOT/.action-commit"
else
  rm -f "$ROOT/.action-commit"
fi
ln -sfn "xray-linux-$ARCH" "$ROOT/bin/xray"
chmod 755 "$ROOT/miniray" "$ROOT/miniray.sh" "$ROOT/install.sh" "$ROOT/update.sh"
chmod 700 "$DB_FOLDER"
if [[ "$ROOT" != /usr/local/miniray ]]; then
  escaped_root=${ROOT//&/\\&}
  sed -i "s|/usr/local/miniray|${escaped_root//|/\\|}|g" "$ROOT/miniray.sh" "$ROOT/update.sh"
fi
install -m 0755 "$ROOT/miniray.sh" /usr/local/bin/miniray
ln -sfn miniray /usr/local/bin/mr
WRAPPER_STAGED=1
case "$(. /etc/os-release; echo "${ID:-}")" in
  arch|manjaro) UNIT=arch;;
  rhel|centos|rocky|almalinux|fedora) UNIT=rhel;;
  *) UNIT=debian;;
esac
UNIT_STAGED=1
if [[ "$SERVICE_MANAGER" == openrc ]]; then
  for name in miniray miniray-core miniray-agent; do
    if [[ "$ROOT" == /usr/local/miniray ]]; then
      install -m 0755 "$ROOT/openrc/$name" "/etc/init.d/$name"
    else
      sed "s|/usr/local/miniray|${escaped_root//|/\\|}|g" "$ROOT/openrc/$name" > "/etc/init.d/$name"
      chmod 755 "/etc/init.d/$name"
    fi
  done
else
for service in miniray-core.service miniray-agent.service "miniray.service.$UNIT"; do
  unit_name=${service%%.service*}.service
  if [[ "$service" == miniray.service.* ]]; then unit_name=miniray.service; fi
  if [[ "$ROOT" == /usr/local/miniray ]]; then
    install -m 0644 "$ROOT/$service" "/etc/systemd/system/$unit_name"
  else
    sed "s|/usr/local/miniray|${escaped_root//|/\\|}|g" "$ROOT/$service" > "/etc/systemd/system/$unit_name"
    chmod 644 "/etc/systemd/system/$unit_name"
  fi
done
systemctl daemon-reload
fi

if (( CLEAR_EXISTING_DB == 1 )); then
  DB_BACKUP_DIR=$(mktemp -d)
  for suffix in '' -wal -shm -journal; do
    if [[ -f "$DB_PATH$suffix" ]]; then mv "$DB_PATH$suffix" "$DB_BACKUP_DIR/miniray.db$suffix"; fi
  done
  if [[ -f "$DB_FOLDER/initial-admin-password" ]]; then
    mv "$DB_FOLDER/initial-admin-password" "$DB_BACKUP_DIR/initial-admin-password"
  fi
fi
if [[ ! -f "$DB_PATH" ]]; then
  CREATED_DB=1
  if (( IMPORT_LEGACY == 1 )); then
    args=(import-legacy --source "$TMP/legacy.db")
    if [[ -n "$LEGACY_ASSETS" ]]; then args+=(--assets "$LEGACY_ASSETS"); fi
    MINIRAY_DB_FOLDER="$DB_FOLDER" "$ROOT/miniray" "${args[@]}"
  else
    MINIRAY_DB_FOLDER="$DB_FOLDER" "$ROOT/miniray" init-db
  fi
fi

if (( COEXIST == 1 && EXISTING_MINIRAY == 0 )); then
  python3 "$TMP/miniray/coexist_ports.py" "$DB_PATH" "$TMP/miniray/bin/template.json" "$LEGACY_DB"
fi

if [[ "$SERVICE_MANAGER" == openrc ]]; then
  install -d -m 0755 /etc/conf.d
  printf 'MINIRAY_SERVICE_MANAGER="openrc"\nMINIRAY_SEPARATE_CORE="1"\nMINIRAY_DB_FOLDER="%s"\n' "$DB_FOLDER" > /etc/conf.d/miniray
  chmod 600 /etc/conf.d/miniray
fi
if [[ -n "${MINIRAY_DB_FOLDER:-}" || "$DB_FOLDER" != /etc/miniray ]]; then
  install -d -m 0755 /etc/default
  touch /etc/default/miniray
  chmod 600 /etc/default/miniray
  sed -i '/^MINIRAY_DB_FOLDER=/d' /etc/default/miniray
  printf 'MINIRAY_DB_FOLDER="%s"\n' "$DB_FOLDER" >> /etc/default/miniray
fi

if (( EXISTING_MINIRAY == 0 )); then
  service_enable miniray-core.service
  service_enable miniray-agent.service
  service_enable miniray.service
else
  [[ "$CORE_BOOT" == disabled || "$CORE_BOOT" == masked ]] || service_enable miniray-core.service
  [[ "$AGENT_BOOT" == disabled || "$AGENT_BOOT" == masked ]] || service_enable miniray-agent.service
  [[ "$PANEL_BOOT" == disabled || "$PANEL_BOOT" == masked ]] || service_enable miniray.service
fi
if (( AGENT_ACTIVE == 1 )); then
  service_action restart miniray-agent.service
  service_active miniray-agent.service
fi
if (( PANEL_ACTIVE == 1 )); then
  service_action start miniray.service
  service_active miniray.service
fi
if (( LEGACY_FOUND == 1 )); then
  if (( EXISTING_MINIRAY == 0 )); then
    for attempt in {1..60}; do
      if service_active miniray-core.service; then break; fi
      sleep 0.5
    done
    service_active miniray-core.service || {
      echo 'Miniray proxy core did not start. See journalctl -u miniray-agent -u miniray-core.' >&2
      exit 1
    }
  fi
  if (( COEXIST == 1 )); then
    echo 'Miniray installed alongside 3x-ui; the legacy service state was preserved.'
  elif (( REMOVE_LEGACY == 1 )); then
    service_active miniray-agent.service
    service_active miniray.service
    "$ROOT/bin/xray" -test -c "$ROOT/bin/config.json"
    service_disable x-ui.service 2>/dev/null || :
    if [[ "$SERVICE_MANAGER" == systemd ]]; then rm -f /etc/systemd/system/x-ui.service /usr/lib/systemd/system/x-ui.service /lib/systemd/system/x-ui.service; fi
    rm -rf /usr/local/x-ui /etc/x-ui
    rm -f /usr/bin/x-ui /usr/local/bin/x-ui
    if [[ "$SERVICE_MANAGER" == systemd ]]; then systemctl daemon-reload; else rm -f /etc/init.d/x-ui; fi
    LEGACY_REMOVED=1
    echo "3x-ui uninstalled; its database and configuration backup is at $BACKUP_DIR"
  fi
fi

INSTALLED_VERSION=$("$ROOT/miniray" -v)
EXPECTED_INSTALLED="$PACKAGE_VERSION"
if [[ -n "$ACTION_RUN_ID" ]]; then
  EXPECTED_INSTALLED="${PACKAGE_VERSION}_${ACTION_COMMIT:0:7}"
  if [[ "$INSTALLED_VERSION" == "dev+${ACTION_COMMIT:0:8}" || "$INSTALLED_VERSION" == "$PACKAGE_VERSION" ]]; then
    EXPECTED_INSTALLED="$INSTALLED_VERSION"
  fi
fi
if [[ "$INSTALLED_VERSION" != "$EXPECTED_INSTALLED" ]]; then
  echo "Installed Miniray version mismatch: expected $EXPECTED_INSTALLED, got $INSTALLED_VERSION" >&2
  exit 1
fi
echo "Miniray $INSTALLED_VERSION installed."
if [[ -f "$DB_FOLDER/initial-admin-password" ]]; then
  echo 'First-login administrator password: run sudo miniray initial-password'
fi
