-- Baseline: the legacy schema of catalog.db, verbatim from docs/data-notes.md. It is a no-op on
-- an existing catalog and creates the schema on an empty database.

-- +goose Up
CREATE TABLE IF NOT EXISTS Product (Id INTEGER PRIMARY KEY AUTOINCREMENT, Name TEXT NOT NULL, Brand TEXT, Category TEXT);
CREATE TABLE IF NOT EXISTS SellerProduct (Id INTEGER PRIMARY KEY AUTOINCREMENT, SellerName TEXT NOT NULL, ProductId INTEGER CONSTRAINT FK_Product_Id REFERENCES Product (Id) NOT NULL, SellerProductId INTEGER NOT NULL);

-- +goose Down
-- Intentionally empty: these tables predate the migrations and are shared with other systems,
-- so rolling back the baseline must never drop them.
