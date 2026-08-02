# FEP-ae0c compatibility and Activity-Relay 3.0 design input

## Purpose

[FEP-ae0c](https://w3id.org/fep/ae0c) is a retrospective description of two
relay protocol families deployed in the Fediverse:

- the Mastodon-style protocol, which subscribes through a one-way `Follow` of
  the ActivityStreams Public collection and forwards eligible activity bodies;
- the LitePub-style protocol, which follows a relay actor bidirectionally and
  distributes content through `Announce`.

It records behavior that emerged in implementations. It is therefore an
interoperability reference, not permission to copy every historical quirk
without evaluating privacy, authenticity, loop prevention, and current
cross-software behavior.

This document characterizes Activity-Relay v2.5.0 against that retrospective
description and defines the questions that must be resolved before the 3.0
signing and relay-policy work changes runtime behavior.

The machine-readable companion is
[`testdata/fep-ae0c/cases.json`](../testdata/fep-ae0c/cases.json).

## Terminology

This repository uses the following terms consistently:

- **relay/forward**: send the received ActivityStreams body to relay receivers
  without replacing it with a relay-authored activity;
- **relay/announce**: create a relay-authored `Announce` and distribute that
  wrapper;
- **traditional subscriber**: a receiver registered through the Mastodon-style
  Public-collection follow flow;
- **follower-style receiver**: an actor participating in the LitePub-style
  reciprocal follow flow;
- **publisher**: a valid signed sender whose public activity is accepted for
  fan-out, whether or not that sender is a receiver.

These terms distinguish relay behavior from ActivityPub collection expansion
and from an HTTP intermediary forwarding an entire request.

## Current v2.5.0 behavior

Activity-Relay v2.5.0 supports both relay families and several deliberate
extensions:

1. A traditional `Follow` may establish an inbox subscription.
2. A follower-style actor may follow the relay actor, receive `Accept` and a
   reciprocal relay `Follow`, then finalize the relationship with `Accept`.
3. Public `Create`, `Update`, `Delete`, and `Move` activities enter the
   traditional forwarding path when the Public collection appears in either
   `to` or `cc`.
4. A follower-style `Announce` with a referenced object is accepted only from an
   established subscriber or follower; the relay fetches the original activity
   before redistribution.
5. A same-origin public embedded `Announce`, as produced by current NodeBB
   category actors, is normalized into one relay-authored `Announce` referencing
   the canonical object ID.
6. Public URL-only or foreign-origin embedded `Announce` activities do not enter
   the NodeBB normalization path; they are retained as publisher observations.
7. Valid HTTP-signed public publishers may send without becoming relay
   receivers.
8. Remote `Application` and `Service` actors are accepted at
   implementation-defined actor paths, with legacy `/relay` and `/friendica`
   compatibility retained.
9. Remote actor and canonical-object fetches are HTTP-signed for authorized
   fetch.
10. Unknown public activity types are acknowledged without fan-out.

Inbound v2.5.0 requests use the established Fediverse HTTP `Signature` profile
and legacy `Digest` field. The relay does not currently generate or verify
Mastodon's historical Linked Data `RsaSignature2017` document proof.

## Compatibility matrix

| Area | FEP-ae0c Mastodon family | FEP-ae0c LitePub family | Activity-Relay v2.5.0 | 3.0 disposition |
| --- | --- | --- | --- | --- |
| Subscription target | Public collection through relay inbox | Relay actor | Supports both | Preserve |
| Relationship | One-way subscription | Reciprocal follow | Supports both receiver types | Preserve |
| Distribution form | Forward eligible body | Relay-authored `Announce` | Both, plus NodeBB normalization | Preserve and document extension |
| Public in `to` | Public | Public | Forwarded | Preserve |
| Public only in `cc` | Commonly interpreted as unlisted | Not central | Forwarded as public relay traffic | Investigate and make policy explicit |
| Original document proof | Historical Mastodon flow uses an embedded LD proof | Not central | Preserved if already in a forwarded body; not generated or verified | Document separately from HTTP signatures |
| HTTP request authentication | Historical Fediverse `Signature` | Historical Fediverse `Signature` | Required inbound and outbound | Preserve; add RFC 9421 and RFC 9530 additively |
| Sender must be receiver | Commonly coupled to subscription | Commonly coupled to reciprocal follow | Open signed publishers are supported | Preserve as an Activity-Relay extension |
| Actor type and path | Historically implementation-specific | Historically constrained in some software | `Application` or `Service`, arbitrary valid path | Preserve |
| Public embedded same-origin `Announce` | Not a core case | Announce-oriented | Canonical NodeBB normalization | Preserve as a documented extension |
| Public URL-only `Announce` | Implementation-dependent | Reference is common | Publisher observation only | Characterize before changing |
| Unsupported public activity | Not exhaustively defined | Not exhaustively defined | HTTP 202 without fan-out | Add explicit observability and tests |
| Duplicate/loop prevention | Must not relay an activity repeatedly | Must not relay an activity repeatedly | Existing storage and normalization reduce several loops | Complete a dedicated invariant audit |
| Optional `state` property | Seen in historical examples | Seen in some implementations | Not part of the public contract | Do not add without evidence |
| Authorized object fetch | Outside the original relay handshake | Referenced objects require fetching | Relay-signed GET supported | Preserve |

## Material differences requiring decisions

### Public in `to` versus `cc`

The current routing condition treats the Public URI in `to` and `cc`
identically. Mastodon commonly maps Public in `to` to public visibility and
Public only in `cc` to unlisted visibility. Relaying both can therefore broaden
the distribution of content whose author expected public accessibility but not
public-timeline amplification.

The 3.0 design should expose an explicit policy rather than retain an accidental
condition:

```yaml
relay_policy:
  traditional_visibility: public_to_or_cc
```

Candidate values are:

- `public_to_only`
- `public_to_or_cc`

The 3.0 default remains undecided until characterization and interoperability
tests demonstrate the consequences for Mastodon, Friendica, NodeBB, and
LitePub-family software. Migration documentation must state whether an existing
installation retains its v2.5 behavior.

### Linked Data proofs and HTTP message signatures

An embedded Linked Data proof authenticates an ActivityStreams document
independently of one HTTP hop. The established Fediverse `Signature` header and
RFC 9421 authenticate an HTTP message exchanged between two systems. RFC 9421
does not replace document-level proof semantics.

Activity-Relay currently:

- preserves a proof already present when forwarding the original body;
- does not generate `RsaSignature2017`;
- does not verify that proof before fan-out;
- authenticates inbound and outbound HTTP requests; and
- creates relay-authored activities for normalization paths.

The first RFC 9421/RFC 9530 milestone must not silently claim origin-document
verification. Document-proof support, if ever added, requires separate
canonicalization, algorithm, context, security, and interoperability review.

### Open publishers

FEP-ae0c is primarily organized around relay-client relationships.
Activity-Relay deliberately separates publishing permission from receiver
membership after validating the HTTP signer, actor binding, domain policy, and
public audience.

This extension enables WordPress and other publishers without requiring them to
receive full relay traffic. It must remain covered by security and regression
tests throughout the 3.0 signing refactor.

### Public Announce normalization

Activity-Relay's NodeBB compatibility path is neither simple body forwarding nor
a generic LitePub relay. It replaces a same-origin embedded transport wrapper
with a relay-authored canonical-object `Announce` and sends the same authenticated
wrapper to all receiver styles.

The same-origin restriction and refusal to amplify URL-only or foreign-origin
public boosts are anti-loop and anti-amplification policy. They must not be
generalized merely to resemble a retrospective example.

### Activity selection and acknowledgement

The current traditional path fans out `Create`, `Update`, `Delete`, and `Move`,
plus the selected public embedded `Announce` extension. Other public activity
types receive HTTP 202 without fan-out.

Before 3.0, every accepted type should have a documented outcome and a bounded
metric or structured reason. HTTP acceptance, policy rejection, publisher
accounting, queue admission, and receiver delivery must remain distinguishable.

### Follow object and historical protocol quirks

A Mastodon-style `Follow` of the Public pseudo-collection is a deployed protocol
behavior even though ActivityPub generally defines a `Follow` object as an
actor. Activity-Relay should preserve demonstrated interoperability while
keeping permissive behavior explicit and test-covered.

The optional historical `state` property is not required by Activity-Relay and
should not be emitted or required without a real interoperability case.

### Duplicate and reflection protection

FEP-ae0c warns about two relays following each other and reflecting activity.
Activity-Relay already limits several Announce paths, retains shared activity
records through fan-out, and rejects some unsafe normalization cases. A
dedicated audit must still prove the invariant across:

- direct body forwarding;
- fetched LitePub references;
- relay-authored Announce wrappers;
- retries and at-least-once recovery;
- repeated inbound IDs;
- two-relay topologies; and
- mixed traditional and follower-style membership.

No 3.0 protocol change should ship until this fixture is converted into
executable unit and multi-relay integration coverage.

## 3.0 design decisions already accepted

- Preserve both deployed relay families.
- Preserve the actor ID, actor key, inbox, collections, Redis compatibility, and
  established Fediverse `Signature` profile.
- Add RFC 9421 HTTP Message Signatures and RFC 9530 `Content-Digest`
  additively.
- Keep legacy-only, dual, and RFC-only signing profiles distinct in code and
  tests; RFC-only must not become the default until interoperability supports
  it.
- Keep open publisher ingestion and its actor-host security checks.
- Keep NodeBB normalization as a documented extension rather than presenting it
  as FEP-ae0c behavior.
- Do not add Linked Data proof generation as an incidental part of RFC 9421
  work.
- Do not implement alternative durable-state backends as part of 3.0.

## Characterization sequence

1. Land this document and the fixture catalog without runtime changes.
2. Convert fixture rows into unit tests that freeze v2.5 behavior.
3. Add multi-relay loop and repeated-ID tests.
4. Test `to` versus `cc` behavior against real receiving software.
5. Refactor request signing and verification behind explicit legacy, RFC 9421,
   and dual profiles.
6. Add RFC 9530 digest generation and verification from the exact transmitted
   body bytes.
7. Repeat traditional, LitePub, open-publisher, NodeBB, authorized-fetch, and
   receiver-presentation integration tests.
8. Select 3.0 defaults only after the compatibility evidence is recorded.

## Executable characterization coverage

The first executable characterization tranche is mapped in
[`testdata/fep-ae0c/coverage.json`](../testdata/fep-ae0c/coverage.json).

Fixture-driven API tests now freeze:

- traditional Public-collection subscription;
- LitePub-style follow and reciprocal `Accept`;
- v2.5 forwarding for Public in `to` and Public only in `cc`;
- byte-for-byte forwarding of a body containing an existing
  `RsaSignature2017` proof, without claiming proof verification;
- open publisher fan-out without receiver membership;
- HTTP 202 without fan-out for unsupported public `Like`;
- publisher-only handling for URL-only and foreign-origin public
  `Announce`;
- the same-origin NodeBB normalization guard; and
- `Application` actors at implementation-defined paths.

Existing tests provide the signed authorized-fetch coverage referenced by the
fixture catalog.

The `litepub-announce-reference` fixture is now executable against a real
remote HTTP origin. The origin verifies the relay's HTTP signature on both the
canonical-activity GET and actor-document GET before returning either document.
The test then verifies canonical publisher accounting and a relay-authored
`Announce` queued for both receiver styles.

The `repeat-id-two-relay-loop` case now has diagnostic executable coverage.
The diagnostic uses two independently configured API processes, two workers,
two Redis instances, independent actor keys, trusted TLS frontends, and a
signed origin. Diagnostic execution is not yet a passing invariant: the
machine-readable classification determines whether the observed behavior is
bounded or requires a runtime loop-suppression fix.

The RFC 9421 dual-profile fixture remains future protocol coverage. External
review feedback may add or refine fixtures without changing these frozen v2.5
characterization assertions.

### Two-relay process-probe contract

The remaining repeated-ID/reflection audit is specified in
[`testdata/fep-ae0c/two-relay-probe-contract.json`](../testdata/fep-ae0c/two-relay-probe-contract.json).
The implemented probe is run through
`contrib/ops/test_fep_ae0c_two_relay_probe.sh`. It uses two API processes, two
workers, two Redis instances, independent actor keys, locally trusted TLS
frontends, and a signed origin. It records every cross-relay inbox POST, signed
remote GET evidence, process configuration and logs, and initial/final Redis
key inventories. A hard POST threshold prevents a discovered reflection cycle
from running indefinitely.

Successful command execution is not itself a passing invariant. Only
`no_reflection_observed`, or a separately reviewed and explicitly bounded
`reflection_settled`, may be promoted to passing coverage. Active or
threshold-reaching reflection requires a runtime loop-suppression fix first.

### Relay-reflection suppression invariant

The process probe demonstrated that two mutually connected v2.5 relays could
continuously mint new relay-authored `Announce` IDs for one canonical activity.
This was not ordinary retry duplication: every wrapper ID was new and every
observed HTTP signature was valid.

Activity-Relay now treats loop prevention as a non-configurable protocol safety
invariant. When processing a referenced relay `Announce`, it:

- excludes the relay that supplied the activity from the new fan-out;
- excludes the canonical origin domain;
- atomically reserves a SHA-256-derived Redis key for the fetched canonical
  activity ID before minting a wrapper;
- acknowledges a duplicate reference without minting another wrapper; and
- retains the marker for the same bounded horizon as the shared relay payload.

Known queue-admission failures release their reservation. The marker contains no
raw actor, object, inbox, or activity URL. The real-process probe is now a
required passing invariant and must report `no_reflection_observed` with no
non-seed cross-relay POST or final delivery backlog.

## Fixture status

The initial fixtures are specifications. They intentionally include both covered
behavior and unresolved audit cases. A fixture marked `specified` is not a claim
that an executable test already proves it. The fixture validation test checks
schema quality, identifiers, decisions, and payload shape; later milestones
will bind each fixture to runtime assertions.
