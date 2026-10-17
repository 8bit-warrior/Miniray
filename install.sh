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
for cmd in curl tar sha256sum systemctl install cp sed; do
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
if [[ -f "$ROOT/miniray" || -f "$DB_PATH" ]]; then EXISTING_MINIRAY=1; fi
if systemctl list-unit-files x-ui.service --no-legend 2>/dev/null | grep -q '^x-ui.service' ||
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
    if (( EXISTING_MINIRAY == 1 )); then
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
    systemctl stop miniray.service miniray-agent.service miniray-core.service 2>/dev/null || :
  fi
  if (( status != 0 && LEGACY_STOPPED == 1 && LEGACY_REMOVED == 0 && LEGACY_WAS_ACTIVE == 1 )); then
    systemctl start x-ui.service || :
  fi
  if (( status != 0 && LEGACY_REMOVED == 0 && EXISTING_MINIRAY == 0 )); then
    if (( CREATED_DB == 1 )); then
      rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm" "$DB_FOLDER/initial-admin-password"
    fi
    if (( UNIT_STAGED == 1 )); then
      rm -f /etc/systemd/system/miniray.service /etc/systemd/system/miniray-core.service /etc/systemd/system/miniray-agent.service
      systemctl daemon-reload || :
    fi
    if (( WRAPPER_STAGED == 1 )); then rm -f /usr/local/bin/miniray; fi
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
  rm -rf "$TMP"
}
trap finish_install EXIT
ASSET="miniray-linux-${ARCH}.tar.gz"
if [[ "$VERSION" == latest ]]; then
  BASE="$REPO/releases/latest/download"
else
  BASE="$REPO/releases/download/$VERSION"
fi
curl -fL --retry 4 -o "$TMP/$ASSET" "$BASE/$ASSET"
curl -fL --retry 4 -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
(cd "$TMP" && grep -F "  $ASSET" SHA256SUMS | sha256sum -c -)
tar -tzf "$TMP/$ASSET" | grep -vE '(^miniray/|^miniray$)' && {
  echo 'Unexpected archive paths' >&2
  exit 1
} || :
tar --no-same-owner -xzf "$TMP/$ASSET" -C "$TMP"
test -x "$TMP/miniray/miniray"
test -x "$TMP/miniray/bin/xray-linux-$ARCH"
if (( COEXIST == 1 && EXISTING_MINIRAY == 0 )); then
  test -s "$TMP/miniray/bin/template.json"
  test -s "$TMP/miniray/coexist_ports.py"
fi

PANEL_BOOT=$(systemctl is-enabled miniray.service 2>/dev/null || :)
CORE_BOOT=$(systemctl is-enabled miniray-core.service 2>/dev/null || :)
AGENT_BOOT=$(systemctl is-enabled miniray-agent.service 2>/dev/null || :)
PANEL_ACTIVE=1
AGENT_ACTIVE=1
if (( EXISTING_MINIRAY == 1 )); then
  PANEL_ACTIVE=0
  AGENT_ACTIVE=0
  if systemctl is-active --quiet miniray.service; then PANEL_ACTIVE=1; fi
  if systemctl is-active --quiet miniray-agent.service; then AGENT_ACTIVE=1; fi
fi

LEGACY_ASSETS=''
if (( REMOVE_LEGACY == 1 )); then
  if systemctl list-unit-files x-ui.service --no-legend 2>/dev/null | grep -q '^x-ui.service'; then
    if systemctl is-active --quiet x-ui.service; then LEGACY_WAS_ACTIVE=1; fi
    systemctl stop x-ui.service
    LEGACY_STOPPED=1
    if systemctl is-active --quiet x-ui.service; then
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

systemctl stop miniray.service 2>/dev/null || :
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
cp -a "$TMP/miniray/." "$ROOT/"
ln -sfn "xray-linux-$ARCH" "$ROOT/bin/xray"
chmod 755 "$ROOT/miniray" "$ROOT/miniray.sh" "$ROOT/install.sh" "$ROOT/update.sh"
chmod 700 "$DB_FOLDER"
if [[ "$ROOT" != /usr/local/miniray ]]; then
  escaped_root=${ROOT//&/\\&}
  sed -i "s|/usr/local/miniray|${escaped_root//|/\\|}|g" "$ROOT/miniray.sh" "$ROOT/update.sh"
fi
install -m 0755 "$ROOT/miniray.sh" /usr/local/bin/miniray
WRAPPER_STAGED=1
case "$(. /etc/os-release; echo "${ID:-}")" in
  arch|manjaro) UNIT=arch;;
  rhel|centos|rocky|almalinux|fedora) UNIT=rhel;;
  *) UNIT=debian;;
esac
UNIT_STAGED=1
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

if [[ -n "${MINIRAY_DB_FOLDER:-}" || "$DB_FOLDER" != /etc/miniray ]]; then
  install -d -m 0755 /etc/default
  touch /etc/default/miniray
  chmod 600 /etc/default/miniray
  sed -i '/^MINIRAY_DB_FOLDER=/d' /etc/default/miniray
  printf 'MINIRAY_DB_FOLDER="%s"\n' "$DB_FOLDER" >> /etc/default/miniray
fi

[[ "$CORE_BOOT" == disabled || "$CORE_BOOT" == masked ]] || systemctl enable miniray-core.service
[[ "$AGENT_BOOT" == disabled || "$AGENT_BOOT" == masked ]] || systemctl enable miniray-agent.service
[[ "$PANEL_BOOT" == disabled || "$PANEL_BOOT" == masked ]] || systemctl enable miniray.service
if (( AGENT_ACTIVE == 1 )); then
  systemctl restart miniray-agent.service
  systemctl is-active --quiet miniray-agent.service
fi
if (( PANEL_ACTIVE == 1 )); then
  systemctl start miniray.service
  systemctl is-active --quiet miniray.service
fi
if (( LEGACY_FOUND == 1 )); then
  systemctl is-active --quiet miniray-core.service
  if (( COEXIST == 1 )); then
    systemctl is-active --quiet x-ui.service
    echo 'Miniray and 3x-ui are running independently.'
  elif (( REMOVE_LEGACY == 1 )); then
    systemctl is-active --quiet miniray-agent.service
    systemctl is-active --quiet miniray.service
    systemctl disable x-ui.service 2>/dev/null || :
    rm -f /etc/systemd/system/x-ui.service /usr/lib/systemd/system/x-ui.service /lib/systemd/system/x-ui.service
    rm -rf /usr/local/x-ui /etc/x-ui
    rm -f /usr/bin/x-ui /usr/local/bin/x-ui
    systemctl daemon-reload
    LEGACY_REMOVED=1
    echo "3x-ui uninstalled; its database and configuration backup is at $BACKUP_DIR"
  fi
fi

echo "Miniray $VERSION installed."
if [[ -f "$DB_FOLDER/initial-admin-password" ]]; then
  echo 'First-login administrator password: run sudo miniray initial-password'
fi
