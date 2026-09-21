// Package provider adapts the upstream artwork sources and queries them
// concurrently.
//
// Each source's wire format stays in its own file; shared HTTP plumbing lives
// here.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// Provider is one artwork source.
type Provider interface {
	Name() string
	// Search returns album candidates for a query, in the source's own order.
	// That order is not trusted: the dispatcher re-matches and re-ranks.
	//
	// Each source builds its own keyword, because search syntax differs — the
	// iTunes catalogue wants "artist album", while the NetEase endpoint wants
	// a track-like phrase.
	Search(ctx context.Context, q model.Query, limit int) ([]*model.Candidate, error)
	// FetchCover downloads the artwork for a candidate at the requested edge
	// length in pixels.
	FetchCover(ctx context.Context, c *model.Candidate, size int) (*model.Cover, error)
}

const (
	// requestTimeout bounds a single upstream request.
	requestTimeout = 12 * time.Second
	// maxImageBytes caps a downloaded image. Providers serve artwork well
	// under this; the cap exists so a misbehaving or hostile response cannot
	// exhaust memory.
	maxImageBytes = 12 << 20 // 12 MiB
)

var defaultHTTPClient = &http.Client{Timeout: requestTimeout}

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

// ErrNoArtwork means the source matched an album but has no image for it.
var ErrNoArtwork = errors.New("provider has no artwork for this album")

// get performs a GET with the shared client and the standard headers.
func get(ctx context.Context, rawURL, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// getJSON performs a GET and decodes a JSON body into out.
func getJSON(ctx context.Context, rawURL, referer string, out any) error {
	resp, err := get(ctx, rawURL, referer)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decoding upstream response: %w", err)
	}
	return nil
}

// downloadImage fetches an image body and reports its content type, sniffing it
// when the server does not say.
func downloadImage(ctx context.Context, rawURL, referer string) ([]byte, string, error) {
	resp, err := get(ctx, rawURL, referer)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("reading image body: %w", err)
	}
	if len(data) == 0 {
		return nil, "", ErrNoArtwork
	}
	if len(data) > maxImageBytes {
		return nil, "", fmt.Errorf("image exceeds the %d byte cap", maxImageBytes)
	}

	contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("upstream returned %q, not an image", contentType)
	}
	return data, contentType, nil
}
