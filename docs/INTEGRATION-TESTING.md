# Integration and release-candidate testing

## Purpose

Integration testing validates the same commit through both supported deployment
forms before a release candidate is tagged:

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

- `relay --version` reports the intended commit or release-candidate version;
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

Build an unreleased package version from the exact tested commit. Inspect package
metadata and contents, run Lintian, and test:

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

## Queue interruption tests

The Redis broker removes a ready task from the queue before the handler
finishes. Therefore an abrupt process death after claim but before completion
requires explicit observation. Test at least these points:

- worker termination before claim;
- termination immediately after claim;
- termination during outbound HTTP delivery;
- termination after remote success but before result-state completion;
- graceful worker shutdown with queued tasks;
- delayed retry scheduled before restart and completed afterward; and
- Redis restart while workers are blocked waiting for tasks.

For every case, record whether the job is retried, duplicated, lost, or remains
recoverable. A release note must document any known at-most-once window or
operator recovery procedure.

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

## Promotion gate

Do not tag a release candidate until both deployment forms pass the applicable
matrix, unresolved failures are documented, and the evidence identifies the
exact commit and artifacts. Production deployment remains a separate state and
must not be inferred from integration success.
