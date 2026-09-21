package matching

import (
	"math"
	"strings"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// Scoring weights. The album carries more than the artist, because for artwork
// the album IS the identity: an artist's discography is a dozen different
// covers, and picking the wrong one is the failure mode being ranked against.
//
// Compare the sibling lyrics service, which weights the title highest — a
// different problem with a different answer.
const (
	scoreAlbumExact     = 60.0
	scoreAlbumContained = 45.0
	scoreAlbumUnknown   = 30.0
	scoreArtistExact    = 40.0
	scoreArtistOverlap  = 32.0
	scoreArtistUnknown  = 20.0
)

// ScoreCover ranks candidates that have already passed the MatchCover gate.
//
// It is a ranking function, never an acceptance test: 60 points for a matching
// album can be reached while the artist is entirely wrong, which is exactly why
// the gate exists separately.
func ScoreCover(q model.Query, artistName, albumName string) float64 {
	total := scoreArtist(q.ArtistName, artistName) + scoreAlbum(q.AlbumName, albumName)
	if total > 100 {
		total = 100
	}
	return math.Round(total*100) / 100
}

func scoreArtist(want, got string) float64 {
	wantNorm, gotNorm := NormalizeForSearch(want), NormalizeForSearch(got)
	switch {
	case wantNorm == "":
		// Unknown artist imposes nothing, and grants nothing.
		return scoreArtistUnknown
	case wantNorm == gotNorm:
		return scoreArtistExact
	case ArtistsOverlap(want, got):
		// A shared credit ("周杰伦" against "周杰伦 / 费玉清") is strong but not
		// equality.
		return scoreArtistOverlap
	default:
		return BigramJaccard(wantNorm, gotNorm) * scoreArtistExact
	}
}

func scoreAlbum(want, got string) float64 {
	wantNorm, gotNorm := NormalizeForSearch(want), NormalizeForSearch(got)
	switch {
	case wantNorm == "":
		return scoreAlbumUnknown
	case wantNorm == gotNorm:
		return scoreAlbumExact
	case strings.Contains(gotNorm, wantNorm) || strings.Contains(wantNorm, gotNorm):
		return scoreAlbumContained
	default:
		return BigramJaccard(wantNorm, gotNorm) * scoreAlbumExact
	}
}
