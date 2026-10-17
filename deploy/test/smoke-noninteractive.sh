#!/usr/bin/env bash
set -euo pipefail
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
python3 "$repo_root/tests/systemd_acceptance.py" "${1:?Pass the built release package directory}" "$repo_root"
