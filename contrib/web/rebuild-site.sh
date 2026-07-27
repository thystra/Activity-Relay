#!/bin/sh
# /etc/activity-relay-web/rebuild-site.sh
set -eu

source_dir="${ACTIVITY_RELAY_WEB_SOURCE:-/etc/activity-relay-web}"
config_file="${ACTIVITY_RELAY_WEB_CONFIG:-${source_dir}/site.json}"
output_dir="${ACTIVITY_RELAY_WEB_OUTPUT:-/var/www/activity-relay/public}"

exec env PYTHONDONTWRITEBYTECODE=1 \
    python3 "${source_dir}/build-site.py" \
    --source "${source_dir}" \
    --config "${config_file}" \
    --output "${output_dir}"
