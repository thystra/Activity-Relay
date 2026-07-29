# Optional Caddy reverse-proxy example

Activity-Relay does not require Caddy and does not install or enable it. This
example is provided alongside the Nginx and Apache templates for operators who
already use Caddy or prefer its automatic HTTPS workflow.

Caddy automatically provisions and renews certificates when the public DNS name
points to the server and ports 80 and 443 reach Caddy. Review the current Caddy
documentation and understand the configuration before deploying it.

## Files and defaults

The source example is:

```text
contrib/caddy/Caddyfile.example
```

The Debian package installs a copy under:

```text
/usr/share/doc/activity-relay/examples/caddy/
```

The example assumes:

```text
Public hostname: relay.example.org
Relay backend:  127.0.0.1:8080
Static root:    /var/www/activity-relay/public
```

Replace the hostname before use. Change the backend or `root` only when the
local deployment uses different paths.

The named matcher proxies these exact relay routes without stripping or
rewriting their paths:

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

Caddy's `reverse_proxy` preserves the incoming `Host` by default and sets the
usual forwarded headers. Do not override the upstream `Host` unless the local
architecture has a reviewed reason; Activity-Relay signs and verifies exact
HTTP authorities for federation interoperability.

## Install and validate

Merge the example site block into the system Caddyfile or copy it to a file
imported by that Caddyfile. The Activity-Relay package deliberately does not
change `/etc/caddy/Caddyfile`.

Validate before reload:

```bash
sudo caddy fmt --diff /etc/caddy/Caddyfile
sudo caddy validate   --config /etc/caddy/Caddyfile   --adapter caddyfile
sudo systemctl reload caddy
```

Validate the repository example directly with a local Caddy binary:

```bash
caddy validate   --config contrib/caddy/Caddyfile.example   --adapter caddyfile
```

Or use the official container without installing Caddy locally:

```bash
docker run --rm   --volume "$PWD/contrib/caddy/Caddyfile.example:/etc/caddy/Caddyfile:ro"   caddy:2.11.2-alpine   caddy validate     --config /etc/caddy/Caddyfile     --adapter caddyfile
```

## Frontend choices

The active fallback `handle` serves the bundled generated site. The relay
endpoints remain in the earlier `handle @relay_endpoints` block in every mode.

### No frontend

Replace only the fallback block with:

```caddyfile
handle {
    respond 404
}
```

### Redirect the root and reject other frontend paths

Replace the fallback block with:

```caddyfile
@root path /
handle @root {
    redir https://example.org/about-this-relay 302
}

handle {
    respond 404
}
```

### Custom website

Point `root` at the custom document root and retain `file_server`:

```caddyfile
handle {
    root * /srv/www/relay.example.org
    file_server
}
```

A same-origin custom page may read `/status.json` without separate CORS
configuration.

## Verification

After reload, verify both the API and selected frontend behavior:

```bash
curl --fail --silent --show-error   https://relay.example.org/actor   >/dev/null

curl --fail --silent --show-error   https://relay.example.org/status.json |
python3 -m json.tool

curl --head https://relay.example.org/
```

If the frontend is intentionally disabled, `/` should return the configured
error or redirect while every relay endpoint remains reachable.
