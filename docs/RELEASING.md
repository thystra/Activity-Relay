# Releasing the maintained fork

This repository uses annotated Git tags and GitHub Releases. Tagged releases
build a multi-architecture GHCR image and an Ubuntu 24.04 `amd64` Debian
package.

## Version and tag policy

- Patch release: backward-compatible fixes, for example `2.4.1`.
- Minor release: backward-compatible features, for example `2.5.0`.
- Major release: incompatible configuration, API, storage, protocol, or
  identity changes.
- Release candidate: append `-rcN`, for example `v2.4.0-rc6`.

Prerelease tags publish only their complete prerelease container tag. They do
not move `latest`, major, or major/minor tags.

The workflow derives the Debian version series from the tag, but preserves
the top changelog version when it already belongs to that series. For
`v2.4.0-rc6` and the tested RC6 changelog, the internal Debian version is:

```text
2.4.0~rc6-5
```

If the top changelog entry does not match the tag series, the workflow starts
that series at Debian revision `-1`.

The uploaded filename replaces `~` with `-` so GitHub does not rewrite it:

```text
activity-relay_2.4.0-rc6-5_amd64.deb
```

The package's internal Debian version remains `2.4.0~rc6-5`.

## Release checklist

1. Work from a clean `master` branch.
2. Review `AGENTS.md`.
3. Update `CHANGELOG.md`, `readme.md`, and affected deployment documentation.
4. Confirm no local notes, generated packages, suspicious filenames, or
   machine-specific paths are tracked.
5. Validate the Compose preflight behavior with missing, directory-valued,
   and valid `actor.pem` files.
6. Run `gofmt` on changed Go files.
7. Run `go vet ./...`.
8. Run Redis-backed `go test -count=1 -p 1 ./...`.
9. Run `python3 -m unittest discover -s contrib/web -p 'test_*.py'`.
10. Run `python3 -m unittest discover -s contrib/ops -p 'test_*.py'`.
11. Run `git diff --check`.
11. Build the local container and verify:
    - `/usr/bin/relay`;
    - `/usr/share/activity-relay/web/build-site.py`;
    - the compiled version string.
12. Build the native Debian package and run Lintian on the `.changes` file.
13. Smoke-test `/actor`, `/nodeinfo/2.1`, and `/status.json`.
14. For publisher/fan-out changes, verify a real accepted publisher activity
    reaches a receiving server.
15. Commit and push the release preparation.
16. Create and push the annotated tag.
17. Verify the GitHub release, checksums, package metadata, and container
    manifests.
## Local validation

Use a disposable Redis instance:

```bash
docker rm -f activity-relay-release-test-redis \
  >/dev/null 2>&1 || true

docker run \
  --detach \
  --rm \
  --name activity-relay-release-test-redis \
  --publish 127.0.0.1:6381:6379 \
  redis:7-alpine

until docker exec activity-relay-release-test-redis redis-cli ping |
  grep -qx PONG
do
  sleep 1
done

REDIS_URL='redis://127.0.0.1:6381' \
  go test -count=1 -p 1 ./...

REDIS_URL='redis://127.0.0.1:6381' \
  go test -race -count=1 -p 1 ./api ./models

go vet ./...

python3 -m unittest discover \
  -s contrib/web \
  -p 'test_*.py'

python3 -m unittest discover \
  -s contrib/ops \
  -p 'test_*.py'

docker rm -f activity-relay-release-test-redis
```

Build and inspect the container:

```bash
ACTIVITY_RELAY_VERSION='2.4.0-rc6' \
docker compose \
  -f compose.yml \
  -f compose.build.yml \
  build

docker run \
  --rm \
  activity-relay:local \
  --version

docker run \
  --rm \
  --entrypoint /bin/sh \
  activity-relay:local \
  -c '
    test -x /usr/bin/relay
    test -f /usr/share/activity-relay/web/build-site.py
    test -f /usr/share/activity-relay/web/site.json.example
    test -x /usr/bin/activity-relay-resource-guard
    test -x /usr/lib/activity-relay/resource-guard.py
  '
```

Build and inspect the Debian package:

```bash
dpkg-parsechangelog --show-field Version

dpkg-buildpackage \
  --build=binary \
  --no-sign

lintian \
  --fail-on error \
  ../activity-relay_2.4.0~rc6-5_amd64.changes
```

## Tag the tested commit

```bash
git switch master
git pull --ff-only origin master

git tag -a v2.4.0-rc6 \
  -m 'Activity-Relay v2.4.0-rc6 release candidate'

git push origin v2.4.0-rc6
```

After final validation:

```bash
git tag -a v2.4.0 \
  -m 'Activity-Relay v2.4.0 maintained fork release'

git push origin v2.4.0
```

Do not move an already-published release tag. Correct mistakes with another
release candidate or patch release.

## Workflow behavior

The release workflow:

- derives the package version from the tag;
- runs Redis-backed Go and static-site tests;
- builds and lints the Debian package;
- tests clean installation, reinstallation, actor identity preservation, and
  inactive services;
- writes `SHA256SUMS`;
- creates a GitHub prerelease for RC tags;
- publishes `linux/amd64` and `linux/arm64` images to GHCR;
- updates stable semantic and `latest` tags only for stable releases.

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
