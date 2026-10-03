-- SellerProduct.SellerProductId becomes TEXT so seller ids (UUIDs) are stored exactly as sent;
-- see docs/adr/0001-seller-product-id-text.md. SQLite can't change a column type, so this is its
-- documented table rebuild: foreign keys off, one explicit transaction, foreign_key_check before
-- COMMIT. Ids and the sqlite_sequence counter are kept; existing integer ids become text.
--
-- If a statement fails, the connection is left inside BEGIN with foreign keys off;
-- Catalog.Migrate rolls back and turns foreign keys on again.

-- +goose NO TRANSACTION

-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE SellerProduct_new (Id INTEGER PRIMARY KEY AUTOINCREMENT, SellerName TEXT NOT NULL, ProductId INTEGER CONSTRAINT FK_Product_Id REFERENCES Product (Id) NOT NULL, SellerProductId TEXT NOT NULL CHECK (length(SellerProductId) BETWEEN 1 AND 64));

INSERT INTO SellerProduct_new (Id, SellerName, ProductId, SellerProductId)
SELECT Id, SellerName, ProductId, CAST(SellerProductId AS TEXT) FROM SellerProduct;

DELETE FROM sqlite_sequence WHERE name = 'SellerProduct_new';
INSERT INTO sqlite_sequence (name, seq) SELECT 'SellerProduct_new', seq FROM sqlite_sequence WHERE name = 'SellerProduct';

DROP TABLE SellerProduct;
ALTER TABLE SellerProduct_new RENAME TO SellerProduct;

CREATE UNIQUE INDEX UX_SellerProduct_SellerName_SellerProductId ON SellerProduct (SellerName, SellerProductId);

-- PRAGMA foreign_key_check only reports; the CHECK turns any violation into an error before COMMIT.
CREATE TEMP TABLE fk_violations (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO fk_violations (n) SELECT count(*) FROM pragma_foreign_key_check;
DROP TABLE fk_violations;

COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
-- Back to the legacy INTEGER column. Lossy: non-numeric ids (UUIDs) are kept as TEXT by INTEGER
-- affinity, numeric-looking ones are converted, and the unique index and CHECK are dropped.
PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE SellerProduct_old (Id INTEGER PRIMARY KEY AUTOINCREMENT, SellerName TEXT NOT NULL, ProductId INTEGER CONSTRAINT FK_Product_Id REFERENCES Product (Id) NOT NULL, SellerProductId INTEGER NOT NULL);

INSERT INTO SellerProduct_old (Id, SellerName, ProductId, SellerProductId)
SELECT Id, SellerName, ProductId, SellerProductId FROM SellerProduct;

DELETE FROM sqlite_sequence WHERE name = 'SellerProduct_old';
INSERT INTO sqlite_sequence (name, seq) SELECT 'SellerProduct_old', seq FROM sqlite_sequence WHERE name = 'SellerProduct';

DROP TABLE SellerProduct;
ALTER TABLE SellerProduct_old RENAME TO SellerProduct;

CREATE TEMP TABLE fk_violations (n INTEGER NOT NULL CHECK (n = 0));
INSERT INTO fk_violations (n) SELECT count(*) FROM pragma_foreign_key_check;
DROP TABLE fk_violations;

COMMIT;
PRAGMA foreign_keys = ON;
