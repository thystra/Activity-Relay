# Activity-Relay roadmap

## Current stable release

`v2.4.0` is the current stable maintained-fork release. Production remains on
that release until the complete v2.5.0 package, release-artifact, and deployment
gates pass.

## v2.5.0 development status

Status recorded 2026-07-30.

### Implemented

- Relay-signed remote actor and object `GET` requests for Mastodon authorized-
  fetch and secure-mode interoperability.
- `Application` relay-actor classification for current Friendica discovery,
  preserving the established actor ID, collections, endpoints, and key identity.
- Shared exact-authority HTTP signing for authorized fetch and outbound
  delivery, including redirect re-signing and bounded remote errors.
- Durable per-receiver delivery success and failure timestamps, consecutive
  failures, and total counters.
- `/status.json` schema version 4 receiver-health output for current receivers
  without inbox URLs, actor IDs, error text, or departed receiver history.
- Optional private observability listener with Prometheus metrics, process
  liveness, Redis-backed readiness, and bounded operational labels.
- Migration from Machinery v1 to the maintained Redis-only Machinery v2 fork
  while preserving queue, task, delayed-retry, result-state, Redis database, and
  v1/v2 producer/consumer compatibility.
- Leased in-flight Redis task claims, lease renewal, acknowledgement after
  successful processing, expired-claim recovery, and atomic delayed promotion.
- Explicit at-least-once delivery semantics: silent post-claim loss is
  prevented, while a remote success followed by local worker death may produce
  an expected duplicate.

### Validated

- Activity-Relay `master` commit
  `75cf98a88e53303c75eb26b4b47b7bcd928e7dad`.
- Machinery reliable-claims dependency commit
  `5efae3f700cd1d6118a564d6dab75a1bc7adc403`.
- Machinery maintained baseline
  `9ccb63336b003d1c9827ebfaf2399bf13ea501a0`; its repository cleanup and CI
  commits do not change the pinned `v2/` module bytes.
- Full serialized Activity-Relay tests, full serialized race tests, vet, focused
  regression repetition, binary builds, and command help.
- Machinery full tests, full race tests, vet, Redis integration, groups, retries,
  TLS, ACL, database selection, Unix sockets, and v1/v2 queue/result
  compatibility.
- Container integration with preserved actor identity, reverse-proxy
  configuration, Redis persistence, private observability, and clean rollback.
- Interruption matrix covering:
  - task queued before claim;
  - graceful shutdown during active delivery;
  - abrupt termination during outbound HTTP;
  - abrupt termination after remote HTTP success but before local completion;
  - delayed retry across abrupt worker downtime; and
  - empty ready, delayed, in-flight payload, and lease state afterward.
- Bidirectional NodeBB-to-Mastodon and Mastodon-to-NodeBB delivery through an
  isolated test relay, including receiver-health success observations and no
  final queued or claimed work.

## Remaining v2.5.0 release gates

1. Validate authorized fetch against a real Mastodon instance explicitly running
   the intended secure-mode or authorized-fetch configuration, including remote
   actor and object fetch behavior; then validate fresh Friendica relay discovery,
   follow/accept registration, delivery, canonical-object fetch, and presentation.
2. Build the final unreleased native package from the release candidate commit
   and complete the package matrix in `docs/INTEGRATION-TESTING.md`, including
   clean install, explicit activation, upgrade from v2.4.0, reinstallation,
   rollback where supported, identity preservation, and removal behavior.
3. Repeat the final Redis outage/reconnect and applicable TLS or Unix-socket
   checks through the integrated release-candidate binary or package rather than
   only the fork-level test suite.
4. Document operator backup, export, restore, and Redis persistence verification
   procedures.
5. Finalize `CHANGELOG.md`, `readme.md`, Debian changelog/version metadata, and
   `docs/releases/v2.5.0.md`.
6. Run final local and GitHub Actions validation, build and lint the Debian
   package, validate container contents and manifests, and verify checksums.
7. Tag and publish a release candidate before any production deployment.
8. Perform production deployment and post-deployment validation as a separate
   explicitly recorded state.

## Deferred work after v2.5.0

- Define stale-receiver retention and cleanup policy after sufficient health
  observation data exists.
- Add RFC 9421 inbound verification and RFC 9530 digest support without removing
  the established Fediverse `Signature` compatibility profile.
- Evaluate replicated or alternative durable state designs without breaking
  current Redis compatibility.
- Continue maintaining and rebasing the Redis-only Machinery fork for relevant
  upstream security and correctness fixes.

## Release gates for future versions

Before any release:

1. update `CHANGELOG.md`, `readme.md`, affected documentation, and versioned
   release notes;
2. run all validation required by `AGENTS.md` and `docs/RELEASING.md`;
3. preserve actor identity, configuration, Redis compatibility, and operator
   website content;
4. validate package installation, upgrade/reinstallation, checksums, metadata,
   and container manifests; and
5. for federation changes, demonstrate a real signed publisher-to-receiver path
   and distinguish transport acceptance, object import, and local presentation.

## Upstream review

Continue monitoring upstream commits, issues, and pull requests under the policy
in `docs/UPSTREAM.md`. Record upstream commit, issue, or pull-request references
when a change is ported.
