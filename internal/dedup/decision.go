package dedup

import (
	"fmt"
	"math"

	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// Kind is the decision for one entry. Callers must switch with a default branch: M2 adds
// KindAmbiguous (a band below Threshold) without changing the existing values. The zero value
// is not a decision.
type Kind int

const (
	// KindNovel means no catalog product is close enough: insert a new Product and link it.
	KindNovel Kind = iota + 1
	// KindDuplicate means the nearest catalog product is the same product: link to it.
	KindDuplicate
)

func (k Kind) String() string {
	switch k {
	case KindNovel:
		return "novel"
	case KindDuplicate:
		return "duplicate"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// Policy turns a nearest-neighbour score into a Kind. M2 adds fields (e.g. an ambiguous floor);
// the zero value of a new field must keep today's behaviour.
type Policy struct {
	Threshold float32 // score >= Threshold is a duplicate
}

// Validate reports an error wrapping errs.ErrInvalidInput unless Threshold is in (0, 1].
func (p Policy) Validate() error {
	t := float64(p.Threshold)
	if math.IsNaN(t) || t <= 0 || t > 1 {
		return fmt.Errorf("threshold %v must be in (0, 1]: %w", p.Threshold, errs.ErrInvalidInput)
	}
	return nil
}

// Decide returns KindDuplicate when found and score >= Threshold, else KindNovel. found is
// false when the index is empty.
func (p Policy) Decide(score float32, found bool) Kind {
	if found && score >= p.Threshold {
		return KindDuplicate
	}
	return KindNovel
}
