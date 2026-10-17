#!/usr/bin/env bash
set -euo pipefail
STATUS_FILE="${MINIRAY_UPDATE_STATUS_FILE:-/etc/miniray/update-status.json}"
RUN_ID="${MINIRAY_UPDATE_RUN_ID:-}"
record_status() {
  local code="$1" state=success
  if (( code != 0 )); then state=failed; fi
  if [[ -n "$RUN_ID" ]]; then
    mkdir -p "$(dirname "$STATUS_FILE")"
    local tmp
    tmp=$(mktemp "${STATUS_FILE}.XXXXXX")
    printf '{"runId":"%s","state":"%s","exitCode":%d,"finishedAt":%s}\n' \
      "$RUN_ID" "$state" "$code" "$(date +%s)" > "$tmp"
    chmod 600 "$tmp"
    mv -f "$tmp" "$STATUS_FILE"
  fi
}
trap 'record_status "$?"' EXIT
ROOT="${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}"
if [[ -x "$ROOT/miniray" ]]; then
  CURRENT_VERSION=$("$ROOT/miniray" -v)
  printf 'Current Miniray version: %s\n' "$CURRENT_VERSION"
fi
TARGET_VERSION="${MINIRAY_UPDATE_TAG:-latest}"
if [[ "$TARGET_VERSION" == latest ]]; then
  RELEASE_JSON=$(curl -fsSL --retry 4 "https://api.github.com/repos/8bit-warrior/Miniray/releases/latest")
  TARGET_VERSION=$(printf '%s\n' "$RELEASE_JSON" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
  [[ "$TARGET_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Invalid latest stable Release tag' >&2; exit 1; }
  printf 'Latest Miniray version: %s\n' "$TARGET_VERSION"
else
  printf 'Target Miniray version: %s\n' "$TARGET_VERSION"
fi
MINIRAY_UPDATE_TAG="$TARGET_VERSION" bash "$ROOT/install.sh" "$@"
printf 'Updated Miniray version: %s\n' "$("$ROOT/miniray" -v)"
