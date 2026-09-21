package provider

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// stubProvider is an offline Provider double. Every test in this file runs
// without touching the network, because the fallback and gate behaviour is
// where correctness lives.
type stubProvider struct {
	name      string
	cands     []*model.Candidate
	searchErr error

	// artwork maps an album name to the bytes served for it. A missing entry
	// with missingErr set models the common real case: the source matched the
	// album but has no image for it.
	artwork    map[string][]byte
	missingErr error
	fetchErr   error

	mu      sync.Mutex
	fetched []string
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
	s.fetched = append(s.fetched, c.AlbumName)
	s.mu.Unlock()

	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	data, ok := s.artwork[c.AlbumName]
	if !ok {
		if s.missingErr != nil {
			return nil, s.missingErr
		}
		return nil, ErrNoArtwork
	}
	return &model.Cover{
		ArtistName:  c.ArtistName,
		AlbumName:   c.AlbumName,
		Source:      s.name,
		ArtworkURL:  c.ArtworkURL,
		ContentType: "image/jpeg",
		Size:        size,
		Data:        data,
	}, nil
}

func (s *stubProvider) fetchOrder() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.fetched...)
}

func cand(artist, album string) *model.Candidate {
	return &model.Candidate{ArtistName: artist, AlbumName: album, ArtworkURL: "https://example.com/a.jpg"}
}

// TestResolveFallsThroughWhenSourceHasNoArtwork is the behaviour the consuming
// player already relies on: the iTunes catalogue often matches an album and
// serves no image for it, while the NetEase entry for the same record does.
func TestResolveFallsThroughWhenSourceHasNoArtwork(t *testing.T) {
	primary := &stubProvider{
		name:    "itunes",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{}, // matches, but no image
	}
	fallback := &stubProvider{
		name:    "netease",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("jpegbytes")},
	}

	d := NewDispatcherWith(primary, fallback)
	cover, err := d.Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if cover == nil {
		t.Fatal("expected the fallback source to supply the image")
	}
	if cover.Source != "netease" {
		t.Errorf("expected the fallback source, got %q", cover.Source)
	}
	if got := len(primary.fetchOrder()); got != 1 {
		t.Errorf("the primary source should have been tried once, got %d", got)
	}
}

// TestResolveRefusesADifferentAlbum is the defect this service exists to fix:
// the player currently takes the first search result, so a wrong cover is
// embedded into the audio file permanently. A confident miss must stay a miss.
func TestResolveRefusesADifferentAlbum(t *testing.T) {
	p := &stubProvider{
		name:    "itunes",
		cands:   []*model.Candidate{cand("周杰伦", "七里香")},
		artwork: map[string][]byte{"七里香": []byte("jpegbytes")},
	}

	d := NewDispatcherWith(p)
	cover, err := d.Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if cover != nil {
		t.Fatalf("a different album must not be returned, got %q", cover.AlbumName)
	}
	if len(p.fetchOrder()) != 0 {
		t.Error("no image should have been downloaded for a rejected candidate")
	}
}

func TestResolveErrorsOnlyWhenEverySourceFails(t *testing.T) {
	boom := errors.New("upstream down")

	allDown := NewDispatcherWith(
		&stubProvider{name: "itunes", searchErr: boom},
		&stubProvider{name: "netease", searchErr: boom},
	)
	if _, err := allDown.Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"}); err == nil {
		t.Fatal("expected an error when every source fails, so the handler can answer 503 instead of 404")
	}

	partial := NewDispatcherWith(
		&stubProvider{name: "itunes", searchErr: boom},
		&stubProvider{
			name:    "netease",
			cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
			artwork: map[string][]byte{"叶惠美": []byte("jpegbytes")},
		},
	)
	cover, err := partial.Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"})
	if err != nil {
		t.Fatalf("a single source failure must not fail the request: %v", err)
	}
	if cover == nil {
		t.Fatal("expected the surviving source to answer")
	}
}

// TestResolvePrefersExactAndProviderOrder pins the tie-breaks: confidence
// first, then the configured source order (iTunes is primary).
func TestResolvePrefersExactAndProviderOrder(t *testing.T) {
	// The fallback offers an exact match; the primary only a relaxed one.
	primary := &stubProvider{
		name:    "itunes",
		cands:   []*model.Candidate{cand("周杰伦 / 费玉清", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("from-primary")},
	}
	fallback := &stubProvider{
		name:    "netease",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("from-fallback")},
	}

	cover, err := NewDispatcherWith(primary, fallback).
		Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"})
	if err != nil || cover == nil {
		t.Fatalf("expected a cover, got %v (err=%v)", cover, err)
	}
	if cover.Source != "netease" {
		t.Errorf("the exact match must win over the relaxed one, got %q", cover.Source)
	}
}

func TestResolveFallsBackToSourcePriorityOnEqualStage(t *testing.T) {
	primary := &stubProvider{
		name:    "itunes",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("from-primary")},
	}
	fallback := &stubProvider{
		name:    "netease",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("from-fallback")},
	}

	cover, err := NewDispatcherWith(primary, fallback).
		Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"})
	if err != nil || cover == nil {
		t.Fatalf("expected a cover, got %v (err=%v)", cover, err)
	}
	if cover.Source != "itunes" {
		t.Errorf("the first configured source must win a tie, got %q", cover.Source)
	}
	if len(fallback.fetchOrder()) != 0 {
		t.Error("the fallback should not have been downloaded from once the primary succeeded")
	}
}

// TestResolveTriesEachSourceOncePerAlbum pins the fallback contract: the same
// album offered by two sources stays two attempts, because the primary may
// match the record and still have no image for it.
func TestResolveTriesEachSourceOncePerAlbum(t *testing.T) {
	primary := &stubProvider{
		name:    "itunes",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{}, // no image, forces a fall-through
	}
	fallback := &stubProvider{
		name:    "netease",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("jpegbytes")},
	}

	cover, err := NewDispatcherWith(primary, fallback).
		Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美"})
	if err != nil || cover == nil {
		t.Fatalf("expected a cover, got %v (err=%v)", cover, err)
	}
	// The primary is tried and misses; the fallback is then tried and hits. If
	// deduplication had merged them by (artist, album), the fallback would
	// never have been reached and this would be a miss.
	total := len(primary.fetchOrder()) + len(fallback.fetchOrder())
	if total != 2 {
		t.Errorf("expected one attempt per source for the same album, got %d", total)
	}
}

func TestResolveClampsSize(t *testing.T) {
	p := &stubProvider{
		name:    "itunes",
		cands:   []*model.Candidate{cand("周杰伦", "叶惠美")},
		artwork: map[string][]byte{"叶惠美": []byte("jpegbytes")},
	}
	cover, err := NewDispatcherWith(p).
		Resolve(context.Background(), model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美", Size: 0})
	if err != nil || cover == nil {
		t.Fatalf("expected a cover, got %v (err=%v)", cover, err)
	}
	if cover.Size != 600 {
		t.Errorf("an unspecified size should resolve to the 600px default, got %d", cover.Size)
	}
}
