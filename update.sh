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
bash "${MINIRAY_MAIN_FOLDER:-/usr/local/miniray}/install.sh" "$@"
