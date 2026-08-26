# AGENTS.md — Oko

## Stack

- **Language:** Go 1.25
- **Runtime deps:** none (pure stdlib: `net/http`, `log/slog`, `html/template`, `context`, `sync`, `os/signal`, `strings`, `regexp`, `strconv`, `time`, `io`, `encoding/json`)
- **Container:** distroless `static-debian12:nonroot` (~10 MB)
- **CI:** Woodpecker on `golang:1.25-alpine`

## Invariants

- **Server-rendered dashboard.** Health data is fetched at request time, cached
  in-memory for `CACHE_TTL_SECS` (default 60s). Never bake health into the
  build artifact — that would make the page lie within seconds.
- **One process, one port.** HTTP server is the only long-lived goroutine
  besides the cache's background refresh ticker and the catalog's mtime poller.
- **Reusable.** No services hardcoded. The catalog is loaded from a JSON file
  (`CONFIG_PATH`, default `/app/config.json`). Mtime-cached, so editing the
  file is enough — no restart needed.
- **Pure stdlib.** No third-party deps. `html/template` escapes user data by
  default — keep it that way.
- **Unknown ≠ down.** A failed gatus fetch leaves the service rendered as
  healthy (no red border) but with no uptime number. Don't conflate "gatus is
  broken" with "service is broken".

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

## Conventions

- **Idiomatic Go:** gofmt-clean, no unused vars/imports, accept interfaces
  return structs where it helps testing
- **Errors:** return `error`; wrap with `fmt.Errorf("...%w", err)`. Background
  goroutines log and continue; request handlers return 500 on real errors
- **Logging:** `log/slog` text handler to stderr; structured keys via
  `slog.Int`, `slog.String`, etc.
- **Concurrency:** `sync.RWMutex` for the catalog cache, `sync.Mutex` for
  the in-flight single-flight on gatus, `context.Context` everywhere I/O
  happens
- **No globals, no `init()` side effects** — all wiring through `main()` →
  constructors

## Catalog schema (JSON)

```json
{
  "title":    "...",
  "subtitle": "...",
  "servers": [
    {
      "name": "Home",
      "services": [
        {
          "name":         "...",
          "url":          "...",
          "icon":         "...",
          "description":  "...",
          "product":      "...",
          "product_url":  "...",
          "endpoint":     "...",
          "gatus_host":   "...",
          "hidden":       false
        }
      ]
    }
  ]
}
```

Required: `servers[].name`, `servers[].services[].name`, `servers[].services[].url`.
Optional: everything else. `endpoint` + `gatus_host` must both be set or both
empty (half-configured gatus is rejected at validation).

## Health parsing rules

- Health: SVG body must contain `#40cc11` (up) or `#e05d44` (down). Anything
  else → unknown → render as healthy
- Uptime: regex `>\s*([\d.]+)%\s*<` extracts the percentage from the badge's
  `<text>` element. No match → unknown → omit uptime pill for that section

These are tied to gatus's default badge SVGs. If gatus changes its colours or
text format, `internal/gatus/client.go` needs updating.

## Adding services to your dashboard

1. Edit your `config.json` — add the service to the appropriate server's
   `services` array.
2. If using gatus, add a matching endpoint to your gatus config (the
   `endpoint` field in JSON must match gatus's endpoint `name`).
3. Save. The next request to oko picks up the new content automatically
   (mtime cache).

## Running tests

```bash
go test ./...
```

Tests use `httptest` for mocking gatus responses and `t.TempDir()` for
catalog fixtures.
