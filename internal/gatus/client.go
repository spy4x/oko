// Package gatus fetches gatus badge SVGs and parses out healthy/uptime.
//
// Gatus's badge API surface (twinproduction/gatus:latest):
//
//	GET /api/v1/endpoints/{key}/health/badge.svg   — SVG with #40cc11 (up) or #e05d44 (down)
//	GET /api/v1/endpoints/{key}/uptimes/30d/badge.svg — SVG with "99.99%" text inside <text>
//	GET /endpoints/{key}                            — HTML detail page (used for the "open on down" link)
//
// There's no JSON listing endpoint on default. We hit each badge per-service
// on cache miss, in parallel.
package gatus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Status is the dashboard's view of one service.
//
// Pointer fields so "no data" is distinguishable from "down" — see the
// "unknown ≠ down" invariant in AGENTS.md.
type Status struct {
	Healthy   *bool    `json:"healthy,omitempty"`
	Uptime    *float64 `json:"uptime30d,omitempty"` // 0..100, last 30 days
	DetailURL string   `json:"detailUrl,omitempty"`
}

// HTTPClient is the subset of *http.Client we depend on. Lets tests pass
// in an httptest.Server without us importing httptest here.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Client fans out badge fetches across the configured gatus instances.
type Client struct {
	hosts   map[string]string // short name → FQDN, e.g. "uptime-cloud" → "uptime-cloud.example.com"
	http    HTTPClient
	timeout time.Duration
	ua      string
}

// NewClient builds a Client from a list of FQDNs. Each FQDN must match a
// Service.GatusHost (the leading subdomain of the FQDN is taken as the
// short name — e.g. "uptime-cloud.example.com" → short "uptime-cloud").
//
// hostTimeout is the per-fetch timeout — applies to both health and
// uptime badge requests via context.WithTimeout.
func NewClient(fqdns []string, hostTimeout time.Duration) *Client {
	hosts := make(map[string]string, len(fqdns))
	for _, fqdn := range fqdns {
		short := fqdn
		if i := strings.Index(fqdn, "."); i >= 0 {
			short = fqdn[:i]
		}
		hosts[short] = fqdn
	}
	return &Client{
		hosts:   hosts,
		http:    &http.Client{Timeout: hostTimeout},
		timeout: hostTimeout,
		ua:      "oko/1.0",
	}
}

// SetHTTPClient swaps the HTTP client. Tests use this to inject a
// httptest.Server-backed transport.
func (c *Client) SetHTTPClient(h HTTPClient) { c.http = h }

var (
	healthGreen = "#40cc11"
	healthRed   = "#e05d44"
	uptimeRe    = regexp.MustCompile(`>\s*([\d.]+)%\s*<`)
)

// FetchAll fans out one health + one uptime fetch per endpoint, in parallel.
//
// keys is a slice of namespaced keys in the form "host|endpoint"
// (see config.EndpointKey). FetchAll resolves host → FQDN and fetches
// both badges in parallel. Per-endpoint failures leave that key absent
// from the result map; callers should treat absent == unknown.
//
// The whole fan-out shares one parent context — pass a request-scoped
// context for cancellation.
func (c *Client) FetchAll(ctx context.Context, keys []string) (map[string]Status, error) {
	if len(keys) == 0 {
		return map[string]Status{}, nil
	}
	work := make([]string, 0, len(keys))
	for _, k := range keys {
		// Validate format early; skip unknowns.
		host, endpoint, ok := splitKey(k)
		if !ok {
			continue
		}
		if _, known := c.hosts[host]; !known {
			continue
		}
		_ = endpoint
		work = append(work, k)
	}
	if len(work) == 0 {
		return map[string]Status{}, nil
	}

	type result struct {
		key string
		st  Status
		// present distinguishes "fetched, no data" from "fetch failed".
		// Either way the key is omitted from the result map (unknown),
		// but present lets us surface counts in logs.
		present bool
	}
	out := make(chan result, len(work))
	var wg sync.WaitGroup
	for _, k := range work {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			host, endpoint, _ := splitKey(key)
			st, ok := c.fetchOne(ctx, host, endpoint)
			out <- result{key: key, st: st, present: ok}
		}(k)
	}
	wg.Wait()
	close(out)

	m := make(map[string]Status, len(work))
	for r := range out {
		if r.present {
			m[r.key] = r.st
		}
	}
	return m, nil
}

// fetchOne returns status and ok=true if at least one of the two badge
// fetches succeeded. Both failing → ok=false (key omitted).
func (c *Client) fetchOne(ctx context.Context, host, endpoint string) (Status, bool) {
	fqdn := c.hosts[host]
	if fqdn == "" {
		return Status{}, false
	}

	type badge struct {
		healthy *bool
		uptime  *float64
	}
	var (
		wg       sync.WaitGroup
		health   *bool
		uptime   *float64
		healthOK bool
		uptimeOK bool
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		b, err := c.fetchBadge(ctx, fqdn, endpoint, "health")
		if err != nil {
			return
		}
		health, err = parseHealth(b)
		if err == nil {
			healthOK = true
		}
	}()
	go func() {
		defer wg.Done()
		b, err := c.fetchBadge(ctx, fqdn, endpoint, "uptimes/30d")
		if err != nil {
			return
		}
		u, err := parseUptime(b)
		if err == nil {
			uptime = u
			uptimeOK = true
		}
	}()
	wg.Wait()

	if !healthOK && !uptimeOK {
		return Status{}, false
	}
	return Status{
		Healthy:   health,
		Uptime:    uptime,
		DetailURL: fmt.Sprintf("https://%s/endpoints/%s", fqdn, endpoint),
	}, true
}

// fetchBadge gets one badge SVG. kind is "health" or "uptimes/30d".
func (c *Client) fetchBadge(ctx context.Context, host, endpoint, kind string) (string, error) {
	url := fmt.Sprintf("https://%s/api/v1/endpoints/%s/%s/badge.svg", host, endpoint, kind)
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "image/svg+xml")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s -> %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parseHealth reads the badge SVG body and returns a *bool for healthy.
// nil pointer = unknown (couldn't decide from colour). We never return
// (false, nil) for an empty body; callers must check err.
func parseHealth(body string) (*bool, error) {
	switch {
	case strings.Contains(body, healthGreen):
		t := true
		return &t, nil
	case strings.Contains(body, healthRed):
		f := false
		return &f, nil
	}
	return nil, fmt.Errorf("unknown badge fill")
}

// parseUptime reads the badge SVG body and returns the percentage.
// Whitespace may sit between the text content and the closing </text>.
func parseUptime(body string) (*float64, error) {
	m := uptimeRe.FindStringSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("no %% in badge")
	}
	var pct float64
	if _, err := fmt.Sscanf(m[1], "%f", &pct); err != nil {
		return nil, err
	}
	return &pct, nil
}

// splitKey parses "host|endpoint" → ("host", "endpoint", true).
// Anything malformed → ("", "", false).
func splitKey(k string) (string, string, bool) {
	i := strings.Index(k, "|")
	if i <= 0 || i >= len(k)-1 {
		return "", "", false
	}
	return k[:i], k[i+1:], true
}
