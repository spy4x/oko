# Build stage — pinned for reproducibility
FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache deps separately from source for faster rebuilds.
COPY go.mod ./
RUN go mod download

COPY . .

# Static, stripped, fully static binary. CGO disabled for portability.
RUN CGO_ENABLED=0 GOOS=linux go build \
  -trimpath \
  -ldflags="-s -w" \
  -o /out/oko \
  ./cmd/oko

# Runtime stage — distroless for minimal attack surface (~10 MB).
# `static` variant: no shell, no wget/curl. Healthcheck probes the listening
# socket via the binary's own `-healthcheck` flag.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/oko /usr/local/bin/oko
COPY --from=build /src/web /app/web

WORKDIR /app

ENV OKO_PORT=8080
EXPOSE 8080

# Self-probe: binary connects to its own listening socket.
# JSON-array form (no shell) is required because distroless-static
# has no shell. exit 0 = alive, exit 1 = dead.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/oko", "-healthcheck"]

USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/oko"]
