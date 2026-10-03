package catalog

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies every pending migration and returns the versions it applied, in order. On an
// up-to-date database it applies nothing and returns an empty slice.
func (c *Catalog) Migrate(ctx context.Context) ([]int64, error) {
	return c.migrate(ctx, 0)
}

// migrate applies the pending migrations up to and including version; 0 means all of them.
func (c *Catalog) migrate(ctx context.Context, version int64) ([]int64, error) {
	dir, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("loading migrations: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, c.db, dir, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return nil, fmt.Errorf("loading migrations: %w", err)
	}

	var results []*goose.MigrationResult
	if version == 0 {
		results, err = p.Up(ctx)
	} else {
		results, err = p.UpTo(ctx, version)
	}
	if err != nil {
		return nil, fmt.Errorf("applying migrations: %w", err)
	}

	applied := make([]int64, 0, len(results))
	for _, r := range results {
		applied = append(applied, r.Source.Version)
	}
	return applied, nil
}
