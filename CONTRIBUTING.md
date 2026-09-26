# Contributing to oko

## Local development

```bash
go test ./...
DOMAIN=example.com UPTIME_HOSTS=uptime-cloud.example.com \
  CONFIG_PATH=config.example.json TEMPLATE_PATH=web/template.html \
  go run ./cmd/oko
```

Tests use `httptest` for mocking gatus responses and `t.TempDir()`
for catalog fixtures.
