package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// busyTimeoutMS is how long a connection waits for a lock held by another process.
const busyTimeoutMS = 5000

// Catalog owns the connection pool of one catalog.db file.
type Catalog struct {
	db *sql.DB
}

// Open opens the existing SQLite file at path and checks that it is a database.
//
// Every connection runs with foreign_keys=ON and a busy timeout. The pool holds a single
// connection: SQLite has one writer, and connection-scoped PRAGMAs (foreign_keys=OFF in a table
// rebuild) must stay on the connection that set them. A missing file is an error, so a typo in
// --db never creates a new, empty catalog.
func Open(ctx context.Context, path string) (*Catalog, error) {
	dsn, err := dataSourceName(path)
	if err != nil {
		return nil, fmt.Errorf("opening catalog %s: %w", path, err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening catalog %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	// The first query reads the header, so it fails on a file that is not a database.
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&n); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("opening catalog %s: %w", path, err)
	}
	return &Catalog{db: db}, nil
}

// Close closes the pool.
func (c *Catalog) Close() error {
	if err := c.db.Close(); err != nil {
		return fmt.Errorf("closing catalog: %w", err)
	}
	return nil
}

// dataSourceName builds a SQLite URI for path: read-write without create, with the per-connection
// PRAGMAs the modernc driver applies through _pragma.
func dataSourceName(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("mode", "rw")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMS))
	return "file:" + (&url.URL{Path: filepath.ToSlash(abs)}).EscapedPath() + "?" + q.Encode(), nil
}
