# Security and HTTP message signatures

## Current compatibility profile

Activity-Relay verifies signed inbound ActivityPub requests and signs outbound
deliveries with the relay actor identity. It also signs remote actor and object
`GET` requests for authorized-fetch and Mastodon secure-mode interoperability.

The current interoperable profile uses the established Fediverse
`Signature`-header format. Authorized-fetch requests sign `(request-target)`,
`Host`, and `Date`. Delivery requests additionally sign `Digest` and
`Content-Type`. Redirected fetches are re-signed for the new target and
authority, and the signed `Host` value matches the authority transmitted on the
wire.

Inbound validation continues to enforce actor and key ownership, actor-host
consistency, digest validation where required, blocked and limited-domain
policy, and person-only policy when configured.

## RFC 9421 roadmap

RFC 9421, HTTP Message Signatures, is a future interoperability workstream rather
than a silent replacement for the established Fediverse signature format.

The implementation plan is:

1. add inbound RFC 9421 verification while retaining current verification;
2. add RFC 9530 `Content-Digest` generation and validation;
3. validate signature creation time and a bounded replay window;
4. cover `@method`, `@target-uri`, authority, redirects, and altered targets;
5. validate key ownership and algorithm policy before accepting the message;
6. add outbound RFC 9421 signing behind explicit compatibility behavior;
7. test fallback and downgrade behavior against real Mastodon secure-mode
   instances; and
8. document the exact supported algorithms and covered components before
   enabling it by default.

References:

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
