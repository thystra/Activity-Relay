# Activity-Relay Directory client contract

## Scope and activation

The `internal/directoryclient` package is the version 1 transport foundation
for explicit operator commands. It does not add a public endpoint, startup
registration, heartbeat scheduler, Redis state, worker task, or deployment
setting. Existing ActivityPub fetch and delivery signing remains unchanged.

`DIRECTORIES` accepts at most eight entries:

```yaml
DIRECTORIES:
  - origin: https://directory.example.org
    enabled: false
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
the strict public status document. Sync performs heartbeat reconciliation only
for the explicit `relay_not_registered` result. Authentication, enrollment,
suspension, lifecycle, and malformed-response errors are not retried.
Transport and `internal_error` failures receive at most three attempts with
bounded backoff. HTTP 429 may use an integer `Retry-After`, capped at thirty
seconds. Every attempt constructs a new request with a fresh nonce and
signature.

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
