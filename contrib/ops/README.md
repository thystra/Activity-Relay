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

## Scheduled summaries

Enable summaries and configure one or more server-local times:

```yaml
DAILY_SUMMARY_EMAIL: true
DAILY_SUMMARY_TIMES:
  - "08:00"
  - "14:30"
MAIL_TIMEOUT_SECONDS: 60
```

Times must use zero-padded 24-hour `HH:MM` notation. The service uses the
server's local timezone. Confirm it with `timedatectl`. The resource guard runs
approximately every five minutes, so `08:30` means the first timer invocation
at or after 08:30 rather than exact-to-the-second delivery.

Each configured time is an independent daily slot. A slot sends at most once
per local calendar day. Changing or adding a time creates a newly eligible slot
even if another summary was already sent that day.

After downtime, the guard sends only the most recent due unsent slot to avoid a
burst of stale mail. The catch-up email lists earlier skipped slots. Skipped
slots are recorded in:

```text
/var/lib/activity-relay-guard/summary-slots.json
```

Successful report bodies are archived under:

```text
/var/lib/activity-relay-guard/summaries/YYYY-MM-DD/
```

A skipped report has no historical body because no point-in-time snapshot was
captured. Inspect state, send a current report, or make a skipped slot eligible
again with:

```bash
sudo activity-relay-resource-guard --show-summary-state
sudo activity-relay-resource-guard --preview-summary
sudo activity-relay-resource-guard --send-summary-now
sudo activity-relay-resource-guard   --reset-summary-slot "14:30"   --force
sudo systemctl start activity-relay-resource-guard.service
```

`--send-summary-now` does not consume a scheduled slot.
`--preview-summary` and `--no-mail` do not record a slot or alter the summary
baseline. A reset resend reflects current state; it cannot reconstruct the
historical state at the skipped time.

Reset every slot for the current local day only when deliberately retesting:

```bash
sudo activity-relay-resource-guard   --reset-summary-state   --force
```

The deprecated `DAILY_SUMMARY_HOUR` setting remains supported as an `HH:00`
single-slot schedule. `DAILY_SUMMARY_MINUTE` may accompany it during migration.
`DAILY_SUMMARY_TIMES` takes precedence when both forms are present.

The systemd unit permits common local-MTA queue paths and limits a complete
oneshot run to two minutes. The Python mail subprocess has its own
`MAIL_TIMEOUT_SECONDS` limit and does not consume a slot after failure or
timeout.

The container image includes `activity-relay-resource-guard` and Python for
manual preview/state administration. The default Compose deployment does not
schedule the guard or include an MTA. Container operators must persist
`/var/lib/activity-relay-guard`, provide a working mail command for actual
delivery, and invoke the guard from a host scheduler or dedicated sidecar.


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
