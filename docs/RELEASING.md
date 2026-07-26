# Releasing the maintained fork

This repository uses annotated Git tags and GitHub Releases. The relay binary receives its version through the existing `main.version` linker variable.

Pushing a `v*` tag builds both the multi-architecture GHCR container image and
the native Ubuntu 24.04 `amd64` package. Stable tags publish the full semantic
version, major/minor, major, and `latest` container tags. Prerelease tags publish
only their full prerelease version and do not move `latest`. The release workflow
runs the Redis-backed tests, derives a Debian version with revision `-1` from the
tag (for example, `v2.4.0-rc1` becomes `2.4.0~rc1-1`), builds and lints the
package, performs a clean installation/reinstall test, verifies actor identity
preservation and disabled services, writes `SHA256SUMS`, and attaches the Debian
package and checksums to the GitHub Release. Prerelease tags create GitHub
prereleases. The container is published to GHCR by the parallel release job.

## Version policy

- Patch release: backward-compatible fixes only, for example `2.1.1`.
- Minor release: backward-compatible features, for example `2.2.0`.
- Major release: incompatible configuration, API, storage, protocol, or identity changes.
- Release candidate: append `-rcN` to the Git tag, for example `v2.4.0-rc1`.
  Release candidates do not update container `latest`, major, or major/minor tags.

The first maintained-fork release is `v2.1.0` because it adds backward-compatible public API and landing-site functionality beyond upstream `v2.0.10`.

## Release checklist

1. Work from a clean `master` branch.
2. Update `CHANGELOG.md` and `readme.md`.
3. Run `gofmt` on changed Go files.
4. Run `git diff --check`.
5. Run the Redis-backed package tests using a disposable Redis instance.
6. Build a release candidate with the intended version string.
7. Build the local Docker image and native Debian package.
8. Smoke-test `/actor`, `/nodeinfo/2.1`, and `/status.json`.
9. Commit and push the release preparation.
10. Create and push the annotated tag.
11. Publish a GitHub Release with notes and checksums.

## Build the release candidate

For version `2.4.0`:

```bash
mkdir -p build

go build \
  -trimpath \
  -ldflags='-X main.version=2.4.0' \
  -o build/relay-2.4.0 \
  .

sha256sum build/relay-2.4.0 \
  > build/relay-2.4.0.sha256
```

## Tag the tested commit

For a release candidate:

```bash
git switch master
git pull --ff-only origin master

git tag -a v2.4.0-rc1 \
  -m 'Activity-Relay v2.4.0-rc1 release candidate'

git push origin v2.4.0-rc1
```

After the release candidate is validated, prepare the final Debian changelog
version and tag the tested final commit:

```bash
git tag -a v2.4.0 \
  -m 'Activity-Relay v2.4.0 maintained fork release'

git push origin v2.4.0
```

Do not move an already-published release tag. Correct mistakes with a new patch release.

## Smoke tests

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/actor \
  >/dev/null

curl --fail --silent --show-error \
  http://127.0.0.1:8080/nodeinfo/2.1 \
  >/dev/null

curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

Release assets replace Debian's `~` prerelease separator with `-` in the uploaded filename so GitHub does not rewrite it. The package's internal Debian version remains unchanged.
