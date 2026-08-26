# Oko

[![Docker](https://img.shields.io/badge/docker-ghcr.io%2Fspy4x%2Foko-blue)](https://github.com/spy4x/oko/pkgs/container/oko)
[![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![CI](https://img.shields.io/badge/CI-woodpecker-blue)](https://github.com/spy4x/oko)

A single-page server-rendered dashboard for any self-hosted homelab.
Drop a JSON catalog at `/app/config.json`, point the env vars at your
gatus instances, and Oko fans out the badge SVGs in parallel behind a
60-second in-memory single-flight cache and renders one HTML page
listing every service with its current status and 30-day uptime.

```
 browser ──GET /──► oko (Go) :8080
                     │
                     ├─ 1. cache hit? return immediately
                     ├─ 2. fetch gatus badges in parallel
                     │     ├─► uptime-cloud.${DOMAIN}
                     │     └─► uptime-home.${DOMAIN}
                     ├─ 3. parse SVG fill + uptime text
                     ├─ 4. render HTML
                     └─ 5. cache 60s
                                      ▲
                                      │
                                  (SVG responses)
```

## Features

- **Single binary, no runtime deps.** Pure Go stdlib: `net/http`,
  `log/slog`, `html/template`, `context`, `sync`, `time`.
- **Server-rendered.** One request → one HTML page. No client-side
  framework; the only JS is a ~30-line search filter.
- **JSON catalog.** Mount any `config.json` at `/app/config.json`. The
  file is re-read on every request only if its `mtime` has changed
  (mtime-cached). Edit the file → next request picks it up.
- **Gatus integration.** For each service that has `endpoint` +
  `gatus_host` set, Oko fetches two badge SVGs in parallel and
  parses the fill colour (`#40cc11` / `#e05d44`) and the 30-day
  uptime percentage from the `<text>` element. The result is cached
  for `CACHE_TTL_SECS` (default 60s) with single-flight so a burst
  of requests only triggers one upstream fetch.
- **Unknown ≠ down.** A failed gatus fetch renders the service as
  healthy (no red border) but with no uptime number. Failed upstream
  is not the same as failed service.
- **`?refresh=1`** bypasses the cache for one request — the next
  request still hits the warm cache, but the forced refetch happens
  synchronously, blocking the caller until done.
- **Distroless runtime.** ~10 MB image, nonroot, no shell, no
  package manager. Healthcheck uses the binary's own `-healthcheck`
  flag (TCP probe of its own listening socket).

## What Oko is NOT

- **Not a status page generator.** Oko renders a single dashboard
  for one homelab. For a public status page use
  [BetterStack](https://betterstack.com/uptime) or similar.
- **Not a metrics dashboard.** Oko reads gatus badges, not
  Prometheus / InfluxDB. For metrics use
  [Grafana](https://grafana.com/).
- **Not a gatus replacement.** Oko complements gatus — it assumes you
  already have [gatus](https://github.com/TwiN/gatus) running and
  configured with endpoints you want displayed.
- **Not a service discovery / health-check tool.** Oko never probes
  services itself. It reads the gatus-emitted SVG and displays it.
  Probe at the source (gatus), display at the dashboard (oko).
- **Not multi-tenant.** One process serves one dashboard. Run multiple
  instances for multiple dashboards.
- **Not a SaaS.** No auth, no rate limiting (rely on your reverse
  proxy), no telemetry, no updates channel.

## Quick start

```bash
# 1. Create a config
cp config.example.json config.json
$EDITOR config.json

# 2. Run with the right env vars
docker run --rm -p 8080:8080 \
  -e DOMAIN=example.com \
  -e UPTIME_HOSTS=uptime-cloud.example.com,uptime-home.example.com \
  -v "$PWD/config.json:/app/config.json:ro" \
  ghcr.io/spy4x/oko:latest
```

Open http://localhost:8080.

For docker-compose or Kubernetes, the same env vars + a mounted
config.json are all you need. See the [homelab
recipe](https://github.com/spy4x/homelab) for an example.

## Configuration

### `config.json` — the catalog

```json
{
  "title": "Service dashboard",
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
          "gatus_host":   "uptime-cloud"
        }
      ]
    }
  ]
}
```

**Required**: `servers[].name`, `servers[].services[].name`,
`servers[].services[].url`.

**Optional**: everything else. The `icon` field accepts any emoji or
short text. The `product` + `product_url` pair is shown as a small
attribution line below the description. The `endpoint` + `gatus_host`
pair is the gatus lookup: if either is missing, the service always
renders as healthy (no status pill). `hidden: true` skips rendering
the card but still runs the gatus fetch — useful when phasing out
a service.

**`${DOMAIN}` substitution**: in `url`, the literal token `${DOMAIN}`
is replaced at render time with the `DOMAIN` env var. No other env
vars are interpolated.

**Render order** matches JSON array order, both for servers and for
services within a server. Empty server groups are skipped.

### Environment

| Var                 | Default      | Description                                          |
| ------------------- | ------------ | ---------------------------------------------------- |
| `DOMAIN`            | *(required)* | Base domain (substituted into `${DOMAIN}`)           |
| `UPTIME_HOSTS`      | *(required)* | Comma-separated gatus FQDNs                          |
| `PORT`              | `8080`       | Listen port                                          |
| `UPTIME_TIMEOUT_SECS` | `5`        | Per-fetch timeout                                    |
| `CACHE_TTL_SECS`    | `60`         | In-memory cache TTL                                  |
| `TEMPLATE_PATH`     | `/app/web/template.html` | Override template path                       |
| `CONFIG_PATH`       | `/app/config.json`       | Override catalog path                        |

### Health parsing

For each service with `endpoint` + `gatus_host` set, Oko fans out two
GETs in parallel:

- `https://<gatus>/api/v1/endpoints/<endpoint>/health/badge.svg` —
  fill colour: `#40cc11` (up) or `#e05d44` (down). Anything else →
  unknown → render as healthy.
- `https://<gatus>/api/v1/endpoints/<endpoint>/uptimes/30d/badge.svg`
  — 30-day uptime percentage parsed from the `<text>` element via the
  regex `>\s*([\d.]+)%\s*<`. No match → unknown → omit the uptime
  pill.

A failed fetch leaves the service as **unknown** — rendered as
healthy but with no uptime number. Unknown is not down; it usually
means gatus itself or the network is broken.

These rules mirror gatus's default badge SVGs. If gatus changes its
colours or text format, `internal/gatus/client.go` needs updating.

## Local development

```bash
go test ./...
DOMAIN=example.com UPTIME_HOSTS=uptime-cloud.example.com \
  go run ./cmd/oko
```

Tests use `httptest` for mocking gatus responses and `t.TempDir()`
for catalog fixtures.

## Deployment

### Docker

The included `Dockerfile` is a multi-stage build:

- `golang:1.25-alpine` — build stage
- `gcr.io/distroless/static-debian12:nonroot` — runtime (~10 MB)

The image's `HEALTHCHECK` uses the binary's own `-healthcheck` flag
(opens a TCP probe to its own listening socket). distroless-static
has no shell, no wget — using the binary's flag avoids a heavier
base image just for health.

### Reverse proxy

Put Oko behind any reverse proxy. Cache headers are deliberately
short so health updates are timely. No special headers required.

### Healthcheck

```bash
docker exec <container> /oko -healthcheck
# exits 0 if the listening socket accepts a TCP connection
```

## Architecture

```
cmd/oko/main.go              — entrypoint, env load, server lifecycle
internal/config/             — env config + JSON catalog loader (mtime cache)
internal/cache/              — single-flight TTL cache with background refresh
internal/gatus/              — fetch + parse gatus badge SVGs
internal/render/             — HTTP handler, html/template wrapper
web/template.html            — single template file, all UI in one
config.example.json          — demo catalog (override via CONFIG_PATH mount)
```

## License

MIT — see [LICENSE](LICENSE).
