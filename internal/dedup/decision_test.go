package dedup_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

func TestDecide(t *testing.T) {
	p := dedup.Policy{Threshold: 0.9}
	tests := []struct {
		name  string
		score float32
		found bool
		want  dedup.Kind
	}{
		{name: "above threshold", score: 0.95, found: true, want: dedup.KindDuplicate},
		{name: "exactly at threshold", score: 0.9, found: true, want: dedup.KindDuplicate},
		{name: "just below", score: math.Nextafter32(0.9, 0), found: true, want: dedup.KindNovel},
		{name: "opposite", score: -1, found: true, want: dedup.KindNovel},
		{name: "empty index", score: 0, found: false, want: dedup.KindNovel},
		{name: "empty index ignores a high score", score: 1, found: false, want: dedup.KindNovel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.Decide(tt.score, tt.found))
		})
	}
}

func TestPolicyValidate(t *testing.T) {
	tests := []struct {
		threshold float32
		ok        bool
	}{
		{threshold: 1, ok: true},
		{threshold: 0.85, ok: true},
		{threshold: math.SmallestNonzeroFloat32, ok: true},
		{threshold: 0},
		{threshold: -0.1},
		{threshold: 1.01},
		{threshold: float32(math.NaN())},
		{threshold: float32(math.Inf(1))},
	}
	for _, tt := range tests {
		err := dedup.Policy{Threshold: tt.threshold}.Validate()
		if tt.ok {
			assert.NoError(t, err, "threshold %v", tt.threshold)
		} else {
			assert.ErrorIs(t, err, errs.ErrInvalidInput, "threshold %v", tt.threshold)
		}
	}
}

func TestKindString(t *testing.T) {
	assert.Equal(t, "novel", dedup.KindNovel.String())
	assert.Equal(t, "duplicate", dedup.KindDuplicate.String())
	assert.Equal(t, "Kind(9)", dedup.Kind(9).String())
	assert.Equal(t, "Kind(0)", dedup.Kind(0).String(), "zero value is not a decision")
}

func TestParseUpdateMode(t *testing.T) {
	tests := []struct {
		in   string
		want dedup.UpdateMode
		ok   bool
	}{
		{in: "fill", want: dedup.UpdateFill, ok: true},
		{in: "none", want: dedup.UpdateNone, ok: true},
		{in: "overwrite", want: dedup.UpdateOverwrite, ok: true},
		{in: ""},
		{in: "FILL"},
		{in: "bogus"},
	}
	for _, tt := range tests {
		got, err := dedup.ParseUpdateMode(tt.in)
		if !tt.ok {
			assert.ErrorIs(t, err, errs.ErrInvalidInput, "%q", tt.in)
			continue
		}
		require.NoError(t, err, "%q", tt.in)
		assert.Equal(t, tt.want, got)
		assert.Equal(t, tt.in, got.String(), "round trip")
	}
	var zero dedup.UpdateMode
	assert.Equal(t, dedup.UpdateFill, zero, "fill is the default")
}
