// Package errs holds the sentinel errors shared across packages. Wrap them with
// fmt.Errorf("doing x: %w", err) and test for them with errors.Is.
package errs

import "errors"

var (
	// ErrNotFound means the requested row or record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict means a write would violate a uniqueness or consistency rule.
	ErrConflict = errors.New("conflict")
	// ErrInvalidInput means an input could not be processed at all (e.g. a broken JSON payload).
	ErrInvalidInput = errors.New("invalid input")
	// ErrUpstream means an external service (e.g. Ollama) failed or answered with something
	// unusable: a non-200 status, a malformed or oversized body, or inconsistent results.
	ErrUpstream = errors.New("upstream failed")
	// ErrNotMigrated means the catalog database has pending migrations.
	ErrNotMigrated = errors.New("catalog not migrated")
)
