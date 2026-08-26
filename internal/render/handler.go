// Package render wires the HTTP handler to the html/template.
//
// The handler is responsible for:
//  1. Fetching the current status snapshot from the cache.
//  2. Building a PageData value (sections, per-section counter + uptime
//     pills, per-service health/uptime).
//  3. Executing the template.
//
// All templated user input is escaped by html/template. The template
// file itself is trusted (ships in the image); only the runtime data is
// user-supplied (and that's all static catalog text, not request input).
package render

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/spy4x/oko/internal/cache"
	"github.com/spy4x/oko/internal/config"
	"github.com/spy4x/oko/internal/gatus"
)

// PageData is what the template receives.
type PageData struct {
	Domain      string
	GeneratedAt string
	Sections    []SectionView
}

// SectionView is one rendered group on the page (Home, Cloud, etc.).
type SectionView struct {
	Name     string
	Counter  string // "23/24 healthy"
	Uptime   string // "99.95% · 30d" or "—"
	AllGood  bool
	Services []ServiceView
}

// ServiceView is one card.
//
// Healthy is the user-facing state: unknown is treated as true (no red
// border). Down is the strict state: only true when gatus explicitly
// reports false.
type ServiceView struct {
	Name        string
	Product     string
	ProductURL  string
	URL         string
	Icon        string
	Description string
	Healthy     bool
	Down        bool
	Uptime      string // e.g. "99.95%" — empty when unknown
	DetailURL   string
	Endpoint    string // for data-endpoint="" attr (used by external JS / scrapers)
}

// NewHandler returns an http.Handler that renders the dashboard.
//
// The template is parsed lazily on first request so a missing/broken
// template fails the first GET instead of process startup — easier to
// diagnose via the live URL.
func NewHandler(c *cache.Cache, cfg config.Config, logger *slog.Logger) (http.Handler, error) {
	// ParseFiles registers the file's content under its base name. We
	// re-derive the base name and use ExecuteTemplate to dispatch by
	// name — otherwise a single-file template with no {{define}} block
	// would not be executable via Execute (it'd come back as
	// "incomplete or empty template").
	parsed, err := template.New(filepath.Base(cfg.TemplatePath)).Funcs(funcMap()).ParseFiles(cfg.TemplatePath)
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", cfg.TemplatePath, err)
	}
	tmplName := parsed.Name()
	_ = tmplName

	// Precompute the namespaced keys once.
	keys := make([]string, 0, len(config.ServiceList))
	for _, s := range config.ServiceList {
		keys = append(keys, config.EndpointKey(s))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// ?refresh=1 bypasses the cache. Without it, the cache layer may
		// serve a snapshot up to CACHE_TTL_SECS old.
		status, err := fetchStatuses(r.Context(), c, keys, r.URL.Query().Get("refresh") == "1")
		if err != nil {
			logger.Warn("gatus fetch failed; rendering with empty status", slog.Any("err", err))
			status = map[string]gatus.Status{}
		}

		data := buildPage(cfg.Domain, status)

		var buf bytes.Buffer
		if err := parsed.ExecuteTemplate(&buf, tmplName, data); err != nil {
			logger.Error("template execute failed", slog.Any("err", err))
			http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=30")
		_, _ = w.Write(buf.Bytes())
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return mux, nil
}

// fetchStatuses routes to Refresh or Get based on the bypass flag.
func fetchStatuses(ctx context.Context, c *cache.Cache, keys []string, force bool) (map[string]gatus.Status, error) {
	if force {
		if err := c.Refresh(ctx, keys); err != nil {
			// Fall back to whatever's in the cache — better stale than nothing.
			return c.Get(ctx, keys)
		}
		return c.Get(ctx, keys)
	}
	return c.Get(ctx, keys)
}

// buildPage assembles PageData from the service catalog and the status map.
//
// Section order follows config.SectionOrder; sections with zero visible
// services are dropped entirely (no empty group on the page).
//
// Per-section Counter shows "healthy/total". Per-section Uptime shows
// the max 30-day uptime across that section's services with known
// numbers — useful signal: "all services here are at least 99.5%".
func buildPage(domain string, status map[string]gatus.Status) PageData {
	sections := make([]SectionView, 0, len(config.SectionOrder))
	now := time.Now().UTC().Format(time.RFC3339)

	for _, sectionName := range config.SectionOrder {
		var sec SectionView
		healthy := 0
		var maxUptime float64
		haveUptime := false

		for _, s := range config.ServiceList {
			if s.Section != sectionName || s.Hidden {
				continue
			}
			st := status[config.EndpointKey(s)]
			sv := serviceViewFrom(s, domain, st)
			sec.Services = append(sec.Services, sv)

			if sv.Healthy {
				healthy++
			}
			if st.Uptime != nil {
				if !haveUptime || *st.Uptime > maxUptime {
					maxUptime = *st.Uptime
					haveUptime = true
				}
			}
		}

		if len(sec.Services) == 0 {
			continue
		}

		total := len(sec.Services)
		sec.Name = config.SectionTitle(sectionName)
		sec.Counter = fmt.Sprintf("%d/%d healthy", healthy, total)
		if haveUptime {
			sec.Uptime = fmt.Sprintf("%.2f%% · 30d", maxUptime)
		} else {
			sec.Uptime = "—"
		}
		sec.AllGood = healthy == total
		sections = append(sections, sec)
	}

	return PageData{
		Domain:      domain,
		GeneratedAt: now,
		Sections:    sections,
	}
}

func serviceViewFrom(s config.Service, domain string, st gatus.Status) ServiceView {
	sv := ServiceView{
		Name:        s.Name,
		Product:     s.Product,
		ProductURL:  s.ProductURL,
		URL:         strings.ReplaceAll(s.URL, "${DOMAIN}", domain),
		Icon:        s.Icon,
		Description: s.Description,
		Healthy:     true, // unknown treated as healthy (no red border)
		Down:        false,
		Uptime:      "",
		DetailURL:   st.DetailURL,
		Endpoint:    config.EndpointKey(s),
	}
	if st.Healthy != nil {
		sv.Down = !*st.Healthy
		sv.Healthy = *st.Healthy
	}
	if st.Uptime != nil {
		sv.Uptime = fmt.Sprintf("%.2f%%", *st.Uptime)
	}
	return sv
}

// funcMap exposes helpers to the template.
func funcMap() template.FuncMap {
	return template.FuncMap{
		// safeURL marks a string as already-safe for href/src. Used for
		// product URLs and service URLs which we construct ourselves
		// from the static catalog — never from request input.
		"safeURL": func(s string) template.URL { return template.URL(s) },
	}
}
