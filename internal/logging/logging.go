// Package logging configures the process-wide structured logger.
//
// The -log-level flag used to be parsed and printed without ever being
// consulted, which made it a documented-but-dead setting. Level selection now
// lives here and the rest of the code logs through log/slog.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// ParseLevel maps the -log-level flag value onto a slog level.
//
// An empty string means the default so that a zero-value config is usable.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q (want debug, info, warn or error)", name)
	}
}

// Setup installs the default logger at the requested level.
func Setup(name string) error {
	level, err := ParseLevel(name)
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})))
	return nil
}
