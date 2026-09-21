// Command server runs the x-cyber-cover-hub album artwork proxy.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/api"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/cache"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/config"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/logging"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/provider"
)

// version is the single source of truth for the reported version. Override it
// at build time rather than editing this file:
//
//	go build -ldflags "-X main.version=$(git describe --tags --always)" ./cmd/server
var version = "1.0.0"

// bannerArt is the wordmark: every word gets one oversized initial and the
// lowercase remainder as literal text, so "Cyber" is a capital C plus "y b e r"
// and "Cover" is a capital C plus "o v e r". Spelling the product word with
// capital glyphs instead would both break that convention and push the artwork
// past 80 columns.
//
// Judge the gaps by the top row, not the baseline: the next word's capital is
// already visible up there, so the space that follows a lowercase word reads as
// a hole even though it looks fine lower down. That is why "o v e r" sits one
// column from the H.
//
// The hyphen and the capital after it need a full blank column between their
// glyph boxes, the same as in x-cyber-lrc-hub. They are easy to fuse by
// accident because the hyphen's box ends on a different row than the letters.
//
// Keep the styles in README.md and here identical; TestBannerArtMatchesREADME
// enforces that.
const bannerArt = `
__  __   ____ y b e r    ____ o v e r _   _       _     
\ \/ /  / ___|          / ___|       | | | | _   | |__  
 \  /  | |      _____  | |           | |_| || |  | '_ \ 
 /  \  | |___  |_____| | |___        |  _  || |_ | |_) |
/_/\_\  \____|          \____|       |_| |_| \___||_.__/
`

func banner() string {
	return fmt.Sprintf("%s             :: X-Cyber Cover Hub :: [v%s]\n"+
		"             :: Album Artwork Proxy ::\n", bannerArt, version)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "[fatal] %v\n", err)
		os.Exit(1)
	}
}

// run holds the real main so that every failure path can return an error and
// deferred cleanup still executes.
func run() error {
	cfg, err := config.ParseFlags()
	if err != nil {
		return err
	}
	if err := logging.Setup(cfg.LogLevel); err != nil {
		return err
	}

	fmt.Print(banner())

	slog.Info("starting x-cyber-cover-hub",
		"version", version,
		"port", cfg.Port,
		"cache", cfg.CachePath,
		"cacheTTL", cfg.CacheTTL.String(),
		"defaultSize", cfg.DefaultSize,
		"logLevel", cfg.LogLevel,
	)

	store, err := cache.NewSQLiteStore(cfg.CachePath, cfg.CacheTTL)
	if err != nil {
		return fmt.Errorf("failed to initialize cache store: %w", err)
	}
	defer store.Close()

	if removed, err := store.PruneExpired(); err != nil {
		slog.Warn("initial cache prune failed", "error", err)
	} else if removed > 0 {
		slog.Info("pruned expired covers", "rows", removed)
	}

	pruneCtx, stopPruner := context.WithCancel(context.Background())
	defer stopPruner()
	go pruneLoop(pruneCtx, store)

	dispatcher := provider.NewDispatcher()
	slog.Info("artwork providers initialized",
		"providers", []string{"itunes", "netease"},
		"note", "iTunes is primary; NetEase covers the Chinese catalogue")

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: api.NewRouter(store, dispatcher, cfg.DefaultSize, version),
		// Image bodies are large compared to JSON, so the write budget is
		// larger than the sibling service needs.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("HTTP server listening", "addr", fmt.Sprintf("http://0.0.0.0:%d", cfg.Port))
		slog.Info("endpoint available", "endpoint", "/api/cover")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serveErr <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case sig := <-stopChan:
		slog.Info("shutting down gracefully", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
	}

	slog.Info("server exiting, goodbye")
	return nil
}

// pruneLoop reclaims disk from expired rows. Reads already treat an expired row
// as a miss, so this is housekeeping rather than correctness.
func pruneLoop(ctx context.Context, store *cache.SQLiteStore) {
	ttl := store.TTL()
	if ttl <= 0 {
		slog.Info("cache expiry disabled; cached covers are kept indefinitely")
		return
	}

	interval := ttl / 4
	if interval > time.Hour {
		interval = time.Hour
	}
	if interval < time.Minute {
		interval = time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			removed, err := store.PruneExpired()
			if err != nil {
				slog.Error("cache prune failed", "error", err)
				continue
			}
			if removed > 0 {
				slog.Info("pruned expired covers", "rows", removed)
			}
		}
	}
}
