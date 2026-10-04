//go:build integration

package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIngestWithOllama runs the whole command against a real Ollama (`make e2e`): a dry run, a
// real run and an idempotent re-run on a copy of the fixture. It fails, never skips, when Ollama
// is down. OLLAMA_HOST and BIFROST_MODEL override localhost and nomic-embed-text.
func TestIngestWithOllama(t *testing.T) {
	db := migratedDB(t)
	args := []string{"ingest", "--db", db, "--input", "../../testdata/ProductEntry.json"}
	if h := os.Getenv("OLLAMA_HOST"); h != "" {
		args = append(args, "--ollama-url", h)
	}
	if m := os.Getenv("BIFROST_MODEL"); m != "" {
		args = append(args, "--model", m)
	}
	before := hashFile(t, db)

	var stdout, stderr bytes.Buffer
	require.Equal(t, 0, run(t.Context(), append(args, "--dry-run"), &stdout, &stderr), stderr.String())
	assert.Equal(t, before, hashFile(t, db), "dry run writes nothing")
	dry := stdout.String()
	t.Log(dry)

	stdout.Reset()
	require.Equal(t, 0, run(t.Context(), args, &stdout, &stderr), stderr.String())
	assert.Equal(t, summary(t, dry), summary(t, stdout.String()), "a dry run predicts the real run")

	stdout.Reset()
	require.Equal(t, 0, run(t.Context(), args, &stdout, &stderr), stderr.String())
	assert.Contains(t, stdout.String(), "inserted 0, linked 0, existing 266, rejected 3, failed 0")
}

// summary returns the counts line of a report.
func summary(t *testing.T, report string) string {
	t.Helper()
	for _, line := range bytes.Split([]byte(report), []byte("\n")) {
		if bytes.HasPrefix(line, []byte("records ")) {
			return string(line)
		}
	}
	t.Fatalf("no summary line in:\n%s", report)
	return ""
}
