package catalog

import (
	"context"
	"database/sql"
)

// DB exposes the pool to the tests in package catalog_test.
func (c *Catalog) DB() *sql.DB { return c.db }

// MigrateTo applies the migrations up to and including version.
func (c *Catalog) MigrateTo(ctx context.Context, version int64) ([]int64, error) {
	return c.migrate(ctx, version)
}
