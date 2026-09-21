package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/cache"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/matching"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/provider"
)

// clientCacheMaxAge is the hint given to clients. The server refetches on its
// own TTL regardless, so this only saves a round trip.
const clientCacheMaxAge = 604800 // 7 days

// CoverHandler handles GET /api/cover.
//
//	GET /api/cover?artist_name=X&album_name=Y[&track_name=Z][&size=600][&format=json]
//
// The response is the image itself by default. Album artwork has to pass
// through this service rather than being handed over as a URL: both sources
// referer-check their image hosts, so a client following the upstream URL
// directly usually gets a 403.
//
// ?format=json returns the same information as metadata instead of bytes, for
// clients that would rather fetch the image themselves.
func CoverHandler(store cache.Store, dispatcher *provider.Dispatcher, defaultSize int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := r.URL.Query()

		q := model.Query{
			ArtistName: strings.TrimSpace(params.Get("artist_name")),
			AlbumName:  strings.TrimSpace(params.Get("album_name")),
			TrackName:  strings.TrimSpace(params.Get("track_name")),
		}

		size, err := parseSize(params.Get("size"), defaultSize)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, BadRequestError(err.Error()))
			return
		}
		q.Size = size

		if q.ArtistName == "" {
			writeJSON(w, http.StatusBadRequest, BadRequestError("artist_name is required"))
			return
		}
		if q.AlbumName == "" && q.TrackName == "" {
			writeJSON(w, http.StatusBadRequest,
				BadRequestError("album_name is required (track_name alone cannot identify an album)"))
			return
		}

		asJSON := strings.EqualFold(strings.TrimSpace(params.Get("format")), "json")

		// 1. Warm path — only when the album is known.
		//
		// Without an album name there is nothing to key on: every album by an
		// artist would collapse onto one row. Those requests always take the
		// cold path, and still warm the cache for later album-bearing ones.
		if q.AlbumName != "" {
			key := cache.GenerateCacheKey(q.ArtistName, q.AlbumName, size)
			cached, err := store.Get(key)
			if err != nil {
				slog.Error("cache read failed", "key", key, "error", err)
			}
			if cache.Matches(q, cached) {
				slog.Debug("cache hit",
					"artist", cached.ArtistName, "album", cached.AlbumName,
					"size", cached.Size, "bytes", len(cached.Data))
				respond(w, r, cached, asJSON)
				return
			}
		}

		// 2. Cold path.
		slog.Debug("resolving artwork",
			"artist", q.ArtistName, "album", q.AlbumName, "track", q.TrackName, "size", size)

		cover, err := dispatcher.Resolve(r.Context(), q)
		if err != nil {
			slog.Error("artwork lookup failed", "artist", q.ArtistName, "album", q.AlbumName, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, UpstreamError())
			return
		}
		if cover == nil {
			slog.Info("no confident artwork match", "artist", q.ArtistName, "album", q.AlbumName)
			writeJSON(w, http.StatusNotFound, CoverNotFoundError())
			return
		}

		// Store under the *resolved* identity, which may differ from what was
		// asked for when the request carried only a track name.
		key := cache.GenerateCacheKey(cover.ArtistName, cover.AlbumName, cover.Size)
		if err := store.Set(key, cover); err != nil {
			slog.Error("cache write failed", "error", err)
		}

		slog.Info("artwork resolved",
			"artist", cover.ArtistName, "album", cover.AlbumName,
			"source", cover.Source, "size", cover.Size, "bytes", len(cover.Data))

		respond(w, r, cover, asJSON)
	}
}

// parseSize reads and validates the ?size parameter.
func parseSize(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return matching.ClampSize(fallback), nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("size %q is not a number", raw)
	}
	if n < matching.MinSize || n > matching.MaxSize {
		return 0, fmt.Errorf("size %d is out of range (%d-%d)", n, matching.MinSize, matching.MaxSize)
	}
	return n, nil
}

// respond writes either the image or its metadata.
func respond(w http.ResponseWriter, r *http.Request, cover *model.Cover, asJSON bool) {
	etag := cover.ETag()

	// The same validator is served for both representations, so a client that
	// switched from the image to the JSON form can still revalidate.
	headers := map[string]string{
		"ETag":          etag,
		"Cache-Control": "public, max-age=" + strconv.Itoa(clientCacheMaxAge),
		// Header values must be ASCII, and artist/album names are frequently
		// not, so these are percent-encoded. Use ?format=json for the plain
		// names.
		"X-Cover-Source": cover.Source,
		"X-Cover-Artist": url.QueryEscape(cover.ArtistName),
		"X-Cover-Album":  url.QueryEscape(cover.AlbumName),
	}

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if asJSON {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		writeJSON(w, http.StatusOK, coverMetadata{
			ArtistName:  cover.ArtistName,
			AlbumName:   cover.AlbumName,
			Source:      cover.Source,
			ContentType: cover.ContentType,
			Size:        cover.Size,
			ByteSize:    len(cover.Data),
			ETag:        etag,
			ArtworkURL:  cover.ArtworkURL,
			ImageURL:    localImageURL(cover),
		})
		return
	}

	writeBytes(w, http.StatusOK, cover.ContentType, cover.Data, headers)
}

type coverMetadata struct {
	ArtistName  string `json:"artistName"`
	AlbumName   string `json:"albumName"`
	Source      string `json:"source"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	ByteSize    int    `json:"byteSize"`
	ETag        string `json:"etag"`
	// ArtworkURL is the upstream URL the image came from; ImageURL points back
	// at this service, which is the one a client can actually fetch.
	ArtworkURL string `json:"artworkUrl"`
	ImageURL   string `json:"imageUrl"`
}

// localImageURL builds the path a client can call to get these bytes.
func localImageURL(cover *model.Cover) string {
	v := url.Values{}
	v.Set("artist_name", cover.ArtistName)
	v.Set("album_name", cover.AlbumName)
	v.Set("size", strconv.Itoa(cover.Size))
	return "/api/cover?" + v.Encode()
}
