# Oko (Russian: око = eye)

[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fspy4x%2Foko-blue)](https://github.com/spy4x/oko/pkgs/container/oko)
[![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

**Oko** (Око — Russian: "eye") is a tiny server-rendered dashboard for
every self-hosted service across a homelab. Each request fetches gatus
badge SVGs in parallel, parses out the health and 30-day uptime, and
renders the full HTML page server-side. There is no client-side JS
beyond a ~30-line filter for the search box.

```
┌────────────────────────────┐
 browser ─────► │  oko (Go) :8080            │
   GET /        │  1. cache hit? return      │
                │  2. fetch gatus badges ═══╪══════► uptime-cloud.${DOMAIN}
                │     in parallel           ║ uptime-home.${DOMAIN}
                │  3. parse SVG fill + %    ║
                │  4. render HTML           ║
                │  5. cache 60s             ║
                └────────────────────────────║ ◄══ (SVG responses)
```

## Why Oko?

The previous dash was a static HTML file generated at deploy time. That
broke two invariants:

1. Deploys should produce immutable artifacts, never call monitoring.
2. Health status is "right now" — baking it into a file lies within seconds.

Oko fetches gatus on every request, behind a 60-second in-memory cache
(single-flight, background refresh). One HTTP server, one binary, one
distroless image. Memory budget: ~10 MB.

## Environment

| Var                 | Default      | Description                                        |
| ------------------- | ------------ | -------------------------------------------------- |
| `DOMAIN`            | *(required)* | Base domain for URLs (e.g. `antonshubin.com`)      |
| `UPTIME_HOSTS`      | *(required)* | Comma-separated gatus FQDNs (e.g. `uptime-cloud.X,uptime-home.X`) |
| `PORT`              | `8080`       | Listen port                                        |
| `UPTIME_TIMEOUT_SECS` | `5`        | Per-fetch timeout                                  |
| `CACHE_TTL_SECS`    | `60`         | In-memory cache TTL                                |
| `TEMPLATE_PATH`     | `/app/web/template.html` | Override template path                   |

## Health data

For each service, Oko fans out two GETs in parallel:

- `https://<gatus>/api/v1/endpoints/<key>/health/badge.svg` — parsed via SVG fill colour: `#40cc11` (up) or `#e05d44` (down)
- `https://<gatus>/api/v1/endpoints/<key>/uptimes/30d/badge.svg` — 30-day uptime percentage parsed from the `<text>` element

A failed fetch leaves the service as **unknown** — rendered as healthy
but with no uptime number. Unknown is not down; it usually means gatus
itself or the network is broken.

## Refresh

`?refresh=1` bypasses the cache for that single request (the next
request still gets a fresh cache, but a forced refetch happens
immediately, blocking the caller until done).

## Adding services

Edit `internal/config/config.go` `ServiceList`. Each entry needs:

- `Name` — user-facing label
- `Product` — underlying product name (footer link)
- `ProductURL` — product homepage
- `Endpoint` — gatus endpoint key
- `GatusHost` — which gatus server exposes that key (`uptime-cloud` / `uptime-home`)
- `URL` — service URL, with `${DOMAIN}` substituted at render time
- `Section` — one of `home` / `cloud` / `offsite` / `portable`
- `Icon` — emoji
- `Description` — short blurb
- `Hidden` — `true` keeps gatus fetch on but skips rendering (for phasing out)

Then rebuild the image and redeploy.

## Local dev

```bash
DOMAIN=antonshubin.com \
UPTIME_HOSTS=uptime-cloud.antonshubin.com,uptime-home.antonshubin.com \
make run
```

Visit http://localhost:8080.

## Architecture

```
cmd/oko/main.go              — entrypoint, flags, signal handling, http.Server lifecycle
internal/config/             — env config + service list
internal/cache/              — single-flight TTL cache
internal/gatus/              — fetch + parse gatus badge SVGs
internal/render/             — handler + html/template
web/template.html            — single template file, all sections in one
```

[Oko GitHub](https://github.com/spy4x/oko)
