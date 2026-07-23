# Production resource guard rails

The example deployment intentionally fails writes instead of evicting relay
metadata when Redis reaches its memory ceiling. Size the limits for the target
host; do not copy values from a development machine.

## Storage placement

Copy `.env.example` to `.env`. The defaults assume a single-disk VPS: Redis data
uses `/var/lib/activity-relay/redis` and replaceable cache/runtime data uses
`/var/cache/activity-relay`. `RELAY_STORAGE_DIR` is mounted as Redis `/data`.
It may instead be placed on durable NAS storage while `RELAY_CACHE_DIR` remains
on faster SSD storage. The
matching `STORAGE_DIR` and `CACHE_DIR` entries in `config.yml` are host paths
used by the resource guard. Docker Compose interpolation happens before
containers start, so these paths must be kept consistent explicitly.

The single-disk default uses `STORAGE_REQUIRE_MOUNT: false`.
`STORAGE_REQUIRE_MOUNT: true` treats an existing-but-unmounted storage path as
critical. Set `STORAGE_MOUNT_POINT` to the NAS mount itself and `STORAGE_DIR` to
the Redis child directory beneath it. Keep `CACHE_REQUIRE_MOUNT: false` for an
ordinary local cache directory.

Redis latency directly affects inbox admission and delivery queues. If the NAS
has high or variable latency, prefer SSD for live Redis data and send snapshots
or backups to NAS. Do not use a network filesystem with unreliable locking or
write semantics as live Redis storage.

The Compose defaults provide:

- a 512 MiB Redis `maxmemory` ceiling with `noeviction`, so durable relay
  metadata is never silently discarded;
- a 768 MiB Redis container limit and 512 MiB limits for server/workers;
- bounded local container logs (five 10 MiB files per container);
- PID limits;
- RDB snapshots with AOF disabled to limit write amplification and disk growth.

Set limits with headroom: the Redis container limit must exceed `maxmemory`.
When Redis refuses writes, the relay may temporarily reject/drop new work but
retains subscriptions and policy data.

## Disk alerts

`resource-guard.py` checks both directory budgets and the underlying filesystem.
The more severe percentage controls the alert. It sends one message when state
changes to warning (75% by default), critical (100%), or recovered. Missing
mounts are critical, which prevents an unnoticed NAS mount failure from looking
like an empty directory.

`STORAGE_LIMIT` and `CACHE_LIMIT` accept raw bytes or human-readable
values. Decimal units (`MB`, `GB`, `TB`) use powers of 1000; binary units (`MiB`,
`GiB`, `TiB`) use powers of 1024. Units are case-insensitive, and decimal values
such as `1.5GB` are accepted.

Install it and the timer:

```bash
sudo install -D -m 0755 contrib/ops/resource-guard.py \
  /usr/local/lib/activity-relay/resource-guard.py
sudo install -m 0644 contrib/ops/activity-relay-resource-guard.service \
  contrib/ops/activity-relay-resource-guard.timer /etc/systemd/system/
sudo install -d -m 0750 /var/lib/activity-relay-guard
sudo systemctl daemon-reload
sudo systemctl enable --now activity-relay-resource-guard.timer
sudo systemctl start activity-relay-resource-guard.service
```

The default `MAIL_BACKEND: mail` uses `/usr/bin/mail` from Ubuntu's `mailutils`
package. Mailutils still needs a configured MTA to deliver beyond the local
mailbox. Install `mailutils` and a provider of `default-mta` or
`mail-transport-agent`, then arrange for mail to `root` to be delivered or
forwarded. Alternatively select `MAIL_BACKEND: sendmail` and set `MAIL_COMMAND`
to a sendmail-compatible executable supplied by Postfix, Exim, msmtp-mta,
nullmailer, or another MTA. `ADMIN_EMAIL` may be a local or external address
accepted by that transport. Test the guard without sending mail using:

```bash
sudo /usr/local/lib/activity-relay/resource-guard.py \
  --config /etc/activity-relay/config.yml --no-mail
```

For the native package, `mailutils` should be the default recommendation along
with `default-mta | mail-transport-agent`; neither a particular MTA nor Nginx
configuration should be selected automatically.

Set `DAILY_SUMMARY_EMAIL: true` to send one summary per local calendar day after
`DAILY_SUMMARY_HOUR` (0–23). The summary reads `SUMMARY_STATUS_URL`, reports the
connected-server count, added and removed domains since the previous successful
status read, and storage/cache usage versus each cap. The first summary creates
the comparison baseline. Domain lists are truncated after 100 entries per
change type, while retaining the complete baseline on disk.

Install `activity-relay.logrotate` under `/etc/logrotate.d/activity-relay` to
bound the Nginx or Apache logs. The Nginx template also limits request bodies,
request rate, and concurrent inbox connections per source address. The Apache
template limits request bodies but requires mod_evasive, mod_security, or an
upstream firewall/reverse proxy for request-count rate limiting.

## Additional recommended host controls

- Alert on Redis rejected writes, container restarts, queue latency, HTTP 429/5xx
  rates, and delivery failure rate in the host monitoring system.
- Reserve free space on the storage filesystem and monitor NAS availability.
- Back up `actor.pem`, `config.yml`, and Redis snapshots; test restoration.
- Generate an initial identity with `relay generate-key --output PATH`; package
  installation may call this only when the configured key does not exist.
- Keep manual subscription approval enabled while establishing traffic levels.
- Set CPU quotas in local Compose overrides after measuring normal peak load.
- Put a host-level firewall in front of Docker and keep Redis unpublished.

The native package should install both Nginx and Apache examples under its
documentation directory without depending on either web server or enabling a
site. The Apache alternative is `contrib/apache/activity-relay.conf.example`.
