# Upstream tracking and integration policy

Activity-Relay is a maintained fork of `yukimochi/Activity-Relay`, based on
upstream release `v2.0.10`. The maintained fork has since diverged in module
path, federation behavior, operational tooling, packaging, website generation,
and release automation.

## Configure the upstream remote

Keep the original repository as a read-only review source:

```bash
git remote add upstream https://github.com/yukimochi/Activity-Relay.git
git fetch upstream --tags
```

If the remote already exists, verify it before fetching:

```bash
git remote get-url upstream
git fetch upstream --prune --tags
```

Do not push maintained-fork branches or tags to the upstream remote.

## Review upstream activity

Useful comparison commands include:

```bash
git log --oneline --decorate master..upstream/master
git diff --stat master...upstream/master
git diff master...upstream/master
```

Review dependency-only changes separately from protocol, storage, packaging, or
identity-related changes.

## Integration policy

Do not wholesale-merge upstream `master` after architectural divergence.
Instead:

1. identify a specific upstream commit, issue, or release change;
2. determine whether the behavior remains applicable to this fork;
3. cherry-pick only when history and surrounding code are still compatible;
4. otherwise port the change manually with tests adapted to this repository;
5. record the upstream commit hash and issue or pull-request reference in the
   maintained-fork commit message or changelog; and
6. preserve authorship, license notices, and attribution.

Any incorporated change must preserve the maintained fork's actor identity,
configuration compatibility, Redis data, package upgrade guarantees, open-
publisher policy enforcement, and optional-frontend design unless a documented
major release intentionally changes them.

## Validation after a port

Follow `AGENTS.md` and `docs/RELEASING.md`. At minimum, run:

- Redis-backed Go tests;
- the focused race tests for `api` and `models`;
- `go vet ./...`;
- static website and operations tests;
- `git diff --check`;
- container validation when the image changes; and
- Debian package plus Lintian validation when packaging changes.

For federation changes, include a real signed publisher-to-receiver test before
release.

## Contributing fixes upstream

A fix that is generally useful and does not depend on maintained-fork-specific
architecture may be proposed upstream. Keep the upstream submission focused,
retain project-neutral examples, and do not include private infrastructure,
operator data, deployment transcripts, or maintained-fork release metadata.

## Reviewed upstream proposals

- PR 60 proposed Caddy-focused deployment tooling. Version 2.4.0 adopts the
  generally useful part as an optional, project-neutral Caddy example beside
  Nginx and Apache; it does not adopt a generator, Makefile workflow, web-root
  ownership, or runtime dependency.
- PR 100 proposes initial Prometheus HTTP metrics and health endpoints. It is a
  design input for the v2.5.0 observability work in `TODO.md`; porting should
  include queue, delivery, receiver, publisher, Redis, and readiness semantics
  appropriate to this fork.
