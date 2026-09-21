package cache

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/x-cyber-space/x-cyber-cover-hub/internal/model"
)

// schemaVersion is bumped whenever the table layout or the cache key format
// changes. The covers table is a pure cache, so a mismatch rebuilds it rather
// than migrating.
const schemaVersion = 1

// timestampFormat is the SQLite expression used for every timestamp column. It
// must produce the same textual shape the TTL comparison builds, because that
// comparison is a lexicographic string compare inside SQLite.
const timestampFormat = `strftime('%Y-%m-%d %H:%M:%f','now')`

// SQLiteStore implements Store using pure-Go SQLite.
type SQLiteStore struct {
	db  *sql.DB
	ttl time.Duration
}

// NewSQLiteStore opens or creates a SQLite database at the specified path.
// A non-positive ttl disables expiry.
func NewSQLiteStore(dbPath string, ttl time.Duration) (*SQLiteStore, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create cache directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// One connection, deliberately. Every query here is an indexed lookup on a
	// small local table, and the real cost of a request is two outbound HTTP
	// calls. A pool would buy nothing measurable while requiring the pragmas
	// below to be re-applied per connection.
	db.SetMaxOpenConns(1)

	store := &SQLiteStore{db: db, ttl: clampTTL(ttl)}
	if err := store.configure(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.initTables(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

// configure applies durability and locking pragmas. WAL plus
// synchronous=NORMAL replaces the rollback journal's per-commit fsyncs with one
// fsync per checkpoint, which is the difference that matters on the disks a
// self-hosted instance runs on.
func (s *SQLiteStore) configure() error {
	var journalMode string
	if err := s.db.QueryRow("PRAGMA journal_mode=WAL").Scan(&journalMode); err != nil {
		return fmt.Errorf("failed to enable WAL: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := s.db.Exec(pragma); err != nil {
			return fmt.Errorf("failed to apply %q: %w", pragma, err)
		}
	}
	return nil
}

func (s *SQLiteStore) initTables() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("failed to read cache schema version: %w", err)
	}
	if version != schemaVersion {
		if _, err := s.db.Exec("DROP TABLE IF EXISTS covers"); err != nil {
			return fmt.Errorf("failed to reset cache table: %w", err)
		}
	}

	schema := fmt.Sprintf(`
	CREATE TABLE IF NOT EXISTS covers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cache_key TEXT UNIQUE NOT NULL,
		artist_name TEXT NOT NULL,
		album_name TEXT NOT NULL,
		source TEXT,
		artwork_url TEXT,
		content_type TEXT NOT NULL,
		size INTEGER NOT NULL,
		byte_size INTEGER NOT NULL,
		data BLOB NOT NULL,
		created_at DATETIME DEFAULT (%s),
		updated_at DATETIME DEFAULT (%s)
	);
	CREATE INDEX IF NOT EXISTS idx_covers_cache_key ON covers(cache_key);
	CREATE INDEX IF NOT EXISTS idx_covers_updated_at ON covers(updated_at);
	PRAGMA user_version = %d;
	`, timestampFormat, timestampFormat, schemaVersion)

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to initialize sqlite tables: %w", err)
	}
	return nil
}

// freshness builds the TTL predicate and its bind argument.
//
// The modifier must be a fractional number of *seconds*: SQLite's date
// modifiers are days, hours, minutes, seconds, months and years only, and an
// invalid modifier makes strftime return NULL, which would turn every
// comparison into a false and every row into a miss.
func (s *SQLiteStore) freshness() (clause string, modifier string) {
	if s.ttl <= 0 {
		return "", ""
	}
	return " AND updated_at >= strftime('%Y-%m-%d %H:%M:%f','now', ?)",
		fmt.Sprintf("-%.3f seconds", s.ttl.Seconds())
}

const coverColumns = `id, artist_name, album_name, source, artwork_url, content_type, size, data`

func scanCover(row interface{ Scan(...any) error }) (*model.Cover, error) {
	var (
		id          int
		artistName  string
		albumName   string
		source      sql.NullString
		artworkURL  sql.NullString
		contentType string
		size        int
		data        []byte
	)
	if err := row.Scan(&id, &artistName, &albumName, &source, &artworkURL, &contentType, &size, &data); err != nil {
		return nil, err
	}
	return &model.Cover{
		ArtistName:  artistName,
		AlbumName:   albumName,
		Source:      source.String,
		ArtworkURL:  artworkURL.String,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	}, nil
}

// Get retrieves a cached cover by key, ignoring expired rows.
func (s *SQLiteStore) Get(cacheKey string) (*model.Cover, error) {
	clause, modifier := s.freshness()
	query := `SELECT ` + coverColumns + ` FROM covers WHERE cache_key = ?` + clause + ` LIMIT 1;`

	args := []any{cacheKey}
	if modifier != "" {
		args = append(args, modifier)
	}

	cover, err := scanCover(s.db.QueryRow(query, args...))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // miss, or expired
		}
		return nil, err
	}
	return cover, nil
}

// Set stores or replaces a cover and returns its row id.
func (s *SQLiteStore) Set(cacheKey string, cover *model.Cover) error {
	query := fmt.Sprintf(`
	INSERT INTO covers (
		cache_key, artist_name, album_name, source, artwork_url,
		content_type, size, byte_size, data, updated_at
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, %s)
	ON CONFLICT(cache_key) DO UPDATE SET
		artist_name=excluded.artist_name,
		album_name=excluded.album_name,
		source=excluded.source,
		artwork_url=excluded.artwork_url,
		content_type=excluded.content_type,
		size=excluded.size,
		byte_size=excluded.byte_size,
		data=excluded.data,
		updated_at=%s
	RETURNING id;
	`, timestampFormat, timestampFormat)

	var id int
	if err := s.db.QueryRow(query,
		cacheKey,
		cover.ArtistName,
		cover.AlbumName,
		cover.Source,
		cover.ArtworkURL,
		cover.ContentType,
		cover.Size,
		len(cover.Data),
		cover.Data,
	).Scan(&id); err != nil {
		return fmt.Errorf("failed to insert/update cover cache: %w", err)
	}
	return nil
}

// PruneExpired deletes rows past the TTL and reports how many were removed.
func (s *SQLiteStore) PruneExpired() (int64, error) {
	_, modifier := s.freshness()
	if modifier == "" {
		return 0, nil
	}
	result, err := s.db.Exec(
		`DELETE FROM covers WHERE updated_at < strftime('%Y-%m-%d %H:%M:%f','now', ?)`,
		modifier)
	if err != nil {
		return 0, fmt.Errorf("failed to prune expired covers: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count pruned rows: %w", err)
	}
	return removed, nil
}

// TTL reports the configured expiry (zero means never).
func (s *SQLiteStore) TTL() time.Duration { return s.ttl }

// Close closes the underlying database.
func (s *SQLiteStore) Close() error { return s.db.Close() }
