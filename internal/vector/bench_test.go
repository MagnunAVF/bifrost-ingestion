package vector_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

func randomVector(r *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = r.Float32()*2 - 1
	}
	return v
}

// BenchmarkNearest measures one exact query against n vectors of 768 dimensions (the
// nomic-embed-text size). index-MB is the vector slab plus the ids: n * (768*4 + 8) bytes.
func BenchmarkNearest(b *testing.B) {
	const dim = 768
	for _, n := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d/d=%d", n, dim), func(b *testing.B) {
			r := rand.New(rand.NewPCG(1, uint64(n))) //nolint:gosec // seeded, reproducible benchmark data
			ix := vector.NewIndex(n)
			for i := range n {
				if err := ix.Add(int64(i+1), randomVector(r, dim)); err != nil {
					b.Fatal(err)
				}
			}
			q := randomVector(r, dim)

			for b.Loop() {
				if _, _, ok, err := ix.Nearest(q); err != nil || !ok {
					b.Fatalf("Nearest: ok=%v err=%v", ok, err)
				}
			}
			b.ReportMetric(float64(n*(dim*4+8))/1e6, "index-MB")
		})
	}
}
