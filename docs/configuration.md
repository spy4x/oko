# Configuration

oko reads two things: a JSON catalog of your services, and a few environment
variables. The quick start in the [README](../README.md#quick-start) shows the
minimum.

## `config.json` — the catalog

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

## Environment

| Var                 | Default      | Description                                          |
| ------------------- | ------------ | ---------------------------------------------------- |
| `DOMAIN`            | *(required)* | Base domain (substituted into `${DOMAIN}`)           |
| `UPTIME_HOSTS`      | *(required)* | Comma-separated gatus FQDNs                          |
| `PORT`              | `8080`       | Listen port                                          |
| `UPTIME_TIMEOUT_SECS` | `5`        | Per-fetch timeout                                    |
| `CACHE_TTL_SECS`    | `60`         | In-memory cache TTL                                  |
| `TEMPLATE_PATH`     | `/app/web/template.html` | Override template path                       |
| `CONFIG_PATH`       | `/app/config.json`       | Override catalog path                        |

## Health parsing

For each service with `endpoint` + `gatus_host` set, Oko fans out two
GETs in parallel:

- `https://<gatus>/api/v1/endpoints/<endpoint>/health/badge.svg` —
  value text `up` or `down`; if the text is missing, the fill colour
  (`#40cc11` up, `#c7130a` or `#e05d44` down). `?` or anything else →
  unknown.
- `https://<gatus>/api/v1/endpoints/<endpoint>/uptimes/30d/badge.svg`
  — 30-day uptime percentage parsed from the `<text>` element via the
  regex `>\s*([\d.]+)%\s*<`. No match → unknown → omit the uptime
  pill.

A failed fetch leaves the service as **unknown**: no red border, a
dashed "unknown" marker, and not counted as healthy. Unknown is not
down; it usually means gatus itself or the network is broken. The
background refresh retries unknown services every `CACHE_TTL_SECS`.

These rules mirror gatus's default badge SVGs. If gatus changes its
colours or text format, `internal/gatus/client.go` needs updating.
