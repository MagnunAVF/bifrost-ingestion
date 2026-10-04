//go:build integration

package dedup_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

// translations are the fixture's semantic duplicates that no name key resolves
// (docs/data-notes.md): entry index → catalog Product.Id.
var translations = map[int]int64{57: 21, 64: 28}

// TestCalibration measures the score distributions the threshold must separate, on the real
// data and a real Ollama (`make e2e`):
//
//   - positives: each accepted fixture entry against its known catalog match;
//   - negatives: each catalog product against its nearest *other* product (975 distinct
//     products, so every one of these scores belongs to a different product).
//
// It reports both with and without the nomic `clustering: ` task prefix. The distributions
// overlap (ADR 0002), so the shipped pair (dedup.DefaultTaskPrefix, dedup.DefaultThreshold) is
// precision-first: it must link no two distinct catalog products, and may miss only the two
// Portuguese translations.
func TestCalibration(t *testing.T) {
	cfg := embed.OllamaConfig{BaseURL: os.Getenv("OLLAMA_HOST"), Model: os.Getenv("BIFROST_MODEL")}
	if cfg.Model == "" {
		cfg.Model = "nomic-embed-text"
	}
	o, err := embed.NewOllama(nil, cfg)
	require.NoError(t, err)

	products, entries, matches := calibrationSet(t)
	t.Logf("calibration set: %d catalog products, %d positive pairs (%d by name key, %d translations)",
		len(products), len(entries), len(entries)-len(translations), len(translations))

	var shipped distributions
	for _, prefix := range []string{"", dedup.DefaultTaskPrefix} {
		d := measure(t, prefixed{o, prefix}, products, entries, matches)
		t.Logf("prefix %q\n%s", prefix, d)
		if prefix == dedup.DefaultTaskPrefix {
			shipped = d
		}
	}

	threshold := float32(dedup.DefaultThreshold)
	assert.Less(t, shipped.negatives[len(shipped.negatives)-1], threshold, "no two distinct catalog products may link")
	var missed []int
	for i, s := range shipped.positiveScores {
		if s < threshold {
			missed = append(missed, shipped.positiveIndex[i])
		}
	}
	slices.Sort(missed)
	assert.Equal(t, []int{57, 64}, missed, "only the Portuguese translations fall below the threshold")
}

type prefixed struct {
	e      embed.Embedder
	prefix string
}

func (p prefixed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p.prefix == "" {
		return p.e.Embed(ctx, texts)
	}
	in := make([]string, len(texts))
	for i, s := range texts {
		in[i] = p.prefix + s
	}
	return p.e.Embed(ctx, in)
}

// calibrationSet returns the catalog, the accepted fixture entries, and for each entry the
// index in products of its known match.
func calibrationSet(t *testing.T) ([]catalog.Product, []ingest.Record, []int) {
	t.Helper()
	d := newDB(t)
	products, err := d.c.ListProducts(t.Context())
	require.NoError(t, err)
	byKey := make(map[string]int, len(products))
	byID := make(map[int64]int, len(products))
	for i, p := range products {
		byKey[matchKey(p.Name)] = i
		byID[p.ID] = i
	}

	f, err := os.Open(fixture.Path(t, "ProductEntry.json"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	var entries []ingest.Record
	var matches []int
	for rec, err := range ingest.Decode(t.Context(), f) {
		require.NoError(t, err)
		if rec.Rejected() {
			continue
		}
		i, ok := byKey[matchKey(rec.Name)]
		if id, tr := translations[rec.Index]; tr {
			i, ok = byID[id], true
		}
		require.True(t, ok, "entry %d %q has no known match", rec.Index, rec.Name)
		entries = append(entries, rec)
		matches = append(matches, i)
	}
	return products, entries, matches
}

// matchKey is the loosest normalization from docs/data-notes.md: accent fold, lowercase, keep
// letters and digits only.
func matchKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

type distributions struct {
	positives, negatives []float32 // sorted ascending
	lowest, highest      []string  // the hardest pairs, for the evidence
	positiveScores       []float32 // in entry order
	positiveIndex        []int     // fixture index of each entry, in entry order
}

func measure(t *testing.T, e embed.Embedder, products []catalog.Product, entries []ingest.Record, matches []int) distributions {
	t.Helper()
	texts := make([]string, len(products))
	for i, p := range products {
		texts[i] = ingest.Identity(p.Name, p.Brand, p.Category)
	}
	pv := embedAll(t, e, texts)
	et := make([]string, len(entries))
	for i, r := range entries {
		et[i] = r.Identity
	}
	ev := embedAll(t, e, et)

	type pair struct {
		score float32
		desc  string
	}
	var d distributions
	pos := make([]pair, len(entries))
	for i := range entries {
		d.positiveScores = append(d.positiveScores, dot(ev[i], pv[matches[i]]))
		d.positiveIndex = append(d.positiveIndex, entries[i].Index)
		pos[i] = pair{dot(ev[i], pv[matches[i]]), fmt.Sprintf("#%d %q ~ %d %q", entries[i].Index, entries[i].Identity, products[matches[i]].ID, texts[matches[i]])}
	}
	neg := make([]pair, len(products))
	for i := range products {
		best, bj := float32(-2), -1
		for j := range products {
			if j != i {
				if s := dot(pv[i], pv[j]); s > best {
					best, bj = s, j
				}
			}
		}
		neg[i] = pair{best, fmt.Sprintf("%d %q ~ %d %q", products[i].ID, texts[i], products[bj].ID, texts[bj])}
	}
	byScore := func(a, b pair) int {
		switch {
		case a.score < b.score:
			return -1
		case a.score > b.score:
			return 1
		}
		return strings.Compare(a.desc, b.desc)
	}
	slices.SortFunc(pos, byScore)
	slices.SortFunc(neg, byScore)

	for _, p := range pos {
		d.positives = append(d.positives, p.score)
	}
	for _, n := range neg {
		d.negatives = append(d.negatives, n.score)
	}
	for _, p := range pos[:min(5, len(pos))] {
		d.lowest = append(d.lowest, fmt.Sprintf("%.4f %s", p.score, p.desc))
	}
	for _, n := range slices.Backward(neg[max(0, len(neg)-10):]) {
		d.highest = append(d.highest, fmt.Sprintf("%.4f %s", n.score, n.desc))
	}
	return d
}

func (d distributions) String() string {
	q := func(s []float32, p float64) float32 { return s[min(len(s)-1, int(p*float64(len(s))))] }
	var b strings.Builder
	fmt.Fprintf(&b, "  positives n=%d: min %.4f p1 %.4f p5 %.4f median %.4f\n", len(d.positives),
		d.positives[0], q(d.positives, 0.01), q(d.positives, 0.05), q(d.positives, 0.5))
	fmt.Fprintf(&b, "  negatives n=%d: max %.4f p99 %.4f p95 %.4f median %.4f\n", len(d.negatives),
		d.negatives[len(d.negatives)-1], q(d.negatives, 0.99), q(d.negatives, 0.95), q(d.negatives, 0.5))
	fmt.Fprintf(&b, "  margin (min positive - max negative): %.4f\n", d.positives[0]-d.negatives[len(d.negatives)-1])
	// For each threshold: known duplicates that would be inserted (missed), and catalog products
	// that would be wrongly linked to a different product if they arrived as new entries.
	b.WriteString("  threshold  missed duplicates  wrong links\n")
	for _, th := range []float32{0.92, 0.93, 0.94, 0.945, 0.95, 0.955, 0.96, 0.965, 0.97, 0.975, 0.98} {
		missed, _ := slices.BinarySearch(d.positives, th)
		below, _ := slices.BinarySearch(d.negatives, th)
		fmt.Fprintf(&b, "  %.3f      %3d / %d          %3d / %d\n", th, missed, len(d.positives), len(d.negatives)-below, len(d.negatives))
	}
	b.WriteString("  lowest positives:\n")
	for _, s := range d.lowest {
		b.WriteString("    " + s + "\n")
	}
	b.WriteString("  highest negatives:\n")
	for _, s := range d.highest {
		b.WriteString("    " + s + "\n")
	}
	return b.String()
}

func embedAll(t *testing.T, e embed.Embedder, texts []string) [][]float32 {
	t.Helper()
	vecs, err := e.Embed(t.Context(), texts)
	require.NoError(t, err)
	for i, v := range vecs {
		vecs[i], err = vector.Normalize(v)
		require.NoError(t, err)
	}
	return vecs
}

func dot(a, b []float32) float32 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return float32(s)
}
