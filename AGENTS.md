# AGENTS.md

## Scope

These instructions apply to the entire Activity-Relay repository.

## Project character

Activity-Relay is a maintained public fork of `yukimochi/Activity-Relay`.
Changes should remain useful to operators outside the maintainer's own
infrastructure. Do not add private hostnames, personal paths, credentials,
deployment transcripts, or machine-specific instructions to the repository.

Keep maintainer-local notes and deployment helpers outside the checkout or in
an ignored `AGENTS.local.md` or `.local/` directory. Private continuity guides
may supplement this file, but they must not be copied into the public
repository.

## Required context

Before a substantial change, review:

1. this file;
2. `ARCHITECTURE.md`;
3. `TODO.md`;
4. `readme.md`;
5. `CHANGELOG.md`;
6. `docs/RELEASING.md` for release work;
7. `docs/UPSTREAM.md` for upstream ports; and
8. the current branch, working-tree state, remotes, and recent commits.

## Working-tree and applicator safety

- Verify the repository, branch, remote, and expected baseline commit before
  changing files.
- Require a clean working tree unless the task explicitly preserves known local
  changes. Never overwrite unknown work.
- Build and validate the prospective result in a temporary tree before changing
  the checkout whenever practical.
- Prefer complete-file installation or a reviewed unified patch. Use structural
  transformations when semantic scope matters; use plain-text replacement only
  for small files with unique, controlled anchors.
- Keep backups outside the Git worktree. After backup creation, use an `EXIT`
  transaction guard that restores replaced and removed files and deletes newly
  created files unless a success flag is set.
- Version applicators as `apply-<project>-<change>-r<number>.sh`. Do not silently
  replace a script that may already have been downloaded or used.
- Applicators must be idempotent or refuse an already-applied state clearly.
- Use `git ls-files` when validating tracked-source manifests. Report ignored
  build output separately.
- Put strict shell modes inside scripts or explicit subshells. Do not enable
  `set -e`, `set -u`, or `pipefail` directly in an interactive operator shell.

## State reporting

Distinguish proposed design, generated patch, applied local changes, passing
local tests, commit, push, tag, built artifact, deployment, and production
validation. Do not claim a later state because an earlier state succeeded.

## Validation

Before committing changes:

1. Run `gofmt` on modified Go files.
2. Run `go vet ./...`.
3. Run Redis-backed tests with a disposable Redis instance:
   `REDIS_URL=redis://127.0.0.1:6381 go test -count=1 -p 1 ./...`
4. Run the full serialized race suite:
   `REDIS_URL=redis://127.0.0.1:6381 go test -race -count=1 -p 1 ./...`
   Activity-Relay package tests share Redis and some packages clear the selected
   database, so package-level test processes must remain serialized unless each
   package receives an isolated Redis database.
5. Run the static-site tests:
   `python3 -m unittest discover -s contrib/web -p 'test_*.py'`
6. Run the operations tests:
   `python3 -m unittest discover -s contrib/ops -p 'test_*.py'`
7. Run `git diff --check`.
8. Validate changed shell scripts with `sh -n` or `bash -n`, as appropriate.
9. When changing the Caddy example, validate it with Caddy 2, for example:
   `docker run --rm -v "$PWD/contrib/caddy/Caddyfile.example:/etc/caddy/Caddyfile:ro" caddy:2.11.2-alpine caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile`
10. When changing the container, build it and verify both `/usr/bin/relay` and
    `/usr/share/activity-relay/web/build-site.py`.
11. When changing Debian packaging, build the binary package, inspect its
    contents and metadata, and run Lintian on the generated `.changes` file.

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
- Reverse-proxy examples are optional operator references, not runtime
  dependencies or package-enabled services.
- The Go module path is `github.com/thystra/Activity-Relay`.

## Documentation responsibilities

- `ARCHITECTURE.md` describes components, data ownership, federation flows,
  security invariants, and failure behavior.
- `TODO.md` tracks current milestones, deferred work, release gates, and dated
  status notes.
- `CHANGELOG.md` records user-visible changes by release; it is not the sole
  development plan.
- Versioned release notes under `docs/releases/` describe the artifact actually
  published for that tag.
- `docs/INTEGRATION-TESTING.md` defines host-neutral container and native-package
  release gates; private transcripts and host details remain outside the repo.
- `docs/SECURITY.md` records the current signature profile and additive security
  roadmap.
- Update `readme.md` and affected deployment documentation whenever behavior,
  packaging, configuration, or release procedures change.

## Releases

- Stable tags use `vX.Y.Z`; release-candidate tags use `vX.Y.Z-rcN`.
- Stable Debian versions use `X.Y.Z-1`; prerelease versions use
  `X.Y.Z~rcN-<revision>`.
- Uploaded release filenames must not contain `~`; the package's internal
  Debian version retains it.
- Prereleases must not move stable container tags such as `latest`, major, or
  major/minor.
- Do not move a published tag. Correct release mistakes with a later release
  candidate or patch release.
- After publication, verify release checksums, internal package version,
  architecture, installed example files, GitHub release metadata, and container
  manifests before declaring the release complete.

## Upstream changes

Follow `docs/UPSTREAM.md`. Review and port specific upstream commits, issues, or
pull requests rather than wholesale-merging upstream after architectural
divergence. Preserve authorship and record upstream references in the commit or
changelog.

## Generated and local files

Do not commit generated `.deb`, `.changes`, `.buildinfo`, coverage, cache,
Redis-data, backup, or local deployment files. Keep external backups in a
maintainer-owned backup directory outside the checkout. Public files should use
repository-relative or portable example paths, never a maintainer-specific
absolute path.
