package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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
		{name: "migrate is a stub until ENG-2", args: []string{"migrate"}, wantCode: 1, wantStderr: "migrate: not implemented yet"},
		{name: "ingest is a stub until ENG-6", args: []string{"ingest"}, wantCode: 1, wantStderr: "ingest: not implemented yet"},
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
