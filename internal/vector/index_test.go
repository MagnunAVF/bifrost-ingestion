package vector_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

type entry struct {
	id  int64
	vec []float32
}

func newIndex(t *testing.T, entries ...entry) *vector.Index {
	t.Helper()
	ix := vector.NewIndex(len(entries))
	for _, e := range entries {
		require.NoError(t, ix.Add(e.id, e.vec))
	}
	return ix
}

func TestNearest(t *testing.T) {
	tests := []struct {
		name      string
		entries   []entry
		query     []float32
		wantID    int64
		wantScore float32
		wantOK    bool
		wantErr   error
	}{
		{name: "empty index", query: []float32{1, 0}, wantOK: false},
		{
			name:    "identical",
			entries: []entry{{7, []float32{1, 2, 3}}},
			query:   []float32{1, 2, 3}, wantID: 7, wantScore: 1, wantOK: true,
		},
		{
			name:    "scaled query is identical",
			entries: []entry{{7, []float32{1, 2, 3}}},
			query:   []float32{10, 20, 30}, wantID: 7, wantScore: 1, wantOK: true,
		},
		{
			name: "huge query does not overflow",
			// In float32 both dot products would overflow to +Inf and tie on the first entry.
			entries: []entry{{6, []float32{1, 1, 0}}, {7, []float32{1, 1, 1}}},
			query:   []float32{3e38, 3e38, 3e38}, wantID: 7, wantScore: 1, wantOK: true,
		},
		{
			name:    "orthogonal",
			entries: []entry{{1, []float32{1, 0}}},
			query:   []float32{0, 5}, wantID: 1, wantScore: 0, wantOK: true,
		},
		{
			name:    "opposite",
			entries: []entry{{1, []float32{1, 2, 3}}},
			query:   []float32{-1, -2, -3}, wantID: 1, wantScore: -1, wantOK: true,
		},
		{
			name: "closest of three",
			entries: []entry{
				{10, []float32{1, 0, 0}},
				{20, []float32{1, 1, 0}},
				{30, []float32{0, 0, 1}},
			},
			query:  []float32{1, 0.9, 0},
			wantID: 20, wantScore: float32((1 + 0.9) / (math.Sqrt2 * math.Sqrt(1+0.81))), wantOK: true,
		},
		{
			name: "tie goes to the earliest insert",
			entries: []entry{
				{5, []float32{1, 0}},
				{3, []float32{0, 1}},
			},
			query:  []float32{1, 1},
			wantID: 5, wantScore: float32(math.Sqrt2 / 2), wantOK: true,
		},
		{
			name:    "query dimension mismatch",
			entries: []entry{{1, []float32{1, 0, 0}}},
			query:   []float32{1, 0}, wantErr: vector.ErrDimensionMismatch,
		},
		{
			name:    "zero query",
			entries: []entry{{1, []float32{1, 0}}},
			query:   []float32{0, 0}, wantErr: vector.ErrZeroVector,
		},
		{
			name:    "NaN query",
			entries: []entry{{1, []float32{1, 0}}},
			query:   []float32{float32(math.NaN()), 1}, wantErr: vector.ErrInvalidVector,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ix := newIndex(t, tt.entries...)
			id, score, ok, err := ix.Nearest(tt.query)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, ok)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantOK, ok)
			if !ok {
				return
			}
			assert.Equal(t, tt.wantID, id)
			assert.InDelta(t, tt.wantScore, score, eps)
		})
	}
}

func TestNearestScoreNeverAboveOne(t *testing.T) {
	// Components whose normalized squares round up in float32.
	vecs := [][]float32{
		{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7},
		{1, 1, 1},
		{1e-3, 7, 1e3, 0.33333334},
		{0.70710677, 0.70710677},
	}
	for _, v := range vecs {
		ix := newIndex(t, entry{1, v})
		_, score, ok, err := ix.Nearest(v)
		require.NoError(t, err)
		require.True(t, ok)
		assert.LessOrEqual(t, score, float32(1))
		assert.InDelta(t, 1, score, eps)
	}
}

func TestAdd(t *testing.T) {
	ix := vector.NewIndex(0)
	assert.Equal(t, 0, ix.Dim())
	assert.Equal(t, 0, ix.Len())

	require.ErrorIs(t, ix.Add(1, []float32{0, 0, 0}), vector.ErrZeroVector)
	assert.Equal(t, 0, ix.Dim(), "a rejected first vector does not fix the dimension")
	assert.Equal(t, 0, ix.Len())

	require.ErrorIs(t, ix.Add(1, nil), vector.ErrZeroVector)
	require.ErrorIs(t, ix.Add(1, []float32{float32(math.Inf(-1)), 1}), vector.ErrInvalidVector)
	assert.Equal(t, 0, ix.Dim())

	require.NoError(t, ix.Add(1, []float32{1, 0, 0}))
	assert.Equal(t, 3, ix.Dim(), "first insert fixes the dimension")
	assert.Equal(t, 1, ix.Len())

	require.ErrorIs(t, ix.Add(2, []float32{1, 0}), vector.ErrDimensionMismatch)
	require.ErrorIs(t, ix.Add(2, []float32{1, 0, 0, 0}), vector.ErrDimensionMismatch)
	assert.Equal(t, 1, ix.Len(), "a mismatched vector is not added")

	require.ErrorIs(t, ix.Add(1, []float32{0, 1, 0}), vector.ErrDuplicateID)
	assert.Equal(t, 1, ix.Len())
	id, score, ok, err := ix.Nearest([]float32{1, 0, 0})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(1), id)
	assert.InDelta(t, 1, score, eps, "a duplicate does not overwrite the stored vector")

	require.ErrorIs(t, ix.Add(2, []float32{0, 0, 0}), vector.ErrZeroVector)
	assert.Equal(t, 1, ix.Len())
	_, _, _, err = ix.Nearest([]float32{0, 1, 0})
	require.NoError(t, err)
	require.NoError(t, ix.Add(2, []float32{0, 1, 0}), "the id of a rejected add is still free")
	assert.Equal(t, 2, ix.Len())
}

func TestAddCopiesTheVector(t *testing.T) {
	v := []float32{1, 0}
	ix := newIndex(t, entry{1, v})
	v[0], v[1] = 0, 1

	_, score, ok, err := ix.Nearest([]float32{1, 0})
	require.NoError(t, err)
	require.True(t, ok)
	assert.InDelta(t, 1, score, eps)
}

func TestNearestDoesNotModifyQuery(t *testing.T) {
	ix := newIndex(t, entry{1, []float32{1, 0}})
	q := []float32{3, 4}
	_, _, _, err := ix.Nearest(q)
	require.NoError(t, err)
	assert.Equal(t, []float32{3, 4}, q)
}

func TestIndexGrowsPastCapacity(t *testing.T) {
	ix := vector.NewIndex(2)
	const n = 10
	for i := range n {
		v := make([]float32, n)
		v[i] = 1
		require.NoError(t, ix.Add(int64(100+i), v))
	}
	require.Equal(t, n, ix.Len())
	for i := range n {
		q := make([]float32, n)
		q[i] = 2
		id, score, ok, err := ix.Nearest(q)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, int64(100+i), id)
		assert.InDelta(t, 1, score, eps)
	}
}

func TestNewIndexNegativeCapacity(t *testing.T) {
	ix := vector.NewIndex(-5)
	require.NoError(t, ix.Add(1, []float32{1}))
	assert.Equal(t, 1, ix.Len())
}
