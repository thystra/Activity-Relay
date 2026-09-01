# Activity-Relay Directory client contract

## Scope and activation

The `internal/directoryclient` package is the version 1 transport foundation
for explicit operator commands and the optional API-process scheduler. It does
not add a public relay endpoint or worker task. Existing ActivityPub fetch and
delivery signing remains unchanged.

`DIRECTORIES` accepts at most eight entries:

```yaml
DIRECTORIES:
  - origin: https://directory.example.org
    enabled: false
```

Scheduling has a separate, false-by-default gate:

```yaml
DIRECTORY_SCHEDULER_ENABLED: false
```

Each origin must be one canonical HTTPS origin with no credentials, path,
query, fragment, or explicit port 443. Origins must be unique. The list is
absent by default and each omitted `enabled` value is false. Register,
heartbeat, and sync refuse lifecycle traffic for a disabled entry.

## Manual commands

The frozen command surface is:

```text
relay directory status [origin]
relay directory register origin
relay directory heartbeat origin
relay directory unregister origin [--remove]
relay directory sync origin
```

Status without an origin lists local entries. Status with an origin retrieves
the strict public status document. The 3.0 client accepts Directory status
schema versions 2 and 3; schema version 3 additionally exposes
`public_listing_enabled` and `public_listing_available`. JSON decoding remains
strict, so a future status schema or field set must be added deliberately rather
than being silently ignored. Sync performs heartbeat reconciliation only for
the explicit `relay_not_registered` result. Authentication, enrollment,
suspension, lifecycle, and malformed-response errors are not retried.
Transport and `internal_error` failures receive at most three attempts with
bounded backoff. Validated `Retry-After` values are accepted as delta seconds or
HTTP dates for retryable 429 and 503 responses and are capped at 24 hours.
Interactive commands honor values up to 30 seconds; for a longer remote lower
bound they return without retrying early. Every attempt constructs a new request
with a fresh nonce and signature.

File-backed unregister structurally edits YAML and durably disables the
selected entry before sending network traffic. It rejects symlinks and
non-regular files, preserves unrelated keys and comments plus ownership and
mode, writes and syncs a same-directory temporary replacement, saves the
original as `CONFIG.activity-relay.bak`, atomically renames, and syncs the
directory. Remote failure leaves the entry disabled and returns retry guidance.
`--remove` is allowed only after remote success and retains the original
pre-disable backup.

Environment-only unregister cannot make that durable edit. It requires
`--acknowledge-external-disable` and warns that the operator must disable the
entry in the external configuration source before restart.

## Optional scheduler

Setting `DIRECTORY_SCHEDULER_ENABLED: true` in a regular YAML configuration
enables scheduling only in the `server` process. Workers never schedule
directory traffic. Environment-only activation is deliberately unsupported
because the scheduler and unregister command must share a durable suppression
source.

At API startup, each enabled entry with no current persisted schedule is
registered. Successful registrations and heartbeats schedule the next
heartbeat after 24 hours plus a stable per-relay, per-directory jitter of at
most two hours. A restart reloads that state rather than immediately repeating
the operation. Only a strict `relay_not_registered` heartbeat response invokes
the client's one-register/one-final-heartbeat reconciliation.

Redis stores one bounded state record and one renewable lease per canonical
directory origin. Redis keys contain a SHA-256 digest, not the raw origin. The
state contains registration state, whole-second timestamps, a bounded attempt
counter, and closed outcome/diagnostic values; it never stores request bodies,
signatures, private keys, or nonces. State expires after 90 days without a
write. The API process renews the one-minute lease every 20 seconds, cancels an
in-flight request if ownership is lost, and releases the lease on completion or
graceful SIGINT/SIGTERM shutdown. Every scheduler state mutation atomically
verifies the current lease token, updates the state hash, and refreshes state
retention. A former owner cannot overwrite a newer owner's reconciliation.
While lease ownership remains healthy, API replicas issue one lifecycle
attempt for a due directory slot. If ownership is lost after a request has
reached the Directory, a successor may repeat the idempotent operation.
Lease-token fencing guarantees that a former owner cannot overwrite the
successor's scheduler state.

Transport, rate-limit, and directory-internal failures start with a 30-second
local backoff, grow exponentially with deterministic positive jitter, and cap
the local component at 15 minutes. For retryable 429 and 503 responses, a
validated remote `Retry-After` may lengthen that local delay up to 24 hours. The
effective delay is `max(local, Retry-After)`, measured from completion of the
lifecycle operation; no later jitter may shorten the remote lower bound. The
scheduler wakes at the earliest persisted retry deadline while retaining a
one-minute maximum observation interval for durable configuration and
cross-process state changes. Authentication, enrollment, suspension, lifecycle, and
malformed responses are recorded without rapid retry and are reconsidered on
the next daily interval. Directory failures never block API startup or
ActivityPub delivery.

File-backed unregister acquires the same directory lease before changing the
file whenever `REDIS_URL` remains present, including after the runtime scheduler
gate has been turned off. It loads and validates scheduler state while holding
that lease, durably disables the entry, performs a lease-token-fenced suppression
write, and only then sends the remote request. State-load or ownership failure
stops before remote traffic. Failure leaves the entry disabled and reports
`unregister-pending`; restart cannot re-register it. Removing the entry or
turning off the scheduler gate is treated as durable suppression rather than an
internal scheduler failure. Local `directory status` uses the bounded states
`configured`, `registered`, `heartbeat-current`, `retrying`, `disabled`, and
`unregister-pending`.

## Request profile

The client reuses the relay's existing actor RSA key and `#main-key` identity,
but not its ActivityPub application profile. Every lifecycle POST covers, in
this order:

1. `@method`;
2. `@authority`;
3. `@target-uri`;
4. `content-digest`;
5. `content-type`; and
6. `date`.

The signature uses label `directory`, tag
`activity-relay-directory-v1`, algorithm `rsa-v1_5-sha256`, a fresh
cryptographic nonce, a current whole-second `created` time, and an `expires`
time exactly five minutes later. `Content-Digest` is the RFC 9530 SHA-256 value
over the exact compact JSON bytes. Register, heartbeat, and unregister target
only their exact version 1 paths.

Every network call builds and signs a new request. Retries added in later work
must therefore receive a new nonce and signature rather than replaying a
request object.

## Response and recovery policy

The client enforces a 15-second maximum request timeout, refuses redirects,
reads at most 16 KiB, requires exact `application/json`, rejects duplicate and
unknown names plus trailing values, and validates protocol version, operation,
outcome, actor, error code, and status-code mapping. Raw bodies and remote human
messages are not retained in returned errors.

Heartbeat reconciliation is closed: only a validated HTTP 409
`relay_not_registered` result permits one register request followed by one
final heartbeat. Generic invalid input, authentication, enrollment, suspension,
rate, lifecycle, internal, transport, and malformed-response failures never
cause registration.

## Compatibility fixture

`testdata/directory/v1/activity-relay-register.valid.json` contains a complete
fixed-clock request generated with the repository's non-production test RSA
key. The client test reproduces its body, fields, signature parameters,
signature, and public key exactly. The identical fixture in the Directory
repository is accepted by that server's real digest, signature, key-binding,
replay, handler, and response path.
