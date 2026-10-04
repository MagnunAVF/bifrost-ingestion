package fixture_test

import (
	"crypto/sha1" //nolint:gosec // matches the checksums recorded in docs/data-notes.md
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

// The checksums recorded in docs/data-notes.md. If one changes, a fixture was modified.
var fixtureSHA1 = map[string]string{
	"catalog.db":        "e7c535cbca9dfc053dfeceb43396cf58458bb059",
	"ProductEntry.json": "65bde7947e57e9e5cf38ea1f37e9de949cc9ddc3",
}

func TestFixturesUnchanged(t *testing.T) {
	for name, want := range fixtureSHA1 {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, want, sha1File(t, fixture.Path(t, name)))
		})
	}
}

func TestCopy(t *testing.T) {
	for name := range fixtureSHA1 {
		t.Run(name, func(t *testing.T) {
			orig := fixture.Path(t, name)
			before, err := os.Stat(orig)
			require.NoError(t, err)

			got := fixture.Copy(t, name)

			assert.NotEqual(t, orig, got)
			assert.Equal(t, name, filepath.Base(got))
			assert.Equal(t, sha1File(t, orig), sha1File(t, got), "copy has the same bytes")

			info, err := os.Stat(got)
			require.NoError(t, err)
			assert.NotZero(t, info.Mode().Perm()&0o200, "copy is writable by the owner")

			after, err := os.Stat(orig)
			require.NoError(t, err)
			assert.Equal(t, before.Mode(), after.Mode(), "original mode unchanged")
			assert.Equal(t, before.ModTime(), after.ModTime(), "original mtime unchanged")
		})
	}
}

func TestCopyIsolatesWrites(t *testing.T) {
	got := fixture.Copy(t, "ProductEntry.json")
	require.NoError(t, os.WriteFile(got, []byte("[]"), 0o600))

	assert.Equal(t, fixtureSHA1["ProductEntry.json"], sha1File(t, fixture.Path(t, "ProductEntry.json")))
}

func TestUnknownFixtureFails(t *testing.T) {
	tests := []struct {
		name string
		call func(testing.TB, string) string
	}{
		{name: "Path", call: fixture.Path},
		{name: "Copy", call: fixture.Copy},
	}

	for _, tt := range tests {
		for _, bad := range []string{"missing.db", "../go.mod", "/etc/hosts", ""} {
			t.Run(fmt.Sprintf("%s/%q", tt.name, bad), func(t *testing.T) {
				tb := &fakeTB{TB: t}

				func() {
					defer func() { _ = recover() }() // fakeTB.Fatalf panics to stop like FailNow
					tt.call(tb, bad)
				}()

				assert.True(t, tb.failed, "expected a fatal failure")
			})
		}
	}
}

// fakeTB records Fatalf instead of failing the real test.
type fakeTB struct {
	testing.TB
	failed bool
}

func (f *fakeTB) Helper() {}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.failed = true
	panic(fmt.Sprintf(format, args...))
}

func sha1File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test-controlled path in testdata/ or TempDir
	require.NoError(t, err)
	sum := sha1.Sum(b) //nolint:gosec // see import
	return hex.EncodeToString(sum[:])
}
