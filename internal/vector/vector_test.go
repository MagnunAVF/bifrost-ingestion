package vector_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

const eps = 1e-6

func norm(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}
	return math.Sqrt(s)
}

func TestNormalize(t *testing.T) {
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	tests := []struct {
		name    string
		in      []float32
		want    []float32 // nil: only check unit length
		wantErr error
	}{
		{name: "3-4-5", in: []float32{3, 4}, want: []float32{0.6, 0.8}},
		{name: "already unit", in: []float32{0, 1, 0}, want: []float32{0, 1, 0}},
		{name: "negative values", in: []float32{-3, 0, 4}, want: []float32{-0.6, 0, 0.8}},
		{name: "large values do not overflow", in: []float32{1e20, 1e20, 1e20}},
		{name: "tiny values do not underflow", in: []float32{1e-30, 1e-30}},
		{name: "all zeros", in: []float32{0, 0, 0}, wantErr: vector.ErrZeroVector},
		{name: "empty", in: []float32{}, wantErr: vector.ErrZeroVector},
		{name: "nil", in: nil, wantErr: vector.ErrZeroVector},
		{name: "NaN", in: []float32{1, nan}, wantErr: vector.ErrInvalidVector},
		{name: "+Inf", in: []float32{inf, 1}, wantErr: vector.ErrInvalidVector},
		{name: "-Inf", in: []float32{-inf, 1}, wantErr: vector.ErrInvalidVector},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := append([]float32(nil), tt.in...)
			got, err := vector.Normalize(tt.in)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, len(tt.in))
			assert.InDelta(t, 1, norm(got), eps)
			for i, x := range got {
				require.False(t, math.IsNaN(float64(x)) || math.IsInf(float64(x), 0), "component %d", i)
			}
			if tt.want != nil {
				assert.InDeltaSlice(t, tt.want, got, eps)
			}
			assert.Equal(t, orig, tt.in, "input must not be modified")
		})
	}
}

func TestNormalizeReturnsCopy(t *testing.T) {
	in := []float32{0, 1}
	got, err := vector.Normalize(in)
	require.NoError(t, err)
	got[0] = 42
	assert.Equal(t, []float32{0, 1}, in)
}

func TestCosine(t *testing.T) {
	v := []float32{1, 2, 3}
	tests := []struct {
		name    string
		a, b    []float32
		want    float32
		wantErr error
	}{
		{name: "identical", a: v, b: v, want: 1},
		{name: "same direction, different length", a: v, b: []float32{3, 6, 9}, want: 1},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 0},
		{name: "opposite", a: v, b: []float32{-1, -2, -3}, want: -1},
		{name: "45 degrees", a: []float32{1, 0}, b: []float32{1, 1}, want: float32(math.Sqrt2 / 2)},
		{name: "dimension mismatch", a: []float32{1, 2}, b: v, wantErr: vector.ErrDimensionMismatch},
		{name: "zero on the left", a: []float32{0, 0, 0}, b: v, wantErr: vector.ErrZeroVector},
		{name: "zero on the right", a: v, b: []float32{0, 0, 0}, wantErr: vector.ErrZeroVector},
		{name: "both empty", a: nil, b: nil, wantErr: vector.ErrZeroVector},
		{name: "NaN", a: []float32{float32(math.NaN()), 1}, b: []float32{1, 1}, wantErr: vector.ErrInvalidVector},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vector.Cosine(tt.a, tt.b)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, eps)
			assert.LessOrEqual(t, got, float32(1))
			assert.GreaterOrEqual(t, got, float32(-1))
		})
	}
}
