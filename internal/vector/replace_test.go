package vector_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

func TestReplace(t *testing.T) {
	ix := newIndex(t, entry{1, []float32{1, 0, 0}}, entry{2, []float32{0, 1, 0}})

	require.NoError(t, ix.Replace(1, []float32{0, 0, 3}))

	assert.Equal(t, 2, ix.Len())
	id, score, ok, err := ix.Nearest([]float32{0, 0, 1})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(1), id)
	assert.InDelta(t, 1, score, 1e-6)

	id, score, _, err = ix.Nearest([]float32{1, 0, 0})
	require.NoError(t, err)
	assert.Less(t, score, float32(0.5), "the old vector is gone (best is %d)", id)
}

func TestReplaceErrors(t *testing.T) {
	tests := []struct {
		name    string
		id      int64
		vec     []float32
		wantErr error
	}{
		{name: "unknown id", id: 9, vec: []float32{1, 0}, wantErr: vector.ErrNotFound},
		{name: "wrong dimension", id: 1, vec: []float32{1, 0, 0}, wantErr: vector.ErrDimensionMismatch},
		{name: "zero vector", id: 1, vec: []float32{0, 0}, wantErr: vector.ErrZeroVector},
		{name: "NaN", id: 1, vec: []float32{float32(math.NaN()), 1}, wantErr: vector.ErrInvalidVector},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := newIndex(t, entry{1, []float32{1, 0}}, entry{2, []float32{0, 1}})

			err := ix.Replace(tt.id, tt.vec)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, 2, ix.Len())
			id, score, _, err := ix.Nearest([]float32{1, 0})
			require.NoError(t, err)
			assert.Equal(t, int64(1), id, "old vector still there")
			assert.InDelta(t, 1, score, 1e-6)
		})
	}
}

func TestReplaceKeepsInsertionOrder(t *testing.T) {
	ix := newIndex(t, entry{1, []float32{0, 1}}, entry{2, []float32{1, 0}})

	require.NoError(t, ix.Replace(1, []float32{1, 0}))

	id, _, _, err := ix.Nearest([]float32{1, 0})
	require.NoError(t, err)
	assert.Equal(t, int64(1), id, "ties still go to the earlier-added id")
}

func TestReplaceCopiesTheVector(t *testing.T) {
	ix := newIndex(t, entry{1, []float32{1, 0}})
	v := []float32{0, 1}
	require.NoError(t, ix.Replace(1, v))

	v[0], v[1] = 1, 0

	_, score, _, err := ix.Nearest([]float32{0, 1})
	require.NoError(t, err)
	assert.InDelta(t, 1, score, 1e-6)
}
