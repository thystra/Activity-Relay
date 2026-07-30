# Activity-Relay architecture

## Overview

Activity-Relay is a Go ActivityPub relay with Redis-backed state and asynchronous
fan-out. The public website, resource guard, reverse-proxy examples, container
image, and native Debian package are operational layers around the same relay
binary; none changes the relay actor identity or ActivityPub protocol model.

## Runtime components

### Relay binary

The `relay` binary provides four principal modes:

- `server`: HTTP API, ActivityPub actor, subscription endpoints, NodeInfo, and
  public status output;
- `worker`: asynchronous delivery jobs and retries;
- `control`: operator management commands for relay state; and
- `generate-key`: safe, non-overwriting RSA actor-key generation.

The compiled version is injected at build time and displayed by
`relay --version`.

### Redis

Redis is the authoritative runtime store for:

- traditional subscribers;
- follower-style server actors and mutual-follow state;
- blocked and limited domains;
- observed publishers and accepted-activity counters;
- durable per-receiver delivery timestamps and counters;
- queued activities and task state; and
- queue-capacity reservations.

The native package uses a dedicated Redis instance. Container deployments use
the Redis service in `compose.yml`. Tests must use a disposable non-production
Redis instance.

Asynchronous tasks use the thystra Redis-only Machinery v2 fork with an
explicitly injected Redis broker, Redis result backend, and in-process lock.
The main module retains the upstream import path and selects
`github.com/thystra/machinery/v2` at `v2.0.17-0.20260730145804-fd43623b7b5c` with a Go `replace`
directive. The established `relay` list, `delayed_tasks` sorted set,
task-signature JSON, and result-state encoding remain wire-compatible with the
earlier Machinery v1 deployment so an upgrade does not require draining pending
work first.

### Workers and delivery

The API validates and records accepted activities, then creates bounded
asynchronous delivery work. Workers sign outbound HTTP requests with the relay
actor key, enforce response limits and timeouts, retry failures according to the
task backend, and surface bounded remote response text in errors. Remote actor
and ActivityPub object resolution also uses the relay actor identity. Authorized-
fetch `GET` requests sign `(request-target)`, `Host`, and `Date`; redirected
fetches are re-signed for the new request target and authority. Delivery `POST`
requests additionally sign `Digest` and `Content-Type`. Both paths sign the exact
`Host` authority transmitted on the wire. Each `relay-v2` fan-out attempt also
atomically records the receiver domain's last success or failure time, total
successes and failures, and consecutive failure count. Registration traffic does
not affect these delivery-health observations.

### Observability

The `server` command may start an optional second HTTP listener configured by
`OBSERVABILITY_BIND`. It is disabled when the setting is empty and exposes only
`/metrics`, `/-/healthy`, and `/-/ready`. The public ActivityPub listener never
registers those routes.

The observability listener owns a private Prometheus registry rather than the
process-global registry. Its foundation metrics cover the API process, build
identity, Redis readiness, and public API request counts and durations. HTTP
labels are restricted to a fixed route set, common methods, and numeric status
codes. Raw paths, query strings, domains, actor IDs, inbox URLs, and error text
are not metric labels.

Liveness is process-only. Readiness performs a bounded Redis ping because Redis
is required for relay state and queue operations. Worker processes do not open
the listener. API and worker processes instead write closed-enum counters to a
shared Redis hash, and the API process exports those counters through its private
registry. Scrape-time collectors add queue depth, temporary reservations, current
receiver and publisher counts, and aggregate receiver-health states.

Operational labels are deliberately bounded. They never contain domains, actor
IDs, inbox URLs, activity IDs, request paths, query strings, response bodies, or
raw errors. Ledger writes and scrape-time collection use short timeouts and never
change relay admission, fan-out, delivery, or retry outcomes.

### Resource guard

The Python resource guard monitors storage and cache budgets and can send
operator alerts and scheduled summaries. Its state and archived reports are
separate from ActivityPub relay state.

### Optional website and reverse proxy

The Go API exposes relay endpoints and `/status.json`; it does not require a
frontend. The Python builder creates an optional static website. Nginx, Apache,
Caddy, or another reverse proxy may serve that site, replace it, redirect it, or
return an error for frontend paths while proxying the exact relay endpoints.

Reverse-proxy examples are optional documentation. The Debian package does not
install or enable a web server.

## Identity and configuration ownership

- `actor.pem` is the long-lived cryptographic identity. Replacing it creates a
  different relay identity.
- `config.yml` is operator-owned behavior and public metadata.
- Redis contains relay registration and publisher state.
- Package-managed website source lives separately from operator-owned website
  settings and overrides.
- Generated website output is disposable and must not be edited directly.

Package upgrades must preserve actor identity, configuration, Redis data, and
operator-owned website content.

## Federation ingress

### Traditional subscription

Mastodon, Misskey, and compatible implementations register through `/inbox`.
The relay records their receiving inboxes and fans out accepted public traffic.

### Follower-style subscription

Pleroma, Akkoma, Friendica, NodeBB, and compatible server actors follow
`/actor`. The relay accepts valid `Application` and `Service` actors at
implementation-defined paths, returns `Accept`, and sends a reciprocal `Follow`.
Legacy `/relay` and `/friendica` paths remain compatible.

### Publisher validation

Inbound public activities require:

1. bounded request decoding;
2. remote actor and public-key resolution;
3. HTTP-signature and digest verification;
4. actor/key-host consistency;
5. blocked-domain and limited-domain policy checks; and
6. person-only policy enforcement when configured.

Valid signed public traffic may be accepted from an unsubscribed publisher. The
publisher registry records bounded public metadata but does not make the sender
a fan-out receiver.

## Activity processing and fan-out

Public `Create`, `Update`, `Delete`, and `Move` activities enter the established
fan-out path after validation.

Some servers, notably NodeBB category actors, publish local content as a public
`Announce` containing an embedded same-domain `Article` or `Note`. Activity-Relay
replaces that transport wrapper with one relay-authored `Announce` referencing
the canonical object ID and sends the same authenticated wrapper to every
receiver style. Receivers then fetch the canonical object and preserve its
original author, media, and tags.

URL-only announcements and embedded announcements for another domain remain
publisher-accounting events. They are not fanned out, preventing relay loops and
ordinary boost amplification.

## Public API and privacy

The actor advertises inbox, outbox, followers, following, and shared-inbox
metadata. Public collection GETs return privacy-filtered empty
`OrderedCollection` documents. Activity-Relay does not expose subscriber
identities or historical relayed activities.

`/status.json` exposes relay identity, policy, software version, connected and
receiving domain counts, bounded publisher metadata, and delivery-health
timestamps and counters for currently registered receivers. Historical health
keys for departed receivers are retained for future policy analysis but are not
returned publicly. The endpoint excludes Redis keys, queue internals, private
configuration, inbox URLs, actor IDs, error text, and blocked lists.

## Failure behavior

- Inbound verification and decoding failures return an error and log bounded
  request metadata without bodies, signatures, or key material.
- Queue admission is bounded and atomic.
- Remote JSON and error bodies are size-limited, including non-success actor
  and object fetch responses.
- Worker failures include actionable bounded response text and are retried by
  the task backend.
- A receiver-health recording failure is logged but does not change the delivery
  result or retry decision.
- An observability bind failure prevents the API server from accepting traffic,
  avoiding a partially started process with missing requested monitoring.
- Readiness failure returns HTTP 503 without terminating the process; liveness
  remains independent of Redis.
- Website-generation failure does not stop relay operation.
- Debian packages remain inactive after first installation and restart only
  already-active Activity-Relay services during upgrades.

## Security and compatibility invariants

- Never overwrite an existing actor key.
- Sign the exact outbound `Host` authority transmitted on the wire.
- Accept both SubjectPublicKeyInfo and legacy PKCS#1 RSA public-key PEM forms.
- Preserve existing endpoints, configuration names, and stored Redis data in
  backward-compatible releases.
- Do not weaken open-publisher policy checks while adding interoperability.
- Do not make the optional website or a particular reverse proxy a relay
  dependency.
- Keep the observability listener disabled by default or restricted to a trusted
  interface, and never attach unbounded or private values to metric labels.

## Packaging and releases

Tagged releases build multi-architecture containers and an Ubuntu 24.04
`amd64` Debian package. Stable and prerelease package versions follow Debian
ordering rules. Release verification includes checksums, package metadata,
identity-preserving install/reinstall tests, inactive services, and container
manifests.

See `docs/RELEASING.md` for gates and `TODO.md` for planned architecture work.
