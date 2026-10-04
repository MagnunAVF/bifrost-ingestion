package dedup_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

func sampleReport() dedup.Report {
	sql := []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonSQLLike}}
	return dedup.Report{
		Threshold: 0.9, Update: dedup.UpdateFill, CatalogSize: 975,
		Inserted: 1, Linked: 2, Existing: 1, Rejected: 1, Failed: 1, Suspicious: 1, Updated: 1,
		Results: []dedup.Result{
			{
				Index: 0, SellerName: "MegaStore", SellerProductID: "a1", Identity: "Router | brand: TP-Link",
				Outcome: dedup.OutcomeLinked, Kind: dedup.KindDuplicate, Score: 0.9987,
				MatchID: 21, MatchIdentity: "Router | brand: TP-Link", ProductID: 21,
			},
			{
				Index: 1, SellerName: "SportsHub", SellerProductID: "b2", Identity: "Toaster | category: Kitchen",
				Outcome: dedup.OutcomeInserted, Kind: dedup.KindNovel, Score: 0.1234,
				MatchID: 7, MatchIdentity: "Pan | category: Kitchen", ProductID: 976,
			},
			{
				Index: 2, SellerName: "MegaStore", SellerProductID: "c3", Identity: "Cable Kit | brand: Acme",
				Outcome: dedup.OutcomeLinked, Kind: dedup.KindDuplicate, Score: 0.95,
				MatchID: 113, MatchIdentity: "Cable Kit | brand: Acme", ProductID: 113,
				Changes: []dedup.Change{{Field: "brand", Old: "", New: "Acme"}},
				Notes:   []string{"category differs: X (entry) vs Y (catalog), kept"},
			},
			{
				Index: 3, SellerName: "MegaStore", SellerProductID: "bad-id",
				Outcome: dedup.OutcomeRejected, Reason: "Id: not a UUID", Flags: sql,
			},
			{
				Index: 4, SellerName: "GardenStore", SellerProductID: "d4", Identity: "Camera",
				Outcome: dedup.OutcomeFailed, Kind: dedup.KindDuplicate, Score: 0.97, MatchID: 50,
				MatchIdentity: "Camera", ErrKind: "conflict", Reason: "linking seller: conflict",
			},
			{
				Index: 5, SellerName: "GardenStore", SellerProductID: "a1",
				Outcome: dedup.OutcomeExisting, ProductID: 21,
			},
		},
	}
}

func TestReportWriteText(t *testing.T) {
	const want = `Bifröst ingest report
threshold 0.900, update fill, catalog 975 products
records 6: inserted 1, linked 2, existing 1, rejected 1, failed 1, suspicious 1, products updated 1

rejected (1):
  #3 MegaStore bad-id: Id: not a UUID

failed (1):
  #4 GardenStore d4: conflict: linking seller: conflict

suspicious (1):
  #3 MegaStore bad-id: Brand: SQL-like content

inserted (1):
  #1 SportsHub b2 → product 976 "Toaster | category: Kitchen" (nearest 7 at 0.123)

changes (1):
  #2 product 113: brand: (null) → Acme

notes (1):
  #2 product 113: category differs: X (entry) vs Y (catalog), kept
`
	var b strings.Builder

	require.NoError(t, sampleReport().WriteText(&b, false))

	assert.Equal(t, want, b.String())
}

func TestReportWriteTextVerbose(t *testing.T) {
	rep := sampleReport()
	rep.DryRun = true
	rep.Stopped = &dedup.RunError{Stage: dedup.StageEmbed, Index: 6, Processed: 6, Err: errors.New("calling ollama: connection refused")}
	var b strings.Builder

	require.NoError(t, rep.WriteText(&b, true))

	out := b.String()
	assert.True(t, strings.HasPrefix(out, "Bifröst ingest report (DRY RUN: nothing written)\n"), out)
	assert.Contains(t, out, "\ndecisions:\n"+
		`  #0 linked duplicate 0.999 → product 21 "Router | brand: TP-Link", nearest 21 "Router | brand: TP-Link"`+"\n"+
		`  #1 inserted novel 0.123 → product 976 "Toaster | category: Kitchen", nearest 7 "Pan | category: Kitchen"`+"\n"+
		`  #2 linked duplicate 0.950 → product 113 "Cable Kit | brand: Acme", nearest 113 "Cable Kit | brand: Acme"`+"\n"+
		"  #3 rejected: Id: not a UUID\n"+
		"  #4 failed: conflict: linking seller: conflict\n"+
		"  #5 existing → product 21\n")
	assert.True(t, strings.HasSuffix(out, "\nstopped: stopped at embed (record 6, 6 done): calling ollama: connection refused\n"), out)
}

func TestReportEmptySections(t *testing.T) {
	var b strings.Builder

	require.NoError(t, dedup.Report{Threshold: 0.9, CatalogSize: 3}.WriteText(&b, false))

	assert.Equal(t, `Bifröst ingest report
threshold 0.900, update fill, catalog 3 products
records 0: inserted 0, linked 0, existing 0, rejected 0, failed 0, suspicious 0, products updated 0
`, b.String())
}

func TestRunErrorUnwraps(t *testing.T) {
	cause := errors.New("cause")
	err := error(&dedup.RunError{Stage: dedup.StageColdStart, Index: -1, Err: cause})

	assert.ErrorIs(t, err, cause)
	assert.Equal(t, "stopped at cold start: cause", err.Error(), "no record position before the first record")
}
