package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

// fakeOllama serves POST /api/embed with embed.Fake vectors, so the whole command runs in unit
// tests without a real Ollama.
func fakeOllama(t *testing.T) *httptest.Server {
	t.Helper()
	f := embed.NewFake(16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		if r.URL.Path != "/api/embed" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		vecs, _ := f.Embed(r.Context(), req.Input)
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vecs})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func statusOllama(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func migratedDB(t *testing.T) string {
	t.Helper()
	path := fixture.Copy(t, "catalog.db")
	var out, errOut bytes.Buffer
	require.Equal(t, 0, run(t.Context(), []string{"migrate", "--db", path}, &out, &errOut), errOut.String())
	return path
}

func hashFile(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test file in TempDir
	require.NoError(t, err)
	return sha256.Sum256(b)
}

func ingestArgs(db, url string, extra ...string) []string {
	return append([]string{"ingest", "--db", db, "--input", "../../testdata/ProductEntry.json", "--ollama-url", url}, extra...)
}

func TestIngestUsageErrors(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStderr string
	}{
		{name: "missing --input", args: []string{"ingest", "--db", "x.db"}, wantCode: 2, wantStderr: "ingest: --input is required"},
		{name: "threshold above 1", args: []string{"ingest", "--db", "x.db", "--input", "in.json", "--threshold", "1.5"}, wantCode: 2, wantStderr: "threshold"},
		{name: "threshold zero", args: []string{"ingest", "--db", "x.db", "--input", "in.json", "--threshold", "0"}, wantCode: 2, wantStderr: "threshold"},
		{name: "bad update mode", args: []string{"ingest", "--db", "x.db", "--input", "in.json", "--update", "bogus"}, wantCode: 2, wantStderr: "update mode"},
		{name: "extra argument", args: []string{"ingest", "--db", "x.db", "--input", "in.json", "extra"}, wantCode: 2, wantStderr: `unexpected argument "extra"`},
		{name: "unknown flag", args: []string{"ingest", "--nope"}, wantCode: 2, wantStderr: "flag provided but not defined"},
		{name: "-h", args: []string{"ingest", "-h"}, wantCode: 0, wantStderr: "-threshold"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(t.Context(), tt.args, &stdout, &stderr)

			assert.Equal(t, tt.wantCode, code)
			assert.Empty(t, stdout.String())
			assert.Contains(t, stderr.String(), tt.wantStderr)
		})
	}
}

func TestIngestFatalErrors(t *testing.T) {
	refused := httptest.NewServer(http.NotFoundHandler())
	refusedURL := refused.URL
	refused.Close()

	tests := []struct {
		name       string
		args       func(t *testing.T) []string
		wantStderr []string
	}{
		{
			name: "unmigrated database",
			args: func(t *testing.T) []string {
				return ingestArgs(fixture.Copy(t, "catalog.db"), fakeOllama(t).URL)
			},
			wantStderr: []string{"not migrated", "hint: run `bifrost migrate --db"},
		},
		{
			name: "missing database",
			args: func(t *testing.T) []string {
				return ingestArgs(filepath.Join(t.TempDir(), "missing.db"), fakeOllama(t).URL)
			},
			wantStderr: []string{"ingest: opening catalog"},
		},
		{
			name: "missing input file",
			args: func(t *testing.T) []string {
				return []string{"ingest", "--db", migratedDB(t), "--input", filepath.Join(t.TempDir(), "nope.json"), "--ollama-url", fakeOllama(t).URL}
			},
			wantStderr: []string{"ingest: opening input"},
		},
		{
			name: "ollama not running",
			args: func(t *testing.T) []string { return ingestArgs(migratedDB(t), refusedURL) },
			wantStderr: []string{
				"ingest: stopped at preflight",
				"hint: Ollama is not reachable at " + refusedURL,
				"`ollama serve`",
				"state: nothing was written",
				"next: re-running the same command is safe",
			},
		},
		{
			name: "model not pulled",
			args: func(t *testing.T) []string {
				return ingestArgs(migratedDB(t), statusOllama(t, http.StatusNotFound, `{"error":"model \"nomic-embed-text\" not found, try pulling it first"}`).URL)
			},
			wantStderr: []string{"hint: model \"nomic-embed-text\" is not pulled: run `ollama pull nomic-embed-text`"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			start := time.Now()

			code := run(t.Context(), tt.args(t), &stdout, &stderr)

			assert.Equal(t, 1, code, stderr.String())
			for _, want := range tt.wantStderr {
				assert.Contains(t, stderr.String(), want)
			}
			assert.Less(t, time.Since(start), 5*time.Second, "fails fast")
		})
	}
}

func TestIngestDryRunOnTheFixture(t *testing.T) {
	db := migratedDB(t)
	before := hashFile(t, db)
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), ingestArgs(db, fakeOllama(t).URL, "--dry-run"), &stdout, &stderr)

	require.Equal(t, 0, code, stderr.String())
	out := stdout.String()
	assert.Contains(t, out, "Bifröst ingest report (DRY RUN: nothing written)")
	assert.Contains(t, out, "threshold 0.900, update fill, catalog 975 products")
	assert.Contains(t, out, "records 269:")
	assert.Contains(t, out, "rejected 3")
	assert.Contains(t, out, "\ndecisions:\n", "a dry run prints every decision")
	assert.Equal(t, before, hashFile(t, db), "nothing written")
}

func TestIngestWriteIsIdempotent(t *testing.T) {
	db := migratedDB(t)
	url := fakeOllama(t).URL
	var stdout, stderr bytes.Buffer

	require.Equal(t, 0, run(t.Context(), ingestArgs(db, url), &stdout, &stderr), stderr.String())
	assert.NotContains(t, stdout.String(), "DRY RUN")
	assert.NotContains(t, stdout.String(), "\ndecisions:\n")

	stdout.Reset()
	require.Equal(t, 0, run(t.Context(), ingestArgs(db, url, "--update", "none", "--threshold", "0.95"), &stdout, &stderr), stderr.String())
	assert.Contains(t, stdout.String(), "inserted 0, linked 0, existing 266, rejected 3, failed 0")
	assert.Contains(t, stdout.String(), "threshold 0.950, update none")
}

func TestIngestFailedRecordsExitThree(t *testing.T) {
	db := migratedDB(t)
	conn, err := sql.Open("sqlite", "file:"+db)
	require.NoError(t, err)
	_, err = conn.ExecContext(t.Context(), `CREATE TRIGGER fail_megastore BEFORE INSERT ON SellerProduct
		WHEN NEW.SellerName = 'MegaStore' BEGIN SELECT RAISE(ABORT, 'megastore is down'); END`)
	require.NoError(t, err)
	require.NoError(t, conn.Close())
	var stdout, stderr bytes.Buffer

	code := run(t.Context(), ingestArgs(db, fakeOllama(t).URL), &stdout, &stderr)

	assert.Equal(t, 3, code, stderr.String())
	assert.Contains(t, stdout.String(), "megastore is down")
	assert.Regexp(t, `ingest: \d+ records failed and were rolled back`, stderr.String())
	assert.Contains(t, stderr.String(), "re-running retries them")
}

func TestHint(t *testing.T) {
	cfg := ingestConfig{db: "cat.db", model: "nomic-embed-text", ollamaURL: "http://localhost:11434"}
	refused := &url.Error{Op: "Post", URL: "http://localhost:11434/api/embed", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
	}}
	run := func(err error) error { return &dedup.RunError{Stage: dedup.StageEmbed, Index: 3, Err: err} }

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "not migrated", err: fmt.Errorf("x: %w", errs.ErrNotMigrated), want: "run `bifrost migrate --db cat.db` first"},
		{name: "connection refused", err: run(fmt.Errorf("calling ollama: %w: %w", errs.ErrUpstream, refused)), want: "Ollama is not reachable at http://localhost:11434. Start it with `ollama serve` or check --ollama-url."},
		{name: "dns", err: run(&net.DNSError{Err: "no such host", Name: "ollama.invalid"}), want: "Ollama is not reachable at http://localhost:11434. Start it with `ollama serve` or check --ollama-url."},
		{name: "model not pulled", err: run(&embed.StatusError{Code: 404, Body: "model not found"}), want: "model \"nomic-embed-text\" is not pulled: run `ollama pull nomic-embed-text`"},
		{name: "other status", err: run(&embed.StatusError{Code: 500, Body: "runner died"}), want: "Ollama answered 500: runner died"},
		{name: "timeout", err: run(fmt.Errorf("calling ollama: %w", context.DeadlineExceeded)), want: "Ollama did not answer in time: the model may still be loading or the machine is swapping. Retry."},
		{name: "interrupted", err: run(context.Canceled), want: "interrupted."},
		{name: "bad payload", err: run(fmt.Errorf("decoding: %w", errs.ErrInvalidInput)), want: "the payload is not one JSON array; records before the error were processed."},
		{name: "unusable answer", err: run(fmt.Errorf("count mismatch: %w", errs.ErrUpstream)), want: "Ollama answered with something unusable: check that --model nomic-embed-text is an embedding model."},
		{name: "unknown", err: errors.New("disk on fire"), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hint(tt.err, cfg))
		})
	}
}

// TestRefusedIsECONNREFUSED guards the hint against the real error chain of a closed port.
func TestRefusedIsECONNREFUSED(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	o, err := embed.NewOllama(nil, embed.OllamaConfig{BaseURL: u, Model: "m"})
	require.NoError(t, err)

	_, err = o.Embed(t.Context(), []string{"x"})

	assert.ErrorIs(t, err, syscall.ECONNREFUSED)
}
