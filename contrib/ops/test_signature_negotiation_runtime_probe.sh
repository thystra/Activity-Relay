#!/usr/bin/env bash

set -Eeuo pipefail

if [[ $# -ne 2 ]]; then
  printf 'Usage: %s REDIS_URL EVIDENCE_DIR\n' "$0" >&2
  exit 2
fi

redis_url="$1"
evidence_dir="$2"
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

mkdir -p -m 0700 "$evidence_dir"

(
  cd "$repo"
  go run ./contrib/ops/signature-negotiation-probe \
    --redis-url "$redis_url" \
    --evidence-dir "$evidence_dir"
)
