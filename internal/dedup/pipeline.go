package dedup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"

	"github.com/MagnunAVF/bifrost-ingestion/internal/catalog"
	"github.com/MagnunAVF/bifrost-ingestion/internal/embed"
	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
	"github.com/MagnunAVF/bifrost-ingestion/internal/vector"
)

// DefaultThreshold is the cosine score at or above which an entry links to its nearest catalog
// product, calibrated with DefaultTaskPrefix on nomic-embed-text: no two distinct catalog
// products score this high, and only the two Portuguese translations in the fixture fall below
// it (ADR 0002).
const DefaultThreshold = 0.975

// DefaultTaskPrefix is the nomic task prefix the calibration chose. It is prepended to every
// text the pipeline embeds (catalog rows and entries alike); the identity string is unchanged.
const DefaultTaskPrefix = "clustering: "

// DefaultBatchSize is how many catalog identities the cold start embeds per call.
const DefaultBatchSize = 64

// preflightText is embedded once before the cold start so a missing Ollama or model fails fast.
const preflightText = "bifrost preflight"

// Store is what the pipeline needs from the catalog; *catalog.Catalog satisfies it.
type Store interface {
	ListProducts(ctx context.Context) ([]catalog.Product, error)
	FindSellerLink(ctx context.Context, seller, sellerProductID string) (int64, error)
	LinkSeller(ctx context.Context, link *catalog.SellerLink, upd *catalog.AttrUpdate) error
	InsertProductAndLink(ctx context.Context, p catalog.NewProduct, seller, sellerProductID string) (int64, error)
}

var _ Store = (*catalog.Catalog)(nil)

// Config tunes a Pipeline.
type Config struct {
	Policy    Policy
	Update    UpdateMode
	DryRun    bool         // decide and report, write nothing; new products get ids -1, -2, ...
	BatchSize int          // cold-start embedding batch; 0 means DefaultBatchSize
	Logger    *slog.Logger // nil means discard
	// TaskPrefix is prepended to every embedded text; a threshold is only valid for the prefix
	// it was calibrated with (DefaultThreshold goes with DefaultTaskPrefix).
	TaskPrefix string
}

// prefixed prepends a task prefix to every text before embedding.
type prefixed struct {
	e      embed.Embedder
	prefix string
}

func (p prefixed) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	in := make([]string, len(texts))
	for i, s := range texts {
		in[i] = p.prefix + s
	}
	return p.e.Embed(ctx, in)
}

// Pipeline decides, for each seller entry, whether to link it to an existing Product or insert a
// new one. It runs on one goroutine and is not safe for concurrent use.
type Pipeline struct {
	store Store
	emb   embed.Embedder
	cfg   Config
	log   *slog.Logger

	// Per-run state, reset by Run.
	index       *vector.Index
	products    map[int64]catalog.Product
	categories  map[string]struct{}
	linked      map[linkKey]int64 // (seller, id) linked in this run → Product.Id
	updatedBy   map[int64]int     // Product.Id → index of the record that first changed it
	provisional int64             // last provisional id handed out in a dry run
}

type linkKey struct{ seller, id string }

// New validates cfg and returns a Pipeline.
func New(store Store, e embed.Embedder, cfg Config) (*Pipeline, error) {
	switch {
	case store == nil:
		return nil, fmt.Errorf("new pipeline: nil store: %w", errs.ErrInvalidInput)
	case e == nil:
		return nil, fmt.Errorf("new pipeline: nil embedder: %w", errs.ErrInvalidInput)
	case cfg.BatchSize < 0:
		return nil, fmt.Errorf("new pipeline: batch size %d: %w", cfg.BatchSize, errs.ErrInvalidInput)
	case cfg.Update < UpdateFill || cfg.Update > UpdateOverwrite:
		return nil, fmt.Errorf("new pipeline: update mode %d: %w", int(cfg.Update), errs.ErrInvalidInput)
	}
	if err := cfg.Policy.Validate(); err != nil {
		return nil, fmt.Errorf("new pipeline: %w", err)
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.TaskPrefix != "" {
		e = prefixed{e: e, prefix: cfg.TaskPrefix}
	}
	return &Pipeline{store: store, emb: e, cfg: cfg, log: log}, nil
}

// Run does the preflight and the cold start, then decides every record in order. It returns
// the report so far together with a fatal *RunError: a failed preflight or cold start, a decode
// error, ctx done, or an embedder error. A store error on one record is not fatal: the record is
// OutcomeFailed (its transaction rolled back) and the run continues.
func (p *Pipeline) Run(ctx context.Context, records iter.Seq2[ingest.Record, error]) (Report, error) {
	rep := Report{DryRun: p.cfg.DryRun, Threshold: p.cfg.Policy.Threshold, Update: p.cfg.Update}
	stop := func(stage Stage, index int, err error) (Report, error) {
		if isCancel(err) {
			stage = StageCancelled
		}
		rep.Stopped = &RunError{Stage: stage, Index: index, Processed: len(rep.Results), Err: err}
		rep.Updated = len(p.updatedBy)
		// The caller reports the RunError; this is only a trace.
		p.log.DebugContext(ctx, "ingest stopped", "stage", stage, "record", index, "err", err)
		return rep, rep.Stopped
	}

	if _, err := p.emb.Embed(ctx, []string{preflightText}); err != nil {
		return stop(StagePreflight, -1, fmt.Errorf("embedding a probe: %w", err))
	}
	if err := p.coldStart(ctx); err != nil {
		return stop(StageColdStart, -1, err)
	}
	rep.CatalogSize = len(p.products)
	p.log.InfoContext(ctx, "cold start done", "products", rep.CatalogSize, "dim", p.index.Dim())

	for rec, err := range records {
		index := len(rep.Results)
		if err != nil {
			return stop(StageDecode, index, err)
		}
		if err := ctx.Err(); err != nil {
			return stop(StageCancelled, rec.Index, err)
		}
		res, err := p.handle(ctx, rec)
		if err != nil {
			return stop(StageEmbed, rec.Index, err)
		}
		rep.add(res)
		p.log.DebugContext(ctx, "decided", "record", res.Index, "outcome", res.Outcome,
			"score", res.Score, "product", res.ProductID)
	}
	rep.Updated = len(p.updatedBy)
	return rep, nil
}

// coldStart loads every Product and fills a fresh index with their identity vectors.
func (p *Pipeline) coldStart(ctx context.Context) error {
	rows, err := p.store.ListProducts(ctx)
	if err != nil {
		return err
	}
	p.products = make(map[int64]catalog.Product, len(rows))
	p.categories = make(map[string]struct{})
	p.linked = make(map[linkKey]int64)
	p.updatedBy = make(map[int64]int)
	p.provisional = 0
	items := make([]vector.Item, len(rows))
	for i, r := range rows {
		p.products[r.ID] = r
		if r.Category != "" {
			p.categories[r.Category] = struct{}{}
		}
		items[i] = vector.Item{ID: r.ID, Text: identity(r)}
	}
	p.index, err = vector.Build(ctx, items, p.emb, p.cfg.BatchSize)
	return err
}

// handle decides one record. The error is fatal (embedding); store errors become OutcomeFailed.
func (p *Pipeline) handle(ctx context.Context, rec ingest.Record) (Result, error) {
	res := Result{
		Index: rec.Index, SellerName: rec.SellerName, SellerProductID: rec.ID,
		Identity: rec.Identity, Flags: rec.Flags,
	}
	if rec.Rejected() {
		res.Outcome, res.Reason = OutcomeRejected, rec.Reason
		return res, nil
	}

	key := linkKey{rec.SellerName, rec.ID}
	productID, linked := p.linked[key]
	if !linked {
		id, err := p.store.FindSellerLink(ctx, rec.SellerName, rec.ID)
		switch {
		case err == nil:
			productID, linked = id, true
		case !errors.Is(err, errs.ErrNotFound):
			return failed(res, err), nil
		}
	}
	if linked {
		res.Outcome, res.ProductID = OutcomeExisting, productID
		p.linked[key] = productID
		return p.write(ctx, res, rec, nil, productID)
	}

	vecs, err := p.emb.Embed(ctx, []string{rec.Identity})
	if err != nil {
		return res, fmt.Errorf("embedding record %d: %w", rec.Index, err)
	}
	if len(vecs) != 1 {
		return res, fmt.Errorf("embedding record %d: got %d vectors for 1 text: %w", rec.Index, len(vecs), errs.ErrUpstream)
	}
	vec := vecs[0]
	matchID, score, found, err := p.index.Nearest(vec)
	if err != nil {
		return res, fmt.Errorf("searching for record %d: %w", rec.Index, err)
	}
	if found {
		res.Score, res.MatchID, res.MatchIdentity = score, matchID, identity(p.products[matchID])
	}

	res.Kind = p.cfg.Policy.Decide(score, found)
	switch res.Kind {
	case KindDuplicate:
		res.Outcome, res.ProductID = OutcomeLinked, matchID
		link := &catalog.SellerLink{SellerName: rec.SellerName, SellerProductID: rec.ID, ProductID: matchID}
		res, err = p.write(ctx, res, rec, link, matchID)
		if err == nil && res.Outcome == OutcomeLinked {
			p.linked[key] = matchID
		}
		return res, err
	case KindNovel:
		return p.insert(ctx, res, rec, vec)
	default:
		return res, fmt.Errorf("record %d: unhandled decision %v", rec.Index, res.Kind)
	}
}

// write links (when link is non-nil) and applies the attribute update rule to productID in one
// store call, then mirrors a successful update in memory and in the index.
func (p *Pipeline) write(ctx context.Context, res Result, rec ingest.Record, link *catalog.SellerLink, productID int64) (Result, error) {
	product, known := p.products[productID]
	var upd *catalog.AttrUpdate
	var changes []Change
	if known {
		updatedBy, ok := p.updatedBy[productID]
		if !ok {
			updatedBy = -1
		}
		upd, changes, res.Notes = planUpdate(p.cfg.Update, product, rec, p.categories, updatedBy)
	}
	if link == nil && upd == nil {
		return res, nil
	}
	if !p.cfg.DryRun {
		if err := p.store.LinkSeller(ctx, link, upd); err != nil {
			return failed(res, err), nil
		}
	}
	if upd == nil {
		return res, nil
	}

	if upd.Brand != nil {
		product.Brand = *upd.Brand
	}
	if upd.Category != nil {
		product.Category = *upd.Category
	}
	p.products[productID] = product
	if _, ok := p.updatedBy[productID]; !ok {
		p.updatedBy[productID] = rec.Index
	}
	res.Changes = changes
	if res.MatchID == productID {
		res.MatchIdentity = identity(product)
	}

	vecs, err := p.emb.Embed(ctx, []string{identity(product)})
	if err != nil {
		return res, fmt.Errorf("re-embedding product %d: %w", productID, err)
	}
	if len(vecs) != 1 {
		return res, fmt.Errorf("re-embedding product %d: got %d vectors for 1 text: %w", productID, len(vecs), errs.ErrUpstream)
	}
	if err := p.index.Replace(productID, vecs[0]); err != nil {
		return res, fmt.Errorf("re-indexing product %d: %w", productID, err)
	}
	return res, nil
}

// insert creates a Product (or a provisional one in a dry run), links it and indexes it.
func (p *Pipeline) insert(ctx context.Context, res Result, rec ingest.Record, vec []float32) (Result, error) {
	np := catalog.NewProduct{Name: rec.Name, Brand: rec.Brand, Category: rec.Category}
	var id int64
	if p.cfg.DryRun {
		p.provisional--
		id = p.provisional
	} else {
		var err error
		if id, err = p.store.InsertProductAndLink(ctx, np, rec.SellerName, rec.ID); err != nil {
			return failed(res, err), nil
		}
	}
	if err := p.index.Add(id, vec); err != nil {
		return res, fmt.Errorf("indexing product %d: %w", id, err)
	}
	p.products[id] = catalog.Product{ID: id, Name: np.Name, Brand: np.Brand, Category: np.Category}
	p.linked[linkKey{rec.SellerName, rec.ID}] = id
	res.Outcome, res.ProductID = OutcomeInserted, id
	return res, nil
}

func failed(res Result, err error) Result {
	res.Outcome, res.Reason, res.ErrKind = OutcomeFailed, err.Error(), errKind(err)
	res.ProductID, res.Changes = 0, nil
	return res
}

func errKind(err error) string {
	switch {
	case errors.Is(err, errs.ErrConflict):
		return "conflict"
	case errors.Is(err, errs.ErrNotFound):
		return "not found"
	default:
		return "other"
	}
}

func identity(p catalog.Product) string { return ingest.Identity(p.Name, p.Brand, p.Category) }

func isCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
