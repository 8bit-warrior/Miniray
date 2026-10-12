#!/usr/bin/env bash
set -euo pipefail
REPO=https://github.com/8bit-warrior/Miniray
VERSION=v0.0.1
ROOT=/usr/local/miniray
((EUID==0))||{ echo 'Run as root' >&2;exit 1;}
for cmd in curl tar sha256sum systemctl;do command -v "$cmd" >/dev/null || { echo "Missing $cmd" >&2;exit 1;};done
case "$(uname -m)" in
 x86_64|amd64) ARCH=amd64;;
 aarch64|arm64) ARCH=arm64;;
 *) echo 'Only Linux amd64 and arm64 supported' >&2;exit 1;;
esac
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
ASSET="miniray-linux-${ARCH}.tar.gz"
BASE="${REPO}/releases/download/${VERSION}"
curl -fL --retry 4 -o "$TMP/$ASSET" "$BASE/$ASSET"
curl -fL --retry 4 -o "$TMP/SHA256SUMS" "$BASE/SHA256SUMS"
(cd "$TMP" && grep -F "  $ASSET" SHA256SUMS | sha256sum -c -)
tar -tzf "$TMP/$ASSET" | grep -vE '(^miniray/|^miniray$)' && { echo 'Unexpected archive paths' >&2;exit 1;} || :
tar -xzf "$TMP/$ASSET" -C "$TMP"
test -x "$TMP/miniray/miniray"
test -x "$TMP/miniray/bin/xray-linux-$ARCH"
systemctl stop miniray.service 2>/dev/null||true
mkdir -p "$ROOT" /etc/miniray /var/log/miniray
cp -a "$TMP/miniray/." "$ROOT/"
ln -sfn "xray-linux-$ARCH" "$ROOT/bin/xray"
chmod 755 "$ROOT/miniray" "$ROOT/miniray.sh" "$ROOT/install.sh"
chmod 700 /etc/miniray
install -m 0755 "$ROOT/miniray.sh" /usr/local/bin/miniray
case "$(. /etc/os-release;echo "${ID:-}")" in
 arch|manjaro) UNIT=arch;;
 rhel|centos|rocky|almalinux|fedora) UNIT=rhel;;
 *) UNIT=debian;;
esac
install -m 0644 "$ROOT/miniray-core.service" /etc/systemd/system/miniray-core.service
install -m 0644 "$ROOT/miniray.service.$UNIT" /etc/systemd/system/miniray.service
systemctl daemon-reload
systemctl enable miniray-core.service miniray.service
# The first panel start generates bin/config.json before it starts the core unit.
systemctl start miniray.service
systemctl is-active --quiet miniray.service || { echo 'Panel failed to start' >&2;exit 1;}
echo "Miniray ${VERSION} installed."
