package ingest_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

const goldenPath = "testdata/ProductEntry.golden.jsonl"

// TestDecodeFixtureGolden pins the Record of every fixture entry, identity strings included.
// The identity format is a contract: if this diff changes, every embedding changes with it.
// Regenerate with `go test ./internal/ingest -run TestDecodeFixtureGolden -update`.
func TestDecodeFixtureGolden(t *testing.T) {
	f, err := os.Open(fixture.Path(t, "ProductEntry.json"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	var (
		buf      bytes.Buffer
		recs     []ingest.Record
		enc      = json.NewEncoder(&buf)
		rejected = map[int]string{}
		flagged  = map[int][]ingest.Flag{}
	)
	enc.SetEscapeHTML(false)
	for rec, err := range ingest.Decode(t.Context(), f) {
		require.NoError(t, err)
		require.NoError(t, enc.Encode(rec))
		recs = append(recs, rec)
		if rec.Rejected() {
			rejected[rec.Index] = rec.Reason
		}
		if len(rec.Flags) > 0 {
			flagged[rec.Index] = rec.Flags
		}
	}

	// Independent of the golden file, so a careless -update cannot hide a regression.
	require.Len(t, recs, 269)
	assert.Equal(t, map[int]string{92: "Id: not a UUID", 180: "Id: not a UUID", 268: "Id: not a UUID"}, rejected)
	assert.Equal(t, map[int][]ingest.Flag{180: {{Field: "Brand", Reason: ingest.ReasonSQLLike}}}, flagged)
	assert.Equal(t, "Cable Organizer Kit | category: Accessories", recs[45].Identity, "null Brand dropped")
	assert.Equal(t, "C\u00e2mera Canon EOS R6 | brand: Canon | category: Photography", recs[55].Identity)
	assert.Equal(t, "TestBrand'; SELECT 1; --", recs[180].Brand, "payload kept as data")

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(goldenPath), 0o750))
		require.NoError(t, os.WriteFile(goldenPath, buf.Bytes(), 0o600))
	}
	want, err := os.ReadFile(goldenPath)
	require.NoError(t, err, "missing golden file; run with -update")
	assert.Equal(t, string(want), buf.String())
}
