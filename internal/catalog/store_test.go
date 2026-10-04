package catalog_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog/db"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

func TestRequireMigrated(t *testing.T) {
	t.Run("migrated copy", func(t *testing.T) {
		require.NoError(t, migrated(t).RequireMigrated(t.Context()))
	})
	t.Run("raw fixture has pending migrations", func(t *testing.T) {
		c := open(t, fixture.Copy(t, "catalog.db"))
		assert.ErrorIs(t, c.RequireMigrated(t.Context()), errs.ErrNotMigrated)
	})
	t.Run("baseline only", func(t *testing.T) {
		c := open(t, fixture.Copy(t, "catalog.db"))
		_, err := c.MigrateTo(t.Context(), baselineVersion)
		require.NoError(t, err)
		assert.ErrorIs(t, c.RequireMigrated(t.Context()), errs.ErrNotMigrated)
	})
}

func TestStoreListProducts(t *testing.T) {
	c := migrated(t)

	got, err := c.ListProducts(t.Context())

	require.NoError(t, err)
	require.Len(t, got, 975)
	for i, p := range got {
		require.Equal(t, int64(i+1), p.ID, "ordered by Id, no gaps")
	}
	assert.Empty(t, got[112].Brand, "Product 113 has a NULL Brand")
	assert.NotEmpty(t, got[112].Name)
	assert.Equal(t, "Router WiFi 6 TP-Link", got[20].Name)
}

func TestStoreFindAndLinkSeller(t *testing.T) {
	c := migrated(t)
	ctx := t.Context()

	_, err := c.FindSellerLink(ctx, "GardenStore", uuid)
	require.ErrorIs(t, err, errs.ErrNotFound)

	require.NoError(t, c.LinkSeller(ctx, &catalog.SellerLink{SellerName: "GardenStore", SellerProductID: uuid, ProductID: 28}, nil))
	got, err := c.FindSellerLink(ctx, "GardenStore", uuid)
	require.NoError(t, err)
	assert.Equal(t, int64(28), got)

	err = c.LinkSeller(ctx, &catalog.SellerLink{SellerName: "GardenStore", SellerProductID: uuid, ProductID: 64}, nil)
	require.ErrorIs(t, err, errs.ErrConflict)

	require.NoError(t, c.LinkSeller(ctx, &catalog.SellerLink{SellerName: "SportsHub", SellerProductID: uuid, ProductID: 21}, nil),
		"ids are unique per seller")

	err = c.LinkSeller(ctx, &catalog.SellerLink{SellerName: "MegaStore", SellerProductID: "fk-1", ProductID: 99999}, nil)
	require.Error(t, err, "foreign key")
	assert.NotErrorIs(t, err, errs.ErrConflict)

	assert.Equal(t, 2, count(t, c, "SELECT count(*) FROM SellerProduct"))
}

func TestStoreLinkSellerWithUpdate(t *testing.T) {
	str := func(s string) *string { return &s }
	link := func(spid string, productID int64) *catalog.SellerLink {
		return &catalog.SellerLink{SellerName: "GardenStore", SellerProductID: spid, ProductID: productID}
	}

	tests := []struct {
		name      string
		setup     func(t *testing.T, c *catalog.Catalog)
		link      *catalog.SellerLink
		upd       *catalog.AttrUpdate
		wantErr   error
		wantLinks int
		wantID    int64
		want      db.Product // checked when wantID != 0
	}{
		{
			name:      "fills a NULL brand and links",
			link:      link("a", 113),
			upd:       &catalog.AttrUpdate{ProductID: 113, Brand: str("Acme")},
			wantLinks: 1,
			wantID:    113,
		},
		{
			name:      "nil field keeps the value",
			link:      link("b", 21),
			upd:       &catalog.AttrUpdate{ProductID: 21, Category: str("Networking")},
			wantLinks: 1,
			wantID:    21,
		},
		{
			name: "conflicting link rolls the update back",
			setup: func(t *testing.T, c *catalog.Catalog) {
				require.NoError(t, c.LinkSeller(t.Context(), link("c", 21), nil))
			},
			link:      link("c", 113),
			upd:       &catalog.AttrUpdate{ProductID: 113, Brand: str("Acme")},
			wantErr:   errs.ErrConflict,
			wantLinks: 1,
			wantID:    113,
		},
		{
			name:      "nil link applies only the update",
			upd:       &catalog.AttrUpdate{ProductID: 113, Brand: str("Acme")},
			wantLinks: 0,
			wantID:    113,
		},
		{
			name:      "update of a missing product rolls the link back",
			link:      link("d", 21),
			upd:       &catalog.AttrUpdate{ProductID: 99999, Brand: str("Acme")},
			wantErr:   errs.ErrNotFound,
			wantLinks: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := migrated(t)
			if tt.setup != nil {
				tt.setup(t, c)
			}
			var before db.Product
			if tt.wantID != 0 {
				before = product(t, c, tt.wantID)
			}

			err := c.LinkSeller(t.Context(), tt.link, tt.upd)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantLinks, count(t, c, "SELECT count(*) FROM SellerProduct"))
			if tt.wantID == 0 {
				return
			}
			want := before
			if tt.wantErr == nil {
				if tt.upd.Brand != nil {
					want.Brand = valid(*tt.upd.Brand)
				}
				if tt.upd.Category != nil {
					want.Category = valid(*tt.upd.Category)
				}
			}
			assert.Equal(t, want, product(t, c, tt.wantID), "Name never changes; rollback leaves the row as it was")
		})
	}
}

func TestStoreInsertProductAndLink(t *testing.T) {
	tests := []struct {
		name string
		p    catalog.NewProduct
		want db.Product
	}{
		{
			name: "all fields",
			p:    catalog.NewProduct{Name: "Router WiFi 7", Brand: "TP-Link", Category: "Electronics"},
			want: db.Product{Name: "Router WiFi 7", Brand: valid("TP-Link"), Category: valid("Electronics")},
		},
		{
			name: "blank brand and category are NULL",
			p:    catalog.NewProduct{Name: "Cable Organizer Kit"},
			want: db.Product{Name: "Cable Organizer Kit"},
		},
		{
			name: "sql payload is stored verbatim",
			p:    catalog.NewProduct{Name: "Security Test Product", Brand: "TestBrand'; SELECT 1; --", Category: "Electronics"},
			want: db.Product{Name: "Security Test Product", Brand: valid("TestBrand'; SELECT 1; --"), Category: valid("Electronics")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := migrated(t)

			id, err := c.InsertProductAndLink(t.Context(), tt.p, "MegaStore", uuid)

			require.NoError(t, err)
			assert.Equal(t, int64(976), id)
			tt.want.ID = id
			assert.Equal(t, tt.want, product(t, c, id))
			assert.Equal(t, 976, count(t, c, "SELECT count(*) FROM Product"))
			assert.Equal(t, 1, count(t, c, "SELECT count(*) FROM SellerProduct WHERE SellerName = 'MegaStore' AND ProductId = ? AND SellerProductId = ?", id, uuid))
			assert.Equal(t, 2, count(t, c, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('Product', 'SellerProduct')"), "tables intact")
		})
	}
}

func TestStoreInsertProductAndLinkRollsBack(t *testing.T) {
	c := migrated(t)
	require.NoError(t, c.LinkSeller(t.Context(), &catalog.SellerLink{SellerName: "MegaStore", SellerProductID: uuid, ProductID: 21}, nil))

	_, err := c.InsertProductAndLink(t.Context(), catalog.NewProduct{Name: "New Thing"}, "MegaStore", uuid)

	require.ErrorIs(t, err, errs.ErrConflict)
	assert.Equal(t, 975, count(t, c, "SELECT count(*) FROM Product"), "product insert rolled back")
	assert.Equal(t, 1, count(t, c, "SELECT count(*) FROM SellerProduct"))
}

func TestStoreCancelledContext(t *testing.T) {
	c := migrated(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := c.InsertProductAndLink(ctx, catalog.NewProduct{Name: "New Thing"}, "MegaStore", uuid)
	require.Error(t, err)
	err = c.LinkSeller(ctx, &catalog.SellerLink{SellerName: "MegaStore", SellerProductID: uuid, ProductID: 21}, nil)
	require.Error(t, err)

	assert.Equal(t, 975, count(t, c, "SELECT count(*) FROM Product"))
	assert.Equal(t, 0, count(t, c, "SELECT count(*) FROM SellerProduct"))
}
