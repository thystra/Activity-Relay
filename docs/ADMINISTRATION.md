# Activity-Relay operator manual

This document is the day-to-day administration and CLI reference. Installation
is covered separately in [`INSTALLATION.md`](INSTALLATION.md).

Examples below use the native-package configuration path:

```text
/etc/activity-relay/config.yml
```

Substitute the path used by your deployment.

## Validate configuration

Check YAML and runtime configuration without contacting a Directory or mutating
Redis state:

```bash
relay \
  --config /etc/activity-relay/config.yml \
  --test-config
```

Use `--strict` when optional Directory-profile warnings should fail validation:

```bash
relay \
  --config /etc/activity-relay/config.yml \
  --test-config \
  --strict
```

## Main commands

Show the application version:

```bash
relay --version
```

Run the API server:

```bash
relay --config /etc/activity-relay/config.yml server
```

Run a worker:

```bash
relay --config /etc/activity-relay/config.yml worker
```

Open the legacy management command tree:

```bash
relay --config /etc/activity-relay/config.yml control
```

For native package installations, normal service management is through
systemd:

```bash
systemctl status activity-relay-server.service
systemctl status activity-relay-worker.service
systemctl restart activity-relay-server.service
systemctl restart activity-relay-worker.service
```

Container installations normally use:

```bash
docker compose ps
docker compose logs --tail=100 server worker redis
docker compose restart server worker
```

## Directory administration

Directory integration is opt-in. A file-backed example is:

```yaml
DIRECTORY_SCHEDULER_ENABLED: true
DIRECTORIES:
  - origin: https://directory.example.org
    enabled: true

DIRECTORY_PROFILE:
  participation_mode: open
  availability: public
  relay_type: general
  languages: [en]
  countries: [US]
  regions: []
  topics: [general]
  contact_fediverse: "@operator@example.social"
  contact_email: operator@example.org
  contact_url: https://relay.example.org/contact
  participation_url: https://relay.example.org/
  notes: Public community relay
```

Known public Directory services are listed in
[`DIRECTORY-INDEX.md`](DIRECTORY-INDEX.md). A listing is a discovery aid, not an
endorsement or trust grant.

### Show local Directory state

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory status
```

This lists configured Directory origins and the local scheduler state, such as
`configured`, `registered`, `heartbeat-current`, `retrying`, `disabled`, or
`unregister-pending`.

### Query a Directory's public status

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory status https://directory.example.org
```

This verifies the Directory status document and shows lifecycle availability,
enrollment state, and Directory version.

### Register

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory register https://directory.example.org
```

Registration creates or refreshes the relay's Directory lifecycle record. With
modern Directory protocols it also sends the normalized descriptive profile and,
when Protocol v3 is advertised, bounded participating-site telemetry.

### Send a heartbeat now

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory heartbeat https://directory.example.org
```

Use this when you want an immediate liveness update rather than waiting for the
scheduled interval. When the file-backed Directory scheduler is enabled, a
successful manual heartbeat coordinates through the same Redis lease as the
scheduler and advances the persisted next-heartbeat deadline. This prevents the
background scheduler from immediately repeating the same successful operation.

### Synchronize profile information

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory sync https://directory.example.org
```

`sync` performs a negotiated registration using the current
`DIRECTORY_PROFILE`. Use it after changing descriptive Directory metadata when
you do not want to wait for the scheduler to notice the profile digest change.
With the file-backed scheduler enabled, successful manual register/sync
operations also update the scheduler's persisted profile state and next
heartbeat deadline.

### Unregister

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory unregister https://directory.example.org
```

For a regular file-backed configuration, unregister first changes the selected
entry to `enabled: false`, writes a sibling backup ending in
`.activity-relay.bak`, coordinates with the scheduler lease, and only then sends
the signed unregister request. A remote failure leaves the entry disabled so a
restart cannot silently re-register it.

To remove the disabled entry as part of a successful unregister, use:

```bash
relay \
  --config /etc/activity-relay/config.yml \
  directory unregister https://directory.example.org \
  --remove
```

Container deployments that bind-mount one `config.yml` file need extra care
after atomic config replacement because an already running container can remain
attached to the old inode. The complete safe container unregister/re-enable
sequence is in [`DIRECTORY-CLIENT.md`](DIRECTORY-CLIENT.md).

### Automatic heartbeat behavior

The Directory scheduler runs only in the API/server process. On server startup,
each enabled Directory receives one immediate reconciliation even when Redis
contains a future daily deadline:

- a relay with current profile state sends a heartbeat;
- a new or locally unsynchronized relay sends registration instead.

A successful authenticated registration also counts as liveness at compatible
Activity-Relay Directory servers, so either operation establishes a current
Directory heartbeat.

After success, the next scheduled heartbeat is 24 hours plus stable
per-relay/per-Directory jitter of up to two hours. Restart does not discard the
persisted state; it only adds the one startup reconciliation. Existing retry,
rate-limit, suspension, authentication, and policy backoff deadlines are not
bypassed by the startup pulse.

## Important configuration areas

Start from `config.yml.example` or the package's
`/etc/activity-relay/config.yml.example` rather than reconstructing the file
from documentation snippets.

Common settings include:

```yaml
ACTOR_PEM: /etc/activity-relay/actor.pem
REDIS_URL: redis://127.0.0.1:6380
RELAY_BIND: 127.0.0.1:8080
OBSERVABILITY_BIND: 127.0.0.1:9090
RELAY_DOMAIN: relay.example.org
RELAY_SERVICENAME: Community ActivityPub Relay
JOB_CONCURRENCY: 10
OUTBOUND_SIGNATURE_PROFILE: dual
PUBLIC_ADDRESS_DISTRIBUTION_POLICY: explicit_public_only
```

`OUTBOUND_SIGNATURE_PROFILE` accepts `legacy`, `rfc9421`, or `dual`. `dual` is
the normal compatibility policy: fetch and delivery capability are learned
independently and a delivery POST is never retried under a different signature
grammar merely for negotiation.

`PUBLIC_ADDRESS_DISTRIBUTION_POLICY` controls which ActivityStreams Public
audiences are eligible for fan-out. Fresh configurations should use the value
shipped in the example file; explicitly retain the older policy on upgrades
when that behavior is desired.

Detailed HTTP-signature behavior is documented in
[`SECURITY.md`](SECURITY.md) and
[`OUTBOUND-SIGNATURE-NEGOTIATION.md`](OUTBOUND-SIGNATURE-NEGOTIATION.md).

## Public status

The API server exposes:

```text
GET /status.json
```

Inspect it locally with:

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

It reports relay identity, software version, public-address policy, connected
instances, receiving instances, and observed publishers without exposing actor
private keys, queue internals, or private configuration.

## Observability

When `OBSERVABILITY_BIND` is configured on the API server, a separate listener
provides:

```text
GET /metrics
GET /-/healthy
GET /-/ready
```

Example local checks:

```bash
curl --fail http://127.0.0.1:9090/-/healthy
curl --fail http://127.0.0.1:9090/-/ready
curl --fail http://127.0.0.1:9090/metrics
```

Keep this listener private. Do not proxy these routes through the public relay
virtual host.

## Public website

The bundled website is optional. Operators may use it, replace it, redirect the
root page, or serve no frontend while continuing to proxy the ActivityPub
endpoints.

Native package installations rebuild the package-managed site with:

```bash
sudo activity-relay-rebuild-site
```

Full customization, Nginx, Apache, Caddy, and container-site instructions are
in [`../contrib/web/README.md`](../contrib/web/README.md).

## Resource guard and scheduled reports

The optional native resource guard can monitor operational state and send
scheduled reports. Administrative examples include:

```bash
sudo activity-relay-resource-guard --show-summary-state
sudo activity-relay-resource-guard --preview-summary
sudo activity-relay-resource-guard --send-summary-now
```

Mail configuration, report scheduling, storage placement, and reset behavior
are documented in [`../contrib/ops/README.md`](../contrib/ops/README.md) and the
package's `README.Debian`.

## Troubleshooting checklist

For a relay that starts but is not behaving as expected:

```bash
relay --config /etc/activity-relay/config.yml --test-config
systemctl status activity-relay-server.service
systemctl status activity-relay-worker.service
journalctl -u activity-relay-server.service -n 100 --no-pager
journalctl -u activity-relay-worker.service -n 100 --no-pager
curl --fail http://127.0.0.1:8080/status.json
```

For Directory-specific problems, add:

```bash
relay --config /etc/activity-relay/config.yml directory status
relay --config /etc/activity-relay/config.yml directory status https://directory.example.org
```

Interoperability-specific notes and known remote implementation behavior are in
[`INTEROPERABILITY.md`](INTEROPERABILITY.md).
