# Self-hosting oko

## Docker run

```bash
# 1. Create a config
mkdir -p oko && cp config.example.json oko/config.json
$EDITOR oko/config.json

# 2. Run with the right env vars
docker run --rm -p 8080:8080 \
  -e DOMAIN=example.com \
  -e UPTIME_HOSTS=uptime-cloud.example.com,uptime-home.example.com \
  -e CONFIG_PATH=/config/config.json \
  -v "$PWD/oko:/config:ro,z" \
  ghcr.io/spy4x/oko:latest
```

Open http://localhost:8080.

For docker-compose or Kubernetes, the same env vars + a mounted
config.json are all you need. Mount the folder, not the file: many
editors save by replacing the file, and a single-file bind mount keeps
showing the old one until the container restarts. See the [rostok
recipe](https://github.com/spy4x/rostok/tree/main/stacks/oko) for an example.

## Docker image

The included `Dockerfile` is a multi-stage build:

- `golang:1.25-alpine` — build stage
- `gcr.io/distroless/static-debian12:nonroot` — runtime (~10 MB)

The image's `HEALTHCHECK` uses the binary's own `-healthcheck` flag
(opens a TCP probe to its own listening socket). distroless-static
has no shell, no wget — using the binary's flag avoids a heavier
base image just for health.

## Reverse proxy

Put Oko behind any reverse proxy. Cache headers are deliberately
short so health updates are timely. No special headers required.

## Healthcheck

```bash
docker exec <container> /usr/local/bin/oko -healthcheck
# exits 0 if the listening socket accepts a TCP connection
```
