package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog/db"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// busyTimeoutMS is how long a connection waits for a lock held by another process.
const busyTimeoutMS = 5000

// Catalog owns the connection pool of one catalog.db file.
type Catalog struct {
	db *sql.DB
	q  *db.Queries
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
	pool, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening catalog %s: %w", path, err)
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(0)
	pool.SetConnMaxIdleTime(0)

	// The first query reads the header, so it fails on a file that is not a database.
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&n); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("opening catalog %s: %w", path, err)
	}
	return &Catalog{db: pool, q: db.New(pool)}, nil
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

// Queries runs the generated queries outside a transaction, e.g. the cold-start ListProducts.
// Don't call it inside a WithTx body: the pool has one connection, which the transaction holds.
func (c *Catalog) Queries() *db.Queries { return c.q }

// WithTx runs fn in one transaction. It commits when fn returns nil and rolls back when fn
// returns an error or panics (the panic is re-raised).
func (c *Catalog) WithTx(ctx context.Context, fn func(q *db.Queries) error) (err error) {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(c.q.WithTx(tx)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rolling back: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// MapErr translates driver errors from the generated queries into the shared sentinels:
// sql.ErrNoRows becomes errs.ErrNotFound and a UNIQUE or PRIMARY KEY violation becomes
// errs.ErrConflict. The original error stays in the chain; any other error is returned as is.
func MapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %w", errs.ErrNotFound, err)
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return fmt.Errorf("%w: %w", errs.ErrConflict, err)
		}
	}
	return err
}
