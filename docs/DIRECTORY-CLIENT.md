# Activity-Relay Directory client contract

## Scope and activation

The `internal/directoryclient` package is a dormant version 1 transport
foundation. It does not add a public endpoint, CLI command, startup
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
absent by default and each omitted `enabled` value is false. Later commands and
scheduling must refuse lifecycle traffic for a disabled entry.

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
