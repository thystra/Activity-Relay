# Security and HTTP message signatures

## Current compatibility profile

Activity-Relay verifies signed inbound ActivityPub requests and signs outbound
deliveries with the relay actor identity. It also signs remote actor and object
`GET` requests for authorized-fetch and Mastodon secure-mode interoperability.

The established Fediverse `Signature`/`Digest` profile remains supported and is
the omitted/default outbound profile for 3.0 RC1. Authorized-fetch requests sign
`(request-target)`, `Host`, and `Date`; delivery requests additionally sign
`Digest` and `Content-Type`. Redirected fetches are re-signed for the new target
and authority, and the signed `Host` value matches the authority transmitted on
the wire.

Activity-Relay 3.0 also implements RFC 9421 HTTP Message Signatures and RFC 9530
`Content-Digest`. Inbound requests containing `Signature-Input` are committed to
the modern verifier and do not downgrade to the legacy parser after modern
verification failure. Modern verification enforces bounded creation time,
request authority and covered-component policy, RSA signature validity, body
digest integrity, actor/key binding, and Redis-backed nonce replay prevention.

Outbound `OUTBOUND_SIGNATURE_PROFILE` accepts fixed `legacy`, fixed `rfc9421`,
or destination-aware `dual`. `dual` selects one concrete wire profile per
operation. It may make one bounded legacy fallback for an idempotent GET after
recognized compatibility evidence, but a delivery POST is never re-sent under
another signature grammar. Capability state is bounded, origin- and
scope-specific, and contains no raw request bodies, signatures, or key material.

The stable 3.0 outbound default remains an interoperability decision. RC1 keeps
`legacy` as the default until the Mastodon, Friendica, NodeBB, WordPress, and
two-relay validation matrix is complete.

## RFC 9421 and RFC 9530 references

- Mastodon security specification:
  <https://docs.joinmastodon.org/spec/security/>
- RFC 9421, HTTP Message Signatures:
  <https://www.rfc-editor.org/rfc/rfc9421.html>
- RFC 9530, Digest Fields:
  <https://www.rfc-editor.org/rfc/rfc9530.html>

## Logging and privacy

Do not log private keys, complete signatures, request bodies, inbox URLs, actor
IDs, or unbounded remote response bodies. Public status and metric labels must
remain bounded and must not expose private relay topology or operator
configuration.
