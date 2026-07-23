# Changelog

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
- The Go module path remains `github.com/yukimochi/Activity-Relay` for compatibility.
