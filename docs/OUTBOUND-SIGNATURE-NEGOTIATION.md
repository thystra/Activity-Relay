# Outbound HTTP-signature negotiation

## Status

This document defines the destination-aware negotiation policy now wired into
runtime configuration. `OUTBOUND_SIGNATURE_PROFILE` accepts `legacy`,
`rfc9421`, and `dual`. `dual` is a policy that selects one concrete wire profile
per operation; it is not itself a signature grammar.

## Identity and scope

Capability state is keyed by normalized HTTP origin and by operation scope:

- `fetch` covers relay-authenticated actor and object GET requests;
- `delivery` covers ActivityPub inbox POST requests.

The normalized origin lower-cases scheme and host, removes a trailing DNS dot,
strips default ports 80 and 443, preserves non-default ports, and excludes
paths, queries, and fragments.

Fetch and delivery evidence are never interchangeable. A remote server may
support modern signatures on one endpoint class but not the other.

## Stored evidence

The core stores only bounded evidence:

- `successful_rfc9421`: an RFC 9421 request was accepted;
- `accept_signature`: a later runtime parser established a compatible
  RFC 9421 `Accept-Signature` request for subsequent messages; or
- `explicit_rfc9421_rejection`: a later runtime classifier established that
  RFC 9421 itself was rejected and a legacy preference should be used
  temporarily.

A generic 4xx or 5xx status, timeout, connection failure, DNS failure, or
unparseable response is not signature-capability evidence.

Positive RFC 9421 evidence defaults to fourteen days. An explicit negative
observation defaults to twenty-four hours. Negative evidence is shorter-lived
so transient incompatibility does not pin a destination to legacy
indefinitely.

Redis keys contain a SHA-256 digest of scope and normalized origin. Stored hash
fields contain the normalized origin, scope, profile, evidence, observation
time, and expiration time. A Lua update rejects observations that are older
than or equal to the current record.

## Planning rules

Fixed profiles remain simple:

- configured `legacy` always emits one legacy request;
- configured `rfc9421` always emits one RFC 9421 request.

`dual` planning is destination-aware:

| Scope | Fresh capability | First profile | Fallback |
|---|---|---|---|
| Fetch | RFC 9421 | RFC 9421 | none |
| Fetch | Legacy | Legacy | none |
| Fetch | Unknown | RFC 9421 | Legacy only after explicit signature rejection |
| Delivery | RFC 9421 | RFC 9421 | none |
| Delivery | Legacy | Legacy | none |
| Delivery | Unknown | Legacy | none |

The plan always contains exactly one first-attempt wire profile. It never
requests simultaneous incompatible `Signature` grammars.

## Non-idempotent delivery invariant

A delivery POST plan never has a fallback. Activity-Relay must not respond to a
remote rejection by sending the same activity again under another signature
grammar. Retries caused by the existing queue remain retries of the same
selected profile and are not negotiation attempts.

## Runtime status

The runtime now:

1. accepts `dual` only with a constructed Redis capability store;
2. parses and validates compatible `Accept-Signature` fields;
3. recognizes only bounded explicit signature-rejection evidence;
4. executes an eligible GET fallback at most once;
5. records successful modern fetch and delivery observations;
6. preserves one selected profile across delayed delivery retries; and
7. includes a mixed-profile real-process negotiation probe.

The remaining release gate is cross-software interoperability: Mastodon,
Friendica, NodeBB, WordPress, and the two-relay topology must be exercised
before selecting the Activity-Relay 3.0 stable default. RC1 therefore retains
`legacy` as the omitted/default outbound profile.

## Runtime wiring

`OUTBOUND_SIGNATURE_PROFILE=dual` is now accepted after RelayConfig constructs
the Redis capability store and destination negotiator. Fixed `legacy` and
`rfc9421` modes continue to bypass capability selection.

### Authorized fetch

The configured signer owns initial and redirected GET signing. For an unknown
origin it sends RFC 9421 first. A single legacy fallback is permitted only when
the final response is `401` or `403` and a `WWW-Authenticate` field explicitly
contains the `Signature` authentication scheme.

The rejected response body is drained only to a small bound and closed before
the fallback. The fallback is executed once; its response cannot trigger
another negotiation fallback. Redirects are re-planned and re-signed for their
own normalized destination origin.

### Accept-Signature evidence

A successful response can advertise RFC 9421 support with
`Accept-Signature`. Activity-Relay accepts the field as evidence only when it
contains exactly one `activitypub` member whose covered components exactly
match the current fetch or delivery profile.

The supported request parameters are:

- bare `created`;
- `alg="rsa-v1_5-sha256"`;
- the relay's exact `keyid`; and
- `tag="activitypub"`.

Parameters may be omitted because the signer can add them. `expires`, a
receiver-selected `nonce`, unknown parameters, component parameters, additional
signature labels, malformed structured fields, a different key, algorithm, or
tag, and a different component order are ignored as capability evidence.

### Delivery profile persistence

New `register` and `relay-v2` Machinery tasks contain a third
`signatureProfile` string argument with the concrete `legacy` or `rfc9421`
wire profile selected before enqueueing. Machinery retries reuse the same task
signature, so delayed retries cannot renegotiate or change profiles.

Workers remain compatible with existing two-argument tasks. A legacy queued
task uses the fixed configured profile when it is concrete; under `dual`, an
old task safely resolves to the unknown-delivery rule, `legacy`.

A delivery response can update capability state for future tasks, but the
current POST is never resent. A successful RFC 9421 delivery records positive
evidence. A successful legacy delivery with a compatible `Accept-Signature`
also records positive evidence. A modern delivery receiving the narrow explicit
legacy challenge records a short-lived legacy preference for future tasks.
