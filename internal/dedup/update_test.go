package dedup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

func TestPlanUpdate(t *testing.T) {
	str := func(s string) *string { return &s }
	categories := map[string]struct{}{"Photography": {}, "Kitchen": {}, "Outdoor": {}, "Clothing": {}}
	sqlFlag := []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonSQLLike}}

	tests := []struct {
		name        string
		mode        dedup.UpdateMode
		product     catalog.Product
		rec         ingest.Record
		updatedBy   int // record index that already changed the product in this run, -1 none
		wantUpd     *catalog.AttrUpdate
		wantChanges []dedup.Change
		wantNotes   []string
	}{
		{
			name:        "fill sets a NULL brand",
			product:     catalog.Product{ID: 113, Name: "Cable Organizer Kit", Category: "Accessories"},
			rec:         ingest.Record{Index: 4, Name: "Cable Organizer Kit", Brand: "Acme", Category: "Accessories"},
			wantUpd:     &catalog.AttrUpdate{ProductID: 113, Brand: str("Acme")},
			wantChanges: []dedup.Change{{Field: "brand", Old: "", New: "Acme"}},
		},
		{
			name:        "fill sets a NULL category and brand together",
			product:     catalog.Product{ID: 7, Name: "Thing"},
			rec:         ingest.Record{Name: "Thing", Brand: "Acme", Category: "Kitchen"},
			wantUpd:     &catalog.AttrUpdate{ProductID: 7, Brand: str("Acme"), Category: str("Kitchen")},
			wantChanges: []dedup.Change{{Field: "brand", New: "Acme"}, {Field: "category", New: "Kitchen"}},
		},
		{
			name:      "fill keeps a differing brand",
			product:   catalog.Product{ID: 322, Name: "Jeans 501", Brand: "Levis", Category: "Clothing"},
			rec:       ingest.Record{Name: "Jeans 501", Brand: "Levi's", Category: "Clothing"},
			wantNotes: []string{"brand differs: Levi's (entry) vs Levis (catalog), kept"},
		},
		{
			name:    "fill ignores a blank entry brand",
			product: catalog.Product{ID: 113, Name: "Cable Organizer Kit"},
			rec:     ingest.Record{Name: "Cable Organizer Kit"},
		},
		{
			name:      "none never changes, reports the fill it skipped",
			mode:      dedup.UpdateNone,
			product:   catalog.Product{ID: 113, Name: "Cable Organizer Kit"},
			rec:       ingest.Record{Name: "Cable Organizer Kit", Brand: "Acme"},
			wantNotes: []string{"brand missing in catalog: Acme (entry), kept"},
		},
		{
			name:        "overwrite replaces a differing brand",
			mode:        dedup.UpdateOverwrite,
			product:     catalog.Product{ID: 322, Name: "Jeans 501", Brand: "Levis", Category: "Clothing"},
			rec:         ingest.Record{Name: "Jeans 501", Brand: "Levi's", Category: "Clothing"},
			wantUpd:     &catalog.AttrUpdate{ProductID: 322, Brand: str("Levi's")},
			wantChanges: []dedup.Change{{Field: "brand", Old: "Levis", New: "Levi's"}},
		},
		{
			name:      "overwrite refuses a category outside the catalog vocabulary",
			mode:      dedup.UpdateOverwrite,
			product:   catalog.Product{ID: 50, Name: "Camera Canon EOS R6", Brand: "Canon", Category: "Photography"},
			rec:       ingest.Record{Name: "Camera Canon EOS R6", Brand: "Canon", Category: "Photo"},
			wantNotes: []string{"category: Photo is not a catalog category, kept Photography"},
		},
		{
			name:        "overwrite accepts a catalog category",
			mode:        dedup.UpdateOverwrite,
			product:     catalog.Product{ID: 60, Name: "Camping Stove", Brand: "Coleman", Category: "Kitchen"},
			rec:         ingest.Record{Name: "Camping Stove", Brand: "Coleman", Category: "Outdoor"},
			wantUpd:     &catalog.AttrUpdate{ProductID: 60, Category: str("Outdoor")},
			wantChanges: []dedup.Change{{Field: "category", Old: "Kitchen", New: "Outdoor"}},
		},
		{
			name:    "name never changes and differences in it are not noted",
			mode:    dedup.UpdateOverwrite,
			product: catalog.Product{ID: 21, Name: "Router WiFi 6 TP-Link", Brand: "TP-Link", Category: "Electronics"},
			rec:     ingest.Record{Name: "Roteador WiFi 6 TP-Link", Brand: "TP-Link", Category: "Electronics"},
		},
		{
			name:    "case-only difference is not a change",
			mode:    dedup.UpdateOverwrite,
			product: catalog.Product{ID: 9, Name: "Pan", Brand: "Acme", Category: "Kitchen"},
			rec:     ingest.Record{Name: "Pan", Brand: "ACME", Category: "kitchen"},
		},
		{
			name:      "flagged input is never applied",
			mode:      dedup.UpdateOverwrite,
			product:   catalog.Product{ID: 113, Name: "Cable Organizer Kit"},
			rec:       ingest.Record{Name: "Cable Organizer Kit", Brand: "X'; DROP TABLE Product; --", Flags: sqlFlag},
			wantNotes: []string{"brand: flagged input, not applied"},
		},
		{
			name:      "overwrite: first record in the run wins",
			mode:      dedup.UpdateOverwrite,
			product:   catalog.Product{ID: 322, Name: "Jeans 501", Brand: "Levi's", Category: "Clothing"},
			rec:       ingest.Record{Name: "Jeans 501", Brand: "Levis", Category: "Clothing"},
			updatedBy: 188,
			wantNotes: []string{"brand: already updated by record 188, kept Levi's"},
		},
		{
			name:        "fill still fills after an earlier change to another field",
			product:     catalog.Product{ID: 7, Name: "Thing", Brand: "Acme"},
			rec:         ingest.Record{Name: "Thing", Brand: "Acme", Category: "Kitchen"},
			updatedBy:   3,
			wantUpd:     &catalog.AttrUpdate{ProductID: 7, Category: str("Kitchen")},
			wantChanges: []dedup.Change{{Field: "category", New: "Kitchen"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updatedBy := tt.updatedBy
			if updatedBy == 0 {
				updatedBy = -1
			}

			upd, changes, notes := dedup.PlanUpdate(tt.mode, tt.product, tt.rec, categories, updatedBy)

			assert.Equal(t, tt.wantUpd, upd)
			assert.Equal(t, tt.wantChanges, changes)
			assert.Equal(t, tt.wantNotes, notes)
		})
	}
}
