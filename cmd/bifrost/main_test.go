package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring; "" means stdout must be empty
		wantStderr string // substring; "" means stderr must be empty
	}{
		{name: "no args prints usage to stderr", args: nil, wantCode: 2, wantStderr: "Usage: bifrost"},
		{name: "unknown command", args: []string{"foo"}, wantCode: 2, wantStderr: `unknown command "foo"`},
		{name: "version", args: []string{"version"}, wantCode: 0, wantStdout: "dev\n"},
		{name: "help", args: []string{"help"}, wantCode: 0, wantStdout: "Usage: bifrost"},
		{name: "-h", args: []string{"-h"}, wantCode: 0, wantStdout: "Usage: bifrost"},
		{name: "--help", args: []string{"--help"}, wantCode: 0, wantStdout: "Usage: bifrost"},
		{name: "migrate needs --db", args: []string{"migrate"}, wantCode: 2, wantStderr: "migrate: --db is required"},
		{name: "migrate rejects unknown flags", args: []string{"migrate", "--nope"}, wantCode: 2, wantStderr: "flag provided but not defined"},
		{name: "migrate rejects extra arguments", args: []string{"migrate", "--db", "x.db", "extra"}, wantCode: 2, wantStderr: `unexpected argument "extra"`},
		{name: "migrate -h", args: []string{"migrate", "-h"}, wantCode: 0, wantStderr: "-db"},
		{name: "ingest needs --db", args: []string{"ingest"}, wantCode: 2, wantStderr: "ingest: --db is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			code := run(context.Background(), tt.args, &stdout, &stderr)

			assert.Equal(t, tt.wantCode, code)
			assertOutput(t, "stdout", tt.wantStdout, stdout.String())
			assertOutput(t, "stderr", tt.wantStderr, stderr.String())
		})
	}
}

func TestMigrateCommand(t *testing.T) {
	tests := []struct {
		name       string
		db         func(t *testing.T) string
		runs       int // how many times migrate runs; the last run is checked
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "migrates a copy of the fixture",
			db:         func(t *testing.T) string { return fixture.Copy(t, "catalog.db") },
			runs:       1,
			wantStdout: `applied="[1 2]"`,
		},
		{
			name:       "second run applies nothing",
			db:         func(t *testing.T) string { return fixture.Copy(t, "catalog.db") },
			runs:       2,
			wantStdout: "applied=[]",
		},
		{
			name:       "missing database file",
			db:         func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.db") },
			runs:       1,
			wantCode:   1,
			wantStderr: "migrate: opening catalog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.db(t)
			var stdout, stderr bytes.Buffer

			code := -1
			for range tt.runs {
				stdout.Reset()
				stderr.Reset()
				code = run(t.Context(), []string{"migrate", "--db", path}, &stdout, &stderr)
			}

			assert.Equal(t, tt.wantCode, code)
			assertOutput(t, "stdout", tt.wantStdout, stdout.String())
			assertOutput(t, "stderr", tt.wantStderr, stderr.String())
		})
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	var stdout bytes.Buffer
	run(context.Background(), []string{"help"}, &stdout, &bytes.Buffer{})

	for _, cmd := range []string{"migrate", "ingest", "version", "help"} {
		assert.Contains(t, stdout.String(), cmd)
	}
}

func assertOutput(t *testing.T, stream, want, got string) {
	t.Helper()
	if want == "" {
		assert.Empty(t, got, stream)
		return
	}
	assert.Contains(t, got, want, stream)
}
