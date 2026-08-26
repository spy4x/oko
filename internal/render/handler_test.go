package render

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spy4x/oko/internal/cache"
	"github.com/spy4x/oko/internal/config"
	"github.com/spy4x/oko/internal/gatus"
)

// minimalTemplate exercises every template directive used in production.
const minimalTemplate = `<!DOCTYPE html>
<html>
<head><title>{{ .Title }}</title></head>
<body>
<h1>{{ .Title }}</h1>
<p class="sub">{{ .Subtitle }}</p>
{{ range .Sections }}
<section data-section="{{ .Name }}">
  <h2>{{ .Name }}</h2>
  <span class="counter">{{ .Counter }}</span>
  <span class="uptime">{{ .Uptime }}</span>
  <ul>
  {{ range .Services }}
    <li class="service{{ if .Down }} service--down{{ end }}" data-endpoint="{{ .Endpoint }}">
      <a href="{{ .URL | safeURL }}">{{ .Icon }} {{ .Name }}</a>
      <p>{{ .Description }}</p>
      <span class="uptime">{{ .Uptime }}</span>
    </li>
  {{ end }}
  </ul>
</section>
{{ end }}
<footer>{{ .Title }} · {{ .Domain }}</footer>
</body>
</html>`

func writeTemplate(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "template.html")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeConfig writes a catalog file and returns its path + a *config.Config
// pointing at it.
func writeConfig(t *testing.T, configJSON string) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	if configJSON != "" {
		if err := os.WriteFile(p, []byte(configJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Construct Config directly with the test file path; we don't need
	// env-driven fields like UptimeHosts for handler tests since the
	// gatus client is only constructed in main.go (not exercised here).
	cfg := &config.Config{
		Port:        "8080",
		Domain:      "example.com",
		CacheTTL:    time.Hour,
		UptimeHosts: []string{"uptime-cloud.example.com"},
	}
	cfg.SetConfigPath(p)
	cfg.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
	return cfg, p
}

// newHandlerForTest builds a handler. statusFn returns the gatus map for
// the requested keys.
func newHandlerForTest(t *testing.T, configJSON string, statusFn func(keys []string) map[string]gatus.Status) (http.Handler, *config.Config) {
	t.Helper()
	templatePath := writeTemplate(t, minimalTemplate)
	cfg, _ := writeConfig(t, configJSON)

	c := cache.New(time.Hour, func(_ context.Context, keys []string) (map[string]gatus.Status, error) {
		if statusFn == nil {
			return map[string]gatus.Status{}, nil
		}
		return statusFn(keys), nil
	})
	t.Cleanup(c.Stop)

	h, err := NewHandler(c, cfg, templatePath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, cfg
}

func TestBuildPage_HealthyAndDown(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Home", "services": [
	      { "name": "Audiobooks", "url": "https://books.${DOMAIN}",
	        "endpoint": "home_audiobookshelf", "gatus_host": "uptime-cloud" },
	      { "name": "Auth",      "url": "https://auth.${DOMAIN}",
	        "endpoint": "home_authelia",       "gatus_host": "uptime-cloud" }
	    ]}
	  ]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, func(keys []string) map[string]gatus.Status {
		healthy := true
		down := false
		return map[string]gatus.Status{
			"uptime-cloud|home_audiobookshelf": {Healthy: &healthy, Uptime: ptrF(99.9)},
			"uptime-cloud|home_authelia":       {Healthy: &down, Uptime: ptrF(50.0)},
		}
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-endpoint="uptime-cloud|home_audiobookshelf"`) {
		t.Error("expected audiobookshelf service in output")
	}
	if !strings.Contains(body, "service--down") {
		t.Error("expected service--down class for down service")
	}
	if !strings.Contains(body, "99.90%") {
		t.Error("expected formatted uptime 99.90%")
	}
}

func TestBuildPage_UnknownIsHealthy(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Home", "services": [
	      { "name": "Audiobooks", "url": "https://books.${DOMAIN}",
	        "endpoint": "home_audiobookshelf", "gatus_host": "uptime-cloud" }
	    ]}
	  ]
	}`
	// Status map has no entry for the key → unknown → healthy, no red border.
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, `data-endpoint="uptime-cloud|home_audiobookshelf"`) {
		t.Errorf("expected first service in output; body=%s", body)
	}
	if strings.Contains(body, "service--down") {
		t.Error("unknown service should not be flagged down")
	}
}

func TestBuildPage_DomainSubstitution(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Home", "services": [
	      { "name": "Audiobooks", "url": "https://books.${DOMAIN}" }
	    ]}
	  ]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, `href="https://books.example.com"`) {
		t.Errorf("${DOMAIN} not substituted: %s", body)
	}
	if strings.Contains(body, "${DOMAIN}") {
		t.Errorf("unsubstituted placeholder in output: %s", body)
	}
}

func TestBuildPage_TitleFromConfig(t *testing.T) {
	cfgJSON := `{
	  "title": "My Homelab",
	  "subtitle": "All my stuff",
	  "servers": [{ "name": "Home", "services": [{ "name": "A", "url": "u" }] }]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, "<title>My Homelab</title>") {
		t.Errorf("expected custom title in <title>; got %s", body)
	}
	if !strings.Contains(body, "<h1>My Homelab</h1>") {
		t.Errorf("expected custom title in header; got %s", body)
	}
	if !strings.Contains(body, "All my stuff") {
		t.Errorf("expected subtitle; got %s", body)
	}
}

func TestBuildPage_DefaultTitleWhenMissing(t *testing.T) {
	cfgJSON := `{
	  "servers": [{ "name": "Home", "services": [{ "name": "A", "url": "u" }] }]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Service dashboard") {
		t.Errorf("expected default title; got %s", body)
	}
}

func TestBuildPage_SectionCounterAndUptime(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Home", "services": [
	      { "name": "A", "url": "u1", "endpoint": "home_a", "gatus_host": "uptime-cloud" },
	      { "name": "B", "url": "u2", "endpoint": "home_b", "gatus_host": "uptime-cloud" },
	      { "name": "C", "url": "u3", "endpoint": "home_c", "gatus_host": "uptime-cloud" }
	    ]}
	  ]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, func(keys []string) map[string]gatus.Status {
		healthy := true
		down := false
		return map[string]gatus.Status{
			"uptime-cloud|home_a": {Healthy: &healthy, Uptime: ptrF(99.0)},
			"uptime-cloud|home_b": {Healthy: &healthy, Uptime: ptrF(95.0)},
			"uptime-cloud|home_c": {Healthy: &down},
		}
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, "2/3 healthy") {
		t.Errorf("expected counter '2/3 healthy', got body=%s", body)
	}
	if !strings.Contains(body, "99.00% · 30d") {
		t.Errorf("expected max uptime pill, got body=%s", body)
	}
}

func TestBuildPage_HiddenServiceSkipped(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Home", "services": [
	      { "name": "Visible", "url": "u1" },
	      { "name": "Hidden",  "url": "u2", "hidden": true }
	    ]}
	  ]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Visible") {
		t.Error("expected Visible in output")
	}
	if strings.Contains(body, "Hidden") {
		t.Error("expected Hidden to be filtered out")
	}
	if !strings.Contains(body, "1/1 healthy") {
		t.Errorf("expected '1/1 healthy' counter, got %s", body)
	}
}

func TestBuildPage_NoGatusServiceAlwaysHealthy(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Home", "services": [
	      { "name": "Plain", "url": "https://example.com" }
	    ]}
	  ]
	}`
	// No endpoint → no lookup, renders as healthy.
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if strings.Contains(body, "service--down") {
		t.Errorf("service without gatus should not be down; got %s", body)
	}
}

func TestBuildPage_EmptyServerSkipped(t *testing.T) {
	cfgJSON := `{
	  "servers": [
	    { "name": "Empty", "services": [] },
	    { "name": "Home",  "services": [{ "name": "A", "url": "u" }] }
	  ]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if strings.Contains(body, "<h2>Empty</h2>") {
		t.Errorf("empty server should be skipped; got %s", body)
	}
	if !strings.Contains(body, "<h2>Home</h2>") {
		t.Errorf("Home server should be present; got %s", body)
	}
}

func TestBuildPage_BadConfigReturns500(t *testing.T) {
	h, _ := newHandlerForTest(t, `{"not-a-server": "x"}`, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	// config has no servers → all sections skipped → empty render is
	// fine. To force a 500 we'd need a truly malformed JSON. Test that
	// separately below.
	if w.Code != 200 {
		t.Errorf("expected 200 for empty catalog (just no services), got %d", w.Code)
	}
}

func TestBuildPage_MalformedConfigReturns500(t *testing.T) {
	h, cfg := newHandlerForTest(t, "", nil)
	// Corrupt the file on disk after handler init.
	if err := os.WriteFile(cfg.ConfigPath, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 {
		t.Errorf("expected 500 on malformed JSON, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildPage_HealthzEndpoint(t *testing.T) {
	h, _ := newHandlerForTest(t, "", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 {
		t.Errorf("status=%d", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body=%q", w.Body.String())
	}
}

func TestBuildPage_OnlyExactRootMatches(t *testing.T) {
	h, _ := newHandlerForTest(t, "", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/something", nil))
	// /something should NOT hit the root handler.
	if w.Code == 200 && strings.Contains(w.Header().Get("Content-Type"), "html") {
		t.Errorf("/something should not render the dashboard; got status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildPage_RefreshBypass(t *testing.T) {
	cfgJSON := `{
	  "servers": [{ "name": "Home", "services": [{ "name": "A", "url": "u",
	    "endpoint": "home_a", "gatus_host": "uptime-cloud" }] }]
	}`
	h, _ := newHandlerForTest(t, cfgJSON, func(_ []string) map[string]gatus.Status {
		healthy := true
		return map[string]gatus.Status{"uptime-cloud|home_a": {Healthy: &healthy}}
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?refresh=1", nil))
	if w.Code != 200 {
		t.Errorf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func ptrF(f float64) *float64 { return &f }
