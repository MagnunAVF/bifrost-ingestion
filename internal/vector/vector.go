package vector

import (
	"errors"
	"fmt"
	"math"
)

var (
	// ErrZeroVector means a vector is empty or all zeros, so it has no direction.
	ErrZeroVector = errors.New("zero or empty vector")
	// ErrInvalidVector means a vector has a NaN or infinite component.
	ErrInvalidVector = errors.New("vector has NaN or Inf")
	// ErrDimensionMismatch means two vectors (or a vector and an index) differ in length.
	ErrDimensionMismatch = errors.New("dimension mismatch")
)

// Normalize returns a unit-length copy of v; v itself is never modified.
func Normalize(v []float32) ([]float32, error) {
	n, err := length(v)
	if err != nil {
		return nil, err
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out, nil
}

// Cosine returns the cosine similarity of two non-zero vectors of the same length, clamped to
// [-1, 1] so rounding never produces a score above 1.
func Cosine(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("cosine of %d and %d dimensions: %w", len(a), len(b), ErrDimensionMismatch)
	}
	na, err := length(a)
	if err != nil {
		return 0, err
	}
	nb, err := length(b)
	if err != nil {
		return 0, err
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return clamp(float32(dot / (na * nb))), nil
}

// length returns the Euclidean norm of v. It accumulates in float64, where the square of any
// finite float32 neither overflows nor underflows to zero.
func length(v []float32) (float64, error) {
	var sum float64
	for i, x := range v {
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("component %d is %v: %w", i, x, ErrInvalidVector)
		}
		sum += f * f
	}
	if sum == 0 {
		return 0, ErrZeroVector
	}
	return math.Sqrt(sum), nil
}

func clamp(s float32) float32 {
	return min(max(s, -1), 1)
}
