package embed_test

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
)

var _ embed.Embedder = (*embed.Fake)(nil)

func TestFakeEmbed(t *testing.T) {
	tests := []struct {
		name  string
		dim   int
		texts []string
	}{
		{name: "single text", dim: 8, texts: []string{"Router WiFi 6 TP-Link"}},
		{name: "several texts", dim: 16, texts: []string{"a", "b", "Câmera Canon EOS R6", "a"}},
		{name: "dimension one", dim: 1, texts: []string{"x"}},
		{name: "large dimension", dim: 768, texts: []string{"Smartphone Galaxy S23 | brand: Samsung"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := embed.NewFake(tt.dim).Embed(t.Context(), tt.texts)
			require.NoError(t, err)
			require.Len(t, got, len(tt.texts))
			for i, v := range got {
				require.Len(t, v, tt.dim, "vector %d", i)
				var norm float64
				for _, x := range v {
					require.False(t, math.IsNaN(float64(x)) || math.IsInf(float64(x), 0))
					assert.GreaterOrEqual(t, x, float32(-1))
					assert.Less(t, x, float32(1))
					norm += float64(x) * float64(x)
				}
				assert.Positive(t, norm, "vector %d is zero", i)
			}
		})
	}
}

func TestFakeIsDeterministic(t *testing.T) {
	texts := []string{"Router WiFi 6 TP-Link", "Roteador WiFi 6 TP-Link"}

	first, err := embed.NewFake(32).Embed(t.Context(), texts)
	require.NoError(t, err)
	again, err := embed.NewFake(32).Embed(t.Context(), texts)
	require.NoError(t, err)
	assert.Equal(t, first, again, "a new instance gives the same vectors")

	f := embed.NewFake(32)
	one, err := f.Embed(t.Context(), texts[:1])
	require.NoError(t, err)
	two, err := f.Embed(t.Context(), texts[:1])
	require.NoError(t, err)
	assert.Equal(t, one, two, "repeated calls give the same vector")
	assert.Equal(t, first[0], one[0], "a vector does not depend on its batch")

	assert.NotEqual(t, first[0], first[1], "different texts give different vectors")
}

func TestFakeEmptyInput(t *testing.T) {
	for _, texts := range [][]string{nil, {}} {
		got, err := embed.NewFake(4).Embed(t.Context(), texts)
		require.NoError(t, err)
		assert.Empty(t, got)
	}
}

func TestFakeCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := embed.NewFake(4).Embed(ctx, []string{"a"})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
}

func TestNewFakeInvalidDim(t *testing.T) {
	for _, dim := range []int{0, -1} {
		assert.Panics(t, func() { embed.NewFake(dim) }, "dim %d", dim)
	}
}
