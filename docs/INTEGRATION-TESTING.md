# Integration, release-candidate, and stable-promotion testing

## Purpose

Integration testing validates the same application commit through both
supported deployment forms before a release candidate is tagged or a stable
release is promoted:

1. a locally built container deployment using the repository Compose files; and
2. a native Debian or Ubuntu package using the packaged systemd and dedicated
   Redis configuration.

The two deployments must use isolated runtime directories and must never share
a live Redis instance. A test may deliberately preserve one copied actor key
between deployment forms to confirm identity continuity, but production keys and
production Redis data must not be used.

## Required evidence

Record the tested commit, build version, package version, image ID, host
operating-system version, architecture, reverse-proxy configuration checksum,
actor public-key fingerprint, Redis persistence settings, start and finish
times, and every observed failure.

Keep host-specific transcripts, addresses, credentials, and deployment paths
outside the public repository. Repository documentation and committed examples
must remain portable.

## Common acceptance checks

Both deployment forms must pass:

- `relay --version` reports the intended commit, candidate, or stable version;
- the actor identity remains unchanged across restart and upgrade tests;
- `/actor`, `/nodeinfo/2.1`, and `/status.json` return valid responses through
  the public HTTPS reverse proxy;
- the observability listener remains private;
- `/-/healthy`, `/-/ready`, and `/metrics` work from the trusted host or
  container network;
- Redis unavailability makes readiness fail without exposing probes publicly;
- accepted signed publisher traffic reaches a configured receiver;
- receiver-health counters and operational metrics reflect the test traffic;
- queue limits, reservations, delayed retries, and result-state cleanup behave
  as documented;
- logs contain actionable bounded errors without request bodies, signatures,
  keys, or unbounded remote responses; and
- restart, rollback, and cleanup leave no stale process or conflicting listener.

## Container matrix

Build from the exact tested checkout with `compose.yml` and
`compose.build.yml`. Verify the image contents, version string, bind mounts,
resource limits, Redis persistence directory, health checks, worker replica
count, and reverse-proxy loopback binding.

Exercise:

1. clean startup;
2. server-only restart while workers continue;
3. worker restart with ready and delayed jobs present;
4. complete Compose restart with persisted Redis data;
5. Redis stop, readiness failure, Redis start, and recovery;
6. an abrupt worker termination while a delivery task is running;
7. image rebuild and replacement without rotating the actor key; and
8. removal of the test stack without deleting retained evidence.

## Native package matrix

Build the candidate or stable package version from the exact tested commit.
Inspect package metadata and contents, run Lintian, and test:

1. clean installation with inactive services;
2. operator configuration and explicit service enablement;
3. the dedicated Redis instance and its persistence directory;
4. server and worker restarts independently;
5. Redis restart and readiness recovery;
6. package reinstallation;
7. package upgrade from the current stable release;
8. downgrade or rollback to the prior package when supported by the test plan;
9. actor-key, configuration, Redis-data, and website-content preservation; and
10. package removal behavior without purging operator-owned state.

## Queue interruption and reliable-claim tests

The go-redis broker atomically moves a task from the ready list into an
in-flight payload hash and lease sorted set. A worker renews the lease while
processing and acknowledges the claim only after successful completion. Expired
claims are atomically returned to their original ready queue.

Test at least these points:

- worker termination before claim, expecting the task to remain ready;
- graceful termination during active delivery, expecting completion and
  acknowledgement before shutdown;
- abrupt termination during outbound HTTP, expecting one durable claim followed
  by retry after lease expiry;
- abrupt termination after remote HTTP success but before local completion,
  expecting a duplicate after lease expiry and eventual acknowledgement;
- delayed retry scheduled before abrupt worker downtime and completed afterward;
- Redis restart while workers are blocked waiting for tasks; and
- final ready, delayed, in-flight payload, and lease counts returning to zero.

For every case, capture the ready queue, delayed set, claim payload hash, lease
set, HTTP-attempt events, local activity/result state, worker lifecycle, and
final cleanup state. The release gate fails on silent loss, premature claim
deletion, stranded claims, or inconclusive timing.

The at-least-once behavior intentionally permits a duplicate when the remote side
has already accepted an operation but the worker dies before local completion
and acknowledgement. This is a passing classification only when the claim
survives, expires, is retried, completes, and is removed cleanly. It is not an
exactly-once guarantee.

The reliable-claims implementation rejects Redis Cluster mode because all keys
used by an atomic claim operation are not guaranteed to share a hash slot.

## Federation checks

Use real test instances where possible. Distinguish:

- relay HTTP acceptance;
- queue admission;
- worker delivery;
- remote HTTP acceptance;
- remote object fetch;
- remote database import; and
- local timeline or user-interface presentation.

For Mastodon secure-mode testing, verify relay-signed actor and object fetches,
redirect re-signing, exact wire authority, and the existing interoperable
`Signature` header format.

FEP-ae0c and Activity-Relay extension changes must update
`testdata/fep-ae0c/cases.json` and
`docs/FEP-AE0C-COMPATIBILITY.md`. Before changing relay routing or wrapping
behavior, first convert the affected specification fixtures into executable
characterization tests. For changes involving direct forwarding, reciprocal
follows, Announce wrapping, audience handling, or loop prevention, exercise both
relay families and a mixed two-relay topology.

### FEP-ae0c characterization coverage

`testdata/fep-ae0c/coverage.json` must contain exactly one coverage record for
every fixture. Unit-characterized cases must name executable Go tests.
Network-dependent and future-protocol cases must remain explicitly classified
rather than being represented by mocks that bypass the behavior under review.

The referenced-Announce and two-relay reflection cases require real process
boundaries, separate Redis state, signed HTTP requests, and bounded delivery
assertions before their coverage status may change to executable.

### Two-relay probe implementation gate

The two-relay probe satisfies
`testdata/fep-ae0c/two-relay-probe-contract.json` through
`contrib/ops/test_fep_ae0c_two_relay_probe.sh`. A simulated graph, shared Redis
database, shared actor key, unsigned origin, or in-process handler pair does not
satisfy this gate. Run it with an absolute evidence directory outside the
repository:

```bash
contrib/ops/test_fep_ae0c_two_relay_probe.sh /path/to/private/evidence
```

The report records cross-relay POSTs by destination and activity ID, legacy
HTTP-signature verification, initial and final Redis key inventories, process
configuration, and logs. The command is now a required passing invariant. It
fails unless the report classifies the run as `no_reflection_observed`, records
zero generated non-seed cross-relay POSTs, validates every observed signature,
retains a canonical loop-suppression marker, and leaves no ready, delayed,
claimed, retry, or retained-activity backlog. Preserve the report and process
logs as promotion evidence before beginning protocol refactoring.

## Promotion gate

Do not tag a release candidate until both deployment forms pass the applicable
matrix, unresolved failures are documented, and the evidence identifies the
exact commit and artifacts. Production deployment remains a separate state and
must not be inferred from integration success.

Before promoting an RC to stable, retain evidence that the exact candidate
application code passed a representative production soak. Classify receiver and
environmental incidents separately from relay regressions. A stable-preparation
commit may update documentation and package metadata without repeating the
application-code matrix; any runtime change requires the affected gates to be
repeated.

After the stable tag is published, verify the release assets, internal Debian
version, checksums, packaged examples, multi-architecture container manifests,
stable semantic tags, and `latest` before declaring publication complete.

## RFC 9421 and RFC 9530 standards core

The standards-core tranche is exercised by:

```text
go test ./internal/httpsignature
```

The focused suite freezes:

- empty profile configuration preserving `legacy`;
- `dual` being rejected by the primitive layer until a delivery policy exists;
- exact RFC 9530 SHA-256 serialization and verification;
- exact wire authority including non-default ports;
- required GET and POST covered-component order;
- RSA `rsa-v1_5-sha256` signature verification;
- random nonce and `created` signature parameters;
- POST body preservation;
- rejection of tampered bodies by both the signature and content digest; and
- unchanged legacy `Digest` and `Signature` behavior.

This tranche does not alter a server, worker, delivery, resolver, package, or
production configuration path. Runtime interoperability probes are required in
the next tranche.

## RFC 9421 inbound verification core

Run the focused inbound suite with Redis available:

```text
REDIS_URL=redis://127.0.0.1:6381 go test -count=1 ./internal/httpsignature
```

The suite verifies:

- successful RFC 9421 RSA verification;
- required ActivityPub POST covered components;
- exact public authority enforcement;
- bounded old and future `created` values;
- RFC 9530 body-tamper rejection;
- nonce reservation only after signature and digest success;
- replay rejection;
- actor, public-key owner, and resolved actor binding;
- atomic Redis `SET NX` behavior;
- positive replay-marker TTL; and
- absence of raw key IDs and nonce values from Redis key names.

The inbox is not yet wired to select this verifier. A subsequent integration
tranche must add an ActivityPub key resolver, dual legacy/modern decoder
selection, metrics, and a real-process signed inbound probe.

## RFC 9421 inbound runtime probe

Run the real-process invariant with evidence outside the repository:

```text
contrib/ops/test_rfc9421_inbound_probe.sh /absolute/private/evidence/path
```

The probe starts:

- one real Activity-Relay API process;
- one independent Redis container;
- a locally trusted TLS frontend for the relay;
- a locally trusted signed ActivityPub actor origin; and
- a private observability listener.

It requires:

- one valid RFC 9421 request to return 202;
- the identical request replay to return 400;
- a body-tampered request to return 400;
- the original valid request after the tamper attempt to return 202, proving
  the invalid body did not consume the nonce;
- at least two valid relay-signed actor GETs;
- zero invalid actor GET signatures;
- exactly two temporary nonce markers; and
- bounded success, replay, and digest metrics with expected counts.

The evidence directory contains the exact relay configuration, process log,
binary checksum, and machine-readable report.

## Outbound signature profile configuration

Focused validation must cover both values:

```text
go test -count=1 ./internal/httpsignature ./models ./api ./deliver
```

The configuration and runtime suite requires:

- omitted and empty configuration to resolve to `legacy`;
- case-normalized `rfc9421` to be accepted;
- unknown and `dual` values to fail configuration construction;
- API and worker configured signers to match `RelayConfig`;
- legacy authorized-fetch GETs and delivery POSTs to retain their established
  fields and cryptographic verification;
- RFC 9421 GETs and POSTs to verify cryptographically;
- RFC 9421 POSTs to use `Content-Digest` and omit legacy `Digest`;
- redirects to remove stale fields and re-sign the redirected target; and
- no automatic fallback or duplicate POST attempt.

Because omitted configuration is the compatibility default, also rerun:

```text
contrib/ops/test_rfc9421_inbound_probe.sh /absolute/private/evidence/path
contrib/ops/test_fep_ae0c_two_relay_probe.sh /absolute/private/evidence/path
```

The first preserves the modern-inbound/legacy-outbound boundary. The second
must retain `no_reflection_observed` with legacy wire signatures.
