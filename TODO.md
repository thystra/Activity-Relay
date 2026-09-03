# Activity-Relay roadmap

## Current stable release

`v3.0.0` is the current stable maintained-fork release line. It promotes the
accepted 3.0 release-candidate runtime after mixed-signature interoperability,
two-relay isolation, Activity-Relay Directory heartbeat/lifecycle acceptance,
and stable-default review.

The release preserves the established relay actor ID, endpoints, collections,
`#main-key` identity, Redis data, existing task readability, operator-owned
website content, and supported deployment models. Destination-aware `dual` is
the omitted/default outbound signature policy; explicit `legacy` and `rfc9421`
remain available.

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

The active 3.0 release milestone is the modern signing architecture informed by
the retrospective FEP-ae0c relay profiles, now in stable-promotion validation.

### Repository authority and CI

- [x] Move repository authority to Forgejo while retaining GitHub as a
  downstream public push mirror and independent-validation surface.
- [x] Add the shared-workstation Forgejo runner pilot and full source,
  container, Caddy, and Debian-package validation workflows.
- [x] Make mirrored GitHub workflows validation-only so mirrored commits and
  tags cannot independently publish edge or release artifacts.
- [x] Add a manual canonical Forgejo release-artifact workflow that requires
  an exact reviewed commit/version, rebuilds the release validation gates, and
  emits one checksummed Debian/SBOM/multi-architecture OCI artifact set without
  publishing it.
- [x] Prepare, canonically build, inspect, tag, and publish
  Activity-Relay `3.0.0-rc1`; prove that the accepted artifact bytes can be
  published without rebuilding them.
- [x] Deploy the accepted RC1 to the isolated test relay and preserve the
  existing actor identity, Redis state, subscriptions, and delivery-health
  history.
- [x] Retest NodeBB 4.15.1 secure-mode canonical-object retrieval with upstream
  signing fix `8e61543`; a fresh Mastodon-to-relay-to-NodeBB path passed with
  the relay in `dual` mode and no new 401/403/424 failure.
- [x] Register the RC1 test relay with the RC-level Directory, verify the public
  healthy projection, and verify local scheduler state survives API restart.
- [x] Correct the RC1 remote Directory-status incompatibility by accepting
  Directory status schema 3 and its public-listing fields while retaining
  schema 2 compatibility.
- [x] Prepare, canonically build, inspect, tag, and publish
  Activity-Relay `3.0.0-rc2`, then deploy those exact bytes to the test relay
  and verify `relay directory status <origin>` against Directory RC4.
- [x] Upgrade `relay.argentwolf.org` to the accepted RC2 code without replacing
  Redis state, subscriptions, actor identity, or key identity; register both
  relays with `directory.argentwolf.org` and verify the public healthy projection.
- [x] Complete natural Directory heartbeat aging and a controlled
  unregister/re-register lifecycle exercise before stable promotion. Both live
  relays refreshed naturally; the test relay was durably disabled, authenticated
  unregister was verified while its public actor endpoint remained online, its
  Directory absence was confirmed, and re-enable plus API recreation returned it
  to scheduler-driven registration and a healthy public projection.
- [x] Prepare the stable 3.0 source/release metadata with application version
  `3.0.0`, Debian version `3.0.0-1`, destination-aware `dual` as the omitted
  outbound-signature policy, and the accepted container Directory lifecycle
  operator sequence.
- [ ] Run the canonical Forgejo stable artifact workflow on the exact reviewed
  stable-preparation commit; independently inspect and retain the exact Debian,
  SBOM, multi-architecture OCI, build-metadata, release-note, and checksum bytes.
- [ ] Create the signed `v3.0.0` tag only after canonical-byte acceptance, then
  publish the exact retained stable artifacts without rebuilding them.
- [ ] Deploy the exact stable bytes first to the isolated test relay and then to
  `relay.argentwolf.org`, preserving actor/key identity, Redis state,
  subscriptions, and operator configuration while validating Directory health.
- [ ] Package and retain the final cross-repository stable-promotion evidence
  after stable deployment. The functional soak gate is complete; this remaining
  item is evidence retention/teardown work rather than an uncompleted 3.0 runtime
  or interoperability gate.


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
- [x] Run mixed-profile Mastodon, Friendica, NodeBB, WordPress, and two-relay
  interoperability and select destination-aware `dual` as the Activity-Relay
  3.0 omitted/default outbound policy.

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

## Deferred work after v3.0.0

- Track the receiver-specific NodeBB 4.15.1 forced-RFC-9421 Announce rejection
  in upstream issue #14732. Treat it as external interoperability unless wire
  evidence identifies an Activity-Relay RFC 9421 defect.
- Consider an outbound signature-profile Prometheus counter after 3.0; current
  bounded worker diagnostics already record the concrete selected profile.
- Define stale-receiver retention and cleanup configuration after sufficient
  delivery-health history exists. Begin in report-only dry-run mode; active
  membership and queued or claimed work remain non-configurable deletion
  barriers.
- Replicated or alternative durable-state backends are tabled. Revisit only
  when production experience, availability requirements, or operator demand
  justify the value and compatibility cost.
- Continue maintaining and rebasing the Redis-only Machinery fork for relevant
  upstream security and correctness fixes.

- The opt-in Activity-Relay Directory version 1 client contract, explicit
  manual commands, and API-process scheduler are implemented:
  bounded independently enabled origins, strict lifecycle documents, a
  directory-specific RFC 9421/RFC 9530 profile, bounded closed responses,
  redirect refusal, one explicit not-registered reconciliation, and a shared
  server-accepted cryptographic fixture. No endpoint is active by default.
  File-backed unregister and the scheduler share a renewable Redis lease;
  state writes are lease-token-fenced, runtime gate disablement and removal are
  durable suppression, and unregister loads valid state before mutation and
  suppresses an entry before its remote request. Automatic retry starts at 30
  seconds, caps local backoff at 15 minutes, and permits validated remote
  `Retry-After` guidance up to 24 hours. The false-by-default scheduler performs
  startup reconciliation and jittered daily heartbeats in API processes only,
  under signal-aware graceful shutdown. RC2 schema-3 status, both live relay
  registrations, natural heartbeat aging, and the controlled unregister/
  re-register lifecycle have passed. The Compose exercise also established that
  atomic replacement of a single-file `config.yml` bind requires API recreation
  before scheduler state is trusted, that the public actor endpoint must remain
  online through authenticated unregister, and that workers should be recreated
  afterward as bind-inode housekeeping. The functional Directory
  stable-promotion soak is complete; final evidence packaging remains
  post-deployment retention work.
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
