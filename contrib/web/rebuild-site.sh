#!/bin/sh
# /etc/activity-relay-web/rebuild-site.sh
set -eu

usage() {
    cat <<'EOF'
Usage: rebuild-site.sh [OPTIONS]

Generate the optional Activity-Relay static website.

Options:
  --source DIR     Website source directory.
  --config FILE    Website JSON configuration file.
  --output DIR     Generated-site output directory.
  -h, --help       Show this help.

Environment defaults:
  ACTIVITY_RELAY_WEB_SOURCE
  ACTIVITY_RELAY_WEB_CONFIG
  ACTIVITY_RELAY_WEB_OUTPUT
EOF
}

source_dir="${ACTIVITY_RELAY_WEB_SOURCE:-/etc/activity-relay-web}"
config_file="${ACTIVITY_RELAY_WEB_CONFIG:-}"
output_dir="${ACTIVITY_RELAY_WEB_OUTPUT:-/var/www/activity-relay/public}"

while [ "$#" -gt 0 ]; do
    case "$1" in
        --source)
            [ "$#" -ge 2 ] || {
                printf 'ERROR: --source requires a directory\n' >&2
                exit 2
            }
            source_dir="$2"
            shift 2
            ;;
        --source=*)
            source_dir="${1#*=}"
            shift
            ;;
        --config)
            [ "$#" -ge 2 ] || {
                printf 'ERROR: --config requires a file\n' >&2
                exit 2
            }
            config_file="$2"
            shift 2
            ;;
        --config=*)
            config_file="${1#*=}"
            shift
            ;;
        --output)
            [ "$#" -ge 2 ] || {
                printf 'ERROR: --output requires a directory\n' >&2
                exit 2
            }
            output_dir="$2"
            shift 2
            ;;
        --output=*)
            output_dir="${1#*=}"
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf 'ERROR: unknown option: %s\n\n' "$1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

if [ -z "$config_file" ]; then
    config_file="${source_dir}/site.json"
fi

exec env PYTHONDONTWRITEBYTECODE=1 \
    python3 "${source_dir}/build-site.py" \
    --source "${source_dir}" \
    --config "${config_file}" \
    --output "${output_dir}"
