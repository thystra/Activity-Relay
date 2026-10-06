# Installing Activity-Relay

This guide covers first installation. Day-to-day commands and configuration
management are in [`ADMINISTRATION.md`](ADMINISTRATION.md).

## Choose a deployment method

Activity-Relay supports:

- Docker and Docker Compose;
- the native Debian/Ubuntu package; or
- a source build for development and custom deployments.

A public deployment also needs a reverse proxy such as Nginx, Apache, or Caddy.
Redis is required by the relay runtime; the native package provides a dedicated
Redis instance.

## Container deployment

Published images use:

```text
ghcr.io/thystra/activity-relay:<version>
```

Copy the examples from the repository:

```bash
cp .env.example .env
cp config.yml.example config.yml
```

Select the release in `.env`:

```dotenv
ACTIVITY_RELAY_IMAGE=ghcr.io/thystra/activity-relay:<version>
```

Release candidates use their complete `-rcN` tag.

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

Back up `actor.pem`. Replacing this file changes the relay's cryptographic
identity.

Before starting Compose, confirm both bind-mounted files exist and are regular
files:

```bash
test -f actor.pem
test -f config.yml
contrib/docker/compose-preflight.sh "$PWD"
```

Validate the Compose configuration and start the stack:

```bash
docker compose config
docker compose up -d
```

The bundled stack runs Redis, workers, and the API server. The API is published
on loopback by default for a host reverse proxy. Adjust
`RELAY_PUBLISH_ADDRESS` or `RELAY_HTTP_PORT` in `.env` when needed.

On Linux hosts running Redis in Docker, enable memory overcommit:

```bash
sudo sysctl -w vm.overcommit_memory=1
printf 'vm.overcommit_memory=1\n' |
  sudo tee /etc/sysctl.d/99-activity-relay-redis.conf
```

Initial checks:

```bash
docker compose ps
docker compose logs --tail=100 server worker redis

curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

To build from the current checkout rather than a published image:

```bash
docker compose \
  -f compose.yml \
  -f compose.build.yml \
  up -d --build
```

## Native Debian/Ubuntu package

Canonical package artifacts are published by the authoritative Forgejo release
process. Install the downloaded package with:

```bash
sudo apt install ./activity-relay_VERSION_amd64.deb
```

The package intentionally does not invent a public hostname or enable network
services. It:

- creates `/etc/activity-relay/actor.pem` if it does not already exist;
- installs `/etc/activity-relay/config.yml.example` but leaves the live
  `config.yml` operator-owned;
- provides server, worker, dedicated Redis, and resource-guard units; and
- preserves identity, local configuration, website content, and Redis data
  across upgrades and removal.

Create the live configuration:

```bash
sudo cp /etc/activity-relay/config.yml.example /etc/activity-relay/config.yml
sudo chown root:activity-relay /etc/activity-relay/config.yml
sudo chmod 0640 /etc/activity-relay/config.yml
sudoedit /etc/activity-relay/config.yml
```

Validate it:

```bash
sudo -u activity-relay \
  /usr/bin/relay \
  --config /etc/activity-relay/config.yml \
  --test-config
```

Then enable the native services:

```bash
sudo systemctl enable --now activity-relay-server.service
sudo systemctl enable --now activity-relay-worker.service
sudo systemctl enable --now activity-relay-resource-guard.timer
```

Starting the API server or worker also starts the package's dedicated Redis
service, normally on `127.0.0.1:6380`.

Package-specific notes are installed as:

```text
/usr/share/doc/activity-relay/README.Debian
```

## Source build

For a tagged release:

```bash
VERSION=<version>
git checkout "v${VERSION}"
mkdir -p build

go build \
  -trimpath \
  -ldflags="-s -w -X main.version=${VERSION}" \
  -o build/relay \
  .
```

For a development checkout:

```bash
mkdir -p build

go build \
  -trimpath \
  -ldflags="-X main.version=$(git describe --tags --always --dirty | sed 's/^v//')" \
  -o build/relay \
  .
```

Development test dependencies and disposable Redis instructions are in
[`DEVELOPMENT.md`](DEVELOPMENT.md).

## Public reverse proxy

A public reverse proxy should forward these routes to the API server:

```text
/inbox
/actor
/actor/outbox
/actor/followers
/actor/following
/status.json
/.well-known/nodeinfo
/.well-known/webfinger
/nodeinfo/2.1
```

Do **not** forward the private observability routes through the public relay
virtual host:

```text
/metrics
/-/healthy
/-/ready
```

The repository includes optional Nginx, Apache, and Caddy examples. The public
website itself is optional; the ActivityPub endpoints still need to be routed.

## First-run verification

Check the running relay locally:

```bash
curl --fail http://127.0.0.1:8080/actor >/dev/null
curl --fail http://127.0.0.1:8080/nodeinfo/2.1 >/dev/null
curl --fail http://127.0.0.1:8080/status.json |
  python3 -m json.tool
```

If `OBSERVABILITY_BIND` is enabled, also check:

```bash
curl --fail http://127.0.0.1:9090/-/healthy
curl --fail http://127.0.0.1:9090/-/ready
```

Continue with [`ADMINISTRATION.md`](ADMINISTRATION.md) for configuration,
service management, Directory commands, observability, and maintenance.
