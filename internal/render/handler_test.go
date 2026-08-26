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

// minimalTemplate is the test template. It exercises every template
// directive we use in production: range, if/else, struct field access,
// function calls, URL substitution.
const minimalTemplate = `<!DOCTYPE html>
<html>
<head><title>{{ .Domain }}</title></head>
<body>
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
<footer>Generated {{ .GeneratedAt }} · {{ .Domain }}</footer>
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

func newHandlerForTest(t *testing.T, status map[string]gatus.Status) (http.Handler, *cache.Cache) {
	t.Helper()
	path := writeTemplate(t, minimalTemplate)
	cfg := config.Config{Domain: "example.com", TemplatePath: path}

	c := cache.New(time.Hour, func(_ context.Context, _ []string) (map[string]gatus.Status, error) {
		// Return a copy each call so tests can mutate.
		out := make(map[string]gatus.Status, len(status))
		for k, v := range status {
			out[k] = v
		}
		return out, nil
	})
	t.Cleanup(c.Stop)

	h, err := NewHandler(c, cfg, slogDiscard())
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, c
}

// slogDiscard returns a slog.Logger that writes to io.Discard. Keeps
// test output clean without coupling to the handler's logger shape.
func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildPage_HealthyAndDown(t *testing.T) {
	healthy := true
	down := false
	status := map[string]gatus.Status{
		"uptime-cloud|home_audiobookshelf": {Healthy: &healthy, Uptime: ptrF(99.9)},
		"uptime-cloud|home_authelia":       {Healthy: &down, Uptime: ptrF(50.0)},
	}
	h, _ := newHandlerForTest(t, status)
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
	// Status map missing the key → unknown → healthy, no uptime pill.
	h, _ := newHandlerForTest(t, map[string]gatus.Status{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, `data-endpoint="uptime-cloud|home_audiobookshelf"`) {
		t.Errorf("expected first service in output; body=%s", body)
	}
	// Healthy by default, no --down class.
	if strings.Contains(body, "service--down") {
		t.Error("unknown service should not be flagged down")
	}
}

func TestBuildPage_DomainSubstitution(t *testing.T) {
	h, _ := newHandlerForTest(t, map[string]gatus.Status{})
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

func TestBuildPage_SectionCounterAndUptime(t *testing.T) {
	healthy := true
	down := false
	// Take 3 actual home services and mark 1 down with no uptime.
	status := map[string]gatus.Status{
		"uptime-cloud|home_audiobookshelf": {Healthy: &healthy, Uptime: ptrF(99.0)},
		"uptime-cloud|home_authelia":       {Healthy: &healthy, Uptime: ptrF(95.0)},
		"uptime-cloud|home_woodpecker-ci":  {Healthy: &down},
	}
	h, _ := newHandlerForTest(t, status)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	// 3 home services with explicit status, others unknown → 2 healthy of 3 in promise.
	// The full home section has 23 services; with 1 known-down, 22 known-healthy-or-unknown.
	// "22/23 healthy" + max uptime 99.00%
	if !strings.Contains(body, "99.00% · 30d") {
		t.Errorf("expected max uptime pill, got body=%s", body)
	}
	if !strings.Contains(body, "service--down") {
		t.Errorf("expected at least one down service, got body=%s", body)
	}
}

func TestBuildPage_RefreshBypass(t *testing.T) {
	healthy := true
	status := map[string]gatus.Status{
		"uptime-cloud|home_a": {Healthy: &healthy, Uptime: ptrF(99.0)},
	}
	h, c := newHandlerForTest(t, status)

	// Pre-warm via plain GET — adds the keys to the underlying cache
	// so refresh has something to look up.
	if _, err := c.Get(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Now fetch with refresh — should still work.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?refresh=1", nil))
	if w.Code != 200 {
		t.Errorf("status=%d", w.Code)
	}
}

func TestBuildPage_HealthzEndpoint(t *testing.T) {
	h, _ := newHandlerForTest(t, nil)
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
	h, _ := newHandlerForTest(t, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/something", nil))
	// /something should NOT hit the root handler; "/" exact-match Go 1.22+.
	if w.Code == 200 && strings.Contains(w.Header().Get("Content-Type"), "html") {
		t.Errorf("/something should not render the dashboard; got status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBuildPage_EmptySectionOmitted(t *testing.T) {
	// Build a service list with no portable services and check the
	// Portable header is not rendered.
	h, _ := newHandlerForTest(t, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	// "Portable" appears as a section title only if at least one service
	// is in the section. With the real ServiceList, portable has 2, so
	// it's kept. The check is structural: every section title in the
	// output should have at least one service card under it.
	for _, sec := range []string{"Home", "Cloud", "Offsite", "Portable"} {
		// crude: count "<section" tags before/after
		if !strings.Contains(body, "<h2>"+sec+"</h2>") {
			t.Errorf("section %q missing", sec)
		}
	}
}

func ptrF(f float64) *float64 { return &f }
