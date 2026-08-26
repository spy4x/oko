# AGENTS.md — Oko

## Stack

- **Language:** Go 1.25
- **Runtime deps:** none (pure stdlib: `net/http`, `log/slog`, `html/template`, `context`, `sync`, `os/signal`, `strings`, `regexp`, `strconv`, `time`, `io`)
- **Container:** distroless `static-debian12:nonroot` (~10 MB)
- **CI:** none yet (Woodpecker config is a future addition)

## Invariants

- **Server-rendered dashboard.** Health data is fetched at request time, cached
  in-memory for `CACHE_TTL_SECS` (default 60s). Never bake health into the
  build artifact — that would make the page lie within seconds.
- **One process, one port.** HTTP server is the only long-lived goroutine
  besides the cache's background refresh ticker.
- **Pure stdlib.** No third-party deps. `html/template` escapes user data by
  default — keep it that way.
- **Unknown ≠ down.** A failed gatus fetch leaves the service rendered as
  healthy (no red border) but with no uptime number. Don't conflate "gatus is
  broken" with "service is broken".

## Architecture

```
cmd/oko/main.go              — entrypoint, env load, server lifecycle
internal/config/             — env config + ServiceList (canonical)
internal/cache/              — single-flight TTL cache, background refresh
internal/gatus/              — fetch + parse gatus badge SVGs
internal/render/             — HTTP handler, html/template wrapper
web/template.html            — single template file, all UI in one
```

## Conventions

- **Idiomatic Go:** gofmt-clean, no unused vars/imports, accept interfaces
  return structs where it helps testing
- **Errors:** return `error`; wrap with `fmt.Errorf("...%w", err)`. Background
  goroutines log and continue; request handlers return 500 on real errors
- **Logging:** `log/slog` text handler to stderr; structured keys via
  `slog.Int`, `slog.String`, etc.
- **Concurrency:** `sync.Mutex` for the cache map, `sync.Mutex` for the
  in-flight single-flight, `context.Context` everywhere I/O happens
- **No globals, no `init()` side effects** — all wiring through `main()` →
  constructors

## Health parsing rules

- Health: SVG body must contain `#40cc11` (up) or `#e05d44` (down). Anything
  else → unknown → render as healthy
- Uptime: regex `>\s*([\d.]+)%\s*<` extracts the percentage from the badge's
  `<text>` element. No match → unknown → omit uptime pill for that section

These are tied to gatus's default badge SVGs. If gatus changes its colours or
text format, `internal/gatus/client.go` needs updating.

## Adding services

Edit `internal/config/config.go` `ServiceList`. Each entry is a `Service`
struct. Sections are rendered in the order they first appear in the list;
services within a section follow list order.

## Running tests

```bash
go test ./...
```

Tests use `httptest` for mocking gatus responses and `t.TempDir()` for
template fixtures.
