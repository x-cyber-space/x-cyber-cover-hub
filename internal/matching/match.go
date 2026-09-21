// Package matching decides whether a provider candidate is the album the
// request asked for.
//
// The identity here is (artist, album) — deliberately not (title, artist,
// duration) as in the sibling lyrics service. An album cover belongs to the
// album, so every track on a record shares it and the track name says nothing
// about which artwork is correct.
package matching

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

var (
	// bracketRegex matches content within the various bracket pairs platforms
	// use for edition markers: "(Deluxe)", "[Remastered]", "（豪华版）", "【超清】".
	bracketRegex = regexp.MustCompile(`(?i)\([^)]*\)|\[[^\]]*\]|（[^）]*）|【[^】]*】|{[^}]*}`)
)

// MatchStage describes how confidently a candidate satisfied a query, in
// increasing order of confidence.
type MatchStage int

const (
	// MatchNone means the candidate is not the requested album.
	MatchNone MatchStage = iota
	// MatchRelaxed means the candidate matched under controlled relaxation:
	// a multi-artist credit, or an album name that merely contains the request.
	MatchRelaxed
	// MatchExact means normalized equality on every field the request supplied.
	MatchExact
)

// AlbumSimilarityFloor is the bigram similarity below which a relaxed album
// comparison is refused. Character-level similarity on short strings is noisy,
// so anything weaker than this is not evidence of the same record.
const AlbumSimilarityFloor = 0.6

// NormalizeIdentity lowercases, strips bracket modifiers and collapses
// whitespace.
//
// Stripping brackets is what makes "叶惠美" match "叶惠美 (Deluxe Edition)" and
// "1989" match "1989 (Taylor's Version)" — the same rule the reference LRCLIB
// service applies to its own fields, and the reason edition markers do not
// cause duplicate cover fetches.
func NormalizeIdentity(s string) string {
	return CollapseSpaces(bracketRegex.ReplaceAllString(strings.ToLower(s), " "))
}

// CollapseSpaces trims and collapses every run of whitespace to one space.
func CollapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// NormalizeForSearch strips bracket modifiers and then everything except
// letters, digits and Han characters, for the fuzzy comparison behind scoring
// and the relaxed stage.
//
// The bracket pass must come first: without it "(Deluxe Edition)" survives as
// the letters "deluxeedition" and drags an otherwise identical album below full
// marks, which is the opposite of what an edition marker means.
func NormalizeForSearch(s string) string {
	stripped := bracketRegex.ReplaceAllString(strings.ToLower(s), " ")
	var b strings.Builder
	for _, r := range stripped {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MatchCover classifies a candidate against a query.
//
// When the request carries no album name there is nothing to identify an album
// by, so the candidate is accepted as MatchRelaxed at best; callers that need a
// confident answer should send album_name.
func MatchCover(q model.Query, artistName, albumName string) MatchStage {
	if matchExact(q, artistName, albumName) {
		return MatchExact
	}
	if matchRelaxed(q, artistName, albumName) {
		return MatchRelaxed
	}
	return MatchNone
}

// matchExact reports whether the candidate equals the request on every
// field the request supplied.
func matchExact(q model.Query, artistName, albumName string) bool {
	wantArtist := NormalizeIdentity(q.ArtistName)
	if wantArtist == "" || wantArtist != NormalizeIdentity(artistName) {
		return false
	}

	if q.AlbumName == "" {
		// An artist-only request cannot confirm the album.
		return false
	}
	if NormalizeIdentity(q.AlbumName) != NormalizeIdentity(albumName) {
		return false
	}
	return true
}

// matchRelaxed reports whether the candidate is acceptable under controlled
// relaxation.
func matchRelaxed(q model.Query, artistName, albumName string) bool {
	if !ArtistsOverlap(q.ArtistName, artistName) {
		return false
	}

	if q.AlbumName == "" {
		// Artist agreement alone is all we have; say so by returning the
		// weaker stage rather than pretending it is a confident match.
		return true
	}

	want, got := NormalizeForSearch(q.AlbumName), NormalizeForSearch(albumName)
	if want == "" || got == "" {
		return false
	}
	if want == got {
		return true
	}
	// A platform may append an edition suffix we did not strip, or the request
	// may carry one the candidate lacks. Require a real containment, not a
	// single shared character.
	shorter, longer := want, got
	if utf8.RuneCountInString(shorter) > utf8.RuneCountInString(longer) {
		shorter, longer = longer, shorter
	}
	if utf8.RuneCountInString(shorter) >= 2 && strings.Contains(longer, shorter) {
		return true
	}
	if BigramJaccard(want, got) >= AlbumSimilarityFloor {
		return true
	}
	return false
}
