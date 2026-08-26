// Package config loads runtime config (env vars + a JSON file).
//
// Runtime settings come from env vars. The service catalog — servers,
// their services, gatus lookup keys — comes from a JSON file. The file
// is re-read on each request with mtime caching, so editing the file
// is enough; no container restart needed.
//
// Schema (JSON, see config.example.json):
//
//	{
//	  "title":    "...",
//	  "subtitle": "...",
//	  "servers": [
//	    {
//	      "name": "Home",
//	      "services": [
//	        {
//	          "name":        "Audiobooks",
//	          "url":         "https://books.${DOMAIN}",
//	          "icon":        "📚",
//	          "description": "Audiobook and podcast library with streaming",
//	          "product":     "Audiobookshelf",
//	          "product_url": "https://www.audiobookshelf.org/",
//	          "endpoint":    "home_audiobookshelf",
//	          "gatus_host":  "uptime-cloud",
//	          "hidden":      false
//	        }
//	      ]
//	    }
//	  ]
//	}
//
// Required: server.name, service.name, service.url.
// Optional: everything else. A service with no `endpoint`+`gatus_host`
// always renders as healthy (no gatus lookup).
package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileConfig is the JSON schema the catalog file uses.
type FileConfig struct {
	Title    string   `json:"title,omitempty"`
	Subtitle string   `json:"subtitle,omitempty"`
	Servers  []Server `json:"servers"`
}

// Server is one UI group on the page (Home, Cloud, ...). The array order
// is the render order.
type Server struct {
	Name     string    `json:"name"`
	Services []Service `json:"services"`
}

// Service is one card on the page.
//
// URL is rendered as-is; if it contains "${DOMAIN}", that token is
// replaced with cfg.Domain at render time. Other env-var interpolation
// is NOT supported — keep the URL literal.
//
// Endpoint is the gatus endpoint key. GatusHost is the short name of
// the gatus instance that exposes that key (must match a host in
// Config.UptimeHosts). If either is empty, no gatus lookup happens
// for this service — it always renders as healthy.
type Service struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Icon        string `json:"icon,omitempty"`
	Description string `json:"description,omitempty"`
	Product     string `json:"product,omitempty"`
	ProductURL  string `json:"product_url,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	GatusHost   string `json:"gatus_host,omitempty"`
	Hidden      bool   `json:"hidden,omitempty"`
}

// HasGatus reports whether this service participates in gatus lookups.
func (s Service) HasGatus() bool { return s.Endpoint != "" && s.GatusHost != "" }

// Key returns the namespaced lookup key for the gatus cache map:
// "host|endpoint". Empty when no gatus is configured for this service.
func (s Service) Key() string {
	if !s.HasGatus() {
		return ""
	}
	return s.GatusHost + "|" + s.Endpoint
}

// Config bundles runtime env config + cached file content.
type Config struct {
	Port          string
	Domain        string
	UptimeHosts   []string
	UptimeTimeout time.Duration
	CacheTTL      time.Duration
	ConfigPath    string // JSON file path (env: CONFIG_PATH)

	// Cached file content (loaded on first request; refreshed on mtime change).
	mu        sync.RWMutex
	file      FileConfig
	fileMtime time.Time
	filePath  string
	logger    *slog.Logger
}

// Load reads env vars. Does NOT read the file — call File() per request.
// Returns an error if a required var is missing.
func Load() (Config, error) {
	port := envOr("PORT", "8080")

	domain := os.Getenv("DOMAIN")
	if domain == "" {
		return Config{}, fmt.Errorf("DOMAIN is required")
	}

	hostsRaw := os.Getenv("UPTIME_HOSTS")
	if hostsRaw == "" {
		return Config{}, fmt.Errorf("UPTIME_HOSTS is required")
	}
	var hosts []string
	for _, h := range splitAndTrim(hostsRaw, ",") {
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return Config{}, fmt.Errorf("UPTIME_HOSTS produced no entries")
	}

	return Config{
		Port:          port,
		Domain:        domain,
		UptimeHosts:   hosts,
		UptimeTimeout: time.Duration(envInt("UPTIME_TIMEOUT_SECS", 5)) * time.Second,
		CacheTTL:      time.Duration(envInt("CACHE_TTL_SECS", 60)) * time.Second,
		ConfigPath:    envOr("CONFIG_PATH", "/app/config.json"),
		filePath:      envOr("CONFIG_PATH", "/app/config.json"),
	}, nil
}

// WithLogger attaches a logger for config-load warnings.
func (c *Config) WithLogger(l *slog.Logger) { c.logger = l }

// SetConfigPath overrides the catalog file path. Used by tests that
// want to point at a temp file without going through env vars.
func (c *Config) SetConfigPath(p string) {
	c.ConfigPath = p
	c.filePath = p
}

// File returns the cached FileConfig, re-reading from disk if mtime changed.
// Errors during re-read are logged and the previous cache is returned.
func (c *Config) File() (FileConfig, error) {
	info, err := os.Stat(c.filePath)
	if err != nil {
		return c.snapshot(), fmt.Errorf("stat config %q: %w", c.filePath, err)
	}

	c.mu.RLock()
	if c.fileMtime.Equal(info.ModTime()) && len(c.file.Servers) > 0 {
		defer c.mu.RUnlock()
		return c.file, nil
	}
	c.mu.RUnlock()

	data, err := os.ReadFile(c.filePath)
	if err != nil {
		return c.snapshot(), fmt.Errorf("read config %q: %w", c.filePath, err)
	}
	var fc FileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return c.snapshot(), fmt.Errorf("parse config %q: %w", c.filePath, err)
	}
	if err := validate(fc); err != nil {
		return c.snapshot(), fmt.Errorf("invalid config %q: %w", c.filePath, err)
	}

	c.mu.Lock()
	c.file = fc
	c.fileMtime = info.ModTime()
	c.mu.Unlock()

	if c.logger != nil {
		c.logger.Info("config reloaded",
			slog.String("path", c.filePath),
			slog.Int("servers", len(fc.Servers)),
			slog.Int("services", totalServices(fc)),
		)
	}
	return fc, nil
}

func (c *Config) snapshot() FileConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.file
}

// TotalServices is the count of services across all servers (excludes
// hidden services from the rendered-page count, but includes them here
// for parity with the gatus lookup set).
func (f FileConfig) TotalServices() int { return totalServices(f) }

// EndpointKeys returns the namespaced gatus lookup keys for every
// service that has both `endpoint` and `gatus_host` set. Hidden services
// are included — the user wants the gatus fetch to stay on even when
// phasing out a card.
func (f FileConfig) EndpointKeys() []string {
	var keys []string
	for _, s := range f.Servers {
		for _, svc := range s.Services {
			if k := svc.Key(); k != "" {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// Validate returns an error if the config is missing required fields
// or has duplicate server/service names. Called from File() and from tests.
func Validate(f FileConfig) error { return validate(f) }

func validate(f FileConfig) error {
	seenServers := map[string]bool{}
	for i, s := range f.Servers {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("servers[%d]: name is required", i)
		}
		if seenServers[s.Name] {
			return fmt.Errorf("servers[%d]: duplicate server name %q", i, s.Name)
		}
		seenServers[s.Name] = true

		if len(s.Services) == 0 {
			continue
		}
		seenServices := map[string]bool{}
		for j, svc := range s.Services {
			if strings.TrimSpace(svc.Name) == "" {
				return fmt.Errorf("servers[%d].services[%d]: name is required", i, j)
			}
			if seenServices[svc.Name] {
				return fmt.Errorf("servers[%d].services[%d]: duplicate service name %q", i, j, svc.Name)
			}
			seenServices[svc.Name] = true

			if strings.TrimSpace(svc.URL) == "" {
				return fmt.Errorf("servers[%d].services[%d] (%s): url is required", i, j, svc.Name)
			}
			// Half-configured gatus is likely a mistake — surface it.
			if (svc.Endpoint == "") != (svc.GatusHost == "") {
				return fmt.Errorf(
					"servers[%d].services[%d] (%s): endpoint and gatus_host must both be set or both empty",
					i, j, svc.Name,
				)
			}
		}
	}
	return nil
}

func totalServices(f FileConfig) int {
	n := 0
	for _, s := range f.Servers {
		n += len(s.Services)
	}
	return n
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
