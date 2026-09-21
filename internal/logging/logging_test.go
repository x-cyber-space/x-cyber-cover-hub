package logging

import (
	"log/slog"
	"testing"
)

// TestParseLevel is the guard that makes -log-level a real setting: every
// documented value must resolve, and an undocumented one must be rejected
// rather than silently ignored the way the flag used to be.
func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"INFO", slog.LevelInfo, false},
		{"  Info  ", slog.LevelInfo, false},
		{"debug", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"trace", 0, true},
		{"verbose", 0, true},
	}

	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) accepted an unknown level", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSetupRejectsUnknownLevel(t *testing.T) {
	if err := Setup("nope"); err == nil {
		t.Fatal("Setup must reject an unknown level instead of starting anyway")
	}
}
