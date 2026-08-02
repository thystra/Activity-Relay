#!/usr/bin/env bash
# File: contrib/ops/test_rfc9421_inbound_probe.sh

set -Eeuo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
evidence_dir="${1:-}"

if [[ -z "$evidence_dir" ]]; then
  printf 'Usage: %s /absolute/path/to/private-evidence-directory\n' "$0" >&2
  exit 2
fi

case "$evidence_dir" in
  /*) ;;
  *)
    printf 'Evidence directory must be absolute.\n' >&2
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

workdir="$(mktemp -d /tmp/activity-relay-rfc9421-inbound-wrapper.XXXXXX)"
cleanup() {
  rm -rf -- "$workdir"
}
trap cleanup EXIT

binary="$workdir/activity-relay"
(
  cd "$repo"
  go build -trimpath -o "$binary" .
  go run ./contrib/ops/rfc9421inbound \
    --relay-binary "$binary" \
    --evidence-dir "$evidence_dir"
)

sha256sum "$binary" > "$evidence_dir/relay-binary.sha256"
python3 -m json.tool "$evidence_dir/report.json"

python3 - "$evidence_dir/report.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    report = json.load(handle)

errors = []
expected = {
    "classification": "rfc9421_inbound_runtime_pass",
    "valid_status": 202,
    "replay_status": 400,
    "tampered_status": 400,
    "valid_after_tamper_status": 202,
    "invalid_actor_gets": 0,
    "nonce_marker_count": 2,
}
for field, value in expected.items():
    if report.get(field) != value:
        errors.append(
            "%s is %r, expected %r"
            % (field, report.get(field), value)
        )

if report.get("signed_actor_gets", 0) < 2:
    errors.append("signed_actor_gets must be at least 2")

metrics = report.get("metrics", {})
expected_metrics = {
    "http_signature_verifications_total|rfc9421|success|accepted": "2",
    "http_signature_verifications_total|rfc9421|failure|replay": "1",
    "http_signature_verifications_total|rfc9421|failure|digest": "1",
}
for field, value in expected_metrics.items():
    if metrics.get(field) != value:
        errors.append(
            "metric %s is %r, expected %r"
            % (field, metrics.get(field), value)
        )

if errors:
    for error in errors:
        print("ERROR:", error, file=sys.stderr)
    raise SystemExit(1)

print("RFC 9421 inbound real-process invariant: PASS")
PY
