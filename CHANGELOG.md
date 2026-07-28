# Changelog

## [2.4.0] - 2026-07-26

### RC6
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
  metadata, with underscore aliases accepted by the static-site builder.
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
