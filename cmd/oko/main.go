// Command oko is a server-rendered dashboard for self-hosted services.
//
// Each HTTP request reads a JSON catalog (mtime-cached, no restart
// needed to pick up edits) and, for services with gatus configured,
// fans out gatus badge SVGs in parallel (behind a 60s in-memory cache).
// The full HTML page is rendered server-side.
//
// Two operating modes:
//
//	oko              — run the HTTP server
//	oko -healthcheck — probe the running server (used by Docker HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spy4x/oko/internal/cache"
	"github.com/spy4x/oko/internal/config"
	"github.com/spy4x/oko/internal/gatus"
	"github.com/spy4x/oko/internal/render"
)

const healthcheckTimeout = 2 * time.Second

func main() {
	healthcheck := flag.Bool("healthcheck", false, "Probe the running server (for Docker HEALTHCHECK). Exit 0 if reachable, 1 if not.")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if *healthcheck {
		os.Exit(runHealthcheck(logger))
	}

	if err := run(logger); err != nil {
		logger.Error("oko failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func runHealthcheck(logger *slog.Logger) int {
	port := os.Getenv("OKO_PORT")
	if port == "" {
		port = "8080"
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, healthcheckTimeout)
	if err != nil {
		logger.Warn("healthcheck failed", slog.String("addr", "127.0.0.1:"+port), slog.Any("err", err))
		return 1
	}
	_ = conn.Close()
	return 0
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.WithLogger(logger)

	// Validate the catalog eagerly at startup so a bad config file
	// surfaces in logs instead of waiting for the first GET.
	if fc, ferr := cfg.File(); ferr != nil {
		logger.Warn("initial config load failed (will retry on first request)", slog.Any("err", ferr))
		_ = fc
	}

	gc := gatus.NewClient(cfg.UptimeHosts, cfg.UptimeTimeout)
	statusCache := cache.New(cfg.CacheTTL, gc.FetchAll)
	defer statusCache.Stop()

	templatePath := os.Getenv("TEMPLATE_PATH")
	if templatePath == "" {
		templatePath = "/app/web/template.html"
	}
	handler, err := render.NewHandler(statusCache, &cfg, templatePath, logger)
	if err != nil {
		return fmt.Errorf("init handler: %w", err)
	}

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		// Render is bounded by the cache fetch (UPTIME_TIMEOUT_SECS × 2
		// endpoints per service, parallel). 15s leaves headroom for cold
		// cache + slow gatus without holding stale connections.
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("oko listening",
			slog.String("port", cfg.Port),
			slog.String("domain", cfg.Domain),
			slog.Any("uptime_hosts", cfg.UptimeHosts),
			slog.Duration("cache_ttl", cfg.CacheTTL),
			slog.String("config", cfg.ConfigPath),
			slog.String("template", templatePath),
		)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown failed", slog.Any("err", err))
	}
	return nil
}
