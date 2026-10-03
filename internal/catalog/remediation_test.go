package catalog_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

func TestRemediationMakesSellerProductIdText(t *testing.T) {
	c := migrated(t)

	var typ string
	var notNull int
	require.NoError(t, c.DB().QueryRowContext(t.Context(),
		`SELECT type, "notnull" FROM pragma_table_info('SellerProduct') WHERE name = 'SellerProductId'`).
		Scan(&typ, &notNull))
	assert.Equal(t, "TEXT", typ)
	assert.Equal(t, 1, notNull)

	ddl := schema(t, c)["SellerProduct"]
	assert.Contains(t, ddl, "CONSTRAINT FK_Product_Id REFERENCES Product (Id)", "named FK kept")
	assert.Contains(t, ddl, "Id INTEGER PRIMARY KEY AUTOINCREMENT")
	assert.Equal(t, 1, count(t, c,
		`SELECT count(*) FROM pragma_foreign_key_list('SellerProduct')
		 WHERE "table" = 'Product' AND "from" = 'ProductId' AND "to" = 'Id'`))
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM sqlite_master WHERE name = 'SellerProduct_new'"))
}

func TestRemediationAddsUniqueSellerIndex(t *testing.T) {
	c := migrated(t)

	var unique int
	require.NoError(t, c.DB().QueryRowContext(t.Context(),
		`SELECT "unique" FROM pragma_index_list('SellerProduct')
		 WHERE name = 'UX_SellerProduct_SellerName_SellerProductId'`).Scan(&unique))
	assert.Equal(t, 1, unique)

	var cols []string
	rows, err := c.DB().QueryContext(t.Context(),
		`SELECT name FROM pragma_index_info('UX_SellerProduct_SellerName_SellerProductId') ORDER BY seqno`)
	require.NoError(t, err)
	for rows.Next() {
		var col string
		require.NoError(t, rows.Scan(&col))
		cols = append(cols, col)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, []string{"SellerName", "SellerProductId"}, cols)
}

func TestRemediationPreservesRows(t *testing.T) {
	c := open(t, fixture.Copy(t, "catalog.db"))
	_, err := c.MigrateTo(t.Context(), baselineVersion)
	require.NoError(t, err)

	// Legacy rows with INTEGER ids; deleting the last one leaves sqlite_sequence ahead of max(Id).
	for _, r := range []struct {
		seller    string
		productID int
		spID      int64
	}{{"MegaStore", 1, 123}, {"GardenStore", 2, 456}, {"SportsHub", 3, 789}, {"Gone", 4, 1}} {
		_, err := c.DB().ExecContext(t.Context(),
			"INSERT INTO SellerProduct (SellerName, ProductId, SellerProductId) VALUES (?, ?, ?)",
			r.seller, r.productID, r.spID)
		require.NoError(t, err)
	}
	_, err = c.DB().ExecContext(t.Context(), "DELETE FROM SellerProduct WHERE SellerName = 'Gone'")
	require.NoError(t, err)
	before := sellerProducts(t, c)
	seqBefore := sequence(t, c, "SellerProduct")
	require.Equal(t, 4, seqBefore)

	_, err = c.Migrate(t.Context())

	require.NoError(t, err)
	assert.Equal(t, before, sellerProducts(t, c), "same rows, ids converted to text")
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM SellerProduct WHERE typeof(SellerProductId) <> 'text'"))
	assert.Equal(t, seqBefore, sequence(t, c, "SellerProduct"), "sqlite_sequence kept")
	assert.Equal(t, 975, count(t, c, "SELECT count(*) FROM Product"))
	assert.Equal(t, 975, sequence(t, c, "Product"))
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestRemediationOnTheFixture(t *testing.T) {
	c := migrated(t)

	assert.Equal(t, 975, count(t, c, "SELECT count(*) FROM Product"))
	assert.Equal(t, 975, sequence(t, c, "Product"))
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM SellerProduct"))
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestRemediationAbortsOnForeignKeyViolation(t *testing.T) {
	path := fixture.Copy(t, "catalog.db")
	c := open(t, path)
	_, err := c.MigrateTo(t.Context(), baselineVersion)
	require.NoError(t, err)
	// A legacy orphan row, possible because the old writers never enabled foreign keys.
	for _, q := range []string{
		"PRAGMA foreign_keys = OFF",
		"INSERT INTO SellerProduct (SellerName, ProductId, SellerProductId) VALUES ('MegaStore', 99999, 1)",
		"PRAGMA foreign_keys = ON",
	} {
		_, err := c.DB().ExecContext(t.Context(), q)
		require.NoError(t, err)
	}

	_, err = c.Migrate(t.Context())

	require.Error(t, err)
	assert.Equal(t, 1, count(t, c, "PRAGMA foreign_keys"), "foreign keys back on")
	_, err = c.DB().ExecContext(t.Context(), "BEGIN")
	require.NoError(t, err, "no transaction left open")
	_, err = c.DB().ExecContext(t.Context(), "ROLLBACK")
	require.NoError(t, err)
	require.NoError(t, c.Close())

	fresh := open(t, path)
	assert.Contains(t, schema(t, fresh)["SellerProduct"], "SellerProductId INTEGER NOT NULL", "rebuild rolled back")
	assert.Equal(t, 1, count(t, fresh, "SELECT count(*) FROM SellerProduct"))
	_, err = fresh.Migrate(t.Context())
	assert.Error(t, err, "version 2 not recorded as applied")
}

func TestSellerProductIdIsStoredVerbatim(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "uuid", id: "e5e5e5e5-f6f6-4a7a-b8b8-c9c9c9c9c9c9"},
		{name: "uppercase uuid", id: "E5E5E5E5-F6F6-4A7A-B8B8-C9C9C9C9C9C9"},
		{name: "malformed id", id: "uddd0000-eeee-4111-ffff-aaaa22223333"},
		{name: "numeric string", id: "123"},
		{name: "leading zeros", id: "0001"},
		{name: "padded number", id: " 42 "},
		{name: "exponent", id: "1e5"},
		{name: "sql payload", id: "x'; DROP TABLE Product; --"},
		{name: "64 chars", id: strings.Repeat("a", 64)},
	}

	c := migrated(t)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.DB().ExecContext(t.Context(),
				"INSERT INTO SellerProduct (SellerName, ProductId, SellerProductId) VALUES (?, ?, ?)",
				"MegaStore", i+1, tt.id)
			require.NoError(t, err)

			var got, typ string
			require.NoError(t, c.DB().QueryRowContext(t.Context(),
				"SELECT SellerProductId, typeof(SellerProductId) FROM SellerProduct WHERE ProductId = ?", i+1).
				Scan(&got, &typ))
			assert.Equal(t, tt.id, got)
			assert.Equal(t, "text", typ)
		})
	}
	assert.Equal(t, 975, count(t, c, "SELECT count(*) FROM Product"))
}

func TestSellerProductConstraints(t *testing.T) {
	tests := []struct {
		name    string
		seller  string
		product int
		id      any
		wantErr bool
	}{
		{name: "empty id", seller: "MegaStore", product: 1, id: "", wantErr: true},
		{name: "65 chars", seller: "MegaStore", product: 1, id: strings.Repeat("a", 65), wantErr: true},
		{name: "null id", seller: "MegaStore", product: 1, id: nil, wantErr: true},
		{name: "unknown product", seller: "MegaStore", product: 99999, id: "fk-1", wantErr: true},
		{name: "first link", seller: "MegaStore", product: 1, id: "dup-1"},
		{name: "same seller and id", seller: "MegaStore", product: 2, id: "dup-1", wantErr: true},
		{name: "same id, other seller", seller: "SportsHub", product: 2, id: "dup-1"},
	}

	c := migrated(t)
	for _, tt := range tests { // in order: later rows depend on earlier inserts
		_, err := c.DB().ExecContext(t.Context(),
			"INSERT INTO SellerProduct (SellerName, ProductId, SellerProductId) VALUES (?, ?, ?)",
			tt.seller, tt.product, tt.id)
		if tt.wantErr {
			assert.Error(t, err, tt.name)
		} else {
			assert.NoError(t, err, tt.name)
		}
	}
}

func migrated(t *testing.T) *catalog.Catalog {
	t.Helper()
	c := open(t, fixture.Copy(t, "catalog.db"))
	_, err := c.Migrate(t.Context())
	require.NoError(t, err)
	return c
}

type sellerProduct struct {
	ID              int64
	SellerName      string
	ProductID       int64
	SellerProductID string
}

func sellerProducts(t *testing.T, c *catalog.Catalog) []sellerProduct {
	t.Helper()
	rows, err := c.DB().QueryContext(t.Context(),
		"SELECT Id, SellerName, ProductId, CAST(SellerProductId AS TEXT) FROM SellerProduct ORDER BY Id")
	require.NoError(t, err)
	defer func() { require.NoError(t, rows.Close()) }()

	var got []sellerProduct
	for rows.Next() {
		var sp sellerProduct
		require.NoError(t, rows.Scan(&sp.ID, &sp.SellerName, &sp.ProductID, &sp.SellerProductID))
		got = append(got, sp)
	}
	require.NoError(t, rows.Err())
	return got
}

func sequence(t *testing.T, c *catalog.Catalog, table string) int {
	t.Helper()
	return count(t, c, "SELECT seq FROM sqlite_sequence WHERE name = ?", table)
}
