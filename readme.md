# Activity Relay Server

## A customizable ActivityPub relay server written in Go

[![GitHub Actions](https://github.com/thystra/Activity-Relay/workflows/Test/badge.svg)](https://github.com/thystra/Activity-Relay/actions)

![Powered by Ayame](docs/ayame.png)

> [!NOTE]
> This repository is a maintained fork of
> [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay),
> based on upstream release `v2.0.10`.
>
> The first maintained-fork release is `v2.1.0`.

## Fork features

Compared with upstream Activity-Relay `v2.0.10`, this fork includes:

- Friendica server actors using the `/friendica` path may follow and unfollow the relay.
- A public `GET /status.json` endpoint.
- A sorted connected-instance count and domain list in the public status response.
- An optional generated landing website with editable home, about, rules, privacy, and footer content.
- A generic Nginx configuration template for serving the website and proxying ActivityPub endpoints.
- Automated tests for Friendica follower paths and the status endpoint.

See [`CHANGELOG.md`](CHANGELOG.md) for release details.

## Compatibility and package paths

The Go module and internal import paths intentionally remain compatible with upstream:

- `github.com/yukimochi/Activity-Relay`
- `github.com/yukimochi/Activity-Relay/api`
- `github.com/yukimochi/Activity-Relay/deliver`
- `github.com/yukimochi/Activity-Relay/control`
- `github.com/yukimochi/Activity-Relay/models`

Keeping the original module path minimizes downstream breakage and makes future upstream synchronization easier. The public source URL reported by this fork's `/status.json` endpoint points to `https://github.com/thystra/Activity-Relay`.

## Requirements

- [Go](https://go.dev/) for source builds
- [Redis](https://github.com/redis/redis)
- Python 3 only when building the optional landing website
- Nginx or another web server/reverse proxy for a public deployment

## Build

For a tagged release build:

```bash
git checkout v2.1.0
mkdir -p build
go build \
  -trimpath \
  -ldflags='-X main.version=2.1.0' \
  -o build/relay \
  .
```

For a development build whose version follows Git:

```bash
mkdir -p build
go build \
  -trimpath \
  -ldflags="-X main.version=$(git describe --tags --always --dirty | sed 's/^v//')" \
  -o build/relay \
  .
```

## Run

### API server

```bash
relay --config /path/to/config.yml server
```

### Job worker

```bash
relay --config /path/to/config.yml worker
```

### CLI management utility

```bash
relay --config /path/to/config.yml control
```

## Configuration

### YAML format

```yaml
ACTOR_PEM: /var/lib/relay/actor.pem
REDIS_URL: redis://redis:6379

RELAY_BIND: 127.0.0.1:8080
RELAY_DOMAIN: relay.example.org
RELAY_SERVICENAME: Example ActivityPub Relay
JOB_CONCURRENCY: 10
# RELAY_SUMMARY: |

# RELAY_ICON: https://
# RELAY_IMAGE: https://
```

Binding the relay to `127.0.0.1` is recommended when Nginx runs on the same host.

### Environment variables

When the configuration file does not exist, the following environment variables may be used:

- `ACTOR_PEM`
- `REDIS_URL`
- `RELAY_BIND`
- `RELAY_DOMAIN`
- `RELAY_SERVICENAME`
- `JOB_CONCURRENCY`
- `RELAY_SUMMARY`
- `RELAY_ICON`
- `RELAY_IMAGE`

## How participating servers subscribe

### Mastodon, Misskey, and compatible software

Subscribe to:

```text
https://<your-relay-server-address>/inbox
```

### Pleroma, Akkoma, Friendica, and compatible software

Follow:

```text
https://<your-relay-server-address>/actor
```

## Public status endpoint

The API server exposes:

```text
GET /status.json
```

It reports the relay name, domain, registration policy, software version, subscription endpoints, and the sorted list of connected domains.

Example local check:

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

The endpoint intentionally does not expose Redis keys, queue internals, blocked-domain lists, or private configuration values.

## Optional landing website

The landing website has two distinct parts:

1. `contrib/web/build-site.py` generates static HTML, CSS, and JavaScript files.
2. The generated browser JavaScript fetches live relay data from `/status.json` through Nginx.

The relay Go process does **not** write files into the Nginx document root. The site builder creates those files before Nginx serves them.

### How the files connect

```text
Editable source and policy text
/etc/activity-relay-web/
        |
        |  build-site.py --output /var/www/activity-relay/public
        v
Generated public website
/var/www/activity-relay/public/
        |
        |  Nginx: root /var/www/activity-relay/public;
        v
https://relay.example.org/
        |
        |  browser fetches /status.json
        v
Nginx proxies /status.json to 127.0.0.1:8080
```

The critical rule is:

> The path passed to `build-site.py --output` must be the same path used by the Nginx `root` directive.

No symbolic link to the relay binary is required. Nginx serves the generated files directly and proxies only the relay API and ActivityPub routes.

### 1. Install a deployment-owned copy of the editable site

Run these commands from the repository root:

```bash
sudo install -d -o root -g root -m 0755 \
  /etc/activity-relay-web \
  /var/www/activity-relay/public

sudo cp -a contrib/web/. /etc/activity-relay-web/

sudo cp --update=none \
  /etc/activity-relay-web/site.json.example \
  /etc/activity-relay-web/site.json
```

Keep local policy and operator text in `/etc/activity-relay-web`, rather than editing a Git checkout used for compiling the relay.

### 2. Customize the site

Edit the site settings:

```bash
sudoedit /etc/activity-relay-web/site.json
```

Edit any of these reusable content files:

```text
/etc/activity-relay-web/content/home.html
/etc/activity-relay-web/content/about.html
/etc/activity-relay-web/content/rules.html
/etc/activity-relay-web/content/privacy.html
/etc/activity-relay-web/content/footer.html
```

The source directory also contains the shared page template and assets:

```text
/etc/activity-relay-web/templates/page.html
/etc/activity-relay-web/assets/relay.css
/etc/activity-relay-web/assets/relay.js
```

### 3. Generate the public files

```bash
sudo env PYTHONDONTWRITEBYTECODE=1 \
python3 /etc/activity-relay-web/build-site.py \
  --source /etc/activity-relay-web \
  --config /etc/activity-relay-web/site.json \
  --output /var/www/activity-relay/public
```

The output should contain:

```text
/var/www/activity-relay/public/index.html
/var/www/activity-relay/public/about/index.html
/var/www/activity-relay/public/rules/index.html
/var/www/activity-relay/public/privacy/index.html
/var/www/activity-relay/public/assets/relay.css
/var/www/activity-relay/public/assets/relay.js
```

Make the generated tree readable by Nginx:

```bash
sudo find /var/www/activity-relay/public \
  -type d -exec chmod 0755 {} +

sudo find /var/www/activity-relay/public \
  -type f -exec chmod 0644 {} +
```

Do not hand-edit files under `/var/www/activity-relay/public`; they are build output and will be replaced the next time the builder runs.

### 4. Connect Nginx to the generated output

Start with:

```text
contrib/nginx/activity-relay.conf.example
```

The important parts are:

```nginx
root /var/www/activity-relay/public;
index index.html;

location / {
    try_files $uri $uri/ =404;
}

location = /status.json {
    proxy_pass http://activity_relay_backend;
}
```

The static `location /` serves the files made by `build-site.py`. The exact `/status.json` location takes precedence and proxies the dashboard's live-data request to the Go API server.

For an Ubuntu/Debian-style Nginx installation:

```bash
sudo cp \
  contrib/nginx/activity-relay.conf.example \
  /etc/nginx/sites-available/activity-relay.conf

sudoedit /etc/nginx/sites-available/activity-relay.conf

sudo ln -sfn \
  /etc/nginx/sites-available/activity-relay.conf \
  /etc/nginx/sites-enabled/activity-relay.conf

sudo nginx -t
sudo systemctl reload nginx
```

Replace the example hostname, certificate paths, log paths, document root, and backend address before reloading. On distributions that do not use `sites-available` and `sites-enabled`, include the template from the local Nginx configuration layout instead.

### 5. Verify the complete path

Check the backend directly:

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool
```

Check the public routes through Nginx:

```bash
curl --fail --silent --show-error \
  https://relay.example.org/ \
  >/dev/null && echo 'Landing page OK'

curl --fail --silent --show-error \
  https://relay.example.org/status.json |
python3 -m json.tool
```

After editing `site.json`, a template, an include, CSS, or JavaScript, rerun the site-builder command. Nginx does not need to be reloaded when only generated website files change.

Additional site-specific documentation is in [`contrib/web/README.md`](contrib/web/README.md).

## Testing

The package tests expect a Redis instance. Do not run them against production Redis. One disposable local example is:

```bash
test_redis_dir="$(mktemp -d)"
test_redis_port=6381

redis-server \
  --bind 127.0.0.1 \
  --port "$test_redis_port" \
  --save '' \
  --appendonly no \
  --daemonize yes \
  --pidfile "$test_redis_dir/redis.pid" \
  --logfile "$test_redis_dir/redis.log" \
  --dir "$test_redis_dir"

REDIS_URL="redis://127.0.0.1:${test_redis_port}" \
go test -p 1 ./api ./deliver ./control ./models

redis-cli -h 127.0.0.1 -p "$test_redis_port" shutdown nosave
rm -rf "$test_redis_dir"
```

## Release documentation

Maintainer release steps are documented in [`docs/RELEASING.md`](docs/RELEASING.md).

## Upstream and attribution

This project is derived from:

- Upstream repository: [`yukimochi/Activity-Relay`](https://github.com/yukimochi/Activity-Relay)
- Fork baseline: upstream `v2.0.10`

Original authorship, commit history, license notices, and Go module paths are retained. Generally useful fixes may still be proposed upstream, while this fork can publish its own tested releases.

The original upstream project and its sponsors are acknowledged in the upstream repository.

## License

GNU Affero General Public License version 3. See [`LICENCE`](LICENCE).
