# Activity-Relay Directory client contract

## Scope and activation

The `internal/directoryclient` package is the version 1 transport foundation
for explicit operator commands and the optional API-process scheduler. It does
not add a public relay endpoint or worker task. Existing ActivityPub fetch and
delivery signing remains unchanged.

`DIRECTORIES` accepts at most eight entries. Entries are opt-in; the public
example below remains disabled until an operator deliberately enables it:

```yaml
DIRECTORIES:
  - origin: https://directory.argentwolf.org
    enabled: false
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

## Finding a Directory

A public Activity-Relay Directory that operators may choose to configure is
`https://directory.argentwolf.org`. The community-maintained
[`DIRECTORY-INDEX.md`](DIRECTORY-INDEX.md) lists other known Directory servers.
The GitHub mirror provides a convenient live view of that list:
<https://github.com/thystra/Activity-Relay/blob/master/docs/DIRECTORY-INDEX.md>.

The index is a discovery aid, not a trust or endorsement list. Activity-Relay
does not automatically enable, register with, or trust a Directory because it
appears in the index.

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

### Compose deployments and single-file config binds

The bundled Compose deployment bind-mounts `./config.yml` as one read-only file.
An atomic host replacement of that file changes the host inode, but an already
running container can remain attached to the old bind-mounted inode. This matters
for Directory lifecycle operations because the API process alone runs the
scheduler and may otherwise continue to observe an older `enabled: true` entry.
It is a container bind-mount behavior, not a Directory protocol or persistence
failure.

For a controlled unregister in a container deployment:

1. persist `enabled: false` for the selected Directory in the host-owned
   `config.yml` before sending remote lifecycle traffic;
2. force-recreate the API/server container so the running scheduler sees that
   disabled host configuration;
3. keep the API/server online while unregistering, because the Directory may
   resolve the relay actor and `#main-key` through the public actor endpoint as
   part of authentication;
4. run a one-off lifecycle container with access to the same actor identity,
   Redis state, and disabled Directory configuration, and with a writable
   configuration source when using file-backed `directory unregister`;
5. verify local `directory status` reports the entry disabled rather than
   registered or retrying, and verify the relay is absent from the Directory's
   public projection; and
6. force-recreate worker containers as housekeeping after any atomic host
   `config.yml` replacement so every long-running container observes the same
   inode, even though workers never run the Directory scheduler.

Do not stop the API/server before the signed unregister request has completed.
A Directory that validates the relay actor by fetching its public actor/key
cannot authenticate the unregister while that endpoint is offline.

The file-backed unregister editor creates sibling temporary and backup files and
uses atomic rename. A one-off container that lets the command perform that edit
therefore needs writable access to the configuration *directory*, not merely a
read-write remount of the single `config.yml` file. Keep that write scope as
narrow as practical and do not make `actor.pem` writable. If configuration is
managed externally instead, persist the disabled state there first and use the
explicit environment-only acknowledgement path described above.

To re-enable a Directory after a successful unregister:

1. persist `enabled: true` in the host-owned configuration;
2. remember that the still-running API/server may continue to see the old
   disabled inode until it is recreated;
3. force-recreate the API/server container;
4. allow the enabled scheduler entry to reconcile registration automatically;
5. verify local status returns to `registered` or `heartbeat-current` and the
   Directory public projection reports the relay healthy; and
6. recreate worker containers as the same bind-inode housekeeping step.

These recreate steps are required after atomic replacement of a single-file bind.
An in-place edit that preserves the inode does not have the same stale-inode
failure mode, but operators should prefer the durable atomic configuration
workflow and explicit container recreation rather than rely on edit mechanics.

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
