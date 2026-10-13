#!/usr/bin/env bash
set -euo pipefail
REPO=https://github.com/8bit-warrior/Miniray
VERSION="${MINIRAY_UPDATE_TAG:-latest}"
ROOT="${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}"
(( EUID == 0 )) || { echo 'Run as root' >&2; exit 1; }
for cmd in curl tar sha256sum systemctl; do
  command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2; exit 1; }
done
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64;;
  aarch64|arm64) ARCH=arm64;;
  *) echo 'Only Linux amd64 and arm64 supported' >&2; exit 1;;
esac
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
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
tar -xzf "$TMP/$ASSET" -C "$TMP"
test -x "$TMP/miniray/miniray"
test -x "$TMP/miniray/bin/xray-linux-$ARCH"

PANEL_BOOT=$(systemctl is-enabled miniray.service 2>/dev/null || :)
CORE_BOOT=$(systemctl is-enabled miniray-core.service 2>/dev/null || :)
AGENT_BOOT=$(systemctl is-enabled miniray-agent.service 2>/dev/null || :)
systemctl stop miniray.service 2>/dev/null || :
mkdir -p "$ROOT/bin" /etc/miniray /var/log/miniray
# Do not overwrite a running Xray executable; its already-pinned version remains in place.
for binary in "$TMP/miniray"/bin/xray-linux-*; do
  [ -f "$binary" ] || continue
  destination="$ROOT/bin/$(basename "$binary")"
  if [ ! -e "$destination" ]; then install -m 0755 "$binary" "$destination"; fi
  rm -f "$binary"
done
cp -a "$TMP/miniray/." "$ROOT/"
ln -sfn "xray-linux-$ARCH" "$ROOT/bin/xray"
chmod 755 "$ROOT/miniray" "$ROOT/miniray.sh" "$ROOT/install.sh" "$ROOT/update.sh"
chmod 700 /etc/miniray
install -m 0755 "$ROOT/miniray.sh" /usr/local/bin/miniray
case "$(. /etc/os-release; echo "${ID:-}")" in
  arch|manjaro) UNIT=arch;;
  rhel|centos|rocky|almalinux|fedora) UNIT=rhel;;
  *) UNIT=debian;;
esac
install -m 0644 "$ROOT/miniray-core.service" /etc/systemd/system/miniray-core.service
install -m 0644 "$ROOT/miniray-agent.service" /etc/systemd/system/miniray-agent.service
install -m 0644 "$ROOT/miniray.service.$UNIT" /etc/systemd/system/miniray.service
systemctl daemon-reload
"$ROOT/miniray" init-db
[[ "$CORE_BOOT" == disabled || "$CORE_BOOT" == masked ]] || systemctl enable miniray-core.service
[[ "$AGENT_BOOT" == disabled || "$AGENT_BOOT" == masked ]] || systemctl enable miniray-agent.service
[[ "$PANEL_BOOT" == disabled || "$PANEL_BOOT" == masked ]] || systemctl enable miniray.service
systemctl start miniray-agent.service
systemctl start miniray.service
systemctl is-active --quiet miniray.service
echo "Miniray $VERSION installed."
if [[ -f /etc/miniray/initial-admin-password ]]; then
  echo "First-login administrator password: run sudo miniray initial-password"
fi
