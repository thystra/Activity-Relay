# Releasing the maintained fork

This repository uses annotated Git tags and GitHub Releases. Tagged releases
build a multi-architecture GHCR image and an Ubuntu 24.04 `amd64` Debian
package.

## Version and tag policy

- Patch release: backward-compatible fixes, for example `2.4.1`.
- Minor release: backward-compatible features, for example `2.5.0`.
- Major release: incompatible configuration, API, storage, protocol, or
  identity changes.
- Release candidate: append `-rcN`, for example `v2.5.0-rc1`.

Prerelease tags publish only their complete prerelease container tag. They do
not move `latest`, major, or major/minor tags.

The workflow derives the Debian version series from the tag and preserves the
top changelog version when it already belongs to that series. The stable
`v2.4.0` release uses:

```text
2.4.0-1
```

A future prerelease such as `v2.5.0-rc1` uses a Debian series such as
`2.5.0~rc1-1`. If the top changelog entry does not match the tag series, the
workflow starts that series at Debian revision `-1`.

Release filenames replace `~` with `-` only when necessary for GitHub asset
compatibility; the package's internal Debian version is unchanged.

## Release checklist

1. Work from a clean `master` branch.
2. Review `AGENTS.md`, `ARCHITECTURE.md`, and `TODO.md`.
3. Update `CHANGELOG.md`, `readme.md`, affected deployment documentation, and
   the versioned release notes under `docs/releases/`.
4. Confirm no local notes, generated packages, suspicious filenames, or
   machine-specific paths are tracked.
5. Validate the Compose preflight behavior with missing, directory-valued,
   and valid `actor.pem` files.
6. Run `gofmt` on changed Go files.
7. Run `go vet ./...`.
8. Run Redis-backed `go test -count=1 -p 1 ./...`.
9. Run Redis-backed `go test -race -count=1 -p 1 ./...`.
10. Run `python3 -m unittest discover -s contrib/web -p 'test_*.py'`.
11. Run `python3 -m unittest discover -s contrib/ops -p 'test_*.py'`.
12. Validate the Caddy example when it changes.
13. Run `git diff --check`.
14. Build the local container and verify:
    - `/usr/bin/relay`;
    - `/usr/share/activity-relay/web/build-site.py`;
    - the compiled version string.
15. Build the native Debian package and run Lintian on the `.changes` file.
16. Smoke-test `/actor`, `/nodeinfo/2.1`, and `/status.json`.
17. Complete the applicable container and native-package matrix in
    `docs/INTEGRATION-TESTING.md`, retaining external evidence for the exact
    commit and artifacts.
18. For task-transport changes, run the interruption matrix and confirm:
    - queued work survives pre-claim downtime;
    - graceful shutdown completes and acknowledges active work;
    - lease expiry recovers work after abrupt termination;
    - remote success followed by local pre-acknowledgement failure produces the
      expected at-least-once duplicate rather than silent loss;
    - delayed retries survive worker downtime; and
    - final ready, delayed, claim-payload, and claim-lease state is empty.
19. Confirm the exact Machinery pseudo-version and commit in `go.mod` match the
    revision covered by local, CI, container, and package evidence.
20. For publisher/fan-out changes, verify a real accepted publisher activity
    reaches a receiving server.
21. Commit and push the release preparation.
22. Create and push the annotated tag.
23. Verify the GitHub release, checksums, package metadata, packaged examples,
    and container manifests.
24. Replace generated release notes with the reviewed versioned release notes.

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
  go test -race -count=1 -p 1 ./...

go vet ./...

python3 -m unittest discover \
  -s contrib/web \
  -p 'test_*.py'

python3 -m unittest discover \
  -s contrib/ops \
  -p 'test_*.py'

docker run --rm \
  --volume "$PWD/contrib/caddy/Caddyfile.example:/etc/caddy/Caddyfile:ro" \
  caddy:2.11.2-alpine \
  caddy validate \
    --config /etc/caddy/Caddyfile \
    --adapter caddyfile

docker rm -f activity-relay-release-test-redis
```

Build and inspect the container:

```bash
ACTIVITY_RELAY_VERSION='2.5.0-rc1' \
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
  ../activity-relay_2.5.0~rc1-1_amd64.changes
```

## Tag the tested commit

```bash
git switch master
git pull --ff-only origin master

git tag -a v2.5.0-rc1 -m 'Activity-Relay v2.5.0-rc1'
git push origin v2.5.0-rc1
```

After the release workflow creates the GitHub release, apply the reviewed notes
stored in the repository:

```bash
gh release edit v2.5.0-rc1 --notes-file docs/releases/v2.5.0.md
```

For a future release candidate, use its complete `vX.Y.Z-rcN` tag and matching
versioned release-notes file. Do not promote a prerelease by moving its tag.

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
