# Activity-Relay development and testing

This document collects contributor and behind-the-scenes material that is not
needed in the primary operator README.

Start with:

- [`../AGENTS.md`](../AGENTS.md) for repository conventions;
- [`../ARCHITECTURE.md`](../ARCHITECTURE.md) for components and data flow;
- [`../TODO.md`](../TODO.md) for maintained roadmap and release gates; and
- [`RELEASING.md`](RELEASING.md) for the release process.

## Local Go validation

Do not run tests against production Redis. Start a disposable instance:

```bash
docker rm -f activity-relay-test-redis \
  >/dev/null 2>&1 || true

docker run \
  --detach \
  --rm \
  --name activity-relay-test-redis \
  --publish 127.0.0.1:6381:6379 \
  redis:7-alpine

until docker exec activity-relay-test-redis redis-cli ping |
  grep -qx PONG
do
  sleep 1
done
```

Run the normal and race suites against that disposable Redis:

```bash
REDIS_URL='redis://127.0.0.1:6381' \
  go test -count=1 -p 1 ./...

REDIS_URL='redis://127.0.0.1:6381' \
  go test -race -count=1 -p 1 ./...

go vet ./...
git diff --check
```

Then remove the disposable Redis instance:

```bash
docker rm -f activity-relay-test-redis
```

## Python validation

Website and operational tooling have independent Python suites:

```bash
python3 -m unittest discover \
  -s contrib/web \
  -p 'test_*.py'

python3 -m unittest discover \
  -s contrib/ops \
  -p 'test_*.py'
```

## Integration and package testing

The cross-deployment validation matrix is maintained in
[`INTEGRATION-TESTING.md`](INTEGRATION-TESTING.md). It covers container and
native-package paths and the interoperability checks expected before release.

Debian/package lifecycle details and canonical artifact generation belong in
[`RELEASING.md`](RELEASING.md), rather than the operator README.

## Protocol and interoperability documents

Use the focused protocol documents when changing wire behavior:

- [`DIRECTORY-CLIENT.md`](DIRECTORY-CLIENT.md) — Directory lifecycle,
  scheduling, persistence, and failure handling;
- [`HTTP-MESSAGE-SIGNATURES.md`](HTTP-MESSAGE-SIGNATURES.md) — HTTP signature
  profile;
- [`OUTBOUND-SIGNATURE-NEGOTIATION.md`](OUTBOUND-SIGNATURE-NEGOTIATION.md) —
  destination-aware `dual` negotiation;
- [`FEP-AE0C-COMPATIBILITY.md`](FEP-AE0C-COMPATIBILITY.md) — relevant FEP
  compatibility;
- [`INTEROPERABILITY.md`](INTEROPERABILITY.md) — implementation-specific
  validation and troubleshooting; and
- [`SECURITY.md`](SECURITY.md) — security boundaries and compatibility policy.

## Release preparation

Before tagging any release, follow the complete checklist in
[`RELEASING.md`](RELEASING.md). The canonical Forgejo artifact workflow is the
release-byte authority; GitHub remains a downstream mirror and independent
validation surface.
