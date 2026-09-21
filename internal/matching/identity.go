package matching

import (
	"fmt"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// DefaultSize is the edge length used when a request does not specify one.
//
// 600 is what the consuming player embeds into an audio file's ID3/FLAC
// artwork frame, so it is the size that must always be available.
const DefaultSize = 600

// SizeBounds are the limits accepted for a requested edge length. The lower
// bound keeps a caller from filling the cache with unreadable thumbnails; the
// upper bound matches what the providers will actually serve.
const (
	MinSize = 64
	MaxSize = 3000
)

// IdentityKey identifies one cached cover.
//
// The key is (artist, album, size) and deliberately NOT the track: every track
// on a record shares the same artwork, so keying per track is what makes a
// twelve-track album fetch and store the same image twelve times.
//
// Size participates because both providers serve any resolution from one
// source image, so different sizes are genuinely different cache entries rather
// than a reason to refetch.
func IdentityKey(artistName, albumName string, size int) string {
	return fmt.Sprintf("%s\x00%s\x00%d",
		NormalizeIdentity(artistName), NormalizeIdentity(albumName), ClampSize(size))
}

// ClampSize normalizes a requested edge length into the accepted range.
func ClampSize(size int) int {
	if size <= 0 {
		return DefaultSize
	}
	if size < MinSize {
		return MinSize
	}
	if size > MaxSize {
		return MaxSize
	}
	return size
}

// Query returns the identity fields of a cover as a query, for re-validation
// of a cached row.
func QueryOf(c *model.Cover, size int) model.Query {
	return model.Query{ArtistName: c.ArtistName, AlbumName: c.AlbumName, Size: size}
}
