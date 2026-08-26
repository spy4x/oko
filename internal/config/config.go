// Package config loads environment variables and exposes the canonical
// ServiceList — every card rendered on the dashboard.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Service is one card on the dashboard.
//
// URL is a template string with the literal "${DOMAIN}" placeholder,
// substituted at render time. Same convention as the previous static
// nginx dash so existing links translate unchanged.
type Service struct {
	Name        string // user-facing label, e.g. "AI Chat"
	Product     string // product name shown in footer link, e.g. "Open WebUI"
	ProductURL  string // product homepage
	Endpoint    string // gatus endpoint key
	GatusHost   string // "uptime-cloud" or "uptime-home" — namespaced at fetch time
	URL         string // service URL, ${DOMAIN} substituted
	Section     string // "home" | "cloud" | "offsite" | "portable"
	Icon        string // emoji
	Description string
	Hidden      bool // skip render but keep gatus fetch on (phasing out)
}

// SectionOrder is the rendered order. Sections not present in any service
// are skipped automatically.
var SectionOrder = []string{"home", "cloud", "offsite", "portable"}

// SectionTitle is the human-readable label shown in the UI.
func SectionTitle(s string) string {
	switch s {
	case "home":
		return "Home"
	case "cloud":
		return "Cloud"
	case "offsite":
		return "Offsite"
	case "portable":
		return "Portable"
	}
	return s
}

// ServiceList is the canonical list. Single source of truth — keep it in
// sync with the gatus endpoint registry on uptime-cloud / uptime-home.
//
// Section is where the card is rendered visually. GatusHost tells which
// gatus server exposes the endpoint (cross-server monitoring).
// Endpoint may include characters that need URL-encoding (e.g. "cloud_mail-(https)")
//
// Hidden = true removes the card but keeps the gatus fetch on (useful
// when phasing out a service — uptime stays tracked while the card is gone).
var ServiceList = []Service{
	// --- Home (visual section) — services running on the home server,
	// monitored by uptime-cloud (cloud gatus), unless noted otherwise.
	{
		Name:        "Audiobooks",
		Product:     "Audiobookshelf",
		ProductURL:  "https://www.audiobookshelf.org/",
		Endpoint:    "home_audiobookshelf",
		GatusHost:   "uptime-cloud",
		URL:         "https://books.${DOMAIN}",
		Section:     "home",
		Icon:        "📚",
		Description: "Audiobook and podcast library with streaming",
	},
	{
		Name:        "Auth",
		Product:     "Authelia",
		ProductURL:  "https://www.authelia.com/",
		Endpoint:    "home_authelia",
		GatusHost:   "uptime-cloud",
		URL:         "https://auth.${DOMAIN}",
		Section:     "home",
		Icon:        "🔐",
		Description: "SSO and 2FA front-door for protected services",
	},
	{
		Name:        "CI/CD",
		Product:     "Woodpecker",
		ProductURL:  "https://woodpecker-ci.org/",
		Endpoint:    "home_woodpecker-ci",
		GatusHost:   "uptime-cloud",
		URL:         "https://ci.${DOMAIN}",
		Section:     "home",
		Icon:        "🔧",
		Description: "Pipeline-based CI/CD for Docker stacks",
	},
	{
		Name:        "OpenCode Web",
		Product:     "OpenCode",
		ProductURL:  "https://opencode.ai/",
		Endpoint:    "home_opencode-web",
		GatusHost:   "uptime-cloud",
		URL:         "https://code.${DOMAIN}",
		Section:     "home",
		Icon:        "💻",
		Description: "AI coding assistant with a browser terminal UI",
	},
	{
		Name:        "Files",
		Product:     "FileBrowser",
		ProductURL:  "https://filebrowser.org/",
		Endpoint:    "home_filebrowser",
		GatusHost:   "uptime-cloud",
		URL:         "https://files.${DOMAIN}",
		Section:     "home",
		Icon:        "📁",
		Description: "Web UI to browse, upload, and edit files",
	},
	{
		Name:        "Git",
		Product:     "Gitea",
		ProductURL:  "https://about.gitea.com/",
		Endpoint:    "home_gitea",
		GatusHost:   "uptime-cloud",
		URL:         "https://git.${DOMAIN}",
		Section:     "home",
		Icon:        "📦",
		Description: "Git hosting with issues, PRs, and packages",
	},
	{
		Name:        "Movies",
		Product:     "Jellyfin",
		ProductURL:  "https://jellyfin.org/",
		Endpoint:    "home_jellyfin",
		GatusHost:   "uptime-cloud",
		URL:         "https://movies.${DOMAIN}",
		Section:     "home",
		Icon:        "🎬",
		Description: "Stream movies, TV shows, and music",
	},
	{
		Name:        "Notifications",
		Product:     "ntfy",
		ProductURL:  "https://ntfy.sh/",
		Endpoint:    "home_ntfy",
		GatusHost:   "uptime-cloud",
		URL:         "https://ntfy-home.${DOMAIN}",
		Section:     "home",
		Icon:        "🔔",
		Description: "Pub/sub push notifications to phone, desktop, or scripts",
	},
	{
		Name:        "AI Chat",
		Product:     "Open WebUI",
		ProductURL:  "https://github.com/open-webui/open-webui",
		Endpoint:    "home_open-webui",
		GatusHost:   "uptime-cloud",
		URL:         "https://ai.${DOMAIN}",
		Section:     "home",
		Icon:        "🤖",
		Description: "Chat with local LLMs and OpenAI-compatible APIs",
	},
	{
		// Vaultwarden runs on the cloud server, but the user-facing link
		// belongs in the Home section because it's how the user reaches
		// passwords from their everyday entry point.
		Name:        "Passwords",
		Product:     "Vaultwarden",
		ProductURL:  "https://github.com/dani-garcia/vaultwarden",
		Endpoint:    "cloud_vaultwarden",
		GatusHost:   "uptime-home",
		URL:         "https://passwords.${DOMAIN}",
		Section:     "home",
		Icon:        "🔑",
		Description: "Bitwarden-compatible password and secrets manager",
	},
	{
		Name:        "Photos",
		Product:     "Immich",
		ProductURL:  "https://immich.app/",
		Endpoint:    "home_immich",
		GatusHost:   "uptime-cloud",
		URL:         "https://photos.${DOMAIN}",
		Section:     "home",
		Icon:        "📷",
		Description: "Google Photos replacement with mobile backups",
	},
	{
		Name:        "Registry",
		Product:     "Docker Registry",
		ProductURL:  "https://github.com/distribution/distribution",
		Endpoint:    "home_docker-registry",
		GatusHost:   "uptime-cloud",
		URL:         "https://registry.${DOMAIN}",
		Section:     "home",
		Icon:        "🐳",
		Description: "Private Docker image registry",
	},
	{
		Name:        "Search",
		Product:     "SearXNG",
		ProductURL:  "https://searxng.org/",
		Endpoint:    "home_searxng",
		GatusHost:   "uptime-cloud",
		URL:         "https://search.${DOMAIN}",
		Section:     "home",
		Icon:        "🔍",
		Description: "Privacy-respecting meta-search across many engines",
	},
	{
		Name:        "Speed",
		Product:     "LibreSpeed",
		ProductURL:  "https://librespeed.org/",
		Endpoint:    "home_librespeed",
		GatusHost:   "uptime-cloud",
		URL:         "https://speed-home.${DOMAIN}",
		Section:     "home",
		Icon:        "🚀",
		Description: "Network speed test for bandwidth and latency",
	},
	{
		Name:        "Sync",
		Product:     "Syncthing",
		ProductURL:  "https://syncthing.net/",
		Endpoint:    "home_syncthing",
		GatusHost:   "uptime-cloud",
		URL:         "https://sync-home.${DOMAIN}",
		Section:     "home",
		Icon:        "🔄",
		Description: "Peer-to-peer file sync between devices and servers",
	},
	{
		Name:        "Talk",
		Product:     "Mirotalk",
		ProductURL:  "https://mirotalk.org/",
		Endpoint:    "home_mirotalk",
		GatusHost:   "uptime-cloud",
		URL:         "https://talk.${DOMAIN}",
		Section:     "home",
		Icon:        "📹",
		Description: "Browser-based WebRTC video calls and conferencing",
	},
	{
		Name:        "Time Tracker",
		Product:     "Traggo",
		ProductURL:  "https://traggo.net/",
		Endpoint:    "home_traggo",
		GatusHost:   "uptime-cloud",
		URL:         "https://time.${DOMAIN}",
		Section:     "home",
		Icon:        "⏰",
		Description: "Tag-based time tracking with reports and charts",
	},
	{
		Name:        "Tools",
		Product:     "Omni Tools",
		ProductURL:  "https://omnitools.app/",
		Endpoint:    "home_omni-tools",
		GatusHost:   "uptime-cloud",
		URL:         "https://tools.${DOMAIN}",
		Section:     "home",
		Icon:        "🛠️",
		Description: "Web toolbox for developers — converters, generators, formatters",
	},
	{
		Name:        "Torrents",
		Product:     "Transmission",
		ProductURL:  "https://transmissionbt.com/",
		Endpoint:    "home_transmission",
		GatusHost:   "uptime-cloud",
		URL:         "https://torrents.${DOMAIN}",
		Section:     "home",
		Icon:        "⬇️",
		Description: "BitTorrent client with a lightweight web UI",
	},
	{
		Name:        "Traefik",
		Product:     "Traefik",
		ProductURL:  "https://traefik.io/",
		Endpoint:    "home_traefik",
		GatusHost:   "uptime-cloud",
		URL:         "https://proxy-home.${DOMAIN}",
		Section:     "home",
		Icon:        "🚦",
		Description: "Reverse proxy and TLS termination dashboard",
	},
	{
		Name:        "Uptime",
		Product:     "Gatus",
		ProductURL:  "https://gatus.io/",
		Endpoint:    "home_gatus",
		GatusHost:   "uptime-cloud",
		URL:         "https://uptime-home.${DOMAIN}",
		Section:     "home",
		Icon:        "⏱️",
		Description: "Health and status monitoring for every service",
	},
	{
		Name:        "YouTube",
		Product:     "Piped",
		ProductURL:  "https://github.com/TeamPiped/Piped",
		Endpoint:    "home_piped",
		GatusHost:   "uptime-cloud",
		URL:         "https://piped.${DOMAIN}",
		Section:     "home",
		Icon:        "📺",
		Description: "Privacy-friendly YouTube frontend, no ads or tracking",
	},
	{
		Name:        "YouTube DL",
		Product:     "MeTube",
		ProductURL:  "https://github.com/alexta69/metube",
		Endpoint:    "home_metube",
		GatusHost:   "uptime-cloud",
		URL:         "https://metube.${DOMAIN}",
		Section:     "home",
		Icon:        "📺",
		Description: "Download videos from YouTube and 1000+ supported sites",
	},

	// --- Cloud (visual section) — services running on the cloud server,
	// monitored by uptime-home (home gatus).
	{
		Name:        "Healthchecks",
		Product:     "Healthchecks",
		ProductURL:  "https://healthchecks.io/",
		Endpoint:    "cloud_healthchecks",
		GatusHost:   "uptime-home",
		URL:         "https://healthchecks.${DOMAIN}",
		Section:     "cloud",
		Icon:        "✅",
		Description: "Cron job and scheduled task monitoring with alerts",
	},
	{
		Name:        "Notifications",
		Product:     "ntfy",
		ProductURL:  "https://ntfy.sh/",
		Endpoint:    "cloud_ntfy",
		GatusHost:   "uptime-home",
		URL:         "https://ntfy-cloud.${DOMAIN}",
		Section:     "cloud",
		Icon:        "🔔",
		Description: "Pub/sub push notifications for the cloud server",
	},
	{
		Name:        "Mail, Calendar & Contacts",
		Product:     "Stalwart",
		ProductURL:  "https://stalw.art/",
		Endpoint:    "cloud_mail-(https)",
		GatusHost:   "uptime-home",
		URL:         "https://mail.${DOMAIN}",
		Section:     "cloud",
		Icon:        "📧",
		Description: "Email server: SMTP, IMAP, JMAP, plus CalDAV / CardDAV calendar and address book sync",
	},
	{
		Name:        "Webmail",
		Product:     "Bulwark",
		ProductURL:  "https://github.com/jordan-wright/bulwark",
		Endpoint:    "cloud_bulwark",
		GatusHost:   "uptime-home",
		URL:         "https://webmail.${DOMAIN}",
		Section:     "cloud",
		Icon:        "📨",
		Description: "Modern JMAP email client with a clean UI",
	},
	{
		Name:        "Speed",
		Product:     "LibreSpeed",
		ProductURL:  "https://librespeed.org/",
		Endpoint:    "cloud_librespeed",
		GatusHost:   "uptime-home",
		URL:         "https://speed-cloud.${DOMAIN}",
		Section:     "cloud",
		Icon:        "🚀",
		Description: "Network speed test from the cloud server",
	},
	{
		Name:        "Sync",
		Product:     "Syncthing",
		ProductURL:  "https://syncthing.net/",
		Endpoint:    "cloud_syncthing",
		GatusHost:   "uptime-home",
		URL:         "https://sync-cloud.${DOMAIN}",
		Section:     "cloud",
		Icon:        "🔄",
		Description: "Peer-to-peer file sync for the cloud server",
	},
	{
		Name:        "Traefik",
		Product:     "Traefik",
		ProductURL:  "https://traefik.io/",
		Endpoint:    "cloud_traefik",
		GatusHost:   "uptime-home",
		URL:         "https://proxy-cloud.${DOMAIN}",
		Section:     "cloud",
		Icon:        "🚦",
		Description: "Reverse proxy and TLS termination dashboard",
	},
	{
		Name:        "Uptime",
		Product:     "Gatus",
		ProductURL:  "https://gatus.io/",
		Endpoint:    "cloud_gatus",
		GatusHost:   "uptime-home",
		URL:         "https://uptime-cloud.${DOMAIN}",
		Section:     "cloud",
		Icon:        "⏱️",
		Description: "Cross-server health and status monitoring",
	},

	// --- Offsite (visual section) — services running on the offsite server.
	{
		Name:        "Speed",
		Product:     "LibreSpeed",
		ProductURL:  "https://librespeed.org/",
		Endpoint:    "offsite_librespeed",
		GatusHost:   "uptime-cloud",
		URL:         "https://speed-offsite.${DOMAIN}",
		Section:     "offsite",
		Icon:        "🚀",
		Description: "Network speed test from the offsite server",
	},
	{
		Name:        "Syncthing",
		Product:     "Syncthing",
		ProductURL:  "https://syncthing.net/",
		Endpoint:    "offsite_syncthing",
		GatusHost:   "uptime-cloud",
		URL:         "https://sync-offsite.${DOMAIN}",
		Section:     "offsite",
		Icon:        "🔄",
		Description: "Offsite peer-to-peer file sync — geographic redundancy",
	},
	{
		Name:        "Traefik",
		Product:     "Traefik",
		ProductURL:  "https://traefik.io/",
		Endpoint:    "offsite_traefik",
		GatusHost:   "uptime-cloud",
		URL:         "https://proxy-offsite.${DOMAIN}",
		Section:     "offsite",
		Icon:        "🚦",
		Description: "Reverse proxy and TLS termination dashboard",
	},

	// --- Portable (visual section) — services running on the mini-PC K11.
	{
		Name:        "Home Assistant",
		Product:     "Home Assistant",
		ProductURL:  "https://www.home-assistant.io/",
		Endpoint:    "home_home-assistant",
		GatusHost:   "uptime-cloud",
		URL:         "https://home.${DOMAIN}",
		Section:     "portable",
		Icon:        "🏠",
		Description: "Smart-home automation hub — lights, sensors, automations",
	},
	{
		Name:        "OpenCode Web",
		Product:     "OpenCode",
		ProductURL:  "https://opencode.ai/",
		Endpoint:    "portable_opencode-web",
		GatusHost:   "uptime-cloud",
		URL:         "https://code2.${DOMAIN}",
		Section:     "portable",
		Icon:        "💻",
		Description: "AI coding assistant — secondary instance on portable",
	},
}

// EndpointKey is the namespaced lookup key: "host|endpoint". Cache and
// gatus client both use this form so a single map covers every endpoint.
func EndpointKey(s Service) string {
	return s.GatusHost + "|" + s.Endpoint
}

// Config is the runtime env config (separate from the static ServiceList).
type Config struct {
	Port          string
	Domain        string
	UptimeHosts   []string // FQDNs (without scheme)
	UptimeTimeout time.Duration
	CacheTTL      time.Duration
	TemplatePath  string
}

// Load reads env vars. Returns an error if a required var is missing.
// UptimeHosts is parsed as a comma-separated list.
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
		TemplatePath:  envOr("TEMPLATE_PATH", "/app/web/template.html"),
	}, nil
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
