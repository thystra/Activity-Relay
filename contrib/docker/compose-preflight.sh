#!/usr/bin/env sh
# contrib/docker/compose-preflight.sh
set -eu

project_dir="${1:-.}"

cd "$project_dir"

fail() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 1
}

[ -f .env ] ||
    fail ".env is missing; copy .env.example to .env and customize it"

[ -f config.yml ] ||
    fail "config.yml is missing; copy config.yml.example and customize it"

[ -e actor.pem ] ||
    fail "actor.pem is missing; generate it before starting Compose"

[ ! -d actor.pem ] ||
    fail "actor.pem is a directory; remove it and generate a key file"

[ -f actor.pem ] ||
    fail "actor.pem exists but is not a regular file"

[ -s actor.pem ] ||
    fail "actor.pem exists but is empty"

[ -r actor.pem ] ||
    fail "actor.pem is not readable"

[ -r config.yml ] ||
    fail "config.yml is not readable"

case "$(stat -c '%a' actor.pem 2>/dev/null || true)" in
    600|400) ;;
    *)
        printf 'WARNING: actor.pem should normally use mode 0600 or 0400\n' >&2
        ;;
esac

printf 'Compose preflight passed.\n'
