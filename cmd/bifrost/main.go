// Command bifrost ingests seller catalogs into catalog.db and links semantic duplicates.
//
// This package only parses flags and wires dependencies; the work lives in internal/.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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
func run(_ context.Context, args []string, stdout, stderr io.Writer) int {
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
	case "migrate", "ingest": // migrate lands in ENG-2, ingest in ENG-6
		_, _ = fmt.Fprintf(stderr, "%s: not implemented yet\n", cmd)
		return 1
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
}
