# Catalog (SQLite) rules

- Keep the legacy table and column names exactly as in docs/data-notes.md (Product, SellerProduct,
  SellerProductId, ...). The schema is shared with other systems.
- Migrations are goose files in migrations/, embedded with embed.FS and applied by `bifrost migrate`.
  `bifrost ingest` refuses to run on an unmigrated database.
- Never edit an existing migration. Create one with:
  `go tool goose -dir internal/catalog/migrations create <name> sql`
- 00001_baseline.sql mirrors the legacy schema with CREATE TABLE/INDEX IF NOT EXISTS, so it is a
  no-op on testdata/catalog.db and creates the schema on an empty database.
- Changing a column type means a table rebuild (SQLite's documented 12-step procedure):
  `-- +goose NO TRANSACTION`, PRAGMA foreign_keys=OFF, BEGIN, CREATE TABLE <name>\_new, copy rows
  (CAST where the type changes), DROP old, ALTER TABLE <name>\_new RENAME TO <name>, recreate
  indexes/triggers/views, PRAGMA foreign_key_check (must return nothing), COMMIT,
  PRAGMA foreign_keys=ON.
- Every query lives in queries/\*.sql with an sqlc annotation; run `make generate` after changes.
- If sqlc can't parse the rebuild migration, point sqlc.yaml's `schema` at schema.sql and
  regenerate it with `make schema`.
- Map sql.ErrNoRows to errs.ErrNotFound and unique-constraint errors to errs.ErrConflict here.
- dedup writes through the plain-typed store methods in store.go (LinkSeller,
  InsertProductAndLink), which own the transaction (one per product) so dedup never imports
  database/sql. WithTx stays the building block inside this package.
