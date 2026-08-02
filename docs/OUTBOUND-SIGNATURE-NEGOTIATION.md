# Outbound HTTP-signature negotiation

## Status

This document defines the reviewed negotiation core. Runtime configuration
continues to accept only `legacy` and `rfc9421`; `dual` remains rejected until
the next tranche wires this policy to authorized fetch and delivery.

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

Future `dual` planning is destination-aware:

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

## Runtime work still required

The next tranche must:

1. accept `dual` only after constructing the Redis capability store;
2. parse and validate compatible `Accept-Signature` fields;
3. define the narrow response evidence that counts as an explicit signature
   rejection;
4. execute the GET fallback at most once;
5. record successful modern fetch and delivery observations;
6. preserve one selected profile across delayed delivery retries; and
7. add mixed legacy/RFC 9421 real-process fixtures.
