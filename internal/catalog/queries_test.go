package catalog_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog/db"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

const uuid = "e5e5e5e5-f6f6-4a7a-b8b8-c9c9c9c9c9c9"

func TestListProducts(t *testing.T) {
	c := migrated(t)

	got, err := c.Queries().ListProducts(t.Context())

	require.NoError(t, err)
	require.Len(t, got, 975)
	for i, p := range got {
		require.Equal(t, int64(i+1), p.ID, "ordered by Id, no gaps")
	}
	assert.False(t, got[112].Brand.Valid, "Product 113 has a NULL Brand")
	assert.True(t, got[0].Category.Valid)
}

func TestInsertProduct(t *testing.T) {
	tests := []struct {
		name string
		arg  db.InsertProductParams
	}{
		{name: "all fields", arg: db.InsertProductParams{Name: "Router WiFi 7", Brand: valid("TP-Link"), Category: valid("Electronics")}},
		{name: "null brand and category", arg: db.InsertProductParams{Name: "Cable Organizer Kit"}},
		{name: "sql payload is data", arg: db.InsertProductParams{Name: "Security Test Product", Brand: valid("TestBrand'; SELECT 1; --"), Category: valid("Electronics")}},
		{name: "non-ascii", arg: db.InsertProductParams{Name: "Câmera Canon EOS R6", Brand: valid("Canon")}},
	}

	c := migrated(t)
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := c.Queries().InsertProduct(t.Context(), tt.arg)

			require.NoError(t, err)
			assert.Equal(t, int64(976+i), id)
			got := product(t, c, id)
			assert.Equal(t, db.Product{ID: id, Name: tt.arg.Name, Brand: tt.arg.Brand, Category: tt.arg.Category}, got)
		})
	}
	assert.Equal(t, 975+len(tests), count(t, c, "SELECT count(*) FROM Product"))
}

func TestSellerLinks(t *testing.T) {
	c := migrated(t)
	q := c.Queries()

	id, err := q.InsertSellerLink(t.Context(), db.InsertSellerLinkParams{SellerName: "GardenStore", ProductID: 28, SellerProductID: uuid})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)

	got, err := q.FindSellerLink(t.Context(), db.FindSellerLinkParams{SellerName: "GardenStore", SellerProductID: uuid})
	require.NoError(t, err)
	assert.Equal(t, db.SellerProduct{ID: 1, SellerName: "GardenStore", ProductID: 28, SellerProductID: uuid}, got)

	_, err = q.InsertSellerLink(t.Context(), db.InsertSellerLinkParams{SellerName: "SportsHub", ProductID: 21, SellerProductID: uuid})
	require.NoError(t, err, "same id from another seller is a different link")

	tests := []struct {
		name    string
		call    func() error
		wantErr error
		notErr  []error
	}{
		{
			name: "not found: unknown id",
			call: func() error {
				_, err := q.FindSellerLink(t.Context(), db.FindSellerLinkParams{SellerName: "GardenStore", SellerProductID: "nope"})
				return err
			},
			wantErr: errs.ErrNotFound,
		},
		{
			name: "not found: id of another seller",
			call: func() error {
				_, err := q.FindSellerLink(t.Context(), db.FindSellerLinkParams{SellerName: "MegaStore", SellerProductID: uuid})
				return err
			},
			wantErr: errs.ErrNotFound,
		},
		{
			name: "conflict: same seller and id",
			call: func() error {
				_, err := q.InsertSellerLink(t.Context(), db.InsertSellerLinkParams{SellerName: "GardenStore", ProductID: 64, SellerProductID: uuid})
				return err
			},
			wantErr: errs.ErrConflict,
		},
		{
			name: "foreign key: unknown product is neither",
			call: func() error {
				_, err := q.InsertSellerLink(t.Context(), db.InsertSellerLinkParams{SellerName: "MegaStore", ProductID: 99999, SellerProductID: "fk-1"})
				return err
			},
			notErr: []error{errs.ErrConflict, errs.ErrNotFound},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := catalog.MapErr(tt.call())

			require.Error(t, err)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
			for _, e := range tt.notErr {
				assert.NotErrorIs(t, err, e)
			}
		})
	}
	assert.Equal(t, 2, count(t, c, "SELECT count(*) FROM SellerProduct"))
}

func TestMapErr(t *testing.T) {
	other := errors.New("disk on fire")

	tests := []struct {
		name string
		in   error
		want []error // every error that must be in the chain; nil means MapErr returns nil
	}{
		{name: "nil", in: nil},
		{name: "no rows", in: sql.ErrNoRows, want: []error{errs.ErrNotFound, sql.ErrNoRows}},
		{name: "wrapped no rows", in: errors.Join(other, sql.ErrNoRows), want: []error{errs.ErrNotFound, other}},
		{name: "other error passes through", in: other, want: []error{other}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := catalog.MapErr(tt.in)

			if tt.want == nil {
				assert.NoError(t, got)
				return
			}
			for _, w := range tt.want {
				assert.ErrorIs(t, got, w)
			}
			if tt.in == other {
				assert.NotErrorIs(t, got, errs.ErrNotFound)
				assert.NotErrorIs(t, got, errs.ErrConflict)
			}
		})
	}
}

func TestWithTx(t *testing.T) {
	boom := errors.New("boom")

	tests := []struct {
		name      string
		fn        func(t *testing.T) func(q *db.Queries) error
		wantErr   error
		wantPanic bool
		wantRows  int // SellerProduct rows after the call
	}{
		{
			name:     "commit on nil",
			fn:       insertProductAndLink(nil),
			wantRows: 1,
		},
		{
			name:     "rollback on error",
			fn:       insertProductAndLink(boom),
			wantErr:  boom,
			wantRows: 0,
		},
		{
			name: "rollback on panic",
			fn: func(t *testing.T) func(q *db.Queries) error {
				inner := insertProductAndLink(nil)(t)
				return func(q *db.Queries) error {
					require.NoError(t, inner(q))
					panic("kaboom")
				}
			},
			wantPanic: true,
			wantRows:  0,
		},
		{
			name: "rollback on a failing query",
			fn: func(_ *testing.T) func(q *db.Queries) error {
				return func(q *db.Queries) error {
					_, err := q.InsertSellerLink(t.Context(), db.InsertSellerLinkParams{SellerName: "MegaStore", ProductID: 99999, SellerProductID: "fk-1"})
					return err
				}
			},
			wantRows: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := migrated(t)
			call := func() error { return c.WithTx(t.Context(), tt.fn(t)) }

			if tt.wantPanic {
				assert.PanicsWithValue(t, "kaboom", func() { _ = call() })
			} else {
				err := call()
				switch {
				case tt.wantErr != nil:
					assert.ErrorIs(t, err, tt.wantErr)
				case tt.wantRows == 0:
					assert.Error(t, err)
				default:
					assert.NoError(t, err)
				}
			}

			assert.Equal(t, tt.wantRows, count(t, c, "SELECT count(*) FROM SellerProduct"))
			assert.Equal(t, 975+tt.wantRows, count(t, c, "SELECT count(*) FROM Product"))
		})
	}
}

func TestWithTxCancelledContext(t *testing.T) {
	c := migrated(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false

	err := c.WithTx(ctx, func(*db.Queries) error { called = true; return nil })

	assert.ErrorIs(t, err, context.Canceled)
	assert.False(t, called)
}

// insertProductAndLink returns a WithTx body that inserts a product and a link to it, then
// returns ret.
func insertProductAndLink(ret error) func(t *testing.T) func(q *db.Queries) error {
	return func(t *testing.T) func(q *db.Queries) error {
		return func(q *db.Queries) error {
			id, err := q.InsertProduct(t.Context(), db.InsertProductParams{Name: "Roteador WiFi 7"})
			if err != nil {
				return err
			}
			if _, err := q.InsertSellerLink(t.Context(), db.InsertSellerLinkParams{SellerName: "MegaStore", ProductID: id, SellerProductID: uuid}); err != nil {
				return err
			}
			return ret
		}
	}
}

func product(t *testing.T, c *catalog.Catalog, id int64) db.Product {
	t.Helper()
	var p db.Product
	require.NoError(t, c.DB().QueryRowContext(t.Context(),
		"SELECT Id, Name, Brand, Category FROM Product WHERE Id = ?", id).
		Scan(&p.ID, &p.Name, &p.Brand, &p.Category))
	return p
}

func valid(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
