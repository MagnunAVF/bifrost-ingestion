// Package catalog is the only package that touches SQLite: it opens catalog.db, applies the
// goose migrations, runs transactions, and wraps the sqlc queries generated into catalog/db.
package catalog
