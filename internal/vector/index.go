package vector

import (
	"errors"
	"fmt"
)

// ErrDuplicateID means an id is already in the index.
var ErrDuplicateID = errors.New("duplicate id")

// Index is an exact nearest-neighbour index over unit vectors. Vectors are normalized once on
// Add and stored back to back in one []float32, so a query is a linear scan of dot products.
// The first Add fixes the dimension. An Index is not safe for concurrent use.
type Index struct {
	dim  int
	cap  int // capacity hint in vectors, applied once dim is known
	ids  []int64
	data []float32 // len(ids) * dim values
	seen map[int64]struct{}
}

// NewIndex returns an empty index with room for about capacity vectors (0 or less is fine).
func NewIndex(capacity int) *Index {
	capacity = max(capacity, 0)
	return &Index{
		cap:  capacity,
		ids:  make([]int64, 0, capacity),
		seen: make(map[int64]struct{}, capacity),
	}
}

// Len returns the number of vectors in the index.
func (ix *Index) Len() int { return len(ix.ids) }

// Dim returns the vector dimension, or 0 before the first successful Add.
func (ix *Index) Dim() int { return ix.dim }

// Add normalizes a copy of vec and stores it under id. On error the index is unchanged.
func (ix *Index) Add(id int64, vec []float32) error {
	if _, dup := ix.seen[id]; dup {
		return fmt.Errorf("adding id %d: %w", id, ErrDuplicateID)
	}
	if ix.dim != 0 && len(vec) != ix.dim {
		return fmt.Errorf("adding id %d: got %d dimensions, index has %d: %w",
			id, len(vec), ix.dim, ErrDimensionMismatch)
	}
	n, err := length(vec)
	if err != nil {
		return fmt.Errorf("adding id %d: %w", id, err)
	}
	if ix.dim == 0 {
		ix.dim = len(vec)
		ix.data = make([]float32, 0, ix.cap*ix.dim)
	}
	for _, x := range vec {
		ix.data = append(ix.data, float32(float64(x)/n))
	}
	ix.ids = append(ix.ids, id)
	ix.seen[id] = struct{}{}
	return nil
}

// Nearest returns the id whose vector has the highest cosine similarity with vec, and that
// score in [-1, 1]. Ties go to the vector added first. ok is false only when the index is
// empty. vec is not modified.
func (ix *Index) Nearest(vec []float32) (id int64, score float32, ok bool, err error) {
	if len(ix.ids) == 0 {
		return 0, 0, false, nil
	}
	if len(vec) != ix.dim {
		return 0, 0, false, fmt.Errorf("query has %d dimensions, index has %d: %w",
			len(vec), ix.dim, ErrDimensionMismatch)
	}
	// Normalizing the query (one small copy) keeps every dot product within [-1, 1], so the
	// float32 sums cannot overflow and the best dot product is the cosine.
	q, err := Normalize(vec)
	if err != nil {
		return 0, 0, false, fmt.Errorf("query: %w", err)
	}
	best, bestDot := 0, float32(0)
	for i := range ix.ids {
		d := dot(q, ix.data[i*ix.dim:(i+1)*ix.dim])
		if i == 0 || d > bestDot {
			best, bestDot = i, d
		}
	}
	return ix.ids[best], clamp(bestDot), true, nil
}

func dot(a, b []float32) float32 {
	b = b[:len(a)]
	var s float32
	for i, x := range a {
		s += x * b[i]
	}
	return s
}
