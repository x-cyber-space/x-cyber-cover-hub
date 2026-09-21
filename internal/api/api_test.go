package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/cache"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
	"github.com/x-cyber-space/x-cyber-cover-hub/internal/provider"
)

// stubProvider is an offline Provider double so the API tests never touch the
// network.
type stubProvider struct {
	name      string
	cands     []*model.Candidate
	searchErr error
	artwork   map[string][]byte

	mu    sync.Mutex
	fetch int
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Search(_ context.Context, _ model.Query, _ int) ([]*model.Candidate, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
	}
	out := make([]*model.Candidate, 0, len(s.cands))
	for _, c := range s.cands {
		clone := *c
		clone.Source = s.name
		out = append(out, &clone)
	}
	return out, nil
}

func (s *stubProvider) FetchCover(_ context.Context, c *model.Candidate, size int) (*model.Cover, error) {
	s.mu.Lock()
	s.fetch++
	s.mu.Unlock()

	data, ok := s.artwork[c.AlbumName]
	if !ok {
		return nil, provider.ErrNoArtwork
	}
	return &model.Cover{
		ArtistName:  c.ArtistName,
		AlbumName:   c.AlbumName,
		Source:      s.name,
		ArtworkURL:  "https://example.com/a.jpg",
		ContentType: "image/png",
		Size:        size,
		Data:        data,
	}, nil
}

func (s *stubProvider) fetches() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fetch
}

func setup(t *testing.T, p provider.Provider) (http.Handler, cache.Store) {
	t.Helper()

	dir, err := os.MkdirTemp("", "coverhub_api_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	// ttl 0 disables expiry: these tests are about matching, not freshness.
	store, err := cache.NewSQLiteStore(filepath.Join(dir, "covers.db"), 0)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return NewRouter(store, provider.NewDispatcherWith(p), 600, "test"), store
}

func matchingProvider(t *testing.T) *stubProvider {
	t.Helper()
	return &stubProvider{
		name:    "stub",
		cands:   []*model.Candidate{{ArtistName: "周杰伦", AlbumName: "叶惠美"}},
		artwork: map[string][]byte{"叶惠美": []byte("\x89PNG\r\n\x1a\nfakeimagebytes")},
	}
}

func TestCoverMissingArtistIsBadRequest(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/cover?album_name=叶惠美", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if !strings.Contains(body.Message, "artist_name") {
		t.Errorf("the message should name the missing field, got %q", body.Message)
	}
}

// TestCoverWithoutAlbumIsBadRequest: without an album there is no album
// identity, only an artist, and every record by that artist would collapse onto
// one cache row.
func TestCoverWithoutAlbumIsBadRequest(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/cover?artist_name=周杰伦", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCoverInvalidSizeIsBadRequest(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	for _, size := range []string{"abc", "10", "99999"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/api/cover?artist_name=周杰伦&album_name=叶惠美&size="+size, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("size=%s should be rejected, got %d", size, rec.Code)
		}
	}
}

func TestCoverReturnsImageBytes(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/cover?artist_name=周杰伦&album_name=叶惠美", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("an image response must carry an ETag")
	}
	if rec.Body.Len() == 0 {
		t.Error("expected image bytes in the body")
	}
}

func TestCoverNotModifiedOnMatchingETag(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	url := "/api/cover?artist_name=周杰伦&album_name=叶惠美"

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, url, nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag on the first response")
	}

	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	router.ServeHTTP(second, req)

	if second.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Errorf("a 304 must not carry a body, got %d bytes", second.Body.Len())
	}
}

func TestCoverJSONFormatReturnsMetadata(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/cover?artist_name=周杰伦&album_name=叶惠美&size=1200&format=json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var meta coverMetadata
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("body is not JSON metadata: %v", err)
	}
	if meta.AlbumName != "叶惠美" || meta.Source != "stub" {
		t.Errorf("unexpected metadata: %+v", meta)
	}
	if meta.Size != 1200 {
		t.Errorf("size = %d, want the requested 1200", meta.Size)
	}
	if !strings.HasPrefix(meta.ImageURL, "/api/cover?") {
		t.Errorf("imageUrl should point back at this service, got %q", meta.ImageURL)
	}
	if meta.ETag == "" || meta.ByteSize == 0 {
		t.Errorf("metadata should carry the validator and size: %+v", meta)
	}
}

// TestCoverServesCacheOnSecondRequest proves the whole point of the service:
// one album costs one upstream lookup no matter how many tracks ask for it.
func TestCoverServesCacheOnSecondRequest(t *testing.T) {
	p := matchingProvider(t)
	router, _ := setup(t, p)
	url := "/api/cover?artist_name=周杰伦&album_name=叶惠美"

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, rec.Code)
		}
	}

	if got := p.fetches(); got != 1 {
		t.Errorf("expected a single upstream download for one album, got %d", got)
	}
}

func TestCoverNotFoundWhenAlbumDoesNotMatch(t *testing.T) {
	p := &stubProvider{
		name:    "stub",
		cands:   []*model.Candidate{{ArtistName: "周杰伦", AlbumName: "七里香"}},
		artwork: map[string][]byte{"七里香": []byte("bytes")},
	}
	router, _ := setup(t, p)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/cover?artist_name=周杰伦&album_name=叶惠美", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("a different album must not be served, got %d", rec.Code)
	}
	var body ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Name != ErrNameCoverNotFound {
		t.Errorf("error name = %q, want %q", body.Name, ErrNameCoverNotFound)
	}
}

// TestCoverUpstreamOutageIs503: hiding an outage behind a 404 makes a broken
// source look like a missing album.
func TestCoverUpstreamOutageIs503(t *testing.T) {
	router, _ := setup(t, &stubProvider{name: "stub", searchErr: errors.New("down")})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/cover?artist_name=周杰伦&album_name=叶惠美", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestCoverRejectsNonGet(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/cover", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("405 should carry no body, got %q", rec.Body.String())
	}
}

func TestHealthEndpoint(t *testing.T) {
	router, _ := setup(t, matchingProvider(t))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// TestCachedRowForAnotherAlbumIsNotServed closes the poisoning path: a row
// sitting in the cache that does not satisfy the request must be ignored.
func TestCachedRowForAnotherAlbumIsNotServed(t *testing.T) {
	router, store := setup(t, &stubProvider{name: "stub"})
	key := cache.GenerateCacheKey("周杰伦", "叶惠美", 600)
	if err := store.Set(key, &model.Cover{
		ArtistName: "周杰伦", AlbumName: "七里香", ContentType: "image/jpeg",
		Size: 600, Data: []byte("stale"),
	}); err != nil {
		t.Fatalf("failed to preload cache: %v", err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/cover?artist_name=周杰伦&album_name=叶惠美", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("a cached row for another album must not be served, got %d", rec.Code)
	}
}
