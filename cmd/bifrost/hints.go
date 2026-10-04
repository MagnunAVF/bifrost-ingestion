package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"

	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// hint suggests what to do about a fatal ingest error. It matches the cause with errors.Is and
// errors.As, never by parsing message text, and returns "" when it has nothing useful to say.
// Order matters: the specific Ollama causes also wrap errs.ErrUpstream.
func hint(err error, cfg ingestConfig) string {
	var status *embed.StatusError
	var dns *net.DNSError
	switch {
	case errors.Is(err, errs.ErrNotMigrated):
		return fmt.Sprintf("run `bifrost migrate --db %s` first", cfg.db)
	case errors.Is(err, syscall.ECONNREFUSED), errors.As(err, &dns):
		return fmt.Sprintf("Ollama is not reachable at %s. Start it with `ollama serve` or check --ollama-url.", cfg.ollamaURL)
	case errors.As(err, &status) && status.Code == http.StatusNotFound:
		return fmt.Sprintf("model %q is not pulled: run `ollama pull %s`", cfg.model, cfg.model)
	case errors.As(err, &status):
		return fmt.Sprintf("Ollama answered %d: %s", status.Code, status.Body)
	case errors.Is(err, context.DeadlineExceeded):
		return "Ollama did not answer in time: the model may still be loading or the machine is swapping. Retry."
	case errors.Is(err, context.Canceled):
		return "interrupted."
	case errors.Is(err, errs.ErrInvalidInput):
		return "the payload is not one JSON array; records before the error were processed."
	case errors.Is(err, errs.ErrUpstream):
		return fmt.Sprintf("Ollama answered with something unusable: check that --model %s is an embedding model.", cfg.model)
	}
	return ""
}
