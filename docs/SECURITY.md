# Security and HTTP message signatures

## Current compatibility profile

Activity-Relay verifies signed inbound ActivityPub requests and signs outbound
deliveries with the relay actor identity. It also signs remote actor and object
`GET` requests for authorized-fetch and Mastodon secure-mode interoperability.

The established Fediverse `Signature`/`Digest` profile remains supported. The
3.0 omitted/default outbound policy is destination-aware `dual`; explicit
`legacy` remains available for fixed compatibility. Legacy authorized-fetch
requests sign `(request-target)`, `Host`, and `Date`; legacy delivery requests
additionally sign `Digest` and `Content-Type`. Redirected fetches are re-signed
for the new target and authority, and the signed authority matches the request
transmitted on the wire.

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

The 3.0 stable default is `dual`. This is a compatibility policy rather than a
wire format: unknown fetches can probe RFC 9421 with one bounded legacy fallback,
while unknown delivery POSTs begin with legacy and never change signature
grammar across retries. Fetch and delivery capability evidence remain separate.

## RFC 9421 and RFC 9530 references

- Mastodon security specification:
  <https://docs.joinmastodon.org/spec/security/>
- RFC 9421, HTTP Message Signatures:
  <https://www.rfc-editor.org/rfc/rfc9421.html>
- RFC 9530, Digest Fields:
  <https://www.rfc-editor.org/rfc/rfc9530.html>

## Logging and privacy

Do not log private keys, complete signatures, request bodies, credentials,
nonces, or unbounded remote response bodies. Delivery diagnostics may contain
public ActivityPub identifiers, destination inbox URLs, and remote response text
up to the documented bound; operators should therefore protect and retain logs
as operationally sensitive data. Public status and metric labels must remain
bounded and must not expose private relay topology or operator configuration.
