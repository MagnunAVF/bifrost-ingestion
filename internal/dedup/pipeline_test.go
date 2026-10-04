package dedup_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"iter"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/dedup"
	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/fixture"
)

const (
	dim       = 16
	threshold = 0.9
	uuidA     = "aaaaaaaa-0000-4000-8000-000000000001"
	uuidB     = "aaaaaaaa-0000-4000-8000-000000000002"
	uuidC     = "aaaaaaaa-0000-4000-8000-000000000003"
)

// --- fixtures and helpers ------------------------------------------------------------------

type testDB struct {
	path string
	c    *catalog.Catalog
}

func newDB(t *testing.T) testDB {
	t.Helper()
	path := fixture.Copy(t, "catalog.db")
	c, err := catalog.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, c.Close()) })
	_, err = c.Migrate(t.Context())
	require.NoError(t, err)
	return testDB{path: path, c: c}
}

// count reads through a second connection; the catalog's own pool is not exposed here.
func (d testDB) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	conn, err := sql.Open("sqlite", "file:"+d.path+"?mode=ro")
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	var n int
	require.NoError(t, conn.QueryRowContext(t.Context(), query, args...).Scan(&n))
	return n
}

func (d testDB) products(t *testing.T) int { return d.count(t, "SELECT count(*) FROM Product") }
func (d testDB) links(t *testing.T) int    { return d.count(t, "SELECT count(*) FROM SellerProduct") }

func (d testDB) product(t *testing.T, id int64) catalog.Product {
	t.Helper()
	all, err := d.c.ListProducts(t.Context())
	require.NoError(t, err)
	for _, p := range all {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("product %d not found", id)
	return catalog.Product{}
}

func (d testDB) firstWith(t *testing.T, match func(catalog.Product) bool) catalog.Product {
	t.Helper()
	all, err := d.c.ListProducts(t.Context())
	require.NoError(t, err)
	for _, p := range all {
		if match(p) {
			return p
		}
	}
	t.Fatal("no matching product")
	return catalog.Product{}
}

func fileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test file in TempDir
	require.NoError(t, err)
	return sha256.Sum256(b)
}

func entry(index int, seller, id, name, brand, category string) ingest.Record {
	return ingest.Record{
		Index: index, ID: id, SellerName: seller, Name: name, Brand: brand, Category: category,
		Identity: ingest.Identity(name, brand, category),
	}
}

func like(index int, seller, id string, p catalog.Product) ingest.Record {
	return entry(index, seller, id, p.Name, p.Brand, p.Category)
}

func records(recs ...ingest.Record) iter.Seq2[ingest.Record, error] {
	return func(yield func(ingest.Record, error) bool) {
		for _, r := range recs {
			if !yield(r, nil) {
				return
			}
		}
	}
}

func cfg(mods ...func(*dedup.Config)) dedup.Config {
	c := dedup.Config{Policy: dedup.Policy{Threshold: threshold}}
	for _, m := range mods {
		m(&c)
	}
	return c
}

func dryRun(c *dedup.Config)    { c.DryRun = true }
func overwrite(c *dedup.Config) { c.Update = dedup.UpdateOverwrite }

func run(t *testing.T, s dedup.Store, e embed.Embedder, c dedup.Config, recs iter.Seq2[ingest.Record, error]) (dedup.Report, error) {
	t.Helper()
	p, err := dedup.New(s, e, c)
	require.NoError(t, err)
	return p.Run(t.Context(), recs)
}

func mustRun(t *testing.T, s dedup.Store, e embed.Embedder, c dedup.Config, recs ...ingest.Record) dedup.Report {
	t.Helper()
	rep, err := run(t, s, e, c, records(recs...))
	require.NoError(t, err)
	assertConsistent(t, rep)
	return rep
}

func assertConsistent(t *testing.T, rep dedup.Report) {
	t.Helper()
	assert.Equal(t, len(rep.Results), rep.Inserted+rep.Linked+rep.Existing+rep.Rejected+rep.Failed)
	suspicious := 0
	for _, r := range rep.Results {
		if r.Suspicious() {
			suspicious++
		}
	}
	assert.Equal(t, suspicious, rep.Suspicious)
}

// nameEmbedder embeds only the Name part of an identity string, so entries that differ from a
// catalog row only in Brand or Category still score 1 (the Fake alone is not semantic).
type nameEmbedder struct{ f *embed.Fake }

func (n nameEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	names := make([]string, len(texts))
	for i, s := range texts {
		names[i], _, _ = strings.Cut(s, " | ")
	}
	return n.f.Embed(ctx, names)
}

// failingEmbedder fails any batch that contains a text in fail, or every batch when all is set.
type failingEmbedder struct {
	f    *embed.Fake
	fail map[string]bool
	all  bool
	err  error
}

func (e failingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	for _, s := range texts {
		if e.all || e.fail[s] {
			return nil, e.err
		}
	}
	return e.f.Embed(ctx, texts)
}

// faultyStore wraps a Store and fails chosen calls.
type faultyStore struct {
	dedup.Store
	listErr    error
	listCalls  int
	insertErr  map[string]error // by SellerProductID
	linkErr    map[string]error // by SellerProductID
	updateErr  error            // any LinkSeller call that carries an update
	linkCalls  int
	insertCall int
}

func (s *faultyStore) ListProducts(ctx context.Context) ([]catalog.Product, error) {
	s.listCalls++
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.Store.ListProducts(ctx)
}

func (s *faultyStore) LinkSeller(ctx context.Context, l *catalog.SellerLink, u *catalog.AttrUpdate) error {
	s.linkCalls++
	if u != nil && s.updateErr != nil {
		return s.updateErr
	}
	if l != nil && s.linkErr[l.SellerProductID] != nil {
		return s.linkErr[l.SellerProductID]
	}
	return s.Store.LinkSeller(ctx, l, u)
}

func (s *faultyStore) InsertProductAndLink(ctx context.Context, p catalog.NewProduct, seller, spid string) (int64, error) {
	s.insertCall++
	if err := s.insertErr[spid]; err != nil {
		return 0, err
	}
	return s.Store.InsertProductAndLink(ctx, p, seller, spid)
}

// --- New -----------------------------------------------------------------------------------

func TestNewValidates(t *testing.T) {
	d := newDB(t)
	f := embed.NewFake(dim)
	tests := []struct {
		name  string
		store dedup.Store
		emb   embed.Embedder
		cfg   dedup.Config
	}{
		{name: "nil store", emb: f, cfg: cfg()},
		{name: "nil embedder", store: d.c, cfg: cfg()},
		{name: "zero threshold", store: d.c, emb: f, cfg: dedup.Config{}},
		{name: "threshold above 1", store: d.c, emb: f, cfg: cfg(func(c *dedup.Config) { c.Policy.Threshold = 1.5 })},
		{name: "negative batch size", store: d.c, emb: f, cfg: cfg(func(c *dedup.Config) { c.BatchSize = -1 })},
		{name: "unknown update mode", store: d.c, emb: f, cfg: cfg(func(c *dedup.Config) { c.Update = 9 })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dedup.New(tt.store, tt.emb, tt.cfg)
			assert.ErrorIs(t, err, errs.ErrInvalidInput)
		})
	}
}

// --- decisions -----------------------------------------------------------------------------

func TestRunLinksADuplicate(t *testing.T) {
	d := newDB(t)
	router := d.product(t, 21)

	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(), like(0, "MegaStore", uuidA, router))

	require.Len(t, rep.Results, 1)
	r := rep.Results[0]
	assert.Equal(t, dedup.OutcomeLinked, r.Outcome)
	assert.Equal(t, dedup.KindDuplicate, r.Kind)
	assert.InDelta(t, 1, r.Score, 1e-5)
	assert.Equal(t, int64(21), r.MatchID)
	assert.Equal(t, int64(21), r.ProductID)
	assert.Equal(t, ingest.Identity(router.Name, router.Brand, router.Category), r.MatchIdentity)
	assert.Equal(t, 975, rep.CatalogSize)
	assert.Equal(t, 1, rep.Linked)
	assert.Equal(t, 975, d.products(t), "no product inserted")
	assert.Equal(t, 1, d.count(t, "SELECT count(*) FROM SellerProduct WHERE SellerName = 'MegaStore' AND ProductId = 21 AND SellerProductId = ?", uuidA))
}

func TestRunInsertsANovelProductAndIndexesIt(t *testing.T) {
	d := newDB(t)

	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(),
		entry(0, "MegaStore", uuidA, "Quantum Toaster 3000", "Acme", "Kitchen"),
		entry(1, "SportsHub", uuidB, "Quantum Toaster 3000", "Acme", "Kitchen"),
	)

	require.Len(t, rep.Results, 2)
	first, second := rep.Results[0], rep.Results[1]
	assert.Equal(t, dedup.OutcomeInserted, first.Outcome)
	assert.Equal(t, dedup.KindNovel, first.Kind)
	assert.Equal(t, int64(976), first.ProductID)
	assert.NotZero(t, first.MatchID, "the nearest catalog product is still reported")
	assert.Equal(t, dedup.OutcomeLinked, second.Outcome, "the new vector is in the index")
	assert.Equal(t, int64(976), second.ProductID)
	assert.Equal(t, 1, rep.Inserted)
	assert.Equal(t, 1, rep.Linked)
	assert.Equal(t, 976, d.products(t))
	assert.Equal(t, 2, d.links(t))
	assert.Equal(t, catalog.Product{ID: 976, Name: "Quantum Toaster 3000", Brand: "Acme", Category: "Kitchen"}, d.product(t, 976))
}

func TestRunDryRunSimulatesWithoutWriting(t *testing.T) {
	d := newDB(t)
	before := fileHash(t, d.path)
	router := d.product(t, 21)

	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(dryRun),
		entry(0, "MegaStore", uuidA, "Quantum Toaster 3000", "Acme", "Kitchen"),
		entry(1, "SportsHub", uuidB, "Quantum Toaster 3000", "Acme", "Kitchen"),
		like(2, "SportsHub", uuidC, router),
		like(3, "SportsHub", uuidC, router),
	)

	require.Len(t, rep.Results, 4)
	assert.True(t, rep.DryRun)
	assert.Equal(t, dedup.OutcomeInserted, rep.Results[0].Outcome)
	assert.Equal(t, int64(-1), rep.Results[0].ProductID, "provisional id")
	assert.Equal(t, dedup.OutcomeLinked, rep.Results[1].Outcome)
	assert.Equal(t, int64(-1), rep.Results[1].ProductID)
	assert.Equal(t, dedup.OutcomeLinked, rep.Results[2].Outcome)
	assert.Equal(t, dedup.OutcomeExisting, rep.Results[3].Outcome, "the simulated link counts for idempotency")
	assert.Equal(t, before, fileHash(t, d.path), "database file unchanged")
	assert.Equal(t, 0, d.links(t))
}

func TestRunThresholdBoundary(t *testing.T) {
	d := newDB(t)
	router := d.product(t, 21)
	probe := entry(0, "MegaStore", uuidA, router.Name+" Pro", router.Brand, router.Category)

	// The fake is deterministic: read the score once, then put the threshold exactly on it.
	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(dryRun, func(c *dedup.Config) { c.Policy.Threshold = 1 }), probe)
	score := rep.Results[0].Score
	require.Greater(t, score, float32(0))
	require.Less(t, score, float32(1))

	at := mustRun(t, d.c, embed.NewFake(dim), cfg(dryRun, func(c *dedup.Config) { c.Policy.Threshold = score }), probe)
	above := mustRun(t, d.c, embed.NewFake(dim), cfg(dryRun, func(c *dedup.Config) { c.Policy.Threshold = math.Nextafter32(score, 1) }), probe)

	assert.Equal(t, dedup.KindDuplicate, at.Results[0].Kind, "score == threshold is a duplicate")
	assert.Equal(t, dedup.KindNovel, above.Results[0].Kind)
}

func TestRunRejectedRecordIsReportedNotEmbedded(t *testing.T) {
	d := newDB(t)
	rejected := ingest.Record{
		Index: 0, ID: "09835342345-4678-9abc-def012345678", SellerName: "MegaStore",
		Name: "Security Test Product", Brand: "TestBrand'; SELECT 1; --",
		Reason: "Id: not a UUID", Flags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonSQLLike}},
	}
	// Any embedding of the rejected name would fail the run.
	e := failingEmbedder{f: embed.NewFake(dim), fail: map[string]bool{"": true, "Security Test Product": true}, err: errors.New("embedded a reject")}

	rep := mustRun(t, d.c, e, cfg(), rejected)

	r := rep.Results[0]
	assert.Equal(t, dedup.OutcomeRejected, r.Outcome)
	assert.Equal(t, "Id: not a UUID", r.Reason)
	assert.Equal(t, rejected.Flags, r.Flags)
	assert.Zero(t, r.Kind)
	assert.True(t, r.Suspicious())
	assert.Equal(t, 1, rep.Rejected)
	assert.Equal(t, 1, rep.Suspicious)
	assert.Equal(t, 975, d.products(t))
	assert.Equal(t, 0, d.links(t))
}

func TestRunSameKeyTwiceFirstWins(t *testing.T) {
	d := newDB(t)
	camera := entry(55, "GardenStore", uuidA, "Câmera Canon EOS R6", "Canon", "Photography")
	again := entry(76, "GardenStore", uuidA, "Camera Canon EOS R6", "Canon", "Photography")

	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(), camera, again)

	assert.Equal(t, dedup.OutcomeExisting, rep.Results[1].Outcome)
	assert.Equal(t, rep.Results[0].ProductID, rep.Results[1].ProductID)
	assert.Zero(t, rep.Results[1].Kind, "not embedded")
	assert.Equal(t, 1, d.links(t))
}

func TestRunIsIdempotent(t *testing.T) {
	d := newDB(t)
	router := d.product(t, 21)
	input := []ingest.Record{
		like(0, "MegaStore", uuidA, router),
		entry(1, "MegaStore", uuidB, "Quantum Toaster 3000", "Acme", "Kitchen"),
	}

	first := mustRun(t, d.c, embed.NewFake(dim), cfg(), input...)
	products, links := d.products(t), d.links(t)
	second := mustRun(t, d.c, embed.NewFake(dim), cfg(), input...)

	assert.Equal(t, 1, first.Inserted)
	assert.Equal(t, 1, first.Linked)
	assert.Equal(t, 2, second.Existing)
	assert.Zero(t, second.Inserted+second.Linked+second.Updated)
	assert.Equal(t, products, d.products(t))
	assert.Equal(t, links, d.links(t))
}

func TestRunStoresTheInjectionPayloadVerbatim(t *testing.T) {
	d := newDB(t)
	const payload = "TestBrand'; SELECT 1; --"
	rec := entry(0, "MegaStore", uuidA, "Security Test Product", payload, "Electronics")
	rec.Flags = []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonSQLLike}}

	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(), rec)

	assert.Equal(t, dedup.OutcomeInserted, rep.Results[0].Outcome)
	assert.Equal(t, 1, rep.Suspicious)
	assert.Equal(t, catalog.Product{ID: 976, Name: "Security Test Product", Brand: payload, Category: "Electronics"}, d.product(t, 976))
	assert.Equal(t, 976, d.products(t))
	assert.Equal(t, 1, d.links(t))
	assert.Equal(t, 1, d.count(t, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'Product'"))
}

// --- product updates -----------------------------------------------------------------------

func TestRunFillsANullBrandAndReembeds(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry run %v", dry), func(t *testing.T) {
			d := newDB(t)
			before := fileHash(t, d.path)
			p := d.product(t, 113)
			require.Empty(t, p.Brand)
			c := cfg(func(c *dedup.Config) { c.DryRun = dry })

			// nameEmbedder matches on Name for the fill; the probe then checks the index with the
			// plain fake: the updated identity must score 1 against product 113.
			rep := mustRun(t, d.c, nameEmbedder{embed.NewFake(dim)}, c,
				entry(0, "MegaStore", uuidA, p.Name, "Acme", p.Category),
				entry(1, "SportsHub", uuidB, p.Name, "Zenith", p.Category),
			)

			r := rep.Results[0]
			assert.Equal(t, dedup.OutcomeLinked, r.Outcome)
			assert.Equal(t, []dedup.Change{{Field: "brand", Old: "", New: "Acme"}}, r.Changes)
			assert.Equal(t, 1, rep.Updated)
			assert.Equal(t, []string{"brand differs: Zenith (entry) vs Acme (catalog), kept"}, rep.Results[1].Notes,
				"the second record sees the (simulated) fill")
			if dry {
				assert.Equal(t, before, fileHash(t, d.path))
				return
			}
			assert.Equal(t, "Acme", d.product(t, 113).Brand)
			assert.Equal(t, p.Name, d.product(t, 113).Name)
		})
	}
}

func TestRunReplacesTheVectorOfAnUpdatedProduct(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry run %v", dry), func(t *testing.T) {
			d := newDB(t)
			p := d.product(t, 113)
			updated := ingest.Identity(p.Name, "Acme", p.Category)
			require.NoError(t, d.c.LinkSeller(t.Context(), &catalog.SellerLink{SellerName: "MegaStore", SellerProductID: uuidA, ProductID: 113}, nil))

			// Record 0 is existing (no embedding) and fills Brand. With the plain fake, record 1 can
			// only score 1 against 113 if 113's vector was replaced by the updated identity's.
			rep := mustRun(t, d.c, embed.NewFake(dim), cfg(func(c *dedup.Config) { c.DryRun = dry }),
				entry(0, "MegaStore", uuidA, p.Name, "Acme", p.Category),
				entry(1, "SportsHub", uuidB, p.Name, "Acme", p.Category),
			)

			require.Equal(t, dedup.OutcomeExisting, rep.Results[0].Outcome)
			require.Len(t, rep.Results[0].Changes, 1)
			r := rep.Results[1]
			assert.Equal(t, dedup.OutcomeLinked, r.Outcome)
			assert.Equal(t, int64(113), r.MatchID)
			assert.InDelta(t, 1, r.Score, 1e-5)
			assert.Equal(t, updated, r.MatchIdentity, "report shows the current identity")
		})
	}
}

func TestRunCategoryDriftIsLinkedNotApplied(t *testing.T) {
	d := newDB(t)
	photo := d.firstWith(t, func(p catalog.Product) bool { return p.Category == "Photography" })

	rep := mustRun(t, d.c, nameEmbedder{embed.NewFake(dim)}, cfg(overwrite),
		entry(87, "GardenStore", uuidA, photo.Name, photo.Brand, "Photo"))

	r := rep.Results[0]
	assert.Equal(t, dedup.OutcomeLinked, r.Outcome)
	assert.Equal(t, photo.ID, r.ProductID)
	assert.Empty(t, r.Changes)
	assert.Equal(t, []string{"category: Photo is not a catalog category, kept Photography"}, r.Notes)
	assert.Equal(t, photo, d.product(t, photo.ID))
	assert.Zero(t, rep.Updated)
}

func TestRunExistingRecordStillFills(t *testing.T) {
	d := newDB(t)
	p := d.product(t, 113)
	require.NoError(t, d.c.LinkSeller(t.Context(), &catalog.SellerLink{SellerName: "MegaStore", SellerProductID: uuidA, ProductID: 113}, nil))

	rep := mustRun(t, d.c, embed.NewFake(dim), cfg(), entry(0, "MegaStore", uuidA, p.Name, "Acme", p.Category))

	r := rep.Results[0]
	assert.Equal(t, dedup.OutcomeExisting, r.Outcome)
	assert.Equal(t, int64(113), r.ProductID)
	assert.Equal(t, []dedup.Change{{Field: "brand", Old: "", New: "Acme"}}, r.Changes)
	assert.Equal(t, 1, rep.Updated)
	assert.Equal(t, "Acme", d.product(t, 113).Brand)
	assert.Equal(t, 1, d.links(t), "no new link")
}

func TestRunRerunAfterUpdatesChangesNothing(t *testing.T) {
	d := newDB(t)
	p := d.product(t, 113)
	in := entry(0, "MegaStore", uuidA, p.Name, "Acme", p.Category)

	mustRun(t, d.c, nameEmbedder{embed.NewFake(dim)}, cfg(), in)
	again := mustRun(t, d.c, nameEmbedder{embed.NewFake(dim)}, cfg(), in)

	assert.Equal(t, 1, again.Existing)
	assert.Zero(t, again.Updated)
	assert.Empty(t, again.Results[0].Changes)
}

// recordingEmbedder records every text it is asked to embed.
type recordingEmbedder struct {
	f     *embed.Fake
	texts *[]string
}

func (r recordingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	*r.texts = append(*r.texts, texts...)
	return r.f.Embed(ctx, texts)
}

func TestRunTaskPrefixIsAppliedToEveryText(t *testing.T) {
	d := newDB(t)
	p := d.product(t, 113)
	require.NoError(t, d.c.LinkSeller(t.Context(), &catalog.SellerLink{SellerName: "MegaStore", SellerProductID: uuidA, ProductID: 113}, nil))
	var texts []string

	rep := mustRun(t, d.c, recordingEmbedder{embed.NewFake(dim), &texts}, cfg(func(c *dedup.Config) { c.TaskPrefix = dedup.DefaultTaskPrefix }),
		entry(0, "MegaStore", uuidA, p.Name, "Acme", p.Category), // existing: fills, so 113 is re-embedded
		entry(1, "SportsHub", uuidB, p.Name, "Acme", p.Category), // embedded
	)

	assert.Equal(t, "clustering: ", dedup.DefaultTaskPrefix)
	require.Len(t, texts, 1+975+1+1, "preflight, cold start, re-embed, entry")
	for _, s := range texts {
		require.True(t, strings.HasPrefix(s, "clustering: "), "%q", s)
	}
	assert.Equal(t, "clustering: "+ingest.Identity(p.Name, "Acme", p.Category), texts[len(texts)-1])
	assert.Equal(t, dedup.OutcomeLinked, rep.Results[1].Outcome, "prefixed on both sides, so identical identities still score 1")
	assert.Equal(t, ingest.Identity(p.Name, "Acme", p.Category), rep.Results[1].Identity, "the report keeps the identity string")
}

func TestDefaultThresholdIsCalibrated(t *testing.T) {
	assert.InDelta(t, 0.975, dedup.DefaultThreshold, 1e-9, "set by the ENG-6 calibration (ADR 0002)")
}

// --- failures ------------------------------------------------------------------------------

func TestRunStoreFailureOnOneRecordDoesNotStopTheRun(t *testing.T) {
	d := newDB(t)
	s := &faultyStore{Store: d.c, insertErr: map[string]error{uuidB: errors.New("disk on fire")}}

	rep, err := run(t, s, embed.NewFake(dim), cfg(), records(
		entry(0, "MegaStore", uuidA, "Novel One", "", ""),
		entry(1, "MegaStore", uuidB, "Novel Two", "", ""),
		entry(2, "MegaStore", uuidC, "Novel Three", "", ""),
	))

	require.NoError(t, err)
	assertConsistent(t, rep)
	assert.Equal(t, dedup.OutcomeInserted, rep.Results[0].Outcome)
	assert.Equal(t, dedup.OutcomeFailed, rep.Results[1].Outcome)
	assert.Equal(t, "other", rep.Results[1].ErrKind)
	assert.Contains(t, rep.Results[1].Reason, "disk on fire")
	assert.Equal(t, dedup.OutcomeInserted, rep.Results[2].Outcome)
	assert.Equal(t, 1, rep.Failed)
	assert.Equal(t, 977, d.products(t))
}

func TestRunFailedErrKind(t *testing.T) {
	d := newDB(t)
	router := d.product(t, 21)
	tests := []struct {
		err  error
		want string
	}{
		{err: fmt.Errorf("linking: %w", errs.ErrConflict), want: "conflict"},
		{err: fmt.Errorf("updating: %w", errs.ErrNotFound), want: "not found"},
		{err: errors.New("boom"), want: "other"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			s := &faultyStore{Store: d.c, linkErr: map[string]error{uuidA: tt.err}}

			rep := mustRun(t, s, embed.NewFake(dim), cfg(), like(0, "MegaStore", uuidA, router))

			assert.Equal(t, dedup.OutcomeFailed, rep.Results[0].Outcome)
			assert.Equal(t, tt.want, rep.Results[0].ErrKind)
		})
	}
}

func TestRunFailedUpdateLeavesMemoryUnchanged(t *testing.T) {
	d := newDB(t)
	p := d.product(t, 113)
	s := &faultyStore{Store: d.c, updateErr: errors.New("update failed")}

	rep := mustRun(t, s, nameEmbedder{embed.NewFake(dim)}, cfg(),
		entry(0, "MegaStore", uuidA, p.Name, "Acme", p.Category),
		entry(1, "SportsHub", uuidB, p.Name, "Zenith", p.Category),
	)

	assert.Equal(t, dedup.OutcomeFailed, rep.Results[0].Outcome)
	assert.Empty(t, rep.Results[0].Changes)
	assert.Equal(t, dedup.OutcomeFailed, rep.Results[1].Outcome, "Brand is still NULL in memory, so record 1 tries to fill too")
	assert.Zero(t, rep.Updated)
	assert.Equal(t, 0, d.links(t))
}

func TestRunFatalErrors(t *testing.T) {
	upstream := fmt.Errorf("calling ollama: %w", errs.ErrUpstream)

	t.Run("preflight", func(t *testing.T) {
		d := newDB(t)
		s := &faultyStore{Store: d.c}

		rep, err := run(t, s, failingEmbedder{all: true, err: upstream}, cfg(), records())

		var re *dedup.RunError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, dedup.StagePreflight, re.Stage)
		assert.Equal(t, -1, re.Index)
		assert.ErrorIs(t, err, errs.ErrUpstream)
		assert.Zero(t, s.listCalls, "fails before the cold start")
		assert.Same(t, re, rep.Stopped)
	})

	t.Run("cold start", func(t *testing.T) {
		d := newDB(t)
		s := &faultyStore{Store: d.c, listErr: errors.New("db gone")}

		_, err := run(t, s, embed.NewFake(dim), cfg(), records())

		var re *dedup.RunError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, dedup.StageColdStart, re.Stage)
		assert.Equal(t, -1, re.Index)
	})

	t.Run("embed on record 2", func(t *testing.T) {
		d := newDB(t)
		boom := entry(2, "MegaStore", uuidC, "Boom", "", "")
		e := failingEmbedder{f: embed.NewFake(dim), fail: map[string]bool{boom.Identity: true}, err: upstream}

		rep, err := run(t, d.c, e, cfg(), records(
			entry(0, "MegaStore", uuidA, "Novel One", "", ""),
			entry(1, "MegaStore", uuidB, "Novel Two", "", ""),
			boom,
			entry(3, "MegaStore", "aaaaaaaa-0000-4000-8000-000000000004", "Never", "", ""),
		))

		var re *dedup.RunError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, dedup.StageEmbed, re.Stage)
		assert.Equal(t, 2, re.Index)
		assert.Equal(t, 2, re.Processed)
		assert.ErrorIs(t, err, errs.ErrUpstream)
		assert.Len(t, rep.Results, 2)
		assert.Equal(t, 2, rep.Inserted)
		assert.Equal(t, 977, d.products(t), "records before the stop are committed")
		assert.Contains(t, err.Error(), "stopped at embed (record 2, 2 done)")
	})

	t.Run("decode error mid-stream", func(t *testing.T) {
		d := newDB(t)
		in := `[{"Id":"` + uuidA + `","SellerName":"MegaStore","Name":"Novel One"}, nope`

		rep, err := run(t, d.c, embed.NewFake(dim), cfg(), ingest.Decode(t.Context(), strings.NewReader(in)))

		var re *dedup.RunError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, dedup.StageDecode, re.Stage)
		assert.Equal(t, 1, re.Index)
		assert.ErrorIs(t, err, errs.ErrInvalidInput)
		require.Len(t, rep.Results, 1)
		assert.Equal(t, dedup.OutcomeInserted, rep.Results[0].Outcome)
	})

	t.Run("cancelled after the first record", func(t *testing.T) {
		d := newDB(t)
		ctx, cancel := context.WithCancel(t.Context())
		in := func(yield func(ingest.Record, error) bool) {
			if !yield(entry(0, "MegaStore", uuidA, "Novel One", "", ""), nil) {
				return
			}
			cancel()
			yield(entry(1, "MegaStore", uuidB, "Novel Two", "", ""), nil)
		}
		p, err := dedup.New(d.c, embed.NewFake(dim), cfg())
		require.NoError(t, err)

		rep, err := p.Run(ctx, in)

		var re *dedup.RunError
		require.ErrorAs(t, err, &re)
		assert.Equal(t, dedup.StageCancelled, re.Stage)
		assert.Equal(t, 1, re.Processed)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Len(t, rep.Results, 1)
		assert.Equal(t, 976, d.products(t))
	})
}

// --- the real fixture ----------------------------------------------------------------------

func TestRunOnTheFixture(t *testing.T) {
	d := newDB(t)
	decode := func() iter.Seq2[ingest.Record, error] {
		f, err := os.Open(fixture.Path(t, "ProductEntry.json"))
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		return ingest.Decode(t.Context(), f)
	}

	rep, err := run(t, d.c, embed.NewFake(dim), cfg(), decode())
	require.NoError(t, err)
	assertConsistent(t, rep)

	require.Len(t, rep.Results, 269)
	assert.Equal(t, 3, rep.Rejected)
	for _, i := range []int{92, 180, 268} {
		assert.Equal(t, dedup.OutcomeRejected, rep.Results[i].Outcome, "entry %d", i)
	}
	assert.Equal(t, 1, rep.Suspicious)
	assert.True(t, rep.Results[180].Suspicious())
	assert.Equal(t, dedup.OutcomeExisting, rep.Results[76].Outcome, "same seller and Id as 55")
	assert.Equal(t, rep.Results[55].ProductID, rep.Results[76].ProductID)
	assert.Zero(t, rep.Failed)
	// The fake is not semantic: only identical identities link to catalog rows. This pins row
	// counts against the outcomes, not match quality (that is the calibration's job).
	assert.Equal(t, 975+rep.Inserted, d.products(t))
	assert.Equal(t, rep.Inserted+rep.Linked, d.links(t))
	assert.Greater(t, rep.Linked, 200)

	again, err := run(t, d.c, embed.NewFake(dim), cfg(), decode())
	require.NoError(t, err)
	assert.Equal(t, 266, again.Existing, "every accepted entry is already linked")
	assert.Zero(t, again.Inserted+again.Linked+again.Failed+again.Updated)
}
