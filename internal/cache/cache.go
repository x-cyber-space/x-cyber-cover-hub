// Package cache stores resolved artwork in a local SQLite database.
//
// A cached row is an optimisation, never an authority: every hit is re-matched
// against the caller's query before it is served, so a row that no longer
// describes the requested album can never be returned just because it happens
// to sit under the same key.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/matching"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// Store defines cache storage operations.
type Store interface {
	Get(cacheKey string) (*model.Cover, error)
	Set(cacheKey string, cover *model.Cover) error
	// PruneExpired deletes rows past the configured TTL and reports how many
	// were removed. Reads already ignore expired rows, so this only reclaims
	// disk.
	PruneExpired() (int64, error)
	Close() error
}

// GenerateCacheKey hashes a cover's identity: artist, album and requested size.
//
// Size is part of the identity because both sources serve any resolution from
// one source image, so a 300px request and a 1200px request are different
// artifacts worth caching separately — not the same row at different fidelity.
func GenerateCacheKey(artistName, albumName string, size int) string {
	raw := matching.IdentityKey(artistName, albumName, size)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}

// Matches reports whether a cached cover still satisfies the request that
// produced its key.
//
// Both the warm and the cold path run this predicate, so a cache hit cannot
// answer a query a fresh lookup would have rejected.
func Matches(q model.Query, cover *model.Cover) bool {
	if cover == nil {
		return false
	}
	if matching.ClampSize(q.Size) != matching.ClampSize(cover.Size) {
		return false
	}
	return matching.MatchCover(q, cover.ArtistName, cover.AlbumName) != matching.MatchNone
}

// clampTTL normalizes a TTL: anything non-positive means "never expire".
func clampTTL(ttl time.Duration) time.Duration {
	if ttl < 0 {
		return 0
	}
	return ttl
}
