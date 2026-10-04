# ADR 0004: Pure-Go SQLite driver (modernc.org/sqlite)

- Status: accepted
- Date: 2026-10-03 (decided in ENG-1/ENG-2, recorded in ENG-7)
- Issue: ENG-7 (plan: docs/plans/ENG-7-architecture-docs.md, decision D4)

## Context

Bifröst reads and writes a shared `catalog.db` file. Go has two mainstream SQLite drivers:

- `github.com/mattn/go-sqlite3` wraps the C library and needs CGO and a C toolchain.
- `modernc.org/sqlite` is the same SQLite code translated to Go and needs no CGO.

The release builds four targets (darwin and linux, arm64 and amd64) from one runner
(`.goreleaser.yaml`). The developer machine is an M1 laptop.

## Decision

Use `modernc.org/sqlite`, registered under the driver name `sqlite` (not `sqlite3`), and build
with `CGO_ENABLED=0`. All access goes through `internal/catalog` and sqlc; no code depends on
driver-specific behaviour beyond the `_pragma` DSN parameters (`foreign_keys(1)`,
`busy_timeout(5000)`) and the error codes mapped to `errs.ErrConflict`.

## Reasons

- No C toolchain on the developer machine or in CI.
- Cross-compilation is plain `GOOS`/`GOARCH`: goreleaser builds every target with
  `CGO_ENABLED=0`.
- A static binary with no shared-library dependency.

## Alternatives rejected

- **mattn/go-sqlite3 (CGO).** Generally faster and the most widely used, but it brings a C
  toolchain into every build and makes cross-compilation need a cross C compiler per target.
- **ncruces/go-sqlite3 (Wasm).** Also CGO-free, but less common, and it would bring a Wasm
  runtime into the binary.

## Consequences

- **The performance difference with the CGO driver was not measured.** In M1 a run is
  dominated by embedding calls (~88 texts/s, ADR 0006), not by SQLite: the whole fixture run
  takes ~16 s, ~11 s of it in the cold-start embeddings.
- SQLite loadable extensions in C (sqlite-vec, sqlite-vss) are not an option with this driver,
  which ADR 0005 also rules out on other grounds.
- Revisit if profiling shows SQLite on the hot path, for example after M4 parallelizes the
  embedding calls.
