#!/usr/bin/env bash
# File: contrib/ops/test_fep_ae0c_two_relay_probe.sh
#
# Run the required process-level FEP-ae0c reflection invariant. The command
# fails unless one seed Announce produces no generated cross-relay response,
# all observed signatures are valid, a canonical marker is retained, and both
# relay queues settle without ready, delayed, claimed, retry, or retained
# activity backlog.

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

python3 - "$evidence_dir/report.json" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    report = json.load(handle)

errors = []
if report.get("classification") != "no_reflection_observed":
    errors.append(
        "classification is %r, expected 'no_reflection_observed'"
        % report.get("classification")
    )
if report.get("seed_status") != 202:
    errors.append("seed status is %r, expected 202" % report.get("seed_status"))
for field in (
    "generated_cross_relay_posts",
    "unique_generated_activity_ids",
    "repeated_identical_deliveries",
    "new_relay_announce_ids",
    "invalid_signed_gets",
    "invalid_signed_posts",
):
    if report.get(field) != 0:
        errors.append("%s is %r, expected 0" % (field, report.get(field)))
if report.get("signed_gets", 0) < 2:
    errors.append("signed_gets is %r, expected at least 2" % report.get("signed_gets"))

final_states = {
    entry.get("relay"): entry
    for entry in report.get("redis", [])
    if str(entry.get("relay", "")).endswith("-final")
}
if set(final_states) != {"relay-a-final", "relay-b-final"}:
    errors.append("final Redis snapshots are incomplete")

backlog_categories = (
    "ready",
    "delayed",
    "claimed_or_reserved",
    "retry",
    "retained_activity",
)
canonical_markers = 0
for relay, state in sorted(final_states.items()):
    categories = state.get("categories", {})
    for category in backlog_categories:
        if categories.get(category, 0) != 0:
            errors.append(
                "%s category %s is %r, expected 0"
                % (relay, category, categories.get(category, 0))
            )
    canonical_markers += sum(
        1
        for key in state.get("keys", [])
        if key.get("category") == "canonical_marker"
    )
if canonical_markers < 1:
    errors.append("no canonical loop-suppression marker was retained")

if errors:
    for error in errors:
        print("ERROR:", error, file=sys.stderr)
    raise SystemExit(1)

print("Two-relay reflection invariant: PASS")
print("Classification: no_reflection_observed")
print("Generated cross-relay posts: 0")
print("Canonical markers retained:", canonical_markers)
PY

# EOF: contrib/ops/test_fep_ae0c_two_relay_probe.sh
