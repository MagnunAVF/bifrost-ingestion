# ENG-2: Database bootstrapping and schema remediation (plan)

Status: approved 2026-10-03 (decisions 1-7 as recommended; 8 is a later issue's job). Plan 1.1, lane A, depends on ENG-1 (closed).
Branch: `feat/ENG-2-schema-remediation`. (The issue body names this file `ENG-7-…` after the
GitHub number; ENG-2 is the id.)

## Goal

Open `catalog.db` safely and rebuild `SellerProduct` so `SellerProductId` stores UUID strings
exactly as sent, without losing rows. Ship the first sqlc queries and `bifrost migrate`.

## Public API (internal/catalog)

```go
// Catalog owns the *sql.DB for one catalog.db file.
type Catalog struct { /* db *sql.DB; q *db.Queries */ }

// Open opens an existing SQLite file with modernc ("sqlite") and verifies it with a ping.
// Every connection gets foreign_keys=ON and busy_timeout=5000 (DSN _pragma). The pool is a
// single connection (SetMaxOpenConns(1)): one writer, and connection-scoped PRAGMAs such as
// foreign_keys=OFF in the rebuild migration stay on the connection that runs the migration.
// A missing file is an error (mode=rw), so a typo in --db never creates a new catalog.
func Open(ctx context.Context, path string) (*Catalog, error)

func (c *Catalog) Close() error

// Migrate applies the embedded goose migrations (goose.NewProvider, no package globals) and
// returns the versions it applied; none on an up-to-date database.
func (c *Catalog) Migrate(ctx context.Context) ([]int64, error)

// WithTx runs fn in one transaction: commit on nil, rollback on error or panic.
func (c *Catalog) WithTx(ctx context.Context, fn func(q *db.Queries) error) error

// Read-only access outside a transaction (e.g. the cold-start ListProducts).
func (c *Catalog) Queries() *db.Queries

// Error mapping, used by callers of the generated queries:
// sql.ErrNoRows → errs.ErrNotFound, SQLITE_CONSTRAINT_UNIQUE → errs.ErrConflict.
func MapErr(err error) error
```

The issue writes `catalog.Open(path)`; it takes `ctx` first because it does I/O (ping).

## Migrations (internal/catalog/migrations, embedded with embed.FS)

- `00001_baseline.sql`: the verbatim legacy DDL from docs/data-notes.md with
  `CREATE TABLE IF NOT EXISTS` (Product, SellerProduct). No-op on the fixture; creates the
  schema on an empty file. `sqlite_sequence` is SQLite-internal and is not declared.
- `00002_seller_product_id_text.sql`: `-- +goose NO TRANSACTION`, then the 12-step rebuild:
  `PRAGMA foreign_keys=OFF; BEGIN; CREATE TABLE SellerProduct_new (… SellerProductId TEXT NOT
  NULL …); INSERT INTO SellerProduct_new (Id, SellerName, ProductId, SellerProductId) SELECT Id,
  SellerName, ProductId, CAST(SellerProductId AS TEXT) FROM SellerProduct; DROP TABLE
  SellerProduct; ALTER TABLE SellerProduct_new RENAME TO SellerProduct; <indexes>;
  PRAGMA foreign_key_check; COMMIT; PRAGMA foreign_keys=ON;`
  Keeps the column names, `Id INTEGER PRIMARY KEY AUTOINCREMENT` (copying Id keeps
  `sqlite_sequence` consistent) and the named `FK_Product_Id` constraint. Down migration: the
  inverse rebuild back to INTEGER, documented as lossy for non-numeric ids.
- Goose adds its own `goose_db_version` table to catalog.db.

goose cannot fail on a non-empty `PRAGMA foreign_key_check` result by itself, so `Migrate`
runs `PRAGMA foreign_key_check` after applying and returns an error if it returns rows.

## Queries (internal/catalog/queries/catalog.sql)

| Name | SQL (shape) | Returns |
| --- | --- | --- |
| ListProducts | `SELECT Id, Name, Brand, Category FROM Product ORDER BY Id` | `:many` |
| InsertProduct | `INSERT INTO Product (Name, Brand, Category) VALUES (?, ?, ?) RETURNING Id` | `:one` |
| FindSellerLink | `SELECT … FROM SellerProduct WHERE SellerName = ? AND SellerProductId = ?` | `:one` |
| InsertSellerLink | `INSERT INTO SellerProduct (SellerName, ProductId, SellerProductId) VALUES (?, ?, ?) RETURNING Id` | `:one` |

## CLI (cmd/bifrost)

`bifrost migrate --db <path>`: opens, migrates, logs the applied versions with slog, exit 0.
Missing `--db` → exit 2 with usage; open/migrate error → exit 1. Nothing else in main.go
changes (the milestone allows only `migrate` here before ENG-6).

## Test table

`internal/catalog/catalog_test.go` (all on `fixture.Copy(t, "catalog.db")` or an empty file in
`t.TempDir()`; the existing `TestFixturesUnchanged` guards the originals):

| Test | Setup | Expectation |
| --- | --- | --- |
| Open: pragmas | fixture copy | `PRAGMA foreign_keys` = 1, `PRAGMA busy_timeout` = 5000, MaxOpenConnections = 1 |
| Open: missing file | path in TempDir that does not exist | error, no file created |
| Open: not a database | TempDir file with text bytes | error |
| Open: cancelled ctx | cancelled context | error wrapping `context.Canceled` |
| Baseline is a no-op on the fixture | fixture copy, apply only 00001 | schema of Product/SellerProduct unchanged (compare `sqlite_master.sql`), 975 products |
| Baseline creates the schema | empty file | Product and SellerProduct exist |
| Migrate: column becomes TEXT | fixture copy | `PRAGMA table_info(SellerProduct)` type of SellerProductId = `TEXT`, NOT NULL; FK_Product_Id still present |
| Migrate: rows preserved | fixture copy seeded through raw SQL with 3 legacy rows (integer ids incl. 123) before migrating | count before == after, same Id/SellerName/ProductId, SellerProductId is `typeof='text'` `'123'`; `sqlite_sequence` for SellerProduct unchanged |
| Migrate: Product untouched | fixture copy | 975 rows, `sqlite_sequence` Product = 975 |
| Migrate: idempotent | migrate twice | second call applies nothing, no error |
| Migrate: foreign_key_check clean | fixture copy | no rows |
| Migrate: foreign key violation | seed a SellerProduct row with ProductId 99999 (FKs off) | Migrate returns an error |
| UUID insert after migrate | InsertSellerLink with `e5e5e5e5-f6f6-4a7a-b8b8-c9c9c9c9c9c9` | stored and read back byte-identical, `typeof='text'` |
| No affinity rewrite | insert `'123'`, `' 42 '`, `'1e5'` | read back unchanged |
| FK enforced | InsertSellerLink with ProductId 99999 | error |
| ListProducts | migrated fixture | 975 rows; NULL Brand comes back as invalid `sql.NullString` (e.g. Product 113) |
| InsertProduct | migrated fixture | returns Id 976 |
| FindSellerLink found / not found | after one insert | the row / `MapErr` → `errs.ErrNotFound` |
| Unique conflict (if decision 3 = yes) | same (SellerName, SellerProductId) twice | `MapErr` → `errs.ErrConflict` |
| WithTx commit / rollback / panic | fn inserts then returns nil / error / panics | row present / absent / absent and panic re-raised |

`cmd/bifrost/main_test.go`: rows for `migrate` without `--db` (2), on a fixture copy (0), twice
(0), on a missing file (1). This needs a change to an existing row; see decision 1.

## Files

- New: `internal/catalog/catalog.go`, `catalog_test.go`, `migrate.go`, `migrations/00001_baseline.sql`,
  `migrations/00002_seller_product_id_text.sql`, `export_test.go`, `queries/catalog.sql`,
  `internal/catalog/db/*` (sqlc generated), `docs/adr/0001-seller-product-id-text.md`.
- Changed: `cmd/bifrost/main.go`, `cmd/bifrost/main_test.go` (decision 1), `go.mod`
  (`modernc.org/sqlite` and `github.com/pressly/goose/v3` move from indirect to direct),
  `docs/milestones/M1.md` (Decisions), `internal/catalog/schema.sql` only if sqlc can't parse 00002.

## Tasks (one commit each)

1. Open + pragmas + pool (tests first).
2. Baseline migration + Migrate + embed.
3. Rebuild migration + row-preservation, UUID and affinity tests.
4. sqlc queries + MapErr + WithTx.
5. `bifrost migrate` command.
6. ADR 0001 + M1 Decisions lines.

## Decisions (approved 2026-10-03)

1. Replace the "migrate is a stub" row in `cmd/bifrost/main_test.go` with rows for the real command.
2. `modernc.org/sqlite` and `goose/v3` become direct dependencies (already in go.mod, no `go get`).
3. 00002 adds `UNIQUE INDEX UX_SellerProduct_SellerName_SellerProductId ON SellerProduct
   (SellerName, SellerProductId)`: per-seller uniqueness, idempotent re-runs.
4. `SellerProductId TEXT NOT NULL CHECK (length(SellerProductId) BETWEEN 1 AND 64)`, not STRICT.
   UUID format validation stays in ingest.
5. Foreign keys ON per connection; the 12-step rebuild (from the issue).
6. `Open` refuses a missing file (`mode=rw`).
7. `journal_mode` stays `delete`.
8. "ingest refuses an unmigrated database" is left to ENG-6.

Implementation notes:

- The migrations are embedded in package catalog (`//go:embed migrations/*.sql`); no extra package.
- 00002 aborts *before* COMMIT on a foreign-key violation: it inserts
  `count(*) FROM pragma_foreign_key_check` into a temp table with `CHECK (n = 0)`.
- If a NO TRANSACTION migration fails half-way, the shared connection may still be inside
  `BEGIN` with foreign keys off; `Migrate` then issues `ROLLBACK` and `PRAGMA foreign_keys=ON`.
