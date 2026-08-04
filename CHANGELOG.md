# Changelog

## [Unreleased]

### Added

- Add `PUBLIC_ADDRESS_DISTRIBUTION_POLICY` with `explicit_public_only` and `public_and_unlisted` values, fatal validation for unknown values, and effective-policy startup reporting.
- Expose the effective public-address distribution policy and human-readable label through `/status.json` schema version 5, and display them in the default generated website footer on every page.

### Changed

- Fresh example configurations explicitly select `explicit_public_only`; omitted configuration retains the pre-3.0 `public_and_unlisted` behavior for upgrade compatibility.
- Under `explicit_public_only`, Public appearing only in `cc` is acknowledged and publisher-accounted without public relay fan-out. Explicit relay-addressed relationship traffic remains separate.

### Fixed

- Retry an unknown idempotent RFC 9421 actor or object fetch once with the
  legacy HTTP-signature profile when a remote implementation returns a generic
  HTTP 400, and cache the short-lived legacy preference only after that retry
  succeeds.
- Remove URI fragments such as `#main-key` from RFC 9421 request targets
  before signing and sending, so `@target-uri` matches the fragment-free HTTP
  target verified by secure-mode ActivityPub servers.

## [2.5.1] - 2026-08-02
### Fixed
- Prevent authenticated relay-to-relay `Announce` reflection by excluding
  the supplying relay and canonical origin from fan-out.
- Atomically suppress repeated canonical activity references for the retained
  delivery horizon without storing raw activity URLs in the marker key.
- Acknowledge duplicate references without minting another relay-authored
  wrapper, and release reservations after known queue-admission failures.
- Add a required real-process two-relay invariant with independent API,
  worker, Redis, actor-key, TLS, and signed-origin boundaries.

## [2.5.0] - 2026-08-01

### Stable release
- Promote the production-soaked RC2 application code and corrected delivery
  retry lifecycle to stable `v2.5.0`.
- Preserve the established relay actor ID, `#main-key` identity, endpoints,
  configuration, Redis data, queued work, and operator-owned website content.
- Confirm Mastodon authorized-fetch and secure-mode operation during the
  production soak without relay-specific signature, private-address, queue,
  Redis, or resource regressions.
- Confirm that receiver-side HTTP failures remain isolated per target while
  successful receivers continue to accept deliveries.
- Publish stable `2.5.0` container tags, the `2.5.0-1` native package, reviewed
  release notes, and updated operator and maintainer documentation.

## [2.5.0-rc2] - 2026-07-31
### Added
- Add bounded structured delivery-attempt diagnostics containing task and
  activity correlation identifiers, origin and receiver domains, attempt
  counters, elapsed time, HTTP status, error class, retry schedule, body digest,
  and bounded non-success response text.
- Add regression tests proving a retriable receiver failure preserves the shared
  activity body and a later retry can deliver it successfully.
### Changed
- Give each `relay-v2` fan-out target five delayed retries using Machinery's
  Fibonacci schedule after the initial delivery attempt.
- Retain shared fan-out activity bodies for fifteen minutes, exceeding the full
  retry horizon with an operational margin.
### Fixed
- Decrement a shared activity's `remain_count` only after successful delivery or
  final retry exhaustion, rather than after every failed attempt.
- Prevent transient receiver timeouts from deleting the payload required by
  their own delayed retries or by other receivers.

## [2.5.0-rc1] - 2026-07-30

### Added
- Sign remote actor and ActivityPub object `GET` requests with the relay actor
  identity for Mastodon authorized-fetch and secure-mode interoperability.
- Add focused regression tests for signed actor and object fetches, exact `Host`
  authority signing, redirect re-signing, signature verification, and bounded
  remote errors.
- Record durable per-receiver delivery success and failure timestamps, total
  counters, and consecutive failures after each relay fan-out attempt.
- Expose delivery health for current receivers through `/status.json` schema
  version 4 without exposing inbox URLs, actor IDs, error text, or departed
  receiver history.
- Add an optional separately bound observability listener exposing Prometheus
  metrics, process-only liveness, and Redis-backed readiness.
- Add private-registry Go, process, build, Redis-readiness, and public API HTTP
  metrics with bounded method, route, and status labels.
- Add relay-wide accepted/rejected activity, queue-admission, fan-out, delivery,
  Redis-failure, queue-depth, receiver, publisher, and receiver-health metrics.
- Aggregate API and worker counters through a bounded Redis ledger so worker
  processes do not open or contend for the observability listener.
- Add a host-neutral container and native-package integration test matrix,
  including queue interruption, Redis recovery, identity preservation, rollback,
  federation, metrics, and evidence requirements.
- Document the current HTTP-signature compatibility profile and an additive RFC
  9421 and RFC 9530 interoperability roadmap.

### Changed
- Publish the relay actor as an ActivityStreams `Application` rather than a
  `Service` so current Friendica relay discovery accepts `/actor` while
  preserving the actor ID, endpoints, collections, and `#main-key` identity.
- Use one shared HTTP-signature implementation for authorized fetch and outbound
  delivery while preserving the established signed POST header set.
- Include bounded non-success response text in remote actor and object fetch
  errors without allowing unbounded response bodies.
- Keep registration deliveries outside receiver fan-out health accounting and
  preserve existing short-lived worker error diagnostics.
- Migrate asynchronous task transport from `yukimochi/machinery-v1` v1.10.10
  to the thystra Redis-only fork of `RichardKnop/machinery/v2` v2.0.16,
  pinned to reliable-claims commit
  `5efae3f700cd1d6118a564d6dab75a1bc7adc403`.
- Move ready work atomically into leased in-flight Redis claims, renew active
  leases, acknowledge only after successful processing, and recover expired
  claims to their original ready queue.
- Define task execution as at-least-once: abrupt termination after remote
  success but before local acknowledgement may produce an expected duplicate,
  while silent loss of the claimed task is prevented.
- Preserve the existing `relay` queue, task-signature JSON, delayed-retry set,
  result-state format, Redis database selection, and v1/v2 producer/consumer
  compatibility across the migration.
- Validate queued work, graceful shutdown, abrupt termination during delivery,
  abrupt termination after remote HTTP success, delayed retry recovery, clean
  final claim state, and bidirectional NodeBB/Mastodon relay interoperability.

### Validation
- Confirm real Mastodon secure-mode behavior with an unsigned canonical-object
  request rejected and the same request accepted when signed by the relay actor.
- Confirm fresh Friendica 2026.05 relay discovery, follow/accept registration,
  Mastodon-to-relay delivery, canonical-object retrieval, media and hashtag
  preservation, and final Friendica presentation.
- Record a NodeBB 4.14.2 receiving-side limitation: application-context object
  fetches are unsigned, so secure-mode canonical objects can produce HTTP 424
  even when the relay-signed delivery itself is valid.

## [2.4.0] - 2026-07-29

### Stable release

- Promote the production-validated RC6 application code to stable `v2.4.0`
  after its soak period, without changing relay identity or stored Redis data.
- Publish stable container, source-build, Debian-package, interoperability, and
  release-note references.
- Add an optional Caddy 2 reverse-proxy and static-site example alongside the
  existing Nginx and Apache examples, without adding a runtime dependency.
- Add `ARCHITECTURE.md` and `TODO.md`, and strengthen `AGENTS.md` with staged,
  transactional, rollback-safe development and release practices.

### RC6
- Preserve a tested matching Debian changelog revision during tagged release builds, so an RC tag cannot publish an older package revision.
- Fan out public same-domain `Announce` activities with embedded objects, as
  emitted by NodeBB categories, by sending one relay-signed `Announce` wrapper
  to both traditional and follower-style receivers. This keeps the authenticated
  HTTP signer aligned with the activity actor for strict Mastodon processing.
- Keep string-object and cross-domain public `Announce` activities publisher-only
  to avoid relay-to-relay loops and unintended boost amplification.
- Log inbound ActivityPub decode, actor-resolution, signature, digest, and JSON
  failures with bounded request metadata before returning `400 Bad Request`.
- Publish the relay actor RSA key as X.509 SubjectPublicKeyInfo PEM
  (`BEGIN PUBLIC KEY`) for broader ActivityPub HTTP-signature interoperability.
- Continue accepting both SubjectPublicKeyInfo and legacy PKCS#1
  (`BEGIN RSA PUBLIC KEY`) actor public keys during inbound verification.
- Preserve the existing `actor.pem`, relay actor ID, and `#main-key` key ID;
  this changes only the public serialization of the same RSA key.
- Fix outbound HTTP signatures so the signed `Host` exactly matches the
  authority transmitted on the wire, including non-default ports.
- Accept `Application` and `Service` server actors at implementation-defined
  paths, including NodeBB `/actor`, when they follow the relay actor.
- Preserve bounded remote error-response text in worker diagnostics so
  signature and interoperability failures are actionable.
- Advertise the required ActivityPub actor outbox, recommended followers and
  following collections, and `endpoints.sharedInbox`.
- Serve privacy-filtered empty `OrderedCollection` documents for the actor
  inbox, outbox, followers, and following endpoints, including HEAD and proper
  method handling.
- Update Nginx, Apache, and endpoint documentation for the actor collection
  routes.
- Replace the ambiguous once-per-day summary hour with one or more explicit
  server-local `HH:MM` delivery slots.
- Send only the most recent due slot after downtime, list skipped slots in the
  catch-up email, retain skipped-slot history, and archive successful reports.
- Add summary preview, immediate-send, state-inspection, per-slot reset, and
  daily-reset administrative commands.
- Preserve deprecated `DAILY_SUMMARY_HOUR` compatibility while allowing a new
  configured time to send after another slot has already completed that day.
- Add Python and systemd mail timeouts, preserve scheduled state after failures,
  and permit tested local-MTA queue access from the hardened systemd unit.
- Include resource-guard tests and the administrative CLI in Debian and
  container release validation.

### RC5

- Build the optional website from current package-managed sources while keeping
  operator rules and other content in upgrade-safe override directories.
- Use `RELAY_ICON` and `RELAY_IMAGE` as default website logo and banner values.
- Add `FEDIVERSE_OPERATOR_ID` and optional `FEDIVERSE_OPERATOR_URL` footer
  metadata, with legacy hyphenated aliases accepted by the static-site builder.
- Remove successful `/status.json` loader text from the production dashboard
  and add defensive hidden-element styling.
- Add a package-managed `activity-relay-rebuild-site` command and document
  every supported customization location.

### RC4

- Make the bundled public frontend explicitly optional and document bundled,
  disabled, redirected, and custom-site deployments.
- Include editable website sources in the published container image and
  document generation with a temporary Python container.
- Replace the stale upstream Compose defaults with a production-oriented
  GHCR deployment and an explicit local-build override.
- Make required Compose file mounts fail when `actor.pem` or `config.yml` is
  missing, add a preflight helper, and document the Redis overcommit setting.
- Hide the successful `/status.json` loader diagnostic while retaining visible
  failure information.
- Add a minimal custom `/status.json` frontend example.
- Add public `AGENTS.md` contributor guidance, remove an accidental tracked
  pager-output file, neutralize the root configuration example, and refresh
  release documentation.

### RC3

- Add optional `activitypub_contact` and `activitypub_contact_url` website
  settings for a public relay-operator fediverse account.
- Add a one-command static-site rebuild wrapper and preserve operator-owned
  copies during package upgrades.

### RC2

- Count the unique union of relay receivers and accepted publishers in
  `connected_instances`, and expose the narrower fan-out set as
  `receiving_instances`.
- Fix relay activity storage by returning success from the Redis Lua script,
  allowing worker task creation to continue.
- Make the generated dashboard list every participating domain, remain usable
  across mixed HTML/JavaScript deployments, and cache-bust its assets.
- Publish Debian release assets with GitHub-safe filenames while retaining the
  Debian version inside the package.

- Remove legacy upstream branding from the CLI, give the root command the `relay` name, and expose the compiled version through `relay --version`.
- Accept valid signed public `Create`, `Update`, `Delete`, and `Move`
  activities from unsubscribed ActivityPub publishers.
- Persist observed publisher domains with first-seen, last-seen, activity type,
  and accepted-activity count metadata.
- Add publisher role and relay-receipt state to `/status.json` schema version 2
  and the generated landing dashboard.
- Bind HTTP signature key hosts to activity actor hosts before accepting open
  publisher traffic, while retaining blocked, limited-domain, and person-only
  policy enforcement.
- Publish multi-architecture release containers to GHCR with semantic-version,
  major/minor, major, and `latest` tags; publish `edge` and commit tags from
  `master`.
- Make the published GHCR image the default Compose deployment and provide a
  separate `compose.build.yml` overlay for local source builds.
- Continue building, linting, installing, and attaching the native Ubuntu 24.04
  `amd64` Debian package for tagged releases.

## [2.4.0-rc6] - 2026-07-28

- Published the application release candidate later promoted to stable
  `v2.4.0` after production interoperability and operational soak validation.

## [2.3.1] - 2026-07-22

- Report whether active Activity-Relay services restarted after a Debian
  package upgrade and provide a diagnostic command if restarting fails.
- Avoid showing initial-install guidance during package upgrades.

## [2.3.0] - 2026-07-22

- Add opt-in HTTP/3/QUIC directives to the Nginx deployment example.
- Make relay state reads concurrency-safe and replace blocking Redis key scans
  and per-field reloads with `SCAN` and pipelined hash reads.
- Add atomic queue-capacity reservations and bounded concurrent fan-out
  publication.
- Reuse bounded HTTP clients, limit remote JSON responses, harden API server
  timeouts, and propagate command and delivery errors correctly.

## [2.2.0] - 2026-07-22

- Add configurable request and fan-out limits, bounded Redis/container resource
  settings, host storage/cache paths, disk budget email alerts, Nginx admission
  controls and log rotation guidance.
- Add optional landing-site logo URL and alternative text settings.
- Add safe, non-overwriting RSA actor identity generation through
  `relay generate-key`.
- Add an optional daily administrator summary with connected-server changes and
  storage usage versus configured caps.
- Support selectable `mail` and `sendmail` alert transports, defaulting to the
  `mailutils` mail command.
- Add an opt-in Apache 2.4 HTTPS, static-site, and reverse-proxy example.

All notable changes to the maintained `thystra/Activity-Relay` fork are documented in this file.

The project is based on [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay).

## [2.1.0] - 2026-07-22

Based on upstream Activity-Relay `v2.0.10`.

### Added

- Public `GET /status.json` endpoint.
- Relay registration state, service endpoints, software metadata, connected-instance count, and sorted connected-domain list in the status response.
- Optional generated landing website.
- Editable home, about, rules, privacy, and footer content.
- Searchable connected-instance dashboard populated from `/status.json`.
- Generic Nginx virtual-host template.
- Deployment documentation explaining how the site-builder output directory maps to the Nginx document root.
- Tests for public status output and supported follower actor paths.

### Changed

- The public status response identifies this maintained fork's source repository.
- The README now distinguishes the maintained fork from upstream and documents build, test, web deployment, and release procedures.

### Fixed

- Friendica server actors using `/friendica` can follow and unfollow the relay.
- Actor paths with a trailing slash are normalized before follower-path validation.

### Compatibility

- Existing ActivityPub endpoints remain unchanged.
- Existing YAML and environment-variable configuration remains compatible.
- Existing Redis data and control commands remain compatible.
- At v2.1.0, the Go module path still matched upstream; the maintained fork later adopted `github.com/thystra/Activity-Relay` in v2.4.0.
