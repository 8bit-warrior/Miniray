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
record_update_progress() {
  local state="$1" percent="$2" tmp
  [[ -n "$RUN_ID" ]] || return 0
  mkdir -p "$(dirname "$STATUS_FILE")"
  tmp=$(mktemp "${STATUS_FILE}.XXXXXX")
  printf '{"runId":"%s","state":"%s","progress":%d}\n' "$RUN_ID" "$state" "$percent" > "$tmp"
  chmod 600 "$tmp"
  mv -f "$tmp" "$STATUS_FILE"
}
trap 'record_status "$?"' EXIT
record_update_progress downloading 0
ROOT="${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}"
if [[ -x "$ROOT/miniray" ]]; then
  CURRENT_VERSION=$("$ROOT/miniray" -v)
  printf 'Current Miniray version: %s\n' "$CURRENT_VERSION"
fi
if [[ -n "${MINIRAY_ACTION_RUN_ID:-}" ]]; then
  [[ "$MINIRAY_ACTION_RUN_ID" =~ ^[1-9][0-9]*$ ]] || { echo 'Invalid Actions run ID' >&2; exit 1; }
  printf 'Target Actions run: %s\n' "$MINIRAY_ACTION_RUN_ID"
  installer=$(mktemp)
  if ! curl -fsSL --retry 4 -o "$installer" "https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh"; then
    rm -f "$installer"
    exit 1
  fi
  if bash "$installer" "$@"; then
    rm -f "$installer"
  else
    code=$?
    rm -f "$installer"
    exit "$code"
  fi
  printf 'Updated Miniray version: %s\n' "$("$ROOT/miniray" -v)"
  exit 0
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
