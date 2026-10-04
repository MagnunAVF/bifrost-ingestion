package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

// exitFailedRecords means the run completed but some records failed and were rolled back.
const exitFailedRecords = 3

type ingestConfig struct {
	db, input, model, ollamaURL string
	threshold                   float64
	update                      string
	dryRun                      bool
}

// runIngest deduplicates --input into --db and prints the report to stdout. Exit codes: 0 done,
// 1 fatal error (the partial report is still printed), 2 usage error, 3 done with failed records.
func runIngest(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg ingestConfig
	fs.StringVar(&cfg.db, "db", "", "path of a migrated catalog.db (required)")
	fs.StringVar(&cfg.input, "input", "", "path of the ProductEntry.json payload (required)")
	fs.StringVar(&cfg.model, "model", "nomic-embed-text", "Ollama embedding model")
	fs.StringVar(&cfg.ollamaURL, "ollama-url", embed.DefaultBaseURL, "Ollama base URL")
	fs.Float64Var(&cfg.threshold, "threshold", dedup.DefaultThreshold, "cosine score in (0, 1] at or above which an entry is a duplicate")
	fs.StringVar(&cfg.update, "update", "fill", "what a matched entry may change on its product: fill, none or overwrite")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "decide and report every entry without writing")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	usageErr := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(stderr, "ingest: "+format+"\n", a...)
		return 2
	}
	switch {
	case fs.NArg() > 0:
		return usageErr("unexpected argument %q", fs.Arg(0))
	case cfg.db == "":
		return usageErr("--db is required")
	case cfg.input == "":
		return usageErr("--input is required")
	}
	policy := dedup.Policy{Threshold: float32(cfg.threshold)}
	if err := policy.Validate(); err != nil {
		return usageErr("%v", err)
	}
	mode, err := dedup.ParseUpdateMode(cfg.update)
	if err != nil {
		return usageErr("%v", err)
	}

	fatal := func(err error) int {
		_, _ = fmt.Fprintf(stderr, "ingest: %v\n", err)
		if h := hint(err, cfg); h != "" {
			_, _ = fmt.Fprintf(stderr, "  hint: %s\n", h)
		}
		return 1
	}

	c, err := catalog.Open(ctx, cfg.db)
	if err != nil {
		return fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.RequireMigrated(ctx); err != nil {
		return fatal(err)
	}
	in, err := os.Open(cfg.input)
	if err != nil {
		return fatal(fmt.Errorf("opening input: %w", err))
	}
	defer func() { _ = in.Close() }()

	emb, err := embed.NewOllama(nil, embed.OllamaConfig{BaseURL: cfg.ollamaURL, Model: cfg.model})
	if err != nil {
		return usageErr("%v", err)
	}
	p, err := dedup.New(c, emb, dedup.Config{
		Policy: policy, Update: mode, DryRun: cfg.dryRun, TaskPrefix: dedup.DefaultTaskPrefix,
		Logger: slog.New(slog.NewTextHandler(stderr, nil)),
	})
	if err != nil {
		return fatal(err)
	}

	rep, runErr := p.Run(ctx, ingest.Decode(ctx, in))
	if err := rep.WriteText(stdout, cfg.dryRun); err != nil {
		_, _ = fmt.Fprintf(stderr, "ingest: %v\n", err)
	}
	if runErr != nil {
		code := fatal(runErr)
		_, _ = fmt.Fprintf(stderr, "  state: %s\n", committed(rep))
		_, _ = fmt.Fprintln(stderr, "  next: re-running the same command is safe (already-linked entries are skipped).")
		return code
	}
	if rep.Failed > 0 {
		_, _ = fmt.Fprintf(stderr, "ingest: %d records failed and were rolled back; see the report. Fix the cause; re-running retries them.\n", rep.Failed)
		return exitFailedRecords
	}
	return 0
}

// committed describes what a stopped run left in the database.
func committed(rep dedup.Report) string {
	if rep.DryRun || rep.Inserted+rep.Linked+rep.Updated == 0 {
		return "nothing was written."
	}
	return fmt.Sprintf("%d linked, %d inserted, %d products updated are committed; nothing from record %d on was written.",
		rep.Linked, rep.Inserted, rep.Updated, rep.Stopped.Index)
}
