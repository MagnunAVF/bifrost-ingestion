package embed

import "context"

// Embedder turns texts into vectors. It returns one vector per text, in input order, and every
// vector has the same length. An empty input returns an empty result without doing any work.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
