# AGENTS.md

## Scope

These instructions apply to the entire Activity-Relay repository.

## Project character

Activity-Relay is a maintained public fork of `yukimochi/Activity-Relay`.
Changes should remain useful to operators outside the maintainer's own
infrastructure. Do not add private hostnames, personal paths, credentials,
deployment transcripts, or machine-specific instructions to the repository.

Keep maintainer-local notes and deployment helpers outside the checkout or in
an ignored `AGENTS.local.md` or `.local/` directory.

## Validation

Before committing changes:

1. Run `gofmt` on modified Go files.
2. Run `go vet ./...`.
3. Run Redis-backed tests with a disposable Redis instance:
   `REDIS_URL=redis://127.0.0.1:6381 go test -count=1 -p 1 ./...`
4. Run the static-site tests:
   `python3 -m unittest discover -s contrib/web -p 'test_*.py'`
5. Run `git diff --check`.
6. Validate changed shell scripts with `sh -n` or `bash -n`, as appropriate.
7. When changing the container, build it and verify both `/usr/bin/relay` and
   `/usr/share/activity-relay/web/build-site.py`.
8. When changing Debian packaging, build the binary package and run Lintian on
   the generated `.changes` file.

Never run tests against production Redis.

## Compatibility and safety

- Preserve the relay actor identity and never overwrite an existing actor key.
- Preserve operator-owned configuration and website content during upgrades.
- Debian packages must not enable or start services automatically.
- Keep existing ActivityPub endpoints and stored Redis data compatible unless a
  documented major release intentionally changes them.
- Open publisher ingestion must retain HTTP-signature, actor-host, blocked
  domain, limited-domain, and person-only policy enforcement.
- The bundled public website is optional. Relay operation must not depend on
  serving or generating it.
- The Go module path is `github.com/thystra/Activity-Relay`.

## Releases

- Release-candidate tags use `vX.Y.Z-rcN`.
- Debian prerelease versions use `X.Y.Z~rcN-1`.
- Uploaded release filenames must not contain `~`; the package's internal
  Debian version retains it.
- Prereleases must not move stable container tags such as `latest`, major, or
  major/minor.
- Update `CHANGELOG.md`, `readme.md`, and relevant deployment documentation
  when behavior, packaging, configuration, or release procedures change.

## Generated and local files

Do not commit generated `.deb`, `.changes`, `.buildinfo`, coverage, cache,
Redis-data, backup, or local deployment files. Keep external backups in a
sibling directory such as `../activity-relay-local/` or
`../activity-relay-backups/`.
