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

- Server-actor follow and unfollow compatibility for NodeBB, Friendica,
  LitePub, and other `Application` or `Service` actors, while preserving
  `/relay` and `/friendica` legacy paths.
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
documents otherwise. Asynchronous work retains the
`github.com/RichardKnop/machinery/v2` import path and uses a Go `replace`
directive pinned to the Redis-only `github.com/thystra/machinery/v2` fork at
`v2.0.17-0.20260730145804-fd43623b7b5c`. The migration preserves the existing `relay` queue, delayed
retries, task-signature JSON, and result-state encoding.

## Requirements

Depending on the installation method:

- Docker and Docker Compose for container deployment;
- Ubuntu 24.04 or a compatible Debian-based system for the native package;
- Go and Redis for source builds;
- Python 3 for resource-guard tooling and optional website generation;
- Nginx, Apache, Caddy, or another reverse proxy for a public deployment.

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

Set the stable release image in `.env`:

```dotenv
ACTIVITY_RELAY_IMAGE=ghcr.io/thystra/activity-relay:2.4.0
```

Release candidates use their complete `-rcN` tag; prereleases do not move
`latest`, major, or major/minor stable tags.

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
# OBSERVABILITY_BIND: 0.0.0.0:9090
RELAY_DOMAIN: relay.example.org
RELAY_SERVICENAME: Community ActivityPub Relay
JOB_CONCURRENCY: 10

MAX_ACTIVITY_BYTES: 1048576
MAX_FANOUT_TARGETS: 5000
MAX_QUEUE_JOBS: 100000

# RELAY_SUMMARY: |
# Optional public relay branding. These are interoperability recommendations,
# not protocol limits; clients may crop or rescale the supplied images.
# RELAY_ICON: square logo/avatar; 512x512 px recommended (128x128 minimum).
# Keep important icon content centered and use a public HTTPS URL.
# RELAY_ICON: https://relay.example.org/assets/relay-icon.png
# RELAY_IMAGE: wide header/banner; 1500x500 px (3:1) recommended.
# Keep important banner content away from the outer edges.
# RELAY_IMAGE: https://relay.example.org/assets/relay-banner.webp
```

Use `127.0.0.1:8080` instead when the relay and reverse proxy run directly on
the same host.

`RELAY_ICON` and `RELAY_IMAGE` are optional public metadata URLs. The suggested
dimensions are compatibility-oriented recommendations rather than enforced
limits. A square icon is least likely to be distorted by clients, while a 3:1
banner matches common profile-header layouts. Clients may still resize or
center-crop either image, so keep identifying content away from the edges.

When no configuration file exists, these runtime values may be supplied as
environment variables:

```text
ACTOR_PEM
REDIS_URL
RELAY_BIND
OBSERVABILITY_BIND
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

## Observability listener

Observability is disabled when `OBSERVABILITY_BIND` is empty or unset. When it
is configured for the `server` command, Activity-Relay starts a separate HTTP
listener exposing only:

```text
GET /metrics
GET /-/healthy
GET /-/ready
```

`/-/healthy` is process-only liveness. `/-/ready` performs a bounded Redis ping
and returns `503 Service Unavailable` when the required runtime store is not
available. `/metrics` uses a private Prometheus registry and initially reports
Go and process metrics, build information, Redis readiness, and public API HTTP
request counts and durations. HTTP labels use a fixed route set, common methods,
and numeric response codes; raw paths, query strings, domains, and error text are
never labels.

For a native installation, prefer a loopback binding such as:

```yaml
OBSERVABILITY_BIND: 127.0.0.1:9090
```

For containers, bind to the private container network only and do not publish
the observability port on an untrusted interface. The bundled Compose file does
not publish an observability port. Do not forward these routes through the public
ActivityPub reverse proxy.

Example local checks:

```bash
curl --fail http://127.0.0.1:9090/-/healthy
curl --fail http://127.0.0.1:9090/-/ready
curl --fail http://127.0.0.1:9090/metrics
```

The worker command does not open this listener. When observability is enabled in
the shared configuration, API and worker processes write bounded operational
counters to Redis and the API process exports them through `/metrics`. The
operational surface includes activity outcomes, queue admission and depth,
fan-out targets, delivery results, Redis-operation failures, current receiver and
publisher counts, and aggregate receiver-health states.

Metric labels are closed enums. Domains, actor IDs, inbox URLs, activity IDs, raw
paths, query strings, response bodies, and error text are never labels. Complete
Redis outages are represented by readiness and collection-success metrics because
a failed Redis server cannot also persist its own failure counter.

## Operational summary scheduling

The native resource guard can send one or more reports per local day:

```yaml
DAILY_SUMMARY_EMAIL: true
DAILY_SUMMARY_TIMES:
  - "08:00"
  - "14:30"
MAIL_TIMEOUT_SECONDS: 60
```

The values are server-local, zero-padded 24-hour `HH:MM` times. The guard timer
runs approximately every five minutes, so mail is processed on the first timer
run at or after the configured time and may not arrive at the exact minute.

Each time is an independent daily slot. Changing or adding a time allows that
new slot to send even when another report was already sent that day. If
multiple slots were missed during downtime, only the most recent due slot is
sent. Its catch-up email lists skipped slots and the commands used to inspect
or reset them.

```bash
sudo activity-relay-resource-guard --show-summary-state
sudo activity-relay-resource-guard --preview-summary
sudo activity-relay-resource-guard --send-summary-now
sudo activity-relay-resource-guard   --reset-summary-slot "14:30"   --force
```

Successful report bodies are archived below
`/var/lib/activity-relay-guard/summaries/`. Skipped slots are recorded in
`/var/lib/activity-relay-guard/summary-slots.json`, but no historical report
body exists for a skipped time because no snapshot was captured. Resetting a
slot and running the guard sends current state rather than reconstructing past
state.

`--send-summary-now` does not consume a scheduled slot. Preview and `--no-mail`
runs do not consume slots. The deprecated `DAILY_SUMMARY_HOUR` remains
compatible as a single `HH:00` schedule.

The container image includes the administrative CLI, but Compose does not
schedule it or provide an MTA. Persist the guard state directory and provide a
mail transport when using it from a host scheduler or sidecar. Full native and
container notes are in
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
/actor/outbox
/actor/followers
/actor/following
/status.json
/.well-known/nodeinfo
/.well-known/webfinger
/nodeinfo/2.1
```

The public proxy should not forward `/metrics`, `/-/healthy`, or `/-/ready`;
those routes belong only to the separately bound observability listener.

The actor advertises `inbox`, `outbox`, `followers`, `following`, and
`endpoints.sharedInbox`. Public GET requests to the collection endpoints return
privacy-filtered empty `OrderedCollection` documents. The relay does not expose
subscriber identities or historical relayed activities, and it does not
implement ActivityPub client-to-server POSTs to the outbox.

The relay accepts standards-style server actors of type `Application` or
`Service` following the relay actor, regardless of whether their actor URL ends
in `/actor`, `/relay`, `/friendica`, or another implementation-defined path.
Legacy `/relay` and `/friendica` actors with incomplete type metadata remain
supported.

Outbound deliveries sign the same `Host` authority that is transmitted on the
wire, including a non-default port. Bounded non-success response text is
included in worker errors to make remote signature-verification failures
diagnosable without logging unbounded response bodies.

## Public Announce interoperability

Some ActivityPub servers, including NodeBB category actors, publish locally
created content as a public `Announce` containing an embedded `Article` or
`Note`. The relay replaces that transport wrapper with one relay-signed
`Announce` referencing the embedded object ID, then sends the same authenticated
wrapper to both traditional relay subscribers and follower-style subscribers.
This keeps the HTTP signer and JSON activity actor aligned for strict receivers
such as Mastodon while preserving the original NodeBB object and author when the
receiver fetches the referenced object.

Public `Announce` activities whose object is only a URL, or whose embedded
object belongs to another domain, remain publisher-accounting events and are
not fanned out. This preserves relay-to-relay loop protection and avoids
amplifying ordinary boosts.

Inbound request failures are logged with the HTTP method, path, remote address,
user agent, and the bounded verification or decoding error. Request bodies,
signatures, and key material are not logged.

See [`docs/INTEROPERABILITY.md`](docs/INTEROPERABILITY.md) for the validated
server matrix, NodeBB-specific behavior, and troubleshooting guidance.

## Relay public-key encoding

The relay actor publishes its RSA public key as X.509 SubjectPublicKeyInfo PEM,
using `-----BEGIN PUBLIC KEY-----`. This is the common interoperable form used
by ActivityPub implementations for `publicKeyPem`. Inbound signature
verification accepts both SubjectPublicKeyInfo and legacy PKCS#1
`-----BEGIN RSA PUBLIC KEY-----` actor keys.

Changing the public encoding does not rotate `actor.pem`, change the relay
actor ID, or change the `#main-key` key ID. Existing subscribers continue to
reference the same cryptographic identity.

## Public status endpoint

The API server exposes:

```text
GET /status.json
```

Schema version 4 reports relay identity and policy, endpoints, software version,
and three related domain views:

- `connected_instances`: the deduplicated set of domains that receive relay
  traffic, publish accepted activities, or do both;
- `receiving_instances`: the narrower set that receives fan-out, including
  delivery-health timestamps and counters for currently registered receivers;
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

After editing operator-owned settings or overrides, rebuild with the current
package-managed source:

```bash
sudo activity-relay-rebuild-site
```

A different web root can be selected explicitly:

```bash
sudo activity-relay-rebuild-site \
  --output /srv/www/relay.example.org
```

The package-managed command is authoritative for Debian installations. For a
source checkout or fully user-owned directories, use
`contrib/web/activity-relay-rebuild-site` with explicit paths.

### Apache frontend choices

A complete Apache 2.4 example is included at:

```text
contrib/apache/activity-relay.conf.example
```

The file lists required modules and includes bundled-site, no-frontend,
redirected-root, and custom-document-root guidance. When changing the output
directory, change both `DocumentRoot` and the corresponding `<Directory>` path.

### Caddy frontend choices

A complete optional Caddy 2 example and operator notes are included at:

```text
contrib/caddy/Caddyfile.example
contrib/caddy/README.md
```

The example uses Caddy's automatic HTTPS, serves the bundled static site, and
proxies only the required relay endpoints to `127.0.0.1:8080`. It is an
operator example only; Activity-Relay does not install, enable, or depend on
Caddy.

### Build the bundled site from a container image

Published images contain website sources at:

```text
/usr/share/activity-relay/web
```

The `2.4.0` image includes Python 3 for resource-guard tooling and website generation.
To customize the website outside the running relay, extract the sources:

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

Customize the source and generate it with the same release image:

```bash
docker run \
  --rm \
  --user "$(id -u):$(id -g)" \
  --entrypoint python3 \
  --volume "$PWD/activity-relay-web:/site:ro" \
  --volume "$PWD/activity-relay-public:/output" \
  "$ACTIVITY_RELAY_IMAGE" \
  /site/build-site.py \
    --source /site \
    --config /site/site.json \
    --output /output
```

Serve `activity-relay-public` with the host reverse proxy or a separate web
container.

Full website, Nginx, Apache, Caddy, customization, and replacement instructions are in
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

python3 -m unittest discover \
  -s contrib/ops \
  -p 'test_*.py'

docker rm -f activity-relay-test-redis
```

Also run:

```bash
git diff --check
```

Contributor and coding-agent expectations are documented in
[`AGENTS.md`](AGENTS.md). See [`ARCHITECTURE.md`](ARCHITECTURE.md) for component
and data-flow design, [`TODO.md`](TODO.md) for the maintained roadmap and
release gates, [`docs/INTEGRATION-TESTING.md`](docs/INTEGRATION-TESTING.md) for
the container and native-package validation matrix, and
[`docs/SECURITY.md`](docs/SECURITY.md) for the HTTP-signature compatibility
profile and RFC 9421 roadmap.

## Releases

Maintainer release steps are documented in
[`docs/RELEASING.md`](docs/RELEASING.md). Versioned release notes are kept under
[`docs/releases/`](docs/releases/), including
[`v2.4.0`](docs/releases/v2.4.0.md). Historical RC notes remain under the same directory.

## Upstream and attribution

This project is derived from:

- [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay)
- upstream baseline `v2.0.10`

Original authorship, commit history, license notices, and attribution are
retained. Generally useful fixes may be proposed upstream; the maintained fork
also publishes its own tested release line. Maintainers should follow
[`docs/UPSTREAM.md`](docs/UPSTREAM.md) when reviewing or porting future upstream
changes.

## License

GNU Affero General Public License version 3. See [`LICENCE`](LICENCE).

## Upgrade-safe website customization

The package separates current program files from operator-owned content:

```text
/usr/share/activity-relay/web/
    Current package-managed builder, templates, JavaScript, CSS, and defaults.

/etc/activity-relay/config.yml
    Relay behavior and shared public metadata:
    RELAY_ICON
    RELAY_IMAGE
    FEDIVERSE_OPERATOR_ID
    FEDIVERSE_OPERATOR_URL

/etc/activity-relay-web/site.json
    Website name, tagline, email/contact URL, source URL, language, and optional
    website-specific logo_url or banner_url overrides.

/etc/activity-relay-web/content/
    Optional operator replacements for:
    home.html
    about.html
    rules.html
    privacy.html
    footer.html

/etc/activity-relay-web/custom-assets/
    Optional replacement or additional public assets.

/var/www/activity-relay/public/
    Generated output. Do not edit it directly.
```

To customize rules without copying the whole website source:

```bash
sudo install -d -o root -g root -m 0755 \
  /etc/activity-relay-web/content

sudo cp \
  /usr/share/activity-relay/web/content/rules.html \
  /etc/activity-relay-web/content/rules.html

sudoedit /etc/activity-relay-web/content/rules.html
```

Use the same pattern for `home.html`, `about.html`, `privacy.html`, or
`footer.html`. Files not present in the override directory come from the
current package, so package fixes remain effective.

Set shared branding and the public operator handle in
`/etc/activity-relay/config.yml`:

```yaml
RELAY_ICON: "https://relay.example.org/images/relay-icon.png"
RELAY_IMAGE: "https://relay.example.org/images/relay-banner.png"
FEDIVERSE_OPERATOR_ID: "@operator@social.example"
# Optional because profile URL patterns differ among fediverse applications:
FEDIVERSE_OPERATOR_URL: "https://social.example/@operator"
```

The canonical configuration names use underscores. The builder also accepts
the legacy hyphenated aliases `FEDIVERSE-OPERATOR-ID` and
`FEDIVERSE-OPERATOR-URL`. The website-specific `logo_url` and `banner_url`
values in `site.json` take precedence when non-empty.

Rebuild with the current package source:

```bash
sudo activity-relay-rebuild-site
```

An older regular file at `/etc/activity-relay-web/rebuild-site.sh` may be
preserved during upgrades. The package-managed command above is authoritative.
