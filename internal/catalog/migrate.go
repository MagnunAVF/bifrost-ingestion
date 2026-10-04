package catalog

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pressly/goose/v3"

	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every pending migration and returns the versions it applied, in order. On an
// up-to-date database it applies nothing and returns an empty slice.
func (c *Catalog) Migrate(ctx context.Context) ([]int64, error) {
	return c.migrate(ctx, 0)
}

// RequireMigrated returns an error wrapping errs.ErrNotMigrated when any migration is pending,
// so `bifrost ingest` never runs against the legacy schema.
func (c *Catalog) RequireMigrated(ctx context.Context) error {
	p, err := c.provider()
	if err != nil {
		return err
	}
	pending, err := p.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("checking migrations: %w", err)
	}
	if pending {
		return fmt.Errorf("catalog has pending migrations (run bifrost migrate): %w", errs.ErrNotMigrated)
	}
	return nil
}

func (c *Catalog) provider() (*goose.Provider, error) {
	dir, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("loading migrations: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, c.db, dir, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, fmt.Errorf("loading migrations: %w", err)
	}
	return p, nil
}

// migrate applies the pending migrations up to and including version; 0 means all of them.
func (c *Catalog) migrate(ctx context.Context, version int64) ([]int64, error) {
	p, err := c.provider()
	if err != nil {
		return nil, err
	}

	var results []*goose.MigrationResult
	if version == 0 {
		results, err = p.Up(ctx)
	} else {
		results, err = p.UpTo(ctx, version)
	}
	if err != nil {
		return nil, fmt.Errorf("applying migrations: %w", errors.Join(err, c.resetAfterFailure(ctx)))
	}

	applied := make([]int64, 0, len(results))
	for _, r := range results {
		applied = append(applied, r.Source.Version)
	}
	return applied, nil
}

// resetAfterFailure undoes what a failed NO TRANSACTION migration leaves on the pool's only
// connection: an open BEGIN and foreign_keys=OFF. It ignores ctx's cancellation, which may be
// the reason the migration failed.
func (c *Catalog) resetAfterFailure(ctx context.Context) error {
	ctx = context.WithoutCancel(ctx)
	var err error
	if _, rbErr := c.db.ExecContext(ctx, "ROLLBACK"); rbErr != nil &&
		!strings.Contains(rbErr.Error(), "no transaction is active") {
		err = fmt.Errorf("rolling back failed migration: %w", rbErr)
	}
	if _, fkErr := c.db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); fkErr != nil {
		err = errors.Join(err, fmt.Errorf("re-enabling foreign keys: %w", fkErr))
	}
	return err
}
