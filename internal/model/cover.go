// Package model holds the shared shapes: the incoming query, a provider
// candidate, and a resolved cover.
package model

// Query is a cover lookup request.
//
// Artist and Album are the identity: an album cover belongs to an album, not to
// a track, so twelve tracks of one record share a single cover. TrackName is
// only used to build a search keyword when the caller cannot supply an album.
type Query struct {
	ArtistName string
	AlbumName  string
	TrackName  string
	// Size is the requested edge length in pixels. Zero means the default.
	Size int
}

// Candidate is one album/artwork match offered by a provider.
type Candidate struct {
	Source     string
	AlbumID    string
	TrackName  string
	ArtistName string
	AlbumName  string
	// ArtworkURL is the provider's original artwork URL. Both providers expose
	// a way to request a different resolution from it (iTunes encodes the size
	// in the path, NetEase accepts a ?param= query), so the URL is kept rather
	// than fetched immediately.
	ArtworkURL string
	Score      float64
}

// Cover is a resolved artwork image plus where it came from.
type Cover struct {
	ArtistName  string
	AlbumName   string
	Source      string
	ArtworkURL  string
	ContentType string
	Size        int
	Data        []byte
}

// ETag is a strong validator derived from the image bytes, so a client's
// If-None-Match can be answered without re-sending the body.
func (c *Cover) ETag() string {
	return etagOf(c.Data)
}
