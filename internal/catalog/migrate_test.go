package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

const baselineVersion = 1

func TestBaselineIsANoOpOnTheFixture(t *testing.T) {
	c := open(t, fixture.Copy(t, "catalog.db"))
	before := schema(t, c)

	applied, err := c.MigrateTo(t.Context(), baselineVersion)

	require.NoError(t, err)
	assert.Equal(t, []int64{baselineVersion}, applied)
	assert.Equal(t, before, schema(t, c), "legacy schema unchanged")
	assert.Equal(t, 975, count(t, c, "SELECT count(*) FROM Product"))
}

func TestBaselineCreatesTheSchemaOnAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	c := open(t, path)

	_, err := c.MigrateTo(t.Context(), baselineVersion)

	require.NoError(t, err)
	got := schema(t, c)
	assert.Contains(t, got, "Product")
	assert.Contains(t, got, "SellerProduct")
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM Product"))
}

func TestMigrateIsIdempotent(t *testing.T) {
	c := open(t, fixture.Copy(t, "catalog.db"))

	first, err := c.Migrate(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, first)

	second, err := c.Migrate(t.Context())
	require.NoError(t, err)
	assert.Empty(t, second, "nothing left to apply")
}

func TestMigrateRespectsContext(t *testing.T) {
	c := open(t, fixture.Copy(t, "catalog.db"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.Migrate(ctx)

	assert.ErrorIs(t, err, context.Canceled)
}

// schema returns the DDL of the application tables (not SQLite's or goose's), keyed by name.
func schema(t *testing.T, c *catalog.Catalog) map[string]string {
	t.Helper()
	rows, err := c.DB().QueryContext(t.Context(),
		`SELECT name, sql FROM sqlite_master
		 WHERE name NOT LIKE 'sqlite_%' AND name NOT LIKE 'goose_%'`)
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	got := map[string]string{}
	for rows.Next() {
		var name, sql string
		require.NoError(t, rows.Scan(&name, &sql))
		got[name] = sql
	}
	require.NoError(t, rows.Err())
	return got
}

func count(t *testing.T, c *catalog.Catalog, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, c.DB().QueryRowContext(t.Context(), query, args...).Scan(&n))
	return n
}
