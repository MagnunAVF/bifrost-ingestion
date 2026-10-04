package vector_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

var (
	_ vector.Embedder = (*embed.Fake)(nil)
	_ vector.Embedder = (*embed.Ollama)(nil)
)

// stub records each call and answers through fn (call is 0-based).
type stub struct {
	calls [][]string
	fn    func(call int, texts []string) ([][]float32, error)
}

func (s *stub) Embed(_ context.Context, texts []string) ([][]float32, error) {
	s.calls = append(s.calls, append([]string(nil), texts...))
	return s.fn(len(s.calls)-1, texts)
}

// unit returns one distinct 3-dimensional vector per text.
func unit(_ int, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, float32(i), 0}
	}
	return out, nil
}

func items(texts ...string) []vector.Item {
	out := make([]vector.Item, len(texts))
	for i, t := range texts {
		out[i] = vector.Item{ID: int64(i + 1), Text: t}
	}
	return out
}

func TestBuildWithFake(t *testing.T) {
	its := items(
		"Router WiFi 6 TP-Link | brand: TP-Link | category: Networking",
		"Processor AMD Ryzen 9 7950X | brand: AMD | category: Computers",
		"Cable Organizer Kit | category: Accessories",
		"Smartphone Galaxy S23 | brand: Samsung | category: Electronics",
		"Basketball Spalding Official | brand: Spalding | category: Sports",
	)
	fake := embed.NewFake(16)
	s := &stub{fn: func(_ int, texts []string) ([][]float32, error) {
		return fake.Embed(context.Background(), texts)
	}}

	ix, err := vector.Build(t.Context(), its, s, 2)
	require.NoError(t, err)
	require.Equal(t, 5, ix.Len())
	assert.Equal(t, 16, ix.Dim())
	assert.Equal(t, [][]string{
		{its[0].Text, its[1].Text},
		{its[2].Text, its[3].Text},
		{its[4].Text},
	}, s.calls, "batches of 2/2/1, in input order")

	for _, it := range its {
		q, err := fake.Embed(t.Context(), []string{it.Text})
		require.NoError(t, err)
		id, score, ok, err := ix.Nearest(q[0])
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, it.ID, id, it.Text)
		assert.InDelta(t, 1, score, eps)
	}
}

func TestBuildBatchLargerThanItems(t *testing.T) {
	s := &stub{fn: unit}
	ix, err := vector.Build(t.Context(), items("a", "b", "c"), s, 64)
	require.NoError(t, err)
	assert.Equal(t, 3, ix.Len())
	assert.Len(t, s.calls, 1)
}

func TestBuildNoItems(t *testing.T) {
	for _, its := range [][]vector.Item{nil, {}} {
		s := &stub{fn: unit}
		ix, err := vector.Build(t.Context(), its, s, 2)
		require.NoError(t, err)
		require.NotNil(t, ix)
		assert.Equal(t, 0, ix.Len())
		assert.Empty(t, s.calls, "no Embed call for an empty catalog")
	}
}

func TestBuildErrors(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name      string
		items     []vector.Item
		batchSize int
		fn        func(call int, texts []string) ([][]float32, error)
		wantErr   error  // checked with errors.Is when set
		wantMsg   string // substring of the error message when set
		wantCalls int
	}{
		{
			name: "batch size zero", items: items("a"), batchSize: 0, fn: unit,
			wantErr: vector.ErrInvalidBatchSize, wantCalls: 0,
		},
		{
			name: "batch size negative", items: items("a"), batchSize: -1, fn: unit,
			wantErr: vector.ErrInvalidBatchSize, wantCalls: 0,
		},
		{
			name: "embedder fails on the second batch", items: items("a", "b", "c", "d", "e"), batchSize: 2,
			fn: func(call int, texts []string) ([][]float32, error) {
				if call == 1 {
					return nil, errBoom
				}
				return unit(call, texts)
			},
			wantErr: errBoom, wantMsg: "items 3-4 of 5", wantCalls: 2,
		},
		{
			name: "too few vectors", items: items("a", "b"), batchSize: 2,
			fn: func(_ int, _ []string) ([][]float32, error) {
				return [][]float32{{1, 0, 0}}, nil
			},
			wantMsg: "1 vectors for 2 texts", wantCalls: 1,
		},
		{
			name: "too many vectors", items: items("a"), batchSize: 2,
			fn: func(_ int, _ []string) ([][]float32, error) {
				return [][]float32{{1, 0, 0}, {0, 1, 0}}, nil
			},
			wantMsg: "2 vectors for 1 texts", wantCalls: 1,
		},
		{
			name: "zero vector names the item", items: []vector.Item{{ID: 21, Text: "a"}, {ID: 42, Text: "b"}},
			batchSize: 2,
			fn: func(_ int, _ []string) ([][]float32, error) {
				return [][]float32{{1, 0}, {0, 0}}, nil
			},
			wantErr: vector.ErrZeroVector, wantMsg: "id 42", wantCalls: 1,
		},
		{
			name: "dimension changes between batches", items: items("a", "b"), batchSize: 1,
			fn: func(call int, _ []string) ([][]float32, error) {
				if call == 0 {
					return [][]float32{{1, 0, 0}}, nil
				}
				return [][]float32{{1, 0}}, nil
			},
			wantErr: vector.ErrDimensionMismatch, wantCalls: 2,
		},
		{
			name:  "duplicate item ids",
			items: []vector.Item{{ID: 1, Text: "a"}, {ID: 2, Text: "b"}, {ID: 1, Text: "c"}}, batchSize: 2,
			fn: unit, wantErr: vector.ErrDuplicateID, wantMsg: "id 1", wantCalls: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stub{fn: tt.fn}
			ix, err := vector.Build(t.Context(), tt.items, s, tt.batchSize)
			require.Error(t, err)
			assert.Nil(t, ix)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}
			if tt.wantMsg != "" {
				assert.ErrorContains(t, err, tt.wantMsg)
			}
			assert.Len(t, s.calls, tt.wantCalls)
		})
	}
}

func TestBuildContextCancelled(t *testing.T) {
	t.Run("before the first batch", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		s := &stub{fn: unit}
		ix, err := vector.Build(ctx, items("a", "b"), s, 1)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, ix)
		assert.Empty(t, s.calls)
	})
	t.Run("between batches", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s := &stub{fn: func(call int, texts []string) ([][]float32, error) {
			cancel()
			return unit(call, texts)
		}}
		ix, err := vector.Build(ctx, items("a", "b", "c"), s, 1)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, ix)
		assert.Len(t, s.calls, 1, "no Embed call after the context is done")
	})
}
