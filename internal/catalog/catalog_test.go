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

func TestOpenSetsPragmasAndPool(t *testing.T) {
	c := open(t, fixture.Copy(t, "catalog.db"))

	var fk, busy int
	require.NoError(t, c.DB().QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&fk))
	require.NoError(t, c.DB().QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&busy))

	assert.Equal(t, 1, fk, "foreign_keys")
	assert.Equal(t, 5000, busy, "busy_timeout")
	assert.Equal(t, 1, c.DB().Stats().MaxOpenConnections, "single connection")
}

func TestOpenAcceptsAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.db")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	open(t, path)
}

func TestOpenAcceptsPathsThatNeedEscaping(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a b?c#d%e")
	require.NoError(t, os.Mkdir(dir, 0o700))
	path := filepath.Join(dir, "catalog.db")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	open(t, path)
}

func TestOpenErrors(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		ctx     context.Context
		path    func(t *testing.T) string
		wantErr error // nil means any error
	}{
		{
			name: "missing file",
			ctx:  context.Background(),
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.db") },
		},
		{
			name: "not a database",
			ctx:  context.Background(),
			path: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "notes.db")
				require.NoError(t, os.WriteFile(p, []byte("this is not a SQLite file, just some text."), 0o600))
				return p
			},
		},
		{
			name:    "cancelled context",
			ctx:     cancelled,
			path:    func(t *testing.T) string { return fixture.Copy(t, "catalog.db") },
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.path(t)
			_, statErr := os.Stat(path)

			c, err := catalog.Open(tt.ctx, path)

			require.Error(t, err)
			assert.Nil(t, c)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			if os.IsNotExist(statErr) {
				_, err := os.Stat(path)
				assert.True(t, os.IsNotExist(err), "Open must not create the file")
			}
		})
	}
}

func open(t *testing.T, path string) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, c.Close()) })
	return c
}
