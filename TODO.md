# Activity-Relay roadmap

## Current stable release

`v2.4.0` is the current stable maintained-fork release. It promotes the RC6
application code after production soak validation and includes optional Nginx,
Apache, and Caddy deployment examples.

## Planned v2.5.0 work

### Signed authorized fetch

- Implemented in the v2.5.0 development line:
  - relay-signed remote actor and object `GET` requests for Mastodon authorized-
    fetch and secure-mode interoperability;
  - a shared signer that preserves exact on-wire `Host` authority behavior for
    both authorized fetch and outbound delivery;
  - bounded success and non-success response handling; and
  - focused signature, redirect, actor-fetch, object-fetch, and error-bound
    regression tests.
- Remaining release gate: validate end-to-end federation with a real Mastodon
  instance running authorized fetch or secure mode.
- Future security work:
  - add inbound RFC 9421 verification without removing the established
    Fediverse signature profile;
  - add RFC 9530 `Content-Digest` support and bounded replay validation;
  - test outbound RFC 9421 signing and fallback behavior against Mastodon; and
  - maintain the security profile in `docs/SECURITY.md`.
- Related upstream issue: <https://github.com/yukimochi/Activity-Relay/issues/94>

### Receiver delivery health

- Implemented in the v2.5.0 development line:
  - atomic per-receiver last-success and last-failure timestamps;
  - consecutive-failure, total-success, and total-failure counters;
  - `/status.json` schema version 4 entries for current receivers only; and
  - focused atomicity, success-reset, failure, privacy, and registration-exclusion
    regression tests.
- Existing short-lived `relay:statistics:<domain>` error diagnostics remain
  compatible.
- Historical health keys are retained but are not publicly listed after a receiver
  unsubscribes.
- Remaining release gate: observe health updates during the consolidated v2.5.0
  end-to-end federation and production validation pass.
- Define stale-receiver and cleanup policy only after observation data exists.
- Related upstream issue: <https://github.com/yukimochi/Activity-Relay/issues/83>

### Prometheus and health endpoints

- Implemented observability foundation in the v2.5.0 development line:
  - optional `OBSERVABILITY_BIND` with no listener when unset;
  - a separate listener exposing only `/metrics`, `/-/healthy`, and `/-/ready`;
  - process-only liveness and a bounded Redis-backed readiness check;
  - a private Prometheus registry with Go, process, build, and dependency metrics;
  - public API HTTP counters and duration histograms using normalized routes,
    common methods, and numeric status codes; and
  - focused tests for private registries, probes, timeouts, bounded labels, and
    panic accounting.
- Implemented operational metrics in the v2.5.0 development line:
  - accepted, rejected, and ignored inbox activities with bounded type and reason
    categories;
  - queue admission decisions, current reservations, and broker backlog;
  - fan-out target outcomes;
  - delivery outcomes with bounded network, TLS, HTTP, URL, and expiration classes;
  - aggregate receiver, publisher, and receiver-health gauges;
  - bounded Redis-operation failure counters when Redis remains writable; and
  - shared API/worker counters through Redis without private or unbounded labels.
- Remaining release gate: validate private binding, Prometheus scraping, liveness,
  and Redis outage/recovery behavior during the consolidated v2.5.0 integration
  pass.
- Design informed by upstream PR 100 but implemented independently with a private
  registry, separate listener, bounded labels, and real dependency readiness.
- Related upstream PR: <https://github.com/yukimochi/Activity-Relay/pull/100>

### Redis transport and durability
- Implemented Machinery v2 migration groundwork in the v2.5.0 development line:
  - the thystra Redis-only fork of `RichardKnop/machinery/v2` v2.0.16 with
    Redis broker and result backend injection;
  - preserved `relay` queue, task-signature JSON, delayed-retry, and result-state
    compatibility;
  - isolated v1 producer to v2 worker and v2 producer to v1 worker validation;
  - a delayed retry scheduled by v1 and completed by v2; and
  - direct and Machinery-path validation for ACL/database Redis, verified TLS,
    and Unix sockets.
- Remaining release gates:
  - validate the exact commit as both a Compose deployment and a native package
    using the matrix in `docs/INTEGRATION-TESTING.md`;
  - repeat `rediss://`, Unix-socket, outage, reconnect, and worker-restart tests
    against the integrated Activity-Relay binary;
  - characterize worker termination after task claim and document any
    at-most-once delivery window or recovery procedure;
  - document backup, export, restore, and persistence verification procedures;
  - maintain and rebase the Redis-only fork for relevant upstream security and
    correctness fixes; and
  - evaluate durable storage or replicated-state options without breaking
    current Redis data.
- Related upstream issues:
  - <https://github.com/yukimochi/Activity-Relay/issues/59>
  - <https://github.com/yukimochi/Activity-Relay/issues/88>
  - <https://github.com/yukimochi/Activity-Relay/issues/67>

## Completed in v2.4.0

- Standard actor outbox, followers, following, and shared-inbox metadata.
- Friendica and standards-style server-actor follow compatibility.
- NodeBB reciprocal follow, public-key, HTTP `Host` signing, and embedded
  `Announce` interoperability.
- Public connected, receiving, and publisher status information.
- Open signed publisher ingestion with policy enforcement.
- Bounded queue, fan-out, remote response, and concurrency controls.
- Multiple explicit daily-summary slots and resilient mail handling.
- Upgrade-safe optional website customization.
- Native package identity preservation and inactive-by-default services.
- Optional Caddy example reviewed from the idea in upstream PR 60, implemented
  as documentation rather than a runtime dependency.

## Release gates for future versions

Before any release:

1. update `CHANGELOG.md`, `readme.md`, affected documentation, and versioned
   release notes;
2. run all validation required by `AGENTS.md` and `docs/RELEASING.md`;
3. preserve actor identity, configuration, Redis compatibility, and operator
   website content;
4. validate package installation, upgrade/reinstallation, checksums, metadata,
   and container manifests; and
5. for federation changes, demonstrate a real signed publisher-to-receiver path
   and distinguish transport acceptance, object import, and local presentation.

## Upstream review

Continue monitoring upstream commits, issues, and pull requests under the policy
in `docs/UPSTREAM.md`. Record upstream commit, issue, or pull-request references
when a change is ported.
