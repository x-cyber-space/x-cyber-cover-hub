package matching

import (
	"regexp"
	"strings"
)

// artistSeparator matches the joiners streaming platforms use in credits:
// "A / B", "A & B", "A、B", "A feat. B" and friends. Splitting both sides the
// same way means an artist whose name legitimately contains a separator still
// compares equal to itself.
var artistSeparator = regexp.MustCompile(
	`(?i)\s*(?:/|&|、|;|；|,|，|·|•|\bfeat\.?|\bft\.?|\bfeaturing\b|\bwith\b)\s*`)

// SplitArtists splits a credit string into the individual artists it names.
func SplitArtists(s string) []string {
	parts := artistSeparator.Split(strings.ToLower(s), -1)
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		name := CollapseSpaces(bracketRegex.ReplaceAllString(p, " "))
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// ArtistsOverlap reports whether two credit strings share at least one artist.
// This is what lets a request for "周杰伦" match a platform credit of
// "周杰伦 / 费玉清" for the same record.
func ArtistsOverlap(a, b string) bool {
	left := SplitArtists(a)
	if len(left) == 0 {
		return false
	}
	right := make(map[string]struct{})
	for _, name := range SplitArtists(b) {
		right[name] = struct{}{}
	}
	for _, name := range left {
		if _, ok := right[name]; ok {
			return true
		}
	}
	return false
}

// BigramJaccard measures similarity between two strings using bigrams.
func BigramJaccard(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	r1, r2 := []rune(s1), []rune(s2)
	if len(r1) < 2 || len(r2) < 2 {
		return charJaccard(r1, r2)
	}

	set1 := make(map[string]struct{}, len(r1))
	for i := 0; i < len(r1)-1; i++ {
		set1[string(r1[i:i+2])] = struct{}{}
	}
	set2 := make(map[string]struct{}, len(r2))
	for i := 0; i < len(r2)-1; i++ {
		set2[string(r2[i:i+2])] = struct{}{}
	}

	intersection := 0
	for k := range set1 {
		if _, ok := set2[k]; ok {
			intersection++
		}
	}
	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

func charJaccard(r1, r2 []rune) float64 {
	set1 := make(map[rune]struct{}, len(r1))
	for _, r := range r1 {
		set1[r] = struct{}{}
	}
	set2 := make(map[rune]struct{}, len(r2))
	for _, r := range r2 {
		set2[r] = struct{}{}
	}
	intersection := 0
	for r := range set1 {
		if _, ok := set2[r]; ok {
			intersection++
		}
	}
	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}
