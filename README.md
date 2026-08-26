# Oko (Russian: око = eye)

[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fspy4x%2Foko-blue)](https://github.com/spy4x/oko/pkgs/container/oko)
[![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Oko** (Око — Russian: "eye") is a tiny server-rendered dashboard for
every self-hosted service across a homelab. Each request reads a JSON
catalog (mtime-cached — edit the file, no restart needed) and, for
services with gatus configured, fans out gatus badge SVGs in parallel
behind a 60-second in-memory single-flight cache. Renders the full HTML
page server-side. No client-side JS beyond a ~30-line search filter.

```
┌────────────────────────────┐
 browser ─────► │  oko (Go) :8080             │
   GET /        │  1. cache hit? return       │
                │  2. fetch gatus badges ════╪══════► uptime-cloud.${DOMAIN}
                │     in parallel            ║ uptime-home.${DOMAIN}
                │  3. parse SVG fill + %     ║
                │  4. render HTML            ║
                │  5. cache 60s              ║
                └────────────────────────────║ ◄══ (SVG responses)
```

## Why Oko

The previous dash was a static HTML file rendered at deploy time. That
broke two invariants:

1. Deploys should produce immutable artifacts, never call monitoring.
2. Health status is "right now" — baking it into a file lies within seconds.

Oko is reusable: no services hardcoded. Drop a JSON config file at
`CONFIG_PATH` (default `/app/config.json`), restart nothing — the file
is re-read whenever its mtime changes.

## Quick start

```bash
# 1. Copy the sample config
cp config.example.json config.json

# 2. Edit it — replace the demo services with your own
$EDITOR config.json

# 3. Run (env vars give the runtime context)
DOMAIN=example.com \
UPTIME_HOSTS=uptime-cloud.example.com \
docker run --rm -p 8080:8080 \
  -v "$PWD/config.json:/app/config.json:ro" \
  ghcr.io/spy4x/oko:latest
```

Visit http://localhost:8080.

## Configuration

### `config.json` — the catalog

```json
{
  "title": "My Homelab",
  "subtitle": "Single page for every self-hosted service — search, jump, check status",
  "servers": [
    {
      "name": "Home",
      "services": [
        {
          "name":         "Audiobooks",
          "url":          "https://books.${DOMAIN}",
          "icon":         "📚",
          "description":  "Audiobook and podcast library with streaming",
          "product":      "Audiobookshelf",
          "product_url":  "https://www.audiobookshelf.org/",
          "endpoint":     "home_audiobookshelf",
          "gatus_host":   "uptime-cloud",
          "hidden":       false
        }
      ]
    }
  ]
}
```

- **Required**: `servers[].name`, `servers[].services[].name`, `servers[].services[].url`
- **Optional**: everything else
- **`${DOMAIN}` substitution**: in `url`, the literal token `${DOMAIN}`
  is replaced at render time with the `DOMAIN` env var. Other env vars
  are NOT interpolated — keep URLs literal or use `${DOMAIN}` only.
- **Gatus fields** (`endpoint` + `gatus_host`): both required to enable
  status lookup. Either missing = service always renders as healthy.
  `gatus_host` must match a short name from `UPTIME_HOSTS` (e.g.
  `uptime-cloud` matches `uptime-cloud.example.com`).
- **`hidden: true`**: skips rendering but keeps the gatus fetch on
  (useful when phasing out a card).

Render order matches JSON array order — both for servers and for
services within a server. Empty servers are skipped.

### Environment

| Var                 | Default      | Description                                        |
| ------------------- | ------------ | -------------------------------------------------- |
| `DOMAIN`            | *(required)* | Base domain (substituted into `${DOMAIN}`)         |
| `UPTIME_HOSTS`      | *(required)* | Comma-separated gatus FQDNs                        |
| `PORT`              | `8080`       | Listen port                                        |
| `UPTIME_TIMEOUT_SECS` | `5`        | Per-fetch timeout                                  |
| `CACHE_TTL_SECS`    | `60`         | In-memory cache TTL                                |
| `TEMPLATE_PATH`     | `/app/web/template.html` | Override template path                   |
| `CONFIG_PATH`       | `/app/config.json`       | Override catalog path                    |

### Health data

For each service with `endpoint` + `gatus_host`, Oko fans out two GETs
in parallel:

- `https://<gatus>/api/v1/endpoints/<endpoint>/health/badge.svg` —
  parsed via SVG fill colour: `#40cc11` (up) or `#e05d44` (down)
- `https://<gatus>/api/v1/endpoints/<endpoint>/uptimes/30d/badge.svg` —
  30-day uptime percentage parsed from the `<text>` element

A failed fetch leaves the service as **unknown** — rendered as healthy
but with no uptime number. Unknown is not down; it usually means gatus
itself or the network is broken.

## Refresh

`?refresh=1` bypasses the cache for that single request (next request
still hits the warm cache, but the forced refetch happens immediately,
blocking the caller until done). A small "refresh" link in the footer
calls this.

## Editing the catalog

The config file is re-read whenever its mtime changes — no restart
needed:

```bash
$EDITOR /path/to/config.json    # save
curl -s "http://localhost:8080/?refresh=1" > /dev/null   # optional: pick up immediately
```

The next request picks up the new content. The catalog file is also
loaded eagerly at startup so syntax errors surface in logs at boot.

## Adding the gatus endpoints you monitor

For each service in `config.json` with `gatus_host: "uptime-cloud"`,
there must be a corresponding entry in your `uptime-cloud` gatus config.
The endpoint key in gatus must match the `endpoint` field in the JSON
exactly.

Example: if your JSON has `"endpoint": "home_audiobookshelf"`,
`uptime-cloud` must have:

```yaml
endpoints:
  - name: home_audiobookshelf
    group: home
    url: "https://books.${DOMAIN}"
    ...
```

## Architecture

```
cmd/oko/main.go              — entrypoint, env load, server lifecycle
internal/config/             — env config + JSON catalog loader (mtime cache)
internal/cache/              — single-flight TTL cache
internal/gatus/              — fetch + parse gatus badge SVGs
internal/render/             — handler + html/template wrapper
web/template.html            — single template file, all UI in one
config.example.json          — demo catalog (override via CONFIG_PATH)
```

## Local dev

```bash
go test ./...                                      # unit tests
DOMAIN=example.com UPTIME_HOSTS=uptime-cloud.example.com \
  go run ./cmd/oko                                 # serve at :8080
```

## Healthcheck

The image's `HEALTHCHECK` uses the binary's own `-healthcheck` flag
(opens a TCP probe to its own listening socket). distroless-static has
no shell, no wget — using the binary's flag avoids a heavier base
image just for health.

[Oko GitHub](https://github.com/spy4x/oko)

<!-- Last verified: 2026-08-26 — deployed live at https://dash.antonshubin.com -->

<!-- retry Wed 26 Aug 2026 02:56:52 PM UTC -->
