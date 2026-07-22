# Activity-Relay landing site

This directory provides a dependency-free generated landing site.

The deployment has two components:

- The Python builder generates static HTML, CSS, and JavaScript.
- The Go relay exposes live data at `/status.json`, which the generated JavaScript requests through Nginx.

The Go relay does not write the public HTML files itself.

## Directory relationship

A recommended installation uses:

```text
/etc/activity-relay-web/          Editable source, configuration, and policy text
/var/www/activity-relay/public/   Generated public files served by Nginx
```

The build command connects those directories:

```text
/etc/activity-relay-web
        |
        | build-site.py --output /var/www/activity-relay/public
        v
/var/www/activity-relay/public
        |
        | Nginx root /var/www/activity-relay/public
        v
Public landing page
```

The `--output` path and the Nginx `root` path must match.

## Initial installation

Run from the repository root:

```bash
sudo install -d -o root -g root -m 0755 \
  /etc/activity-relay-web \
  /var/www/activity-relay/public

sudo cp -a contrib/web/. /etc/activity-relay-web/

sudo cp -n \
  /etc/activity-relay-web/site.json.example \
  /etc/activity-relay-web/site.json
```

For upgrades, keep local content in `/etc/activity-relay-web` rather than editing a Git checkout used to compile the relay. Review new upstream templates before copying them over local policy text.

## Customize

Edit:

```bash
sudoedit /etc/activity-relay-web/site.json
```

Reusable HTML content is stored in:

```text
content/home.html
content/about.html
content/rules.html
content/privacy.html
content/footer.html
```

Shared presentation files are stored in:

```text
templates/page.html
assets/relay.css
assets/relay.js
```

## Build

```bash
sudo env PYTHONDONTWRITEBYTECODE=1 \
python3 /etc/activity-relay-web/build-site.py \
  --source /etc/activity-relay-web \
  --config /etc/activity-relay-web/site.json \
  --output /var/www/activity-relay/public
```

The generated tree contains only public files. The editable source and configuration remain outside the Nginx document root.

Expected output:

```text
index.html
about/index.html
rules/index.html
privacy/index.html
assets/relay.css
assets/relay.js
```

Do not edit generated files under `/var/www/activity-relay/public`; rerun the builder after changing source content.

Nginx does not need to be reloaded when only generated files change.

## Nginx

Start with `../nginx/activity-relay.conf.example`.

The template uses:

```nginx
root /var/www/activity-relay/public;
```

That must match the builder's:

```bash
--output /var/www/activity-relay/public
```

The static catch-all serves generated files:

```nginx
location / {
    try_files $uri $uri/ =404;
}
```

The exact status route is proxied to the Go relay:

```nginx
location = /status.json {
    proxy_pass http://activity_relay_backend;
}
```

Because the status location is an exact match, it takes precedence over the static catch-all. The landing-page JavaScript can therefore request `/status.json` from the same public hostname without CORS configuration.

Replace the hostname, certificate paths, document root, log paths, and backend address for the local installation, then run:

```bash
sudo nginx -t
sudo systemctl reload nginx
```

## Verify

```bash
curl --fail --silent --show-error \
  http://127.0.0.1:8080/status.json |
python3 -m json.tool

curl --fail --silent --show-error \
  https://relay.example.org/ \
  >/dev/null && echo 'Landing page OK'

curl --fail --silent --show-error \
  https://relay.example.org/status.json |
python3 -m json.tool
```
