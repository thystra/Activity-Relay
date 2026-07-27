# Activity Relay Server

## A maintained and deployable ActivityPub relay written in Go

[![GitHub Actions](https://github.com/thystra/Activity-Relay/workflows/Test/badge.svg)](https://github.com/thystra/Activity-Relay/actions)

<img width="1672" height="941" alt="0f3f8ae2-a325-4a2e-9ace-b30fbc79230c" src="https://github.com/user-attachments/assets/bc4f95b0-bd69-4eb4-a2d2-b81ae07fd1ff" />


> [!NOTE]
> This repository is a maintained fork of
> [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay),
> based on upstream release `v2.0.10`.

## Highlights

Compared with the upstream baseline, this fork includes:

- Friendica relay follow and unfollow compatibility, including `/friendica`
  server-actor paths.
- Acceptance of valid HTTP-signed public activities from unsubscribed
  publishers, including WordPress ActivityPub sites.
- Publisher first-seen, last-seen, activity-type, and accepted-activity
  counters.
- `/status.json` schema version 3 with:
  - `connected_instances`: all unique participating domains;
  - `receiving_instances`: domains that receive relay fan-out;
  - `publishers`: observed sending domains and their roles.
- A tested Redis-backed fan-out pipeline with bounded queue and response
  controls.
- Multi-architecture `linux/amd64` and `linux/arm64` container releases on GHCR.
- Native Ubuntu 24.04 `amd64` Debian packages with systemd units, a dedicated
  Redis instance, operational resource monitoring, and upgrade-safe identity
  preservation.
- An optional generated public website. Operators may use it, replace it,
  redirect it, or serve no frontend at all.

See [`CHANGELOG.md`](CHANGELOG.md) for release details.

## Module and compatibility

The maintained fork uses:

```text
github.com/thystra/Activity-Relay
```

Existing ActivityPub endpoints, YAML settings, environment variables, Redis
state, and control commands remain compatible unless a release explicitly
documents otherwise. The `github.com/yukimochi/machinery-v1` module remains an
external dependency inherited from the original project.

## Requirements

Depending on the installation method:

- Docker and Docker Compose for container deployment;
- Ubuntu 24.04 or a compatible Debian-based system for the native package;
- Go and Redis for source builds;
- Python 3 only when generating the optional bundled website;
- Nginx, Apache, or another reverse proxy for a public deployment.

## Installation

### Container deployment

Stable releases and release candidates are published to:

```text
ghcr.io/thystra/activity-relay
```

Copy the examples:

```bash
cp .env.example .env
cp config.yml.example config.yml
```

Set a release image in `.env`, for example:

```dotenv
ACTIVITY_RELAY_IMAGE=ghcr.io/thystra/activity-relay:2.4.0
```

Release candidates use their complete tag, such as `2.4.0-rc4`; prereleases do
not move `latest`.

Generate the actor identity once:

```bash
docker run \
  --rm \
  --user "$(id -u):$(id -g)" \
  --volume "$PWD:/work" \
  "$ACTIVITY_RELAY_IMAGE" \
  generate-key \
  --output /work/actor.pem
```

Back up `actor.pem`. Replacing it changes the relay's cryptographic identity.

Before starting Compose, verify that both required bind mounts are regular
files:

```bash
test -f actor.pem
test -f config.yml
contrib/docker/compose-preflight.sh "$PWD"
```

Do not run `docker compose up` with a missing `actor.pem`. Older Compose
configurations may create a directory named `actor.pem`, which the relay cannot
read as a private-key file. The included Compose file disables that automatic
directory creation and fails early when either required file is absent.


Validate the resolved configuration and start the relay:

```bash
docker compose config
docker compose up -d
```

The Compose deployment pulls the image selected by
`ACTIVITY_RELAY_IMAGE`, runs Redis, two workers, and the API server, and
publishes the API on `127.0.0.1:8080` by default for a host reverse proxy.
Change `RELAY_PUBLISH_ADDRESS` or `RELAY_HTTP_PORT` in `.env` when needed.

On Linux hosts running Redis in Docker, enable memory overcommit to avoid
background-save failures under memory pressure:

```bash
sudo sysctl -w vm.overcommit_memory=1

printf 'vm.overcommit_memory=1
' |
  sudo tee /etc/sysctl.d/99-activity-relay-redis.conf
```


Inspect the deployment:

```bash
docker compose ps
docker compose logs --tail=100 server worker redis

curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

To build from the current checkout instead of using a published image:

```bash
docker compose \
  -f compose.yml \
  -f compose.build.yml \
  up -d --build
```

Verify an image:

```bash
docker run \
  --rm \
  ghcr.io/thystra/activity-relay:2.4.0 \
  --version
```

The image uses `/usr/bin/relay` as its entrypoint.

### Native Debian/Ubuntu package

Tagged releases attach an Ubuntu 24.04 `amd64` package and `SHA256SUMS` to the
GitHub Release.

Install a downloaded package:

```bash
sudo apt install ./activity-relay_VERSION_amd64.deb
```

The package:

- generates `/etc/activity-relay/actor.pem` without replacing an existing key;
- installs an example configuration but does not create an active `config.yml`;
- installs inactive-by-default server, worker, Redis, and resource-guard units;
- does not enable a web-server configuration;
- preserves actor identity, local configuration, website content, and Redis
  data during upgrades and removal.

Continue with:

```text
/usr/share/doc/activity-relay/README.Debian
```

### Source build

For a tagged stable build:

```bash
VERSION=2.4.0

git checkout "v${VERSION}"
mkdir -p build

go build \
  -trimpath \
  -ldflags="-s -w -X main.version=${VERSION}" \
  -o build/relay \
  .
```

For a development build:

```bash
mkdir -p build

go build \
  -trimpath \
  -ldflags="-X main.version=$(git describe --tags --always --dirty | sed 's/^v//')" \
  -o build/relay \
  .
```

## Running the relay

API server:

```bash
relay --config /path/to/config.yml server
```

Worker:

```bash
relay --config /path/to/config.yml worker
```

Management CLI:

```bash
relay --config /path/to/config.yml control
```

Version:

```bash
relay --version
```

## Configuration

A minimal container-oriented YAML configuration:

```yaml
ACTOR_PEM: /var/lib/relay/actor.pem
REDIS_URL: redis://redis:6379

RELAY_BIND: 0.0.0.0:8080
RELAY_DOMAIN: relay.example.org
RELAY_SERVICENAME: Community ActivityPub Relay
JOB_CONCURRENCY: 10

MAX_ACTIVITY_BYTES: 1048576
MAX_FANOUT_TARGETS: 5000
MAX_QUEUE_JOBS: 100000

# RELAY_SUMMARY: |
#   A community-operated ActivityPub relay.
# RELAY_ICON: https://relay.example.org/assets/icon.png
# RELAY_IMAGE: https://relay.example.org/assets/banner.png
```

Use `127.0.0.1:8080` instead when the relay and reverse proxy run directly on
the same host.

When no configuration file exists, these runtime values may be supplied as
environment variables:

```text
ACTOR_PEM
REDIS_URL
RELAY_BIND
RELAY_DOMAIN
RELAY_SERVICENAME
JOB_CONCURRENCY
MAX_ACTIVITY_BYTES
MAX_FANOUT_TARGETS
MAX_QUEUE_JOBS
RELAY_SUMMARY
RELAY_ICON
RELAY_IMAGE
```

Operational storage, cache, mail, and daily-summary settings are documented in
[`contrib/ops/README.md`](contrib/ops/README.md).

## Federation endpoints

Mastodon, Misskey, and compatible software subscribe to:

```text
https://relay.example.org/inbox
```

Pleroma, Akkoma, Friendica, and compatible software follow:

```text
https://relay.example.org/actor
```

A public reverse proxy should forward these relay routes:

```text
/inbox
/actor
/status.json
/.well-known/nodeinfo
/.well-known/webfinger
/nodeinfo/2.1
```

## Public status endpoint

The API server exposes:

```text
GET /status.json
```

Schema version 3 reports relay identity and policy, endpoints, software version,
and three related domain views:

- `connected_instances`: the deduplicated set of domains that receive relay
  traffic, publish accepted activities, or do both;
- `receiving_instances`: the narrower set that receives fan-out;
- `publishers`: observed sending domains, last-seen metadata, activity count,
  and whether each domain also receives the relay.

Example:

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

The endpoint does not expose Redis keys, queue internals, blocked-domain lists,
actor IDs, inbox URLs, or private configuration.

Open publisher ingestion still enforces signature validation, actor/key-host
matching, blocked and limited-domain policy, and person-only policy.

## Optional public website

The bundled frontend is optional and has no effect on relay operation.
Operators may:

1. use the bundled generated site;
2. serve a custom site that reads `/status.json`;
3. redirect the root page elsewhere;
4. return `404` for all non-relay paths.

### Disable the frontend with Nginx

Keep the exact relay endpoint locations, and use:

```nginx
location / {
    return 404;
}
```

### Redirect the root page

```nginx
location = / {
    return 302 https://example.org/about-this-relay;
}

location / {
    return 404;
}
```

### Custom status page

A same-origin custom page can request:

```javascript
fetch("/status.json")
  .then((response) => response.json())
  .then((status) => {
    document.querySelector("#server-count").textContent =
      status.connected_instances.count;
  });
```

When using the sample Content Security Policy, place JavaScript in a separate
same-origin file. A ready-to-copy example is included under:

```text
contrib/web/examples/
```

### Build the bundled site on a native installation

The Debian package installs website sources in
`/usr/share/activity-relay/web`. Keep the operator-owned editable copy in
`/etc/activity-relay-web` and generated output in
`/var/www/activity-relay/public`.

After editing the site:

```bash
sudo /etc/activity-relay-web/rebuild-site.sh
```

A different web root can be selected explicitly:

```bash
sudo /etc/activity-relay-web/rebuild-site.sh   --output /srv/www/relay.example.org
```

The same wrapper can run without root when its source, configuration, and output
directories are user-owned.

### Apache frontend choices

A complete Apache 2.4 example is included at:

```text
contrib/apache/activity-relay.conf.example
```

The file lists required modules and includes bundled-site, no-frontend,
redirected-root, and custom-document-root guidance. When changing the output
directory, change both `DocumentRoot` and the corresponding `<Directory>` path.

### Build the bundled site from a container image

Published images contain website sources at:

```text
/usr/share/activity-relay/web
```

They intentionally do not include Python. Extract the sources:

```bash
export ACTIVITY_RELAY_IMAGE='ghcr.io/thystra/activity-relay:2.4.0'

mkdir -p \
  activity-relay-web \
  activity-relay-public

container_id="$(docker create "$ACTIVITY_RELAY_IMAGE")"

docker cp \
  "$container_id:/usr/share/activity-relay/web/." \
  ./activity-relay-web/

docker rm "$container_id"

cp -n \
  ./activity-relay-web/site.json.example \
  ./activity-relay-web/site.json
```

Customize the source and generate it with a temporary Python container:

```bash
docker run \
  --rm \
  --user "$(id -u):$(id -g)" \
  --volume "$PWD/activity-relay-web:/site:ro" \
  --volume "$PWD/activity-relay-public:/output" \
  python:3.13-alpine \
  python3 /site/build-site.py \
    --source /site \
    --config /site/site.json \
    --output /output
```

Serve `activity-relay-public` with the host reverse proxy or a separate web
container.

Full website, Nginx, Apache, customization, and replacement instructions are in
[`contrib/web/README.md`](contrib/web/README.md).

## Testing

Do not run tests against production Redis. One disposable Docker-based test
run is:

```bash
docker rm -f activity-relay-test-redis \
  >/dev/null 2>&1 || true

docker run \
  --detach \
  --rm \
  --name activity-relay-test-redis \
  --publish 127.0.0.1:6381:6379 \
  redis:7-alpine

until docker exec activity-relay-test-redis redis-cli ping |
  grep -qx PONG
do
  sleep 1
done

REDIS_URL='redis://127.0.0.1:6381' \
  go test -count=1 -p 1 ./...

go vet ./...

python3 -m unittest discover \
  -s contrib/web \
  -p 'test_*.py'

docker rm -f activity-relay-test-redis
```

Also run:

```bash
git diff --check
```

Contributor and coding-agent expectations are documented in
[`AGENTS.md`](AGENTS.md).

## Releases

Maintainer release steps are documented in
[`docs/RELEASING.md`](docs/RELEASING.md).

## Upstream and attribution

This project is derived from:

- [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay)
- upstream baseline `v2.0.10`

Original authorship, commit history, license notices, and attribution are
retained. Generally useful fixes may be proposed upstream; the maintained fork
also publishes its own tested release line.

## License

GNU Affero General Public License version 3. See [`LICENCE`](LICENCE).
