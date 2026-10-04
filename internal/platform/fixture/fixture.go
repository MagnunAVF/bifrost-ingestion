// Package fixture gives tests access to the read-only files in testdata/.
//
// Tests never open testdata/ directly for writing: they call Copy and work on the copy in
// t.TempDir().
package fixture

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Path returns the absolute path of testdata/<name> in the module root. Use it only to read.
// name must be a plain file name; anything else, or a missing file, fails the test.
func Path(tb testing.TB, name string) string {
	tb.Helper()

	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		tb.Fatalf("fixture: %q is not a plain file name", name)
	}
	root, err := moduleRoot()
	if err != nil {
		tb.Fatalf("fixture: %v", err)
	}
	path := filepath.Join(root, "testdata", name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		tb.Fatalf("fixture: %s is not a regular file in testdata/", name)
	}
	return path
}

// Copy copies testdata/<name> into tb.TempDir() and returns the path of the writable copy.
func Copy(tb testing.TB, name string) string {
	tb.Helper()

	src := Path(tb, name)
	dst := filepath.Join(tb.TempDir(), name)
	if err := copyFile(src, dst); err != nil {
		tb.Fatalf("fixture: copying %s: %v", name, err)
	}
	return dst
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(src) //nolint:gosec // src comes from Path, which only allows names in testdata/
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // dst is in TempDir
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()

	_, err = io.Copy(out, in)
	return err
}

// moduleRoot walks up from the working directory (the package dir under go test) to go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
