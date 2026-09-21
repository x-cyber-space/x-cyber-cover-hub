// Package config turns command-line flags into the runtime configuration.
package config

import (
	"flag"
	"fmt"
	"time"
)

// DefaultCacheTTL is how long a fetched cover is served before it is refetched.
//
// Album artwork changes far less often than the lyrics of a song, but it does
// change: editions are reissued, covers are replaced, and a wrong match deserves
// to be corrected eventually rather than served forever.
const DefaultCacheTTL = 90 * 24 * time.Hour

// Config represents runtime configuration.
type Config struct {
	Port      int
	CachePath string
	CacheTTL  time.Duration
	LogLevel  string
	// DefaultSize is the artwork edge length used when a request omits ?size.
	DefaultSize int
}

// ParseFlags parses command line arguments into Config.
func ParseFlags() (*Config, error) {
	cfg := &Config{}

	flag.IntVar(&cfg.Port, "port", 3400, "Server listening port")
	flag.StringVar(&cfg.CachePath, "cache", "./data/covers.db", "SQLite cache file path")
	flag.DurationVar(&cfg.CacheTTL, "cache-ttl", DefaultCacheTTL,
		"How long a cached cover stays fresh, e.g. 2160h; 0 keeps cached covers forever")
	flag.StringVar(&cfg.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.IntVar(&cfg.DefaultSize, "size", 600, "Default artwork edge length in pixels when a request omits ?size")
	flag.Parse()

	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("port %d is out of range (1-65535)", cfg.Port)
	}
	if cfg.CachePath == "" {
		return nil, fmt.Errorf("cache path must not be empty")
	}
	if cfg.CacheTTL < 0 {
		return nil, fmt.Errorf("cache ttl %s must not be negative", cfg.CacheTTL)
	}
	if cfg.DefaultSize <= 0 {
		return nil, fmt.Errorf("default size %d must be positive", cfg.DefaultSize)
	}

	return cfg, nil
}
