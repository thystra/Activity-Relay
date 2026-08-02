# HTTP message-signature architecture

Activity-Relay supports the established Fediverse HTTP Signature profile and is
adding RFC 9421 HTTP Message Signatures with RFC 9530 digest fields in bounded
tranches.

## Compatibility boundary

The existing `SignGET` and `SignPOST` methods remain the legacy profile. The
first standards tranche adds explicit profile-aware primitives without changing
any production call site or default.

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

## Security and rollout gates

Inbound RFC 9421 verification and explicit outbound profile selection are
runtime-selectable. Remaining rollout gates are:

1. destination capability state and an explicitly reviewed dual negotiation
   policy;
2. outbound profile metrics and negotiation outcomes where applicable; and
3. mixed-version interoperability tests before changing the default.

Linked Data proofs remain separate from HTTP request authentication.

## Inbound verification core

The inbound core verifies the modern ActivityPub POST profile without changing
the active inbox decoder yet.

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

Runtime actor retrieval, inbox profile selection, metrics, and HTTP response
classification remain for the next tranche.

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

- `legacy`: the established Fediverse `Signature` and `Digest` profile; and
- `rfc9421`: RFC 9421 `Signature-Input` and `Signature`, with RFC 9530
  `Content-Digest` on POST requests.

An empty or omitted value is normalized to `legacy`. This preserves existing
installations and generated test configurations. Unknown values fail startup.
`dual` also fails startup because it is a destination-aware policy rather than
one safe request format.

The API server and every worker construct the same configured signer from the
same validated `RelayConfig`. Authorized-fetch redirects remove all legacy and
modern signature fields before re-signing the new request target.

Selecting `rfc9421` is an explicit operator action. It does not probe a
destination, retry a POST with another signature grammar, or fall back after a
remote rejection.

## Destination-aware negotiation core

The negotiation core is documented in
`docs/OUTBOUND-SIGNATURE-NEGOTIATION.md`.

Capability identity is origin- and scope-specific. Fetch evidence and delivery
evidence are not interchangeable. Positive RFC 9421 observations expire after
fourteen days by default; explicit negative observations expire after one day.

An unknown fetch plan may select RFC 9421 first and expose a legacy fallback,
but the fallback is eligible only after a later runtime layer has explicitly
classified a signature rejection. Transport errors and generic HTTP failures
never authorize fallback.

An unknown delivery plan remains legacy. Delivery plans never contain a
fallback, preventing blind duplicate POST delivery.

This tranche does not make `dual` a valid runtime configuration value.
