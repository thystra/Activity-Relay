# Activity-Relay roadmap

## Current stable release

`v2.5.1` is the current stable maintained-fork patch release. It is prepared
from loop-suppression merge commit
`855ea56fe088e738dc708b99690d1224da0be330`.

The patch preserves the established relay actor ID, endpoints, collections,
`#main-key` identity, configuration, Redis data, queue formats, operator-owned
website content, and supported deployment models.

## v2.5.1 completed

- Prevent authenticated follower-style relays from reflecting one canonical
  activity indefinitely through freshly minted `Announce` wrapper IDs.
- Exclude both the supplying relay and canonical origin from referenced-
  `Announce` fan-out.
- Atomically suppress repeated canonical references for the retained delivery
  horizon using a SHA-256-derived Redis marker without raw URLs.
- Retain normal acknowledgement for duplicate references and release known
  failed queue-admission reservations.
- Require the real-process two-relay test to report
  `no_reflection_observed`, valid signatures, retained canonical markers, and
  no final queue backlog.

## v2.5.0 completed

- Relay-signed actor and object `GET` requests for Mastodon authorized-fetch and
  secure-mode interoperability.
- `Application` relay-actor classification for current Friendica discovery
  without rotating or replacing the actor identity.
- Shared exact-authority HTTP signing for remote fetch and outbound delivery.
- Durable receiver delivery-health state and `/status.json` schema version 4.
- Optional private Prometheus, liveness, and Redis-readiness listener.
- Redis-backed operational metrics with bounded labels.
- Migration to the maintained Redis-only Machinery v2 fork pinned to
  `v2.0.17-0.20260730204902-5efae3f700cd`.
- Leased in-flight claims, renewal, acknowledgement, expired-claim recovery, and
  explicit at-least-once behavior.
- Five delayed fan-out retries after the initial target attempt.
- Fifteen-minute shared activity retention and terminal-only `remain_count`
  decrementing.
- Bounded structured delivery diagnostics with task, activity, receiver,
  timing, status, retry, digest, and remote-response correlation.
- Container, native-package, interruption, identity, Redis, secure-mode,
  interoperability, production-deployment, and overnight-soak validation.

## v2.5.0 release verification completed

- Stable GitHub release metadata, reviewed release notes, checksums, internal
  Debian version, package contents, and required examples passed independent
  verification.
- The `2.5.0`, `2.5`, `2`, and `latest` container tags publish matching
  `linux/amd64` and `linux/arm64` manifests.
- The stable `2.5.0-1` package is deployed in production with preserved actor
  and key identity, dedicated Redis state, healthy API and worker services,
  private observability, and healthy public endpoints.
- RC2, stable-promotion, publication, and production-deployment evidence is
  retained outside the public repository.

## Activity-Relay 3.0 design

The next active protocol milestone is a modern signing architecture informed by
the retrospective FEP-ae0c relay profiles.

### RFC 9421 / RFC 9530 implementation tranches

- [x] Add pinned RFC 9421 structured-field, RSA-signing, and RFC 9530 digest
  primitives while preserving every existing runtime call site on `legacy`.
- [x] Add the inbound RFC 9421 verification core with actor/key binding,
  required component policy, bounded time validation, and Redis-backed nonce
  replay prevention.
- [x] Wire the inbound core into the ActivityPub decoder with authenticated
  actor-key retrieval, strict legacy/modern profile selection, bounded
  metrics, and a real-process signed inbound/replay/tamper probe.
- [x] Add explicit outbound `legacy` and `rfc9421` configuration with safe
  startup validation, shared server/worker wiring, and no default change.
- [x] Add the destination-aware negotiation core with origin- and
  scope-specific expiring capability state, GET-only explicit-rejection
  fallback planning, and a hard no-fallback invariant for delivery POSTs.
- [x] Wire `dual` runtime negotiation with validated `Accept-Signature`
  evidence, one bounded explicit-challenge GET fallback, stable queued
  delivery-profile selection across retries, and no blind duplicate POST
  delivery.
- [ ] Run mixed-profile Mastodon, Friendica, NodeBB, WordPress, and two-relay
  interoperability before selecting the Activity-Relay 3.0 default.

- Land and maintain `docs/FEP-AE0C-COMPATIBILITY.md` and the machine-readable
  fixture catalog before changing runtime behavior.
- Convert current behavior into executable characterization tests. The first
  fixture-driven API tranche covers traditional and LitePub relationship
  handling, audience routing, exact-body forwarding, open publishers, NodeBB
  normalization guards, unsupported activity handling, and actor-path
  compatibility. Referenced-Announce now has a signed-origin integration
  test. The authenticated two-relay reflection defect is now guarded by
  non-configurable source-relay exclusion, canonical-reference deduplication,
  and a required real-process `no_reflection_observed` invariant.
- Resolve Public-in-`to` versus Public-only-in-`cc` through explicit
  configuration and real interoperability evidence rather than an implicit
  routing condition.
- Add RFC 9421 HTTP Message Signatures and RFC 9530 `Content-Digest` support
  additively while preserving the established Fediverse `Signature` profile.
- Keep document-level Linked Data proof semantics separate from HTTP message
  authentication; do not claim or add proof verification incidentally.
- Retain the passing mixed two-relay no-reflection invariant while the 3.0
  signing architecture is developed.

## Deferred work after v2.5.1

- Investigate NodeBB follow interoperability separately from Activity-Relay:
  reproduce failed attempts to follow `Julian@activitypub.space` from Friendica
  and WordPress, use a controlled NodeBB instance, and inspect the corresponding
  application logs.
- Define stale-receiver retention and cleanup configuration after sufficient
  delivery-health history exists. Begin in report-only dry-run mode; active
  membership and queued or claimed work remain non-configurable deletion
  barriers.
- Implement the Activity-Relay 3.0 RFC 9421 and RFC 9530 signing milestone
  under the FEP-ae0c characterization and interoperability gates above.
- Replicated or alternative durable-state backends are tabled. Revisit only
  when production experience, availability requirements, or operator demand
  justify the value and compatibility cost.
- Continue maintaining and rebasing the Redis-only Machinery fork for relevant
  upstream security and correctness fixes.

- The opt-in Activity-Relay Directory version 1 client contract is implemented:
  bounded independently enabled origins, strict lifecycle documents, a
  directory-specific RFC 9421/RFC 9530 profile, bounded closed responses,
  redirect refusal, one explicit not-registered reconciliation, and a shared
  server-accepted cryptographic fixture. No endpoint is active by default.
  Remaining work is the reviewed manual command surface, durable unregister
  configuration semantics, API-process scheduler/lease, health and public
  directory behavior, and cross-repository staging soak.
## Release gates for future versions

Before any release:

1. update `CHANGELOG.md`, `readme.md`, affected documentation, and versioned
   release notes;
2. run all validation required by `AGENTS.md` and `docs/RELEASING.md`;
3. preserve actor identity, configuration, Redis compatibility, and
   operator-owned website content;
4. validate package installation, upgrade or reinstallation, checksums,
   metadata, and container manifests;
5. for federation changes, demonstrate a real signed publisher-to-receiver path
   and distinguish transport acceptance, object import, and presentation; and
6. before stable promotion from an RC, complete and retain a production soak of
   the exact candidate application code.

## Upstream review

Continue monitoring upstream commits, issues, and pull requests under the
policy in `docs/UPSTREAM.md`. Record upstream commit, issue, or pull-request
references when a change is ported.
