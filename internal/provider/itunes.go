package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// ITunesProvider reads artwork from the public iTunes Search API.
//
// It is the primary source rather than one of the Chinese platforms, because it
// is free, needs no key, does not hotlink-protect its images, and — the useful
// part — encodes the resolution in the artwork URL, so one lookup can serve any
// requested size without a second search.
type ITunesProvider struct{}

// NewITunesProvider returns the iTunes artwork source.
func NewITunesProvider() *ITunesProvider { return &ITunesProvider{} }

func (p *ITunesProvider) Name() string { return "itunes" }

// itunesArtworkSize rewrites the "100x100bb.jpg" segment of an artwork URL so
// the same image can be requested at any edge length, e.g. 600x600bb.jpg.
var itunesArtworkSize = regexp.MustCompile(`\d+x\d+bb\.(jpg|png|jpeg)`)

type iTunesSearchResp struct {
	ResultCount int `json:"resultCount"`
	Results     []struct {
		CollectionID   int64  `json:"collectionId"`
		ArtistName     string `json:"artistName"`
		CollectionName string `json:"collectionName"`
		TrackName      string `json:"trackName"`
		ArtworkURL100  string `json:"artworkUrl100"`
	} `json:"results"`
}

// Search queries albums when the request knows the album name, and tracks
// otherwise: the album endpoint is a much more direct match for an album
// lookup, while with only a track name the album is unknown, so the track has
// to be found first and its artwork used.
func (p *ITunesProvider) Search(ctx context.Context, q model.Query, limit int) ([]*model.Candidate, error) {
	if limit <= 0 {
		limit = 5
	}

	entity := "album"
	term := joinNonEmpty(q.ArtistName, q.AlbumName)
	if strings.TrimSpace(q.AlbumName) == "" {
		entity = "song"
		term = joinNonEmpty(q.ArtistName, q.TrackName)
	}
	if term == "" {
		return nil, nil
	}

	rawURL := fmt.Sprintf("https://itunes.apple.com/search?media=music&entity=%s&limit=%d&term=%s",
		entity, limit, url.QueryEscape(term))

	var resp iTunesSearchResp
	if err := getJSON(ctx, rawURL, "", &resp); err != nil {
		return nil, err
	}

	candidates := make([]*model.Candidate, 0, len(resp.Results))
	for _, r := range resp.Results {
		if strings.TrimSpace(r.ArtworkURL100) == "" {
			continue
		}
		candidates = append(candidates, &model.Candidate{
			Source:     p.Name(),
			AlbumID:    fmt.Sprint(r.CollectionID),
			TrackName:  r.TrackName,
			ArtistName: r.ArtistName,
			AlbumName:  r.CollectionName,
			ArtworkURL: r.ArtworkURL100,
		})
	}
	return candidates, nil
}

// FetchCover downloads the artwork, resizing the request rather than the image:
// the catalogue serves every resolution from the same source.
func (p *ITunesProvider) FetchCover(ctx context.Context, c *model.Candidate, size int) (*model.Cover, error) {
	artURL := resizeITunesArtwork(c.ArtworkURL, size)
	if artURL == "" {
		return nil, ErrNoArtwork
	}

	data, contentType, err := downloadImage(ctx, artURL, "")
	if err != nil {
		return nil, err
	}
	return &model.Cover{
		ArtistName:  c.ArtistName,
		AlbumName:   c.AlbumName,
		Source:      p.Name(),
		ArtworkURL:  artURL,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	}, nil
}

// resizeITunesArtwork rewrites the size segment of an artwork URL. A URL in an
// unexpected shape is returned unchanged rather than mangled.
func resizeITunesArtwork(rawURL string, size int) string {
	if !itunesArtworkSize.MatchString(rawURL) {
		return rawURL
	}
	return itunesArtworkSize.ReplaceAllString(rawURL, fmt.Sprintf("%dx%dbb.$1", size, size))
}

// joinNonEmpty joins the parts that carry content, preserving their order.
func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
