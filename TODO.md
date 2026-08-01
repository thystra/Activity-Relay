# Activity-Relay roadmap

## Current stable release

`v2.5.0` is the current stable maintained-fork release. It promotes the
production-soaked RC2 application code from commit
`0ed2ff5a3aff1700172feebde91ad3ed9022e3f8`.

The stable release preserves the established relay actor ID, endpoints,
collections, `#main-key` identity, configuration, Redis data, queue formats,
operator-owned website content, and supported deployment models.

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

## Immediate post-release verification

- Verify the stable GitHub release metadata, release notes, checksums, internal
  Debian version, package contents, and required examples.
- Verify `linux/amd64` and `linux/arm64` container manifests plus the `2.5.0`,
  `2.5`, `2`, and `latest` tags.
- Deploy the stable `2.5.0-1` package to production and reconfirm actor identity,
  dedicated Redis state, API and worker health, private observability, and
  public endpoints.
- Retain the RC2 and stable-promotion evidence outside the public repository.

## Deferred work after v2.5.0

- Investigate NodeBB follow interoperability separately from Activity-Relay:
  reproduce failed attempts to follow `Julian@activitypub.space` from Friendica
  and WordPress, use a controlled NodeBB instance, and inspect the corresponding
  application logs.
- Define stale-receiver retention and cleanup policy after sufficient delivery
  health history exists.
- Add RFC 9421 message-signature and RFC 9530 content-digest support
  additively, without removing the established Fediverse `Signature` profile.
- Evaluate replicated or alternative durable-state designs without breaking
  current Redis compatibility.
- Continue maintaining and rebasing the Redis-only Machinery fork for relevant
  upstream security and correctness fixes.

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
