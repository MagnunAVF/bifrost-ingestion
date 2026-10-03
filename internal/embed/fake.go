package embed

import (
	"context"
	"fmt"
	"hash/fnv"
)

// Fake is a deterministic Embedder for tests. A text's vector depends only on the text and Dim:
// FNV-1a of the text seeds a splitmix64 generator that fills Dim values in [-1, 1). Similar
// texts do not get similar vectors.
type Fake struct {
	Dim int
}

var _ Embedder = (*Fake)(nil)

// NewFake returns a Fake producing vectors of length dim. It panics when dim <= 0.
func NewFake(dim int) *Fake {
	if dim <= 0 {
		panic(fmt.Sprintf("embed.NewFake: dim must be positive, got %d", dim))
	}
	return &Fake{Dim: dim}
}

// Embed returns one vector per text, or ctx.Err() when ctx is already done.
func (f *Fake) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.vector(t)
	}
	return out, nil
}

func (f *Fake) vector(text string) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	state := h.Sum64()

	v := make([]float32, f.Dim)
	zero := true
	for i := range v {
		state += 0x9e3779b97f4a7c15
		z := state
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		z ^= z >> 31
		// The top 24 bits fit a float32 mantissa exactly: u in [0, 1), so 2u-1 in [-1, 1).
		u := float32(z>>40) / (1 << 24)
		v[i] = 2*u - 1
		zero = zero && v[i] == 0
	}
	if zero {
		v[0] = 0.5
	}
	return v
}
