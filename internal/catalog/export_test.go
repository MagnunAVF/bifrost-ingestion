package catalog

import "database/sql"

// DB exposes the pool to the tests in package catalog_test.
func (c *Catalog) DB() *sql.DB { return c.db }
