#!/usr/bin/env bash
set -Eeuo pipefail
umask 022

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${1:?usage: build-debian-artifacts.sh OUTPUT_DIR}"
mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"

for command in \
    date dpkg dpkg-buildpackage dpkg-deb dpkg-parsechangelog go lintian \
    python3 sha256sum tar; do
    command -v "$command" >/dev/null || {
        echo "missing release-build command: $command" >&2
        exit 1
    }
done

DEB_VERSION="$(dpkg-parsechangelog -l"$ROOT/debian/changelog" -SVersion)"
PACKAGE="$(dpkg-parsechangelog -l"$ROOT/debian/changelog" -SSource)"
ARCH="$(dpkg --print-architecture)"
DEB_UPSTREAM="${DEB_VERSION%-*}"
APP_VERSION="${DEB_UPSTREAM//\~/-}"
PUBLIC_DEB_VERSION="${DEB_VERSION//\~/-}"
SOURCE_DATE_EPOCH="$(
    date -u -d "$(dpkg-parsechangelog -l"$ROOT/debian/changelog" -SDate)" +%s
)"
export SOURCE_DATE_EPOCH
export GOTOOLCHAIN=local

if [[ "$(go env GOVERSION)" != "go1.25.0" ]]; then
    echo "release artifacts require exact Go 1.25.0; got $(go env GOVERSION)" >&2
    exit 1
fi

SOURCE_IDENTITY="${RELEASE_COMMIT:-uncommitted-validation}:${RELEASE_TREE:-unknown-tree}"
DPKG_DEB_VERSION="$(dpkg-deb --version)"
DPKG_DEB_VERSION="${DPKG_DEB_VERSION%%$'\n'*}"

TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

mkdir -p "$TMP/src" "$OUT/public" "$OUT/evidence"
tar -C "$ROOT" \
    --exclude=.git \
    --exclude='__pycache__' \
    --exclude='*.pyc' \
    --exclude='*.pyo' \
    --exclude='./*.deb' \
    --exclude='./*.changes' \
    --exclude='./*.buildinfo' \
    --exclude='./dist' \
    -cf - . | tar -C "$TMP/src" -xf -

(
    cd "$TMP/src"
    dpkg-buildpackage -us -uc -b -d
)

DEB="$TMP/${PACKAGE}_${DEB_VERSION}_${ARCH}.deb"
BUILDINFO="$(find "$TMP" -maxdepth 1 -type f -name "${PACKAGE}_*.buildinfo" -print -quit)"
CHANGES="$(find "$TMP" -maxdepth 1 -type f -name "${PACKAGE}_*.changes" -print -quit)"
[[ -f "$DEB" && -f "$BUILDINFO" && -f "$CHANGES" ]]

PUBLIC_DEB="$OUT/public/${PACKAGE}_${PUBLIC_DEB_VERSION}_${ARCH}.deb"
cp -a "$DEB" "$PUBLIC_DEB"
cp -a "$BUILDINFO" "$OUT/evidence/"
cp -a "$CHANGES" "$OUT/evidence/"

EXTRACT="$TMP/extract"
CONTROL="$OUT/evidence/control"
mkdir -p "$EXTRACT" "$CONTROL"
dpkg-deb -x "$PUBLIC_DEB" "$EXTRACT"
dpkg-deb --control "$PUBLIC_DEB" "$CONTROL"

BIN="$EXTRACT/usr/bin/relay"
[[ -x "$BIN" ]]
[[ "$("$BIN" --version)" == "relay version $DEB_VERSION" ]]

(
    cd "$TMP/src"
    python3 scripts/release/generate-cyclonedx.py \
        --version "$APP_VERSION" \
        --debian-version "$DEB_VERSION" \
        --binary "$BIN" \
        --source-date-epoch "$SOURCE_DATE_EPOCH" \
        --source-identity "$SOURCE_IDENTITY" \
        --output "$OUT/public/${PACKAGE}_${APP_VERSION}_${ARCH}.cdx.json"
)

dpkg-deb --info "$PUBLIC_DEB" >"$OUT/evidence/dpkg-deb-info.txt"
dpkg-deb --contents "$PUBLIC_DEB" >"$OUT/evidence/dpkg-deb-contents.txt"

if find "$EXTRACT" \
    \( -type d -name __pycache__ -o -type f \( -name '*.pyc' -o -name '*.pyo' \) \) \
    -print -quit | grep -q .; then
    echo "canonical Debian package contains generated Python bytecode/cache artifacts" >&2
    exit 1
fi

# Debian bookworm ships Lintian 2.116.3, which is affected by Debian #1019690:
# --show-overrides can make an overridden error satisfy --fail-on error. Keep
# the gating pass free of override display, then collect overrides separately as
# non-gating release evidence.
{
    echo "lintian_version=$(lintian --print-version)"
    echo "lintian_gate=--fail-on error"
} >"$OUT/evidence/lintian.txt"

set +e
lintian --allow-root --fail-on error "$PUBLIC_DEB" \
    >>"$OUT/evidence/lintian.txt" 2>&1
LINTIAN_RC=$?
set -e
if (( LINTIAN_RC != 0 )); then
    cat "$OUT/evidence/lintian.txt" >&2
    echo "lintian error or runtime failure: rc=$LINTIAN_RC" >&2
    exit 1
fi
echo "lintian_exit_code=$LINTIAN_RC" >>"$OUT/evidence/lintian.txt"

{
    echo
    echo "--- lintian overrides (informational; non-gating) ---"
    lintian --allow-root --show-overrides --fail-on none "$PUBLIC_DEB" || true
} >>"$OUT/evidence/lintian.txt" 2>&1

{
    echo "package=$PACKAGE"
    echo "application_version=$APP_VERSION"
    echo "debian_version=$DEB_VERSION"
    echo "architecture=$ARCH"
    echo "source_date_epoch=$SOURCE_DATE_EPOCH"
    echo "source_identity=$SOURCE_IDENTITY"
    echo "go_version=$(go version)"
    echo "go_env_GOVERSION=$(go env GOVERSION)"
    echo "dpkg_deb_version=$DPKG_DEB_VERSION"
    echo "deb_sha256=$(sha256sum "$PUBLIC_DEB" | awk '{print $1}')"
    echo "packaged_binary_sha256=$(sha256sum "$BIN" | awk '{print $1}')"
    echo "packaged_binary_version=$("$BIN" --version)"
} >"$OUT/public/BUILD-METADATA.txt"

(
    cd "$OUT/public"
    rm -f SHA256SUMS
    find . -maxdepth 1 -type f ! -name SHA256SUMS -printf '%P\0' |
        sort -z |
        xargs -0 sha256sum >SHA256SUMS
    sha256sum -c SHA256SUMS
)

echo "public_artifacts=$OUT/public"
echo "build_evidence=$OUT/evidence"
