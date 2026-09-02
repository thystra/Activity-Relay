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
`github.com/thystra/machinery/v2` at `v2.0.17-0.20260730204902-5efae3f700cd` with a Go `replace`
directive. The maintained Machinery branch also contains repository cleanup and
CI commits, while Activity-Relay remains pinned to the exact dependency commit
validated in the integration environment.
The established `relay` list, `delayed_tasks` sorted set, task-signature JSON,
and result-state encoding remain wire-compatible with the earlier Machinery v1
deployment so an upgrade does not require draining pending work first.

The go-redis broker atomically moves a ready task into an in-flight payload hash
and lease sorted set. A processing worker renews the lease and acknowledges the
claim only after `TaskProcessor.Process` returns successfully. Another worker
atomically recovers expired claims to their original ready queue. Delayed-task
promotion is also atomic.

This gives abrupt worker and host failure at-least-once semantics. Work is not
silently lost after claim, but a remote side effect may be repeated when the
worker dies after remote success and before local completion and
acknowledgement. Handlers and remote integrations should therefore use stable
operation identifiers or otherwise tolerate duplicate delivery. Redis Cluster
mode is rejected because the ready, payload, and lease keys are not guaranteed
to share a cluster hash slot. The required interruption matrix and evidence
expectations are documented in `docs/INTEGRATION-TESTING.md`.

### Workers and delivery

The API validates and records accepted activities, then creates bounded
asynchronous delivery work. Workers sign outbound HTTP requests with the relay
actor key, enforce response limits and timeouts, retry failures according to the
task backend, and surface bounded remote response text in errors. Remote actor
and ActivityPub object resolution also uses the relay actor identity. In `dual`
mode, an unknown idempotent fetch probes RFC 9421 and may retry once with the
legacy profile after an explicit signature challenge or the bounded HTTP `400`
compatibility signal. A legacy preference is cached only after that retry
succeeds. Redirected fetches
are re-signed for the new request target and authority. URI fragments are
removed before RFC 9421 signing because they are not part of the HTTP request
target; this keeps `@target-uri` identical to what the receiver verifies.
Delivery `POST` requests never change signature profile after queueing. Both
paths sign the exact `Host` authority transmitted on the wire. Each `relay-v2`
target receives one initial attempt and five delayed retries
using Machinery's Fibonacci schedule. The shared activity body remains in Redis
for fifteen minutes, which exceeds the complete retry horizon. A target reduces
the shared `remain_count` only after success or final retry exhaustion, so one
temporary receiver failure cannot delete the payload needed by its own retry or
by another receiver. Every attempt atomically records receiver delivery health;
registration traffic remains outside these observations.

Worker logs contain bounded structured correlation fields: stable task UUID,
storage ID, public activity and object identifiers when present, origin and
receiver domains, body digest, attempt counters, elapsed time, HTTP status,
error class, next retry delay, and bounded non-success response text. Raw
activity bodies, signatures, private keys, and unbounded remote content are not
logged or exported as metric labels.

The established Fediverse `Signature`-header profile remains supported as the
legacy compatibility wire format. Activity-Relay 3.0 also implements RFC 9421
verification and signing; the omitted/default `dual` policy selects one concrete
wire profile per operation using scoped destination capability evidence. Fixed
`legacy` and `rfc9421` policies remain available, and delivery POSTs never switch
wire profile after queueing. See `docs/SECURITY.md`.

### Directory client and API scheduler

`internal/directoryclient` defines the separate Activity-Relay Directory
version 1 transport profile without changing ActivityPub delivery behavior.
It reuses the relay actor RSA key but requires the
`activity-relay-directory-v1` application tag, created and expiry times, a
fresh nonce, RFC 9530 digest, and exact directory lifecycle targets. Responses
are bounded and decoded against closed success and error vocabulary; redirects
are refused. Only `relay_not_registered` can trigger one register
reconciliation.

Configuration accepts at most eight canonical HTTPS origins with independent
`enabled` booleans. The list is absent by default. Manual `relay directory`
commands consume configured entries. A separate
`DIRECTORY_SCHEDULER_ENABLED` gate, false by default, permits only the API
server process to perform startup reconciliation and daily heartbeats. Workers
never import or start the scheduler.

The scheduler stores bounded per-directory state in Redis under digest-derived
keys and coordinates API replicas with a renewable per-directory lease. It
persists the last success, next attempt, bounded outcome and diagnostic, retry
attempt, and the last observed wall clock; it never persists signatures,
nonces, bodies, keys, or raw directory origins. Stable heartbeat jitter spreads
daily traffic. Retry starts at 30 seconds, uses deterministic positive jitter,
caps the local component at 15 minutes, and permits a validated remote
`Retry-After` to lengthen the effective delay up to 24 hours, measured from
lifecycle-operation completion. The run loop wakes at the earliest persisted
retry deadline while retaining a one-minute maximum observation interval for
durable configuration and shared-state changes. Wall-clock regression cannot
move a persisted schedule backward. Lease loss cancels the
request, and every state mutation is atomically fenced by the current lease
token so a former owner cannot overwrite a successor. Directory unavailability
is isolated from ActivityPub startup and delivery.

File-backed unregister first rewrites the selected entry to `enabled: false`
through a structural YAML edit. The replacement and recoverable backup are
written in the configuration directory, retain file metadata, are individually
synced, and become visible through atomic rename plus directory sync. Remote
failure cannot restore the enabled state. Environment-only configuration
requires an explicit acknowledgement because the process cannot mutate its
external source. See `docs/DIRECTORY-CLIENT.md`.

Scheduled unregister uses the same lease as registration and heartbeat. A
file-backed command coordinates whenever `REDIS_URL` remains available, even if
the current scheduler gate is false, because an API process may have started
from the earlier enabled configuration. It loads state under the lease, durably
disables the regular YAML entry, and persists token-fenced suppression before
remote traffic. Runtime gate disablement and entry removal are durable
suppression, not internal scheduler failures. Remote failure therefore cannot
race a restart into re-registration. Scheduler state has a 90-day idle TTL and
metric labels contain only closed result and diagnostic enums, never directory
URLs. The API command uses a signal-aware lifecycle so SIGINT/SIGTERM cancels
in-flight scheduler work, gracefully shuts down HTTP listeners, waits for
scheduler completion, and releases the owned lease.

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
Directory scheduler attempts add only closed result and diagnostic labels.

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
`/actor`. Activity-Relay publishes that relay actor as `Application` with
`preferredUsername` `relay`, allowing current Friendica relay discovery while
retaining the established actor ID, collections, and key identity. The relay
accepts valid remote `Application` and `Service` actors at implementation-
defined paths, returns `Accept`, and sends a reciprocal `Follow`. Legacy
`/relay` and `/friendica` paths remain compatible.

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
- Worker failures include bounded structured correlation fields and response
  text. Retriable attempts preserve the shared activity body; success and final
  exhaustion are the only terminal target outcomes that reduce `remain_count`.
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
