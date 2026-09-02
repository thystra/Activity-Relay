# HTTP message-signature architecture

Activity-Relay supports both the established Fediverse HTTP Signature profile
and RFC 9421 HTTP Message Signatures with RFC 9530 digest fields. The 3.0
runtime keeps the wire formats distinct and uses destination-aware negotiation
when the outbound policy is `dual`.

## Compatibility boundary

The existing `SignGET` and `SignPOST` methods remain the low-level legacy
profile. Profile-aware primitives and the runtime negotiating signer select the
wire format explicitly without placing both incompatible signature grammars in
one request.

Profiles are:

- `legacy`: the established `Signature` and `Digest` behavior;
- `rfc9421`: `Signature-Input`, RFC 9421 `Signature`, and RFC 9530
  `Content-Digest`; and
- `dual`: a destination-aware negotiation policy, not a single wire format.

The legacy and RFC 9421 protocols both use a field named `Signature` with
incompatible grammars. Activity-Relay therefore does not place both forms in
one request. Blind POST retry is also not introduced in the core tranche,
because a remote server may accept a delivery before returning an ambiguous
response.

## Initial modern profile

The modern profile uses the relay's existing RSA actor key with the registered
`rsa-v1_5-sha256` algorithm. This preserves actor identity while changing the
HTTP signature representation.

The signature label and application tag are both `activitypub`.

Authorized-fetch GET covers, in order:

1. `@method`
2. `@authority`
3. `@target-uri`
4. `date`

Delivery POST covers, in order:

1. `@method`
2. `@authority`
3. `@target-uri`
4. `content-digest`
5. `content-type`
6. `date`

The exact wire authority, including a non-default port, is used. Modern
signatures include `created`, `keyid`, `alg`, `tag`, and a random nonce.

## RFC 9530 behavior

POST bodies use a Structured Field dictionary member named `sha-256` whose
value is the SHA-256 digest of the exact content bytes. Verification accepts
other dictionary algorithms but requires and constant-time compares the
`sha-256` member.

The modern primitive removes stale legacy `Digest` and legacy `Signature`
material before signing. GET requests do not carry a content digest.

## Security and rollout status

Inbound RFC 9421 verification, fixed outbound profiles, and destination-aware
`dual` negotiation are runtime-selectable and covered by unit, real-process, and
cross-software testing. Activity-Relay 3.0 selects `dual` when the outbound
setting is empty or omitted; explicit `legacy` and `rfc9421` remain available.

Inbound verifier outcomes are exported as bounded Prometheus metrics. Outbound
delivery diagnostics record the concrete selected `signature_profile`; a
dedicated outbound-profile counter is a future observability enhancement rather
than a 3.0 protocol requirement.

Linked Data proofs remain separate from HTTP request authentication.

## Inbound verification core

The inbound core verifies the modern ActivityPub POST profile and is wired into
the active inbox decoder whenever `Signature-Input` is present.

The verifier requires:

- HTTP `POST`;
- the configured public scheme and exact authority;
- exactly one signature carrying `tag="activitypub"`;
- `keyid`, `created`, and nonce parameters;
- the registered `rsa-v1_5-sha256` algorithm;
- every required ActivityPub POST component;
- a `created` value no older than five minutes and no more than thirty seconds
  in the future;
- a non-expired `expires` value when one is supplied;
- a valid RSA signature;
- an RFC 9530 `sha-256` digest matching the exact body bytes; and
- an atomic nonce reservation.

Additional covered components are permitted, but duplicate covered components
are rejected.

The nonce is reserved only after both cryptographic signature verification and
body-digest verification succeed. This prevents unauthenticated requests from
burning a valid sender's nonce. Redis stores only a SHA-256-derived key over the
key ID and nonce, never either raw value. The default replay marker lifetime is
ten minutes.

A successful verification result carries the resolved key owner and actor
identity. `BindActivityActor` requires both identities to match the activity's
actor URL after scheme and authority case normalization.

Runtime actor retrieval, inbox profile selection, bounded metrics, and HTTP
response classification are part of the 3.0 runtime.

## Inbound runtime selection

The public inbox now selects RFC 9421 verification whenever a
`Signature-Input` field is present, including an empty malformed field. Once selected, verification failure is
terminal for that request; Activity-Relay does not fall back to the legacy
parser. Requests without `Signature-Input` continue through the established
legacy verification path.

The runtime key resolver fetches the key ID as an authenticated ActivityPub
GET and requires:

- a non-empty actor ID;
- an embedded RSA public key;
- the fetched public-key ID to exactly equal `keyid`;
- a non-empty public-key owner; and
- a parseable RSA public key.

The core verifier then requires the public-key owner and resolved actor ID to
match the activity actor exactly after URL scheme and authority case
normalization. The activity actor document fetched for normal relay processing
is checked through the same exact binding.

The verifier is initialized from the relay's configured public HTTPS authority
and existing Redis client. Inbound support is additive and independent of the
operator-selected outbound profile.

## Inbound signature metrics

The private metrics surface exports:

```text
activity_relay_http_signature_verifications_total
```

with bounded `profile`, `result`, and `reason` labels. Profiles are `legacy`
and `rfc9421`. Results are `success` and `failure`. Reasons are bounded to
accepted, parse, key, crypto, digest, replay, time, actor, authority, policy,
Redis, activity, or other. Raw actor IDs, key IDs, nonce values, URLs, and
error text are never metric labels.

## Outbound runtime profile

`OUTBOUND_SIGNATURE_PROFILE` controls both relay-authenticated remote GETs and
worker delivery POSTs.

Accepted values are:

- `legacy`: the established Fediverse `Signature` and `Digest` profile;
- `rfc9421`: RFC 9421 `Signature-Input` and `Signature`, with RFC 9530
  `Content-Digest` on POST requests; and
- `dual`: a destination-aware policy that selects one concrete `legacy` or
  `rfc9421` wire profile for each fetch or delivery operation.

An empty or omitted operator value is normalized to `dual`, the 3.0
compatibility default. Unknown values fail startup. The low-level fixed-profile
parser and fixed signer retain empty-as-legacy behavior for internal backward
compatibility because they cannot negotiate; runtime `RelayConfig` uses the
outbound-policy parser and constructs the Redis capability store and destination
negotiator before accepting `dual`. Authorized-fetch redirects remove all
legacy and modern signature fields before re-planning and re-signing the new
request target.

Selecting fixed `rfc9421` is an explicit operator action. It does not probe a
destination, retry a POST with another signature grammar, or fall back after a
remote rejection.

## Destination-aware negotiation core

The negotiation core is documented in
`docs/OUTBOUND-SIGNATURE-NEGOTIATION.md`.

Capability identity is origin- and scope-specific. Fetch evidence and delivery
evidence are not interchangeable. Positive RFC 9421 observations expire after
fourteen days by default; explicit negative observations expire after one day.

An unknown fetch plan selects RFC 9421 first and exposes one legacy fallback.
The fallback is eligible after either an explicit legacy `Signature` challenge
or the bounded HTTP 400 compatibility signal used for unknown idempotent GETs.
A legacy preference is cached after the 400 path only when the legacy retry
succeeds. Transport errors, 5xx responses, timeouts, DNS failures, and arbitrary
response text never authorize fallback.

An unknown delivery plan remains legacy. Delivery plans never contain a
fallback, preventing blind duplicate POST delivery.

The low-level negotiation-core tests remain independent of network I/O; the
runtime behavior is covered separately below.

## Runtime dual negotiation

`OUTBOUND_SIGNATURE_PROFILE=dual` is destination-aware and never a wire format.
The API server and worker both initialize the same Redis-backed planner.

Authorized fetch may make one legacy fallback after an explicit legacy
`Signature` authentication challenge. Delivery never falls back. New delivery
tasks persist one concrete profile across every delayed retry, while old
two-argument tasks remain readable.

Successful compatible `Accept-Signature` responses and successful RFC 9421
requests create positive capability evidence. An explicit RFC 9421 rejection,
or a successful legacy retry after the bounded unknown-GET HTTP 400 signal, can
create short-lived legacy evidence. Other generic status codes, body text,
network failures, and malformed or incompatible `Accept-Signature` fields do
not.
