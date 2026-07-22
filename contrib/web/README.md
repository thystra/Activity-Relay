# Activity-Relay landing site

This directory provides a dependency-free static landing site. The Go relay serves live data at `/status.json`; Nginx serves the generated HTML, CSS, and JavaScript.

## Customize

Copy `site.json.example` to a deployment-owned location such as:

```text
/etc/activity-relay-web/site.json
```

Edit these reusable HTML includes as needed:

```text
content/home.html
content/about.html
content/rules.html
content/privacy.html
content/footer.html
```

For upgrades, it is usually better to copy the entire `contrib/web` directory to `/etc/activity-relay-web` and keep local policy text there rather than editing a Git checkout used for builds.

## Build

Example:

```bash
sudo install -d -m 0755 /var/www/activity-relay/public
sudo python3 /etc/activity-relay-web/build-site.py \
  --source /etc/activity-relay-web \
  --config /etc/activity-relay-web/site.json \
  --output /var/www/activity-relay/public
```

The generated tree contains only public files. The editable source and configuration can remain outside the Nginx document root.

## Nginx

Start with `../nginx/activity-relay.conf.example`. Replace the hostname, certificate paths, document root, and backend address for the local installation.
