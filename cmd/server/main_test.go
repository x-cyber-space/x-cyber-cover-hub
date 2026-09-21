package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestBannerArtMatchesREADME guards the invariant that the startup wordmark and
// the one shown in README.md are the same artwork.
//
// They drifted apart once: the README carried a hand-squeezed "COVER HUB" while
// bannerArt ended the product word with a second capital C and read as "CC".
// Editing the art in one place and forgetting the other is the easy mistake, so
// it is worth a failing test rather than a comment nobody reads.
func TestBannerArtMatchesREADME(t *testing.T) {
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}

	// The banner is the first fenced block in the README.
	block := regexp.MustCompile("(?s)```\n(.*?)```").FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatal("README.md has no fenced code block to compare against")
	}

	readme := normalizeArt(strings.Split(block[1], "\n"))
	if len(readme) == 0 {
		t.Fatal("the first fenced block in README.md is empty")
	}
	art := normalizeArt(strings.Split(bannerArt, "\n"))

	if len(art) != len(readme) {
		t.Fatalf("banner has %d lines, README has %d", len(art), len(readme))
	}
	for i := range art {
		if art[i] != readme[i] {
			t.Errorf("line %d differs\n  banner: %q\n  README: %q", i+1, art[i], readme[i])
		}
	}
}

// normalizeArt drops blank lines, trailing spaces and the caption lines that
// only the README block carries.
func normalizeArt(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if line == "" || strings.Contains(line, "::") {
			continue
		}
		out = append(out, line)
	}
	return out
}
