package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	return newTestStoreWithTTL(t, 0)
}

func newTestStoreWithTTL(t *testing.T, ttl time.Duration) *SQLiteStore {
	t.Helper()

	dir, err := os.MkdirTemp("", "coverhub_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := NewSQLiteStore(filepath.Join(dir, "sub", "covers.db"), ttl)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	return store
}

func sampleCover() *model.Cover {
	return &model.Cover{
		ArtistName:  "周杰伦",
		AlbumName:   "叶惠美",
		Source:      "itunes",
		ArtworkURL:  "https://example.com/600x600bb.jpg",
		ContentType: "image/jpeg",
		Size:        600,
		Data:        bytes.Repeat([]byte{0xFF}, 4096),
	}
}

func TestStoreRoundTripPreservesBytes(t *testing.T) {
	store := newTestStore(t)
	key := GenerateCacheKey("周杰伦", "叶惠美", 600)
	cover := sampleCover()

	if miss, err := store.Get(key); err != nil || miss != nil {
		t.Fatalf("expected a clean miss, got %v (err=%v)", miss, err)
	}

	if err := store.Set(key, cover); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected the stored cover back")
	}
	if !bytes.Equal(got.Data, cover.Data) {
		t.Errorf("image bytes did not round-trip: %d bytes in, %d out", len(cover.Data), len(got.Data))
	}
	if got.ContentType != cover.ContentType || got.Size != cover.Size || got.Source != cover.Source {
		t.Errorf("metadata did not round-trip: %+v", got)
	}
}

func TestStoreReplacesOnSameKey(t *testing.T) {
	store := newTestStore(t)
	key := GenerateCacheKey("周杰伦", "叶惠美", 600)

	first := sampleCover()
	if err := store.Set(key, first); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	second := sampleCover()
	second.Source = "netease"
	second.Data = []byte("replacement")
	if err := store.Set(key, second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := store.Get(key)
	if err != nil || got == nil {
		t.Fatalf("Get failed: %v (%v)", err, got)
	}
	if got.Source != "netease" || !bytes.Equal(got.Data, second.Data) {
		t.Errorf("the second write should have replaced the first: %+v", got)
	}
}

// TestStoreExpiresRows pins the freshness contract: an expired row reads as a
// miss, and pruning reclaims it.
func TestStoreExpiresRows(t *testing.T) {
	store := newTestStoreWithTTL(t, 60*time.Millisecond)
	key := GenerateCacheKey("周杰伦", "叶惠美", 600)

	if err := store.Set(key, sampleCover()); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if got, _ := store.Get(key); got == nil {
		t.Fatal("a freshly written row must be readable")
	}

	time.Sleep(120 * time.Millisecond)

	if got, _ := store.Get(key); got != nil {
		t.Error("an expired row must read as a miss")
	}
	removed, err := store.PruneExpired()
	if err != nil {
		t.Fatalf("PruneExpired failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 row pruned, got %d", removed)
	}
}

func TestStoreWithoutTTLKeepsRows(t *testing.T) {
	store := newTestStoreWithTTL(t, 0)
	key := GenerateCacheKey("周杰伦", "叶惠美", 600)
	if err := store.Set(key, sampleCover()); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if removed, _ := store.PruneExpired(); removed != 0 {
		t.Errorf("expiry is disabled, nothing should be pruned (got %d)", removed)
	}
}

// TestGenerateCacheKeySeparatesSizes: both sources serve any resolution from
// one source image, so different sizes are distinct artifacts worth caching
// separately rather than one row reused at the wrong fidelity.
func TestGenerateCacheKeySeparatesSizes(t *testing.T) {
	if GenerateCacheKey("周杰伦", "叶惠美", 600) == GenerateCacheKey("周杰伦", "叶惠美", 1200) {
		t.Error("different sizes must not share a cache row")
	}
	if GenerateCacheKey("周杰伦", "叶惠美", 600) != GenerateCacheKey("周杰伦", "叶惠美 (Deluxe Edition)", 600) {
		t.Error("edition markers are stripped, so these are the same album")
	}
}

// TestMatchesRejectsStaleRow closes the poisoning path: a row that does not
// satisfy the caller's query is treated as a miss.
func TestMatchesRejectsStaleRow(t *testing.T) {
	row := &model.Cover{ArtistName: "周杰伦", AlbumName: "七里香", Size: 600}

	if Matches(model.Query{ArtistName: "周杰伦", AlbumName: "叶惠美", Size: 600}, row) {
		t.Error("a different album must not satisfy the query")
	}
	if !Matches(model.Query{ArtistName: "周杰伦", AlbumName: "七里香", Size: 600}, row) {
		t.Error("the row must satisfy its own query")
	}
	if Matches(model.Query{ArtistName: "周杰伦", AlbumName: "七里香", Size: 1200}, row) {
		t.Error("a size mismatch must not satisfy the query")
	}
	if Matches(model.Query{ArtistName: "周杰伦", AlbumName: "七里香", Size: 600}, nil) {
		t.Error("a nil row is never a match")
	}
}

func TestStoreUsesWAL(t *testing.T) {
	store := newTestStore(t)

	var mode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}
