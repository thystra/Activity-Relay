# ActivityPub interoperability

## Delivery retries and slow receivers

RC2 retries each relay fan-out target five times after the initial attempt and
retains the shared activity body for the full retry horizon. Receiver-side
ActivityPub implementations may synchronously fetch actors, keys, objects,
parents, contexts, or collections before returning from `/inbox`; transient
timeouts therefore no longer become immediate terminal loss at the relay.
Retries remain at-least-once and a receiver must tolerate duplicate delivery
when it commits an activity after the relay client has timed out.

Structured worker diagnostics record public identifiers, hashed body identity,
attempt and elapsed-time data, receiver and origin domains, HTTP status, error
classification, and bounded error responses. They intentionally exclude raw
activity bodies, signatures, and key material.

This document records interoperability behavior validated for the stable
Activity-Relay `v2.4.0` release and the `v2.5.0-rc1` candidate line. The
release-candidate results remain prerelease evidence; no matrix can guarantee
that every version or configuration of every ActivityPub server behaves
identically.

## Subscription models

Activity-Relay supports both common relay subscription models:

- Traditional relay subscribers register an inbox with the relay endpoint at
  `/inbox`.
- Follower-style servers follow the relay actor at `/actor` and receive an
  `Accept` plus a reciprocal relay `Follow`.

Activity-Relay publishes its own relay actor as `Application`, with
`preferredUsername` `relay` at `/actor`. This matches current Friendica relay
discovery while preserving the established actor ID, collections, endpoints,
and `#main-key` identity.

The relay actor may be followed by standards-style remote `Application` or
`Service` actors at implementation-defined actor paths. Legacy `/relay` and
`/friendica` server actors remain supported.

## v2.4.0 validation matrix

The following paths were validated with real signed activities before the stable `v2.4.0`
release:

| Publisher path | Relay behavior | Receiving software | Result |
| --- | --- | --- | --- |
| Friendica public `Create` | Validate, record, and fan out | NodeBB | Received and displayed |
| NodeBB category public embedded `Announce` | Replace with one relay-signed `Announce` referencing the canonical object | Friendica | Fetched and displayed |
| NodeBB category public embedded `Announce` | Replace with one relay-signed `Announce` referencing the canonical object | Mastodon | Fetched, imported, tagged, and displayed |
| NodeBB category public embedded `Announce` | Replace with one relay-signed `Announce` referencing the canonical object | Another NodeBB server | Canonical object fetched |

The NodeBB-to-Mastodon test was performed without relying on an indirect
Friendica follow path.

## v2.5.0 release-candidate validation

The v2.5.0 line was exercised through an isolated relay after the Machinery v2
reliable-claims migration and again after the Friendica actor-profile correction:

| Publisher or fetch path | Receiving software or endpoint | Result |
| --- | --- | --- |
| NodeBB category public embedded `Announce` | Mastodon | Accepted, fetched, imported, and displayed |
| Mastodon public `Create` with ordinary fetch policy | NodeBB | Delivered and displayed |
| Mastodon public `Create` with secure mode enabled | Friendica 2026.05 through Activity-Relay | Relay registration, delivery, canonical-object and media retrieval, hashtags, and presentation passed |
| Unsigned canonical-object `GET` | Mastodon secure-mode endpoint | Rejected with HTTP 401 `Request not signed` |
| Relay-signed canonical-object `GET` using the deployed actor identity | Same Mastodon secure-mode endpoint | Returned HTTP 200 ActivityPub JSON for the intended object |

The ordinary bidirectional NodeBB/Mastodon test ruled out another configured relay
as the delivery path. The secure-mode control used the same relay actor key and
HTTP-signature implementation as the deployed candidate. Friendica was freshly
registered only after it fetched the corrected `Application` actor profile.

### NodeBB secure-mode canonical-object limitation

NodeBB 4.14.2 accepted the relay-signed delivery but returned HTTP 424 when the
referenced Mastodon object required authorized fetch. Receiving-side evidence
showed NodeBB's application-context canonical-object request was unsigned and the
secure-mode server returned HTTP 401. The same object was returned when fetched
with the Activity-Relay signature implementation.

This is a downstream NodeBB receiving limitation, not a failure of the relay POST
signature. It should be reported upstream. Operators using NodeBB receivers and
remote servers that require signed `GET` requests should expect this limitation
until NodeBB signs application-actor object fetches.

## Public embedded Announce normalization

Some servers, including NodeBB category actors, publish locally created content
as a public `Announce` with an embedded `Article` or `Note`. For a same-domain
embedded object, Activity-Relay creates one relay-authored `Announce` whose
object is the embedded object's canonical ID and sends that same authenticated
wrapper to every receiver style.

This keeps the HTTP signer and JSON activity actor aligned for strict receivers
while preserving the original author, content, media, and tags when the
receiver fetches the canonical object.

The relay deliberately does not fan out:

- public `Announce` activities whose object is only a URL; or
- public embedded `Announce` activities whose object belongs to another domain.

Those activities remain publisher-accounting events. This avoids relay loops
and unintended amplification of ordinary boosts.

## Signatures and actor keys

Outbound HTTP signatures bind the signed `Host` value to the exact authority
sent on the wire, including a non-default port.

The relay actor publishes `publicKeyPem` as X.509 SubjectPublicKeyInfo PEM:

```text
-----BEGIN PUBLIC KEY-----
```

Inbound verification accepts both SubjectPublicKeyInfo and legacy PKCS#1 RSA
public keys. Changing the public serialization does not rotate `actor.pem`, the
relay actor ID, or the `#main-key` key ID.

## Canonical objects, tags, and discovery

The relay-generated wrapper references the publisher's canonical object. The
receiving server fetches that object and is responsible for importing its
content, author, attachments, and ActivityStreams `Hashtag` entries.

A successful import does not guarantee that a post appears in every discovery
surface. Local hashtag review, trend approval, moderation, language, and
timeline settings may affect visibility independently of relay delivery.

## Troubleshooting

Start with the public relay request status:

- `202 Accepted` means the relay accepted the signed activity for processing.
- `400 Bad Request` commonly indicates actor resolution, signature or digest
  verification, or JSON decoding failed. Version 2.4.0 logs the bounded failure reason
  with request metadata, but never logs request bodies, signatures, or key
  material.

For NodeBB specifically, verify that:

1. the relay relationship is active;
2. the topic belongs to a publicly readable positive-numbered category;
3. the category actor URL returns a valid ActivityPub actor and public key;
4. the canonical post URL returns the expected `Article` or `Note`; and
5. retry records identify the relay inbox as their destination.

During pre-release validation, NodeBB 4.14.2 exposed two useful failure signatures:

- Uncategorized topics could serialize a numeric audience such as
  `"to": [-1]`, which is not a valid string-valued ActivityPub audience.
- A category actor endpoint could fail if its configured icon file was absent,
  preventing the relay from resolving the signing key.

Check the current NodeBB release before assuming those version-specific issues
remain unresolved.

When a receiver appears not to show a post, distinguish transport from local
presentation: confirm the canonical object was fetched, then inspect the
receiver's stored status and tags before concluding relay delivery failed.
