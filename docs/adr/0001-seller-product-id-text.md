# ADR 0001: SellerProductId becomes TEXT, unique per seller

- Status: accepted
- Date: 2026-10-03
- Issue: ENG-2 (plan: docs/plans/ENG-2-schema-remediation.md)

## Context

Sellers identify their products with UUID-shaped strings, but the legacy column
`SellerProduct.SellerProductId` is `INTEGER NOT NULL` (docs/data-notes.md). SQLite's INTEGER affinity
does not reject strings. It does two worse things:

- It rewrites numeric-looking strings: `'0001'` → `1`, `' 42 '` → `42`, `'1e5'` → `100000`.
  The id we store would no longer be the id the seller sent.
- sqlc generates `int64` for the column, so a stored UUID fails to scan in typed code.

The column has no uniqueness, so nothing stops `ingest` from inserting the same link twice. The
fixture reuses 13 ids across *different* sellers, and these entries are legitimate.

The database file is shared with other systems, and its writers never turned foreign keys on.

## Decision

Migration 00002 rebuilds `SellerProduct` (SQLite can't change a column type in place), following
SQLite's documented table-rebuild procedure: `-- +goose NO TRANSACTION`, `foreign_keys=OFF`,
one explicit transaction, `foreign_key_check` before COMMIT.

- `SellerProductId TEXT NOT NULL CHECK (length(SellerProductId) BETWEEN 1 AND 64)`. TEXT affinity
  stores strings exactly as sent; existing integers are copied with `CAST(… AS TEXT)`.
- `UNIQUE INDEX UX_SellerProduct_SellerName_SellerProductId (SellerName, SellerProductId)`: an
  id is unique per seller, not globally. Re-running `ingest` cannot duplicate a link, and a
  duplicate insert surfaces as `errs.ErrConflict`.
- Table and column names, `Id` values, the named `FK_Product_Id` constraint and the
  `sqlite_sequence` counter are kept.
- A foreign-key violation aborts the migration before COMMIT: `PRAGMA foreign_key_check` only
  reports, so its row count goes into a temp table with `CHECK (n = 0)`.
- Every connection runs `foreign_keys=ON` and `busy_timeout=5000`; the pool has one connection
  so the rebuild's connection-scoped PRAGMAs apply to the statements that follow them. If a
  non-transactional migration fails half-way, `Catalog.Migrate` rolls back and turns foreign
  keys on again.

## Alternatives rejected

- **`STRICT` table.** It would also stop affinity conversion, but it changes the whole table's
  typing and needs SQLite ≥ 3.37 in every system that opens the file. TEXT affinity is enough.
- **Global uniqueness on `SellerProductId`.** It would turn the 13 legitimate cross-seller
  collisions in the fixture into conflicts.
- **Format validation (UUID shape) in a CHECK.** The fixture has 3 malformed ids whose
  handling is still an open question; validation belongs in ingest, where it can be reported.
- **WAL journal mode.** A persistent change to a shared file; not needed for M1.

## Consequences

- A legacy database that already holds duplicate `(SellerName, SellerProductId)` pairs or
  orphan `ProductId`s will fail migration 00002 and must be cleaned first. The fixture holds
  neither (SellerProduct is empty).
- The down migration restores the INTEGER column but is lossy (affinity rewrites numeric ids).
- `goose_db_version` is added to the shared file.
- Ids longer than 64 characters, or empty ones, are rejected by the database.

## Updates

- 2026-10-03 (ENG-3): the open question on malformed ids is answered. `ingest` rejects any Id
  without the 8-4-4-4-12 hex shape and stores accepted ids lowercased, so fixture entries 92,
  180 and 268 are rejected and reported. The database CHECK stays a length check only.
