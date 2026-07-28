# Activity-Relay optional landing site

The relay does not require a frontend. This directory provides an optional,
dependency-free static site and examples for operators who want one.

The public website and the relay runtime are separate:

- The Go relay exposes ActivityPub endpoints and `/status.json`.
- `build-site.py` generates static HTML, CSS, and JavaScript.
- Nginx, Apache, or another reverse proxy serves the chosen frontend and
  proxies the relay endpoints to the Go service.

## Frontend choices

An operator may use any of these arrangements:

1. Generate and serve the bundled website.
2. Serve a completely custom website.
3. Redirect `/` to another page.
4. Return `404` for `/` and all non-relay paths.

The reverse proxy must continue forwarding these routes to the relay:

```text
/inbox
/actor
/actor/outbox
/actor/followers
/actor/following
/status.json
/.well-known/nodeinfo
/.well-known/webfinger
/nodeinfo/2.1
```

The actor collection routes return privacy-filtered empty ActivityPub
`OrderedCollection` documents. They must remain proxied even when the optional
frontend is disabled, redirected, or replaced.

### Disable the frontend with Nginx

Replace the generated-site `location /` block with:

```nginx
location / {
    return 404;
}
```

The exact relay endpoint locations remain proxied.

### Redirect the root page with Nginx

```nginx
location = / {
    return 302 https://example.org/about-this-relay;
}

location / {
    return 404;
}
```

### Use a custom website

Point the web server document root at the custom generated or hand-written
files. A custom page may read `/status.json` from the same hostname without CORS
configuration.

A minimal CSP-compatible example is provided in:

```text
examples/status-widget.html
examples/status-widget.js
```

Copy both files to the chosen document root, or use them as a starting point.

## Native or Debian installation

A recommended native installation uses:

```text
/etc/activity-relay-web/          Editable source and configuration
/var/www/activity-relay/public/   Generated public files
```

For the Debian package, initialize an editable copy without overwriting local
files:

```bash
sudo cp -an \
  /usr/share/activity-relay/web/. \
  /etc/activity-relay-web/

sudo cp --update=none \
  /etc/activity-relay-web/site.json.example \
  /etc/activity-relay-web/site.json
```

For a source checkout:

```bash
sudo install -d -o root -g root -m 0755 \
  /etc/activity-relay-web \
  /var/www/activity-relay/public

sudo cp -a contrib/web/. /etc/activity-relay-web/

sudo cp --update=none \
  /etc/activity-relay-web/site.json.example \
  /etc/activity-relay-web/site.json
```

Customize:

```bash
sudoedit /etc/activity-relay-web/site.json
```

The optional `activitypub_contact` value displays a fediverse handle. Set
`activitypub_contact_url` to its absolute HTTPS profile URL to make it
clickable. These settings affect only generated website content.

Build to the package's default document root with the current
package-managed source:

```bash
sudo activity-relay-rebuild-site
```

Build to a different document root:

```bash
sudo activity-relay-rebuild-site \
  --output /srv/www/relay.example.org
```

The package-managed command also accepts `--source`, `--config`,
`--relay-config`, `--content-overrides`, and `--asset-overrides`. An older
regular file at `/etc/activity-relay-web/rebuild-site.sh` may remain after an
upgrade, but it is not authoritative.

For a source checkout or completely user-owned directories, invoke the checkout
wrapper directly:

```bash
"$HOME/activity-relay-web/activity-relay-rebuild-site" \
  --source "$HOME/activity-relay-web" \
  --config "$HOME/activity-relay-web/site.json" \
  --output "$HOME/public_html/relay"
```

The expanded equivalent command is:

```bash
sudo env PYTHONDONTWRITEBYTECODE=1 \
python3 /etc/activity-relay-web/build-site.py \
  --source /etc/activity-relay-web \
  --config /etc/activity-relay-web/site.json \
  --output /var/www/activity-relay/public
```

The web-server document root must match the builder's `--output` path. Do not
edit files under `/var/www/activity-relay/public`; they are generated output.

## Container installation

Before using the Compose deployment, create `.env`,
`config.yml`, and the relay identity file `actor.pem`. Run
`contrib/docker/compose-preflight.sh` from the repository root to confirm that
the required bind mounts are regular files.


The published relay image contains the editable website sources at:

```text
/usr/share/activity-relay/web
```

The runtime image includes Python 3 for resource-guard tooling and website
generation. Extract the source before customizing it:

```bash
export ACTIVITY_RELAY_IMAGE='ghcr.io/thystra/activity-relay:2.4.0-rc6'

mkdir -p \
  activity-relay-web \
  activity-relay-public

container_id="$(docker create "$ACTIVITY_RELAY_IMAGE")"

docker cp \
  "$container_id:/usr/share/activity-relay/web/." \
  ./activity-relay-web/

docker rm "$container_id"

cp -n \
  ./activity-relay-web/site.json.example \
  ./activity-relay-web/site.json
```

Edit `activity-relay-web/site.json` and any templates or assets, then build
with the same release image:

```bash
docker run \
  --rm \
  --user "$(id -u):$(id -g)" \
  --entrypoint python3 \
  --volume "$PWD/activity-relay-web:/site:ro" \
  --volume "$PWD/activity-relay-public:/output" \
  "$ACTIVITY_RELAY_IMAGE" \
  /site/build-site.py \
    --source /site \
    --config /site/site.json \
    --output /output
```

Serve `activity-relay-public` from the host reverse proxy or a separate web
container. Re-run the builder after changing source files; the relay containers
do not need restarting.

## Nginx and Apache

Separate complete examples are included for both servers:


```text
../nginx/activity-relay.conf.example
../apache/activity-relay.conf.example
```

Both templates use the bundled site by default and include comments describing
how to disable, redirect, or replace the frontend. Neither template is enabled
automatically by the Debian package.

For Apache on Debian or Ubuntu, enable the modules listed at the top of the
example file before enabling the virtual host:

```bash
sudo a2enmod   headers   http2   proxy   proxy_http   proxy_wstunnel   rewrite   ssl
```

When changing the generated output path, update both Apache's `DocumentRoot`
and its matching `<Directory>` path. For Nginx, update the `root` directive.

## Verification

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool

curl --fail --silent --show-error \
  https://relay.example.org/status.json |
python3 -m json.tool
```

When using a frontend, also verify its public root. When intentionally disabling
the frontend, verify that `/` returns the chosen redirect or error status while
the relay endpoints remain available.

## Upgrade-safe website customization

The package separates current program files from operator-owned content:

```text
/usr/share/activity-relay/web/
    Current package-managed builder, templates, JavaScript, CSS, and defaults.

/etc/activity-relay/config.yml
    Relay behavior and shared public metadata:
    RELAY_ICON
    RELAY_IMAGE
    FEDIVERSE_OPERATOR_ID
    FEDIVERSE_OPERATOR_URL

/etc/activity-relay-web/site.json
    Website name, tagline, email/contact URL, source URL, language, and optional
    website-specific logo_url or banner_url overrides.

/etc/activity-relay-web/content/
    Optional operator replacements for:
    home.html
    about.html
    rules.html
    privacy.html
    footer.html

/etc/activity-relay-web/custom-assets/
    Optional replacement or additional public assets.

/var/www/activity-relay/public/
    Generated output. Do not edit it directly.
```

To customize rules without copying the whole website source:

```bash
sudo install -d -o root -g root -m 0755 \
  /etc/activity-relay-web/content

sudo cp \
  /usr/share/activity-relay/web/content/rules.html \
  /etc/activity-relay-web/content/rules.html

sudoedit /etc/activity-relay-web/content/rules.html
```

Use the same pattern for `home.html`, `about.html`, `privacy.html`, or
`footer.html`. Files not present in the override directory come from the
current package, so package fixes remain effective.

Set shared branding and the public operator handle in
`/etc/activity-relay/config.yml`:

```yaml
RELAY_ICON: "https://relay.example.org/images/relay-icon.png"
RELAY_IMAGE: "https://relay.example.org/images/relay-banner.png"
FEDIVERSE_OPERATOR_ID: "@operator@social.example"
# Optional because profile URL patterns differ among fediverse applications:
FEDIVERSE_OPERATOR_URL: "https://social.example/@operator"
```

The canonical configuration names use underscores. The builder also accepts
the legacy hyphenated aliases `FEDIVERSE-OPERATOR-ID` and
`FEDIVERSE-OPERATOR-URL`. The website-specific `logo_url` and `banner_url`
values in `site.json` take precedence when non-empty.

Rebuild with the current package source:

```bash
sudo activity-relay-rebuild-site
```

An older regular file at `/etc/activity-relay-web/rebuild-site.sh` may be
preserved during upgrades. The package-managed command above is authoritative.

