package vector

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidBatchSize means Build was called with a batch size of zero or less.
var ErrInvalidBatchSize = errors.New("batch size must be positive")

// Embedder turns texts into vectors, one per text and in input order. embed.Embedder has the
// same shape; this package declares its own so it stays standard library only.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Item is one text to index under an id: a catalog Product.Id and its identity string.
type Item struct {
	ID   int64
	Text string
}

// Build embeds items batchSize texts at a time and adds them to a new Index in input order (the
// catalog cold start). It checks ctx before every batch and returns no index on error.
func Build(ctx context.Context, items []Item, e Embedder, batchSize int) (*Index, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("building index with batch size %d: %w", batchSize, ErrInvalidBatchSize)
	}
	ix := NewIndex(len(items))
	texts := make([]string, 0, min(batchSize, len(items)))
	for start := 0; start < len(items); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("building index: %w", err)
		}
		batch := items[start:min(start+batchSize, len(items))]
		texts = texts[:0]
		for _, it := range batch {
			texts = append(texts, it.Text)
		}
		vecs, err := e.Embed(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("embedding items %d-%d of %d: %w",
				start+1, start+len(batch), len(items), err)
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf("embedding items %d-%d of %d: embedder returned %d vectors for %d texts",
				start+1, start+len(batch), len(items), len(vecs), len(batch))
		}
		for i, it := range batch {
			if err := ix.Add(it.ID, vecs[i]); err != nil {
				return nil, fmt.Errorf("building index: %w", err)
			}
		}
	}
	return ix, nil
}
