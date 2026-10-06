#!/usr/bin/env bash
set -Eeuo pipefail
umask 022

ROOT="${1:?usage: package-canonical-bundle.sh CANONICAL_RELEASE_DIR OUTPUT_TAR}"
OUTPUT="${2:?usage: package-canonical-bundle.sh CANONICAL_RELEASE_DIR OUTPUT_TAR}"

ROOT="$(cd "$ROOT" && pwd)"
mkdir -p "$(dirname "$OUTPUT")"
OUTPUT_DIR="$(cd "$(dirname "$OUTPUT")" && pwd)"
OUTPUT="$OUTPUT_DIR/$(basename "$OUTPUT")"

for c in awk find grep mktemp mv sha256sum stat tar wc; do
    command -v "$c" >/dev/null || {
        echo "missing canonical-bundle command: $c" >&2
        exit 1
    }
done

TAR_VERSION="$(tar --version 2>/dev/null || true)"
[[ "$TAR_VERSION" == *"GNU tar"* ]] || {
    echo "canonical bundle requires GNU tar" >&2
    exit 1
}

[[ -d "$ROOT/public" && -d "$ROOT/evidence" ]]
[[ -f "$ROOT/public/BUILD-METADATA.txt" ]]
[[ -f "$ROOT/public/SHA256SUMS" ]]

metadata_value() {
    local key="$1"
    local value count
    count="$(grep -Ec "^${key}=.*$" "$ROOT/public/BUILD-METADATA.txt" || true)"
    [[ "$count" == "1" ]] || {
        echo "BUILD-METADATA.txt must contain exactly one ${key}= entry" >&2
        exit 1
    }
    value="$(awk -F= -v key="$key" '$1 == key { sub(/^[^=]*=/, ""); print; exit }' "$ROOT/public/BUILD-METADATA.txt")"
    [[ -n "$value" ]] || {
        echo "BUILD-METADATA.txt has an empty ${key}= entry" >&2
        exit 1
    }
    printf '%s\n' "$value"
}

PACKAGE="$(metadata_value package)"
APP_VERSION="$(metadata_value application_version)"
DEB_VERSION="$(metadata_value debian_version)"
ARCH="$(metadata_value architecture)"
SOURCE_DATE_EPOCH="$(metadata_value source_date_epoch)"
PUBLIC_DEB_VERSION="${DEB_VERSION//\~/-}"

[[ "$SOURCE_DATE_EPOCH" =~ ^[0-9]+$ ]] || {
    echo "invalid source_date_epoch: $SOURCE_DATE_EPOCH" >&2
    exit 1
}

PUBLIC_DEB="${PACKAGE}_${PUBLIC_DEB_VERSION}_${ARCH}.deb"
PUBLIC_SBOM="${PACKAGE}_${APP_VERSION}_${ARCH}.cdx.json"
PUBLIC_OCI="${PACKAGE}_${APP_VERSION}_linux_amd64_arm64.oci.tar"

for asset in \
    "$PUBLIC_DEB" \
    "$PUBLIC_SBOM" \
    "$PUBLIC_OCI" \
    BUILD-METADATA.txt \
    RELEASE-NOTES.md; do
    [[ -f "$ROOT/public/$asset" ]] || {
        echo "canonical public asset is missing: public/$asset" >&2
        exit 1
    }
done

if [[ -n "$(find "$ROOT/public" "$ROOT/evidence" -type l -print -quit)" ]]; then
    echo "canonical release tree must not contain symlinks" >&2
    exit 1
fi

(
    cd "$ROOT/public"
    [[ "$(find . -maxdepth 1 -type f ! -name SHA256SUMS | wc -l)" -eq 5 ]]
    [[ "$(wc -l < SHA256SUMS)" -eq 5 ]]
    sha256sum -c SHA256SUMS
)

TMP_OUTPUT="$(mktemp "${OUTPUT}.tmp.XXXXXX")"
VERIFY_DIR="$(mktemp -d)"
cleanup() {
    rm -f "$TMP_OUTPUT"
    rm -rf "$VERIFY_DIR"
}
trap cleanup EXIT

(
    cd "$ROOT"
    tar \
        --sort=name \
        --format=gnu \
        --mtime="@${SOURCE_DATE_EPOCH}" \
        --owner=0 \
        --group=0 \
        --numeric-owner \
        -cf "$TMP_OUTPUT" \
        evidence public
)

# Forgejo's outer artifact wrapper is transport only and may rewrite its own
# entry metadata. Re-extract this deterministic carrier and verify the canonical
# public bytes before allowing it to become the retained release artifact.
tar -xf "$TMP_OUTPUT" -C "$VERIFY_DIR"
(
    cd "$VERIFY_DIR/public"
    [[ "$(find . -maxdepth 1 -type f ! -name SHA256SUMS | wc -l)" -eq 5 ]]
    [[ "$(wc -l < SHA256SUMS)" -eq 5 ]]
    sha256sum -c SHA256SUMS
)
for asset in \
    "$PUBLIC_DEB" \
    "$PUBLIC_SBOM" \
    "$PUBLIC_OCI" \
    BUILD-METADATA.txt \
    RELEASE-NOTES.md; do
    [[ -f "$VERIFY_DIR/public/$asset" ]] || {
        echo "transport bundle omitted canonical public asset: public/$asset" >&2
        exit 1
    }
done
while IFS= read -r -d '' asset; do
    [[ "$(stat -c %Y "$asset")" == "$SOURCE_DATE_EPOCH" ]] || {
        echo "transport bundle member has unexpected mtime: $asset" >&2
        exit 1
    }
done < <(find "$VERIFY_DIR/public" "$VERIFY_DIR/evidence" -type f -print0)

mv -f "$TMP_OUTPUT" "$OUTPUT"
trap - EXIT
rm -rf "$VERIFY_DIR"

echo "canonical_bundle=$OUTPUT"
echo "canonical_bundle_sha256=$(sha256sum "$OUTPUT" | awk '{print $1}')"
echo "canonical_bundle_source_date_epoch=$SOURCE_DATE_EPOCH"
