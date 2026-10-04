package catalog

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog/db"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// The methods in this file are the store the dedup pipeline uses. They take and return plain
// Go types ("" for NULL), so callers never need database/sql, and each write is one transaction.

// Product is a catalog row; Brand and Category are "" for NULL.
type Product struct {
	ID       int64
	Name     string
	Brand    string
	Category string
}

// NewProduct is a Product to insert; a "" Brand or Category is stored as NULL.
type NewProduct struct {
	Name     string
	Brand    string
	Category string
}

// SellerLink is one SellerProduct row to insert.
type SellerLink struct {
	SellerName      string
	SellerProductID string
	ProductID       int64
}

// AttrUpdate sets Brand and/or Category of one Product; a nil field is left alone. There is no
// Name field: a product's name is never updated by ingestion.
type AttrUpdate struct {
	ProductID int64
	Brand     *string
	Category  *string
}

// ListProducts returns every Product ordered by Id.
func (c *Catalog) ListProducts(ctx context.Context) ([]Product, error) {
	rows, err := c.q.ListProducts(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing products: %w", err)
	}
	out := make([]Product, len(rows))
	for i, r := range rows {
		out[i] = Product{ID: r.ID, Name: r.Name, Brand: r.Brand.String, Category: r.Category.String}
	}
	return out, nil
}

// FindSellerLink returns the Product.Id linked to (seller, sellerProductID), or an error
// wrapping errs.ErrNotFound.
func (c *Catalog) FindSellerLink(ctx context.Context, seller, sellerProductID string) (int64, error) {
	row, err := c.q.FindSellerLink(ctx, db.FindSellerLinkParams{SellerName: seller, SellerProductID: sellerProductID})
	if err != nil {
		return 0, fmt.Errorf("finding seller link: %w", MapErr(err))
	}
	return row.ProductID, nil
}

// LinkSeller inserts link (when non-nil) and applies upd (when non-nil) in one transaction. A
// duplicate (SellerName, SellerProductID) wraps errs.ErrConflict, an update of a missing Product
// wraps errs.ErrNotFound, and on any error nothing is written.
func (c *Catalog) LinkSeller(ctx context.Context, link *SellerLink, upd *AttrUpdate) error {
	err := c.WithTx(ctx, func(q *db.Queries) error {
		if link != nil {
			if err := insertLink(ctx, q, *link); err != nil {
				return err
			}
		}
		if upd != nil {
			return updateAttributes(ctx, q, *upd)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("linking seller: %w", err)
	}
	return nil
}

// InsertProductAndLink inserts p and its SellerProduct row in one transaction and returns the
// new Product.Id. On any error neither row exists.
func (c *Catalog) InsertProductAndLink(ctx context.Context, p NewProduct, seller, sellerProductID string) (int64, error) {
	var id int64
	err := c.WithTx(ctx, func(q *db.Queries) error {
		var err error
		id, err = q.InsertProduct(ctx, db.InsertProductParams{
			Name: p.Name, Brand: nullable(p.Brand), Category: nullable(p.Category),
		})
		if err != nil {
			return fmt.Errorf("inserting product: %w", MapErr(err))
		}
		return insertLink(ctx, q, SellerLink{SellerName: seller, SellerProductID: sellerProductID, ProductID: id})
	})
	if err != nil {
		return 0, fmt.Errorf("inserting product and link: %w", err)
	}
	return id, nil
}

func insertLink(ctx context.Context, q *db.Queries, l SellerLink) error {
	if _, err := q.InsertSellerLink(ctx, db.InsertSellerLinkParams{
		SellerName: l.SellerName, ProductID: l.ProductID, SellerProductID: l.SellerProductID,
	}); err != nil {
		return fmt.Errorf("inserting seller link: %w", MapErr(err))
	}
	return nil
}

func updateAttributes(ctx context.Context, q *db.Queries, u AttrUpdate) error {
	n, err := q.UpdateProductAttributes(ctx, db.UpdateProductAttributesParams{
		Brand: nullablePtr(u.Brand), Category: nullablePtr(u.Category), ID: u.ProductID,
	})
	if err != nil {
		return fmt.Errorf("updating product %d: %w", u.ProductID, MapErr(err))
	}
	if n != 1 {
		return fmt.Errorf("updating product %d: %w", u.ProductID, errs.ErrNotFound)
	}
	return nil
}

func nullable(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func nullablePtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return nullable(*s)
}
