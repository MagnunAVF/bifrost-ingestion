package dedup

import (
	"fmt"
	"strings"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// UpdateMode says what a matched entry may change on its catalog Product. Name is never
// changed in any mode: it carries the matching signal.
type UpdateMode int

const (
	// UpdateFill sets Brand or Category only where the Product has NULL (the default).
	UpdateFill UpdateMode = iota
	// UpdateNone links only; differences are reported as notes.
	UpdateNone
	// UpdateOverwrite also replaces a differing value: the first record in a run wins, and a
	// Category must already be one of the catalog's categories.
	UpdateOverwrite
)

var updateModeNames = [...]string{UpdateFill: "fill", UpdateNone: "none", UpdateOverwrite: "overwrite"}

func (m UpdateMode) String() string {
	if m >= 0 && int(m) < len(updateModeNames) {
		return updateModeNames[m]
	}
	return fmt.Sprintf("UpdateMode(%d)", int(m))
}

// ParseUpdateMode parses "fill", "none" or "overwrite"; anything else wraps errs.ErrInvalidInput.
func ParseUpdateMode(s string) (UpdateMode, error) {
	for m, name := range updateModeNames {
		if s == name {
			return UpdateMode(m), nil
		}
	}
	return 0, fmt.Errorf("update mode %q: want fill, none or overwrite: %w", s, errs.ErrInvalidInput)
}

// Change is one attribute update, kept for the report; Old "" means it was NULL.
type Change struct {
	Field string
	Old   string
	New   string
}

// planUpdate decides which of rec's Brand and Category may be written to product under mode.
// categories is the catalog's category vocabulary; updatedBy is the index of the record that
// already changed this product in the run, or -1. It returns nil when nothing changes, the
// changes for the report, and notes for differences it left alone.
func planUpdate(mode UpdateMode, product catalog.Product, rec ingest.Record,
	categories map[string]struct{}, updatedBy int,
) (*catalog.AttrUpdate, []Change, []string) {
	var (
		upd     *catalog.AttrUpdate
		changes []Change
		notes   []string
	)
	field := func(name, cur, next string, set func(u *catalog.AttrUpdate, v *string)) {
		if next == "" || strings.EqualFold(cur, next) {
			return
		}
		note := func(format string, args ...any) { notes = append(notes, fmt.Sprintf(name+format, args...)) }
		switch {
		case len(rec.Flags) > 0:
			note(": flagged input, not applied")
			return
		case mode == UpdateNone && cur == "":
			note(" missing in catalog: %s (entry), kept", next)
			return
		case cur != "" && mode != UpdateOverwrite:
			note(" differs: %s (entry) vs %s (catalog), kept", next, cur)
			return
		case cur != "" && updatedBy >= 0:
			note(": already updated by record %d, kept %s", updatedBy, cur)
			return
		case cur != "" && name == "category" && !inVocabulary(categories, next):
			note(": %s is not a catalog category, kept %s", next, cur)
			return
		}
		if upd == nil {
			upd = &catalog.AttrUpdate{ProductID: product.ID}
		}
		v := next
		set(upd, &v)
		changes = append(changes, Change{Field: name, Old: cur, New: next})
	}
	field("brand", product.Brand, rec.Brand, func(u *catalog.AttrUpdate, v *string) { u.Brand = v })
	field("category", product.Category, rec.Category, func(u *catalog.AttrUpdate, v *string) { u.Category = v })
	return upd, changes, notes
}

func inVocabulary(categories map[string]struct{}, c string) bool {
	if _, ok := categories[c]; ok {
		return true
	}
	for k := range categories {
		if strings.EqualFold(k, c) {
			return true
		}
	}
	return false
}
