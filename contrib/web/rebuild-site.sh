#!/bin/sh
# contrib/web/rebuild-site.sh
set -eu

script_dir="$(
    CDPATH= cd -- "$(dirname -- "$0")" &&
    pwd
)"

ACTIVITY_RELAY_WEB_SOURCE="${ACTIVITY_RELAY_WEB_SOURCE:-$script_dir}" \
exec "${script_dir}/activity-relay-rebuild-site" "$@"
