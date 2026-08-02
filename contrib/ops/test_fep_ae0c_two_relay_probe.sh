#!/usr/bin/env bash
# File: contrib/ops/test_fep_ae0c_two_relay_probe.sh
#
# Run a bounded, process-level FEP-ae0c reflection diagnostic. A zero exit
# status means the infrastructure and observation completed; inspect the JSON
# classification before treating the result as a protocol invariant.

set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
evidence_dir="${1:-}"

if [[ -z "$evidence_dir" ]]; then
  printf 'Usage: %s /absolute/path/to/private-evidence-directory\n' "$0" >&2
  exit 2
fi

case "$evidence_dir" in
  /*)
    ;;
  *)
    printf 'Evidence directory must be an absolute path.\n' >&2
    exit 2
    ;;
esac

python3 - "$repo" "$evidence_dir" <<'PY'
from pathlib import Path
import sys

repo = Path(sys.argv[1]).resolve()
evidence = Path(sys.argv[2]).resolve()
try:
    evidence.relative_to(repo)
except ValueError:
    pass
else:
    raise SystemExit("Evidence directory must be outside the repository")
PY

for command_name in docker go python3 sha256sum; do
  command -v "$command_name" >/dev/null 2>&1 || {
    printf 'Required command not found: %s\n' "$command_name" >&2
    exit 1
  }
done

docker info >/dev/null
mkdir -p -m 0700 "$evidence_dir"

workdir="$(mktemp -d /tmp/activity-relay-fepmesh-wrapper.XXXXXX)"
cleanup() {
  rm -rf -- "$workdir"
}
trap cleanup EXIT

binary="$workdir/activity-relay"
(
  cd "$repo"
  go build -trimpath -o "$binary" .
  go run ./contrib/ops/fepmesh \
    --relay-binary "$binary" \
    --evidence-dir "$evidence_dir"
)

sha256sum "$binary" > "$evidence_dir/relay-binary.sha256"
python3 -m json.tool "$evidence_dir/report.json"

classification="$(
  python3 - "$evidence_dir/report.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as handle:
    print(json.load(handle)["classification"])
PY
)"

case "$classification" in
  no_reflection_observed|reflection_settled|reflection_active_at_timeout|reflection_threshold_reached)
    printf 'Two-relay probe classification: %s\n' "$classification"
    ;;
  infrastructure_failure)
    printf 'Two-relay probe infrastructure failed.\n' >&2
    exit 1
    ;;
  *)
    printf 'Unexpected two-relay probe classification: %s\n' "$classification" >&2
    exit 1
    ;;
esac

# EOF: contrib/ops/test_fep_ae0c_two_relay_probe.sh
