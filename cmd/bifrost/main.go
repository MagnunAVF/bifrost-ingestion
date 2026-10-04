// Command bifrost ingests seller catalogs into catalog.db and links semantic duplicates.
//
// This package only parses flags and wires dependencies; the work lives in internal/.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
)

// version is set at release time by goreleaser (-X main.version).
var version = "dev"

const usage = `Usage: bifrost <command> [flags]

Commands:
  migrate   apply the catalog database migrations
  ingest    deduplicate a ProductEntry.json payload into the catalog
  version   print the version
  help      show this help
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run dispatches to a subcommand and returns the process exit code:
// 0 on success, 1 when the command fails, 2 on a usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}

	switch cmd := args[0]; cmd {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "migrate":
		return runMigrate(ctx, args[1:], stdout, stderr)
	case "ingest":
		return runIngest(ctx, args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}

// runMigrate applies the catalog migrations to --db and logs the versions it applied.
func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "path of an existing catalog.db (required)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "migrate: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *dbPath == "" {
		_, _ = fmt.Fprintln(stderr, "migrate: --db is required")
		fs.Usage()
		return 2
	}

	c, err := catalog.Open(ctx, *dbPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	defer func() { _ = c.Close() }()

	applied, err := c.Migrate(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	slog.New(slog.NewTextHandler(stdout, nil)).InfoContext(ctx, "migrated", "db", *dbPath, "applied", applied)
	return 0
}
