// Package render wires the HTTP handler to the html/template.
//
// The handler is responsible for:
//  1. Reading the current catalog (file-config, mtime-cached).
//  2. Fetching the current gatus status snapshot (cache-backed).
//  3. Building a PageData value (sections, per-section counter + uptime
//     pills, per-service health/uptime).
//  4. Executing the template.
//
// All templated user input is escaped by html/template. The template
// file itself is trusted (ships in the image); only the catalog data is
// user-supplied, and it's loaded from a file the operator controls — not
// from request input.
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

// Defaults applied to PageData when the catalog has no explicit values.
const (
	DefaultTitle    = "Service dashboard"
	DefaultSubtitle = "Single page for every self-hosted service — search, jump, check status"
)

// PageData is what the template receives.
type PageData struct {
	Title       string
	Subtitle    string
	Domain      string
	GeneratedAt string
	Sections    []SectionView
}

// SectionView is one rendered group on the page (Home, Cloud, ...).
type SectionView struct {
	Name        string
	Counter     string // "23/24 healthy"
	Uptime      string // "99.95% · 30d" or "—"
	UptimeClass string // CSS modifier for the section pill: "good"/"ok"/"warn"/"bad"/""
	AllGood     bool
	Services    []ServiceView
}

// ServiceView is one card. Healthy is the user-facing state: unknown is
// treated as true (no red border). Down is the strict state: only true
// when gatus explicitly reports false.
type ServiceView struct {
	Name        string
	Product     string
	ProductURL  string
	URL         string
	Icon        string
	Description string
	Healthy     bool
	Down        bool
	Uptime      string // formatted "99.95%" or ""
	UptimeClass string // CSS modifier for the pill: "good"/"ok"/"warn"/"bad"/""
	DetailURL   string // gatus per-endpoint detail page (set when fetch ok)
	Endpoint    string
}

// NewHandler returns an http.Handler that renders the dashboard.
//
// cfg.TemplatePath is the template file (separate from cfg.ConfigPath,
// which is the catalog). Both default to /app/web/template.html and
// /app/config.json respectively.
//
// The template is parsed lazily on first request so a missing/broken
// template fails the first GET instead of process startup — easier to
// diagnose via the live URL.
func NewHandler(c *cache.Cache, cfg *config.Config, templatePath string, logger *slog.Logger) (http.Handler, error) {
	if templatePath == "" {
		return nil, fmt.Errorf("template path is empty")
	}
	parsed, err := template.New(filepath.Base(templatePath)).Funcs(funcMap()).ParseFiles(templatePath)
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", templatePath, err)
	}
	tmplName := parsed.Name()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		file, err := cfg.File()
		if err != nil {
			logger.Error("config load failed", slog.Any("err", err))
			http.Error(w, "config load error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		status, err := fetchStatuses(r.Context(), c, file.EndpointKeys(), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			logger.Warn("gatus fetch failed; rendering with empty status", slog.Any("err", err))
			status = map[string]gatus.Status{}
		}

		data := buildPage(cfg.Domain, file, status)

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

// buildPage assembles PageData from the catalog and the status map.
//
// Sections are rendered in the order they appear in the catalog (which
// matches the JSON array order — operators control it directly).
//
// Per-section Counter shows "healthy/total". Per-section Uptime shows
// the max 30-day uptime across that section's services with known
// numbers — useful signal: "all services here are at least 99.5%".
func buildPage(domain string, file config.FileConfig, status map[string]gatus.Status) PageData {
	title := file.Title
	if title == "" {
		title = DefaultTitle
	}
	subtitle := file.Subtitle
	if subtitle == "" {
		subtitle = DefaultSubtitle
	}

	sections := make([]SectionView, 0, len(file.Servers))
	now := time.Now().UTC().Format(time.RFC3339)

	for _, server := range file.Servers {
		var sec SectionView
		healthy := 0
		var maxUptime float64
		haveUptime := false

		for _, svc := range server.Services {
			if svc.Hidden {
				continue
			}
			var st gatus.Status
			if k := svc.Key(); k != "" {
				st = status[k]
			}
			sv := serviceViewFrom(svc, domain, st)
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
		sec.Name = server.Name
		sec.Counter = fmt.Sprintf("%d/%d healthy", healthy, total)
		sec.AllGood = healthy == total
		if haveUptime {
			sec.Uptime = fmt.Sprintf("%.2f%% · 30d", maxUptime)
		} else {
			sec.Uptime = "—"
		}
		// Colour the section pill only when the section is fully healthy
		// — otherwise the green pill next to a red "N/M healthy" counter
		// reads as a contradiction. Uptime is the MAX across the section,
		// so a single 100%-uptime service can mask a down neighbour.
		if haveUptime && sec.AllGood {
			sec.UptimeClass = uptimeClass(&maxUptime)
		}
		sections = append(sections, sec)
	}

	return PageData{
		Title:       title,
		Subtitle:    subtitle,
		Domain:      domain,
		GeneratedAt: now,
		Sections:    sections,
	}
}

func serviceViewFrom(s config.Service, domain string, st gatus.Status) ServiceView {
	url := s.URL
	if strings.Contains(url, "${DOMAIN}") {
		url = strings.ReplaceAll(url, "${DOMAIN}", domain)
	}
	sv := ServiceView{
		Name:        s.Name,
		Product:     s.Product,
		ProductURL:  s.ProductURL,
		URL:         url,
		Icon:        s.Icon,
		Description: s.Description,
		Healthy:     true, // unknown treated as healthy (no red border)
		Down:        false,
		Uptime:      "",
		DetailURL:   st.DetailURL,
		Endpoint:    s.Key(),
	}
	if st.Healthy != nil {
		sv.Down = !*st.Healthy
		sv.Healthy = *st.Healthy
	}
	if st.Uptime != nil {
		sv.Uptime = fmt.Sprintf("%.2f%%", *st.Uptime)
		sv.UptimeClass = uptimeClass(st.Uptime)
	}
	return sv
}

// uptimeClass maps a 0..100 uptime percentage to a CSS modifier.
//
// Thresholds follow common SLA tiers: ≥99.9% "four nines" is the high
// bar, ≥99.0% "three nines" is the everyday floor, ≥95.0% is degraded
// but still serving, below is effectively broken. The class names are
// the suffixes used in the template's `<span class="pill--X">` and
// `<a class="service-uptime--X">` so they stay in sync with CSS.
//
// nil → "" — caller decides how to render the absence (we just don't
// emit a class).
func uptimeClass(pct *float64) string {
	if pct == nil {
		return ""
	}
	switch {
	case *pct >= 99.9:
		return "good"
	case *pct >= 99.0:
		return "fair" // ≥99% but below four-nines — "fair" avoids the
		// "amber = warning" reading that an `ok` label would trigger
		// while the colour is amber.
	case *pct >= 95.0:
		return "warn"
	default:
		return "bad"
	}
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
