# Activity Relay Server

## A maintained and deployable ActivityPub relay written in Go

[![GitHub mirror CI](https://github.com/thystra/Activity-Relay/workflows/Test/badge.svg)](https://github.com/thystra/Activity-Relay/actions)

<img width="1672" height="941" alt="Activity-Relay status page" src="https://github.com/user-attachments/assets/bc4f95b0-bd69-4eb4-a2d2-b81ae07fd1ff" />

> [!NOTE]
> This repository is a maintained fork of
> [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay),
> based on upstream release `v2.0.10`.
>
> The authoritative repository is
> [`forgejo.argentwolf.org/alan/activity-relay`](https://forgejo.argentwolf.org/alan/activity-relay).
> [`github.com/thystra/Activity-Relay`](https://github.com/thystra/Activity-Relay)
> is a public mirror and independent-validation surface. The public Go module
> path remains `github.com/thystra/Activity-Relay`.

## What this project provides

Activity-Relay receives public ActivityPub traffic and redistributes eligible
activities to participating servers. This maintained fork focuses on practical
Fediverse interoperability, bounded Redis-backed delivery, modern HTTP
signatures, operational visibility, and upgrade-safe native and container
packaging.

Highlights include:

- Mastodon authorized-fetch and secure-mode interoperability;
- RFC 9421 HTTP Message Signatures and RFC 9530 `Content-Digest`, with
  destination-aware `dual` negotiation;
- support for relay followers and publishers from Mastodon, Friendica, NodeBB,
  WordPress ActivityPub, LitePub-family software, and compatible implementations;
- bounded Redis-backed fan-out with leased in-flight recovery;
- `/status.json` operational and participation information;
- optional Activity-Relay Directory registration, profile synchronization,
  participating-site telemetry, automatic heartbeat scheduling, and manual CLI
  lifecycle commands;
- native Debian/Ubuntu packages and multi-architecture container images; and
- an optional generated public relay website.

See [`CHANGELOG.md`](CHANGELOG.md) for release-by-release changes.

## Install

Activity-Relay can be deployed with Docker Compose, the native Debian/Ubuntu
package, or from source.

For complete installation instructions, prerequisites, actor-key handling,
reverse-proxy requirements, and first-start checks, see:

**[`docs/INSTALLATION.md`](docs/INSTALLATION.md)**

Published container images use:

```text
ghcr.io/thystra/activity-relay:<version>
```

A native package install begins with:

```bash
sudo apt install ./activity-relay_VERSION_amd64.deb
```

The relay identity key is persistent state. Back up `actor.pem`; replacing it
changes the relay's cryptographic identity.

## Operate

The API server and workers use the same YAML configuration:

```bash
relay --config /etc/activity-relay/config.yml server
relay --config /etc/activity-relay/config.yml worker
```

Validate configuration before restarting:

```bash
relay --config /etc/activity-relay/config.yml --test-config
```

Directory lifecycle operations are available from the same executable:

```bash
relay --config /etc/activity-relay/config.yml directory status
relay --config /etc/activity-relay/config.yml directory heartbeat https://directory.example.org
```

For the operator command reference, configuration examples, Directory
administration, observability, website maintenance, and troubleshooting, see:

**[`docs/ADMINISTRATION.md`](docs/ADMINISTRATION.md)**

## Federation endpoints

Traditional relay subscribers post to or subscribe through:

```text
https://relay.example.org/inbox
```

Follower-style relay software follows:

```text
https://relay.example.org/actor
```

A public reverse proxy should forward the relay's ActivityPub, WebFinger,
NodeInfo, and status routes to the API server. The observability listener is
separate and should not be exposed through the public reverse proxy. The full
route list is in the installation and administration guides.

## Documentation

- **Install or upgrade:** [`docs/INSTALLATION.md`](docs/INSTALLATION.md)
- **Operate the relay and use the CLI:** [`docs/ADMINISTRATION.md`](docs/ADMINISTRATION.md)
- **Directory client/protocol behavior:** [`docs/DIRECTORY-CLIENT.md`](docs/DIRECTORY-CLIENT.md)
- **Known Directory services:** [`docs/DIRECTORY-INDEX.md`](docs/DIRECTORY-INDEX.md)
- **Interoperability notes:** [`docs/INTEROPERABILITY.md`](docs/INTEROPERABILITY.md)
- **Security and HTTP signatures:** [`docs/SECURITY.md`](docs/SECURITY.md)
- **Development and testing:** [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md)
- **Architecture:** [`ARCHITECTURE.md`](ARCHITECTURE.md)
- **Release process:** [`docs/RELEASING.md`](docs/RELEASING.md)
- **Roadmap/release gates:** [`TODO.md`](TODO.md)

Website-specific documentation lives under [`contrib/web/`](contrib/web/), and
resource-guard/operational tooling is documented under
[`contrib/ops/`](contrib/ops/).

## Releases and compatibility

Existing ActivityPub endpoints, configuration names, Redis state, and control
commands remain compatible unless a release explicitly documents otherwise.
Release candidates use complete `-rcN` version identifiers and do not move
stable tags.

Forgejo is the release authority. GitHub is a downstream mirror and validation
surface. Versioned release notes are retained under [`docs/releases/`](docs/releases/).

## Upstream and attribution

This project is derived from
[`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay),
upstream baseline `v2.0.10`. Original authorship, history, notices, and
attribution are retained. See [`docs/UPSTREAM.md`](docs/UPSTREAM.md).

## License

GNU Affero General Public License version 3. See [`LICENCE`](LICENCE).
