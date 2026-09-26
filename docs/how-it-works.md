# How oko works

What oko does, what it deliberately leaves out, and how the code is laid out.

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
  parses the health text (`up` / `down`) and the 30-day uptime
  percentage from the `<text>` elements. The result is cached
  for `CACHE_TTL_SECS` (default 60s) with single-flight so a burst
  of requests only triggers one upstream fetch.
- **Unknown ≠ down, and unknown ≠ healthy.** When gatus gives no
  answer for a service, the card gets a dashed "unknown" marker
  instead of a red border, and the section counter reports it
  separately ("18/22 healthy · 4 unknown"). Failed upstream is not
  the same as failed service.
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
