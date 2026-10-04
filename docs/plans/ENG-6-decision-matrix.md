# ENG-6: Contextual upsert decision matrix (plan)

Status: approved 2026-10-03 (decisions D1-D12 below, revision 2). Plan 1.5, depends on
ENG-2, ENG-3, ENG-4, ENG-5 (all closed). Branch: `feat/ENG-6-decision-matrix`.

## Goal

`bifrost ingest` streams ProductEntry.json, embeds each accepted entry's identity string, finds
the nearest catalog Product and either links the seller to it (duplicate) or inserts a new
Product and links it (novel). One transaction per product, idempotent re-runs, `--dry-run`, and
a run report (inserted, linked, existing, rejected, failed, suspicious). The threshold is
calibrated on real data with Ollama.

## Flow

1. `catalog.Open` → `RequireMigrated` (refuses an unmigrated DB, per internal/catalog/CLAUDE.md).
2. Preflight: embed one probe text, so a missing Ollama or model fails in about a second with a
   hint, before the cold start (D8).
3. Cold start: `ListProducts` → `ingest.Identity(Name, Brand, Category)` → `vector.Build`
   (batch 64). An id → Product map (975 rows, a few KB) holds the current attributes.
4. For each record from `ingest.Decode`, in order:
   - rejected → `rejected` (reason + flags), no embedding.
   - `(SellerName, Id)` already linked in the DB, or earlier in this run → `existing` (first one
     wins, D5). No embedding and no new link, but the attribute update rule (D6) still runs
     against the linked product.
   - embed identity → `Index.Nearest` → `Policy.Decide`:
     - `Duplicate` → work out the attribute update (D6) → `LinkSeller` with the optional
       update, one transaction → `linked` (plus `updated` if the Product changed). On an
       update, the product is re-embedded and its index vector replaced.
     - `Novel` → `InsertProductAndLink` (one tx), then `Index.Add(newID, vec)` → `inserted`.
   - a store error → `failed` with the error, and the run continues.
5. Print the report (text to stdout); logs go to stderr through slog.

In `--dry-run`, nothing is written, but the run is simulated in memory (D7): a novel product
gets a provisional id (-1, -2, …) and its vector goes into the index, and attribute updates are
applied to the in-memory product map. The dry-run report then shows the same counts and the same
old → new values as a real run.

## Public API

### internal/catalog (plain types, so dedup never needs database/sql; D1)

```go
// Product is a catalog row; Brand and Category are "" for NULL.
type Product struct {
	ID       int64
	Name     string
	Brand    string
	Category string
}

// NewProduct is a Product to insert; "" Brand or Category is stored as NULL.
type NewProduct struct{ Name, Brand, Category string }

func (c *Catalog) RequireMigrated(ctx context.Context) error // errs.ErrNotMigrated when goose has pending migrations
func (c *Catalog) ListProducts(ctx context.Context) ([]Product, error)
// FindSellerLink returns the linked Product.Id, or errs.ErrNotFound.
func (c *Catalog) FindSellerLink(ctx context.Context, seller, sellerProductID string) (int64, error)
// AttrUpdate sets Brand and/or Category of one Product; a nil field is left alone. Name is
// never updated (D6).
type AttrUpdate struct {
	ProductID int64
	Brand     *string
	Category  *string
}

// LinkSeller inserts one SellerProduct row and applies upd (when non-nil) in one transaction;
// a duplicate key is errs.ErrConflict and then nothing is written. A nil link (an `existing`
// record) applies only upd.
func (c *Catalog) LinkSeller(ctx context.Context, link *SellerLink, upd *AttrUpdate) error
type SellerLink struct {
	SellerName, SellerProductID string
	ProductID                   int64
}
// InsertProductAndLink inserts the Product and its SellerProduct row in one transaction and
// returns the new Product.Id; on any error neither row exists.
func (c *Catalog) InsertProductAndLink(ctx context.Context, p NewProduct, seller, sellerProductID string) (int64, error)
```

New sqlc query `UpdateProductAttributes` (`UPDATE Product SET Brand = coalesce(sqlc.narg(brand),
Brand), Category = coalesce(sqlc.narg(category), Category) WHERE Id = ?`, which must affect
exactly one row, else `errs.ErrNotFound`); then `make generate`. New sentinel:
`errs.ErrNotMigrated` (D10).

### internal/vector (one additive method, so an updated product's vector stays current)

```go
// Replace normalizes a copy of vec and stores it in place of id's vector. An unknown id is
// ErrNotFound (new vector sentinel); dimension and zero/NaN checks match Add; on error the
// index is unchanged.
func (ix *Index) Replace(id int64, vec []float32) error
```

### internal/embed (additive, for the D8 hints)

```go
// StatusError is a non-200 answer from Ollama. It still wraps errs.ErrUpstream.
type StatusError struct {
	Code int
	Body string // first 512 bytes, trimmed
}
```

### internal/dedup

```go
// Kind is the decision for one entry. Callers must switch with a default branch: M2 adds
// KindAmbiguous (a band below Threshold) without changing existing values.
type Kind int

const (
	KindNovel Kind = iota + 1
	KindDuplicate
)

func (k Kind) String() string

// Policy turns a nearest-neighbour score into a Kind. M2 adds fields (e.g. AmbiguousFloor);
// the zero value of a new field must keep today's behaviour.
type Policy struct {
	Threshold float32 // score >= Threshold is a duplicate
}

func (p Policy) Validate() error                        // Threshold in (0, 1], else errs.ErrInvalidInput
func (p Policy) Decide(score float32, found bool) Kind  // found == false (empty index) → KindNovel

// Store is what the pipeline needs from the catalog; *catalog.Catalog satisfies it.
type Store interface {
	ListProducts(ctx context.Context) ([]catalog.Product, error)
	FindSellerLink(ctx context.Context, seller, sellerProductID string) (int64, error)
	LinkSeller(ctx context.Context, link *catalog.SellerLink, upd *catalog.AttrUpdate) error
	InsertProductAndLink(ctx context.Context, p catalog.NewProduct, seller, sellerProductID string) (int64, error)
}

// UpdateMode says what a matched entry may change on its catalog Product (D6).
type UpdateMode int

const (
	UpdateFill      UpdateMode = iota // default: set Brand/Category only where the Product has NULL
	UpdateNone                        // link only; drift is reported as a Note
	UpdateOverwrite                   // also replace a differing value, under the guards in D6
)

// ParseUpdateMode parses "fill", "none" or "overwrite" (the --update flag).
func ParseUpdateMode(s string) (UpdateMode, error)

type Config struct {
	Policy    Policy
	Update    UpdateMode
	DryRun    bool
	BatchSize int          // cold-start embedding batch; 0 means 64
	Logger    *slog.Logger // nil means discard
}

type Pipeline struct{ /* store, embedder, cfg, index, products */ }

func New(store Store, e embed.Embedder, cfg Config) (*Pipeline, error)

// Run does the preflight and the cold start, then decides every record in order. It returns
// the report so far together with a fatal *RunError (D8). Store errors on one record are not
// fatal.
func (p *Pipeline) Run(ctx context.Context, records iter.Seq2[ingest.Record, error]) (Report, error)

// Stage says where a run stopped.
type Stage string // "preflight", "cold start", "decode", "embed", "cancelled"

// RunError is a fatal error with what the operator needs: where it stopped, how far it got,
// and that a re-run is safe. errors.Is/As see through it to the cause (errs.ErrUpstream,
// *embed.StatusError, context.Canceled, errs.ErrInvalidInput).
type RunError struct {
	Stage     Stage
	Index     int    // record index being handled, -1 before the first record
	Processed int    // records fully handled before the stop
	Err       error
}

func (e *RunError) Error() string // "stopped at embed (record 37, 36 done): …"
func (e *RunError) Unwrap() error

type Outcome string

const (
	OutcomeInserted Outcome = "inserted" // novel: Product + SellerProduct
	OutcomeLinked   Outcome = "linked"   // duplicate: SellerProduct only
	OutcomeExisting Outcome = "existing" // (seller, id) already linked: nothing written
	OutcomeRejected Outcome = "rejected" // ingest rejected the record
	OutcomeFailed   Outcome = "failed"   // store error; transaction rolled back
)

type Result struct {
	Index           int
	SellerName      string
	SellerProductID string
	Identity        string
	Outcome         Outcome
	Kind            Kind    // 0 for existing / rejected
	Score           float32 // nearest score, when embedded
	MatchID         int64   // nearest Product.Id, when found
	MatchIdentity   string
	ProductID       int64   // the product linked to (provisional < 0 in dry run)
	Reason          string  // reject reason or error text
	ErrKind         string  // for failed: "conflict", "not found", "foreign key", "other"
	Flags           []ingest.Flag
	Changes         []Change // attribute updates applied (or simulated) on ProductID
	Notes           []string // drift left alone, with why (D6)
}

// Change is one attribute update, kept for the report: old "" means it was NULL.
type Change struct{ Field, Old, New string }

// Suspicious reports whether the record had flags (rejects included).
func (r Result) Suspicious() bool

type Report struct {
	DryRun                                                   bool
	Threshold                                                float32
	Update                                                   UpdateMode
	CatalogSize                                              int
	Inserted, Linked, Existing, Rejected, Failed, Suspicious int
	Updated                                                  int // distinct Products changed
	Results                                                  []Result
	Stopped                                                  *RunError // nil when the run finished
}

// WriteText prints a summary, then one line per non-trivial result (rejected, failed,
// suspicious, inserted, linked with notes); with verbose (always in dry run) every result
// with its decision, score and nearest match.
func (r Report) WriteText(w io.Writer, verbose bool) error
```

### cmd/bifrost `ingest`

```
bifrost ingest --db PATH --input PATH [--model nomic-embed-text] [--ollama-url URL]
               [--threshold T] [--update fill|none|overwrite] [--dry-run]
```

Exit codes: 0 when the run completed with no `failed` result; 1 on a fatal error (the partial
report is still printed); 2 on a usage error (missing --db/--input, threshold out of (0, 1], bad
--update, extra arguments); 3 when the run completed but some records `failed`. The input is
opened with os.Open and streamed (never read whole). The Makefile's `run` target already passes
the required flags.

Error output (D8). On a fatal error, stderr gets one block: what happened, where it stopped, what
was written, and what to do. The hint comes from the cause, matched with errors.Is/As, never by
parsing message text:

```
ingest: stopped at embed (record 37 of the input, 36 done): calling ollama: connection refused
  hint: Ollama is not reachable at http://localhost:11434. Start it with `ollama serve`.
  state: 12 linked, 0 inserted, 2 updated are committed; nothing after record 36 was written.
  next: re-running the same command is safe (already-linked entries are skipped).
```

| Cause | Hint |
| --- | --- |
| `syscall.ECONNREFUSED` / DNS error | Ollama not reachable at URL; `ollama serve` or check `--ollama-url` |
| `*embed.StatusError` 404 | model not pulled; `ollama pull <model>` |
| other `*embed.StatusError` | Ollama answered `<code>`: `<body>` |
| `context.DeadlineExceeded` from the client timeout | the model is still loading or the machine is swapping; retry |
| `errs.ErrUpstream`, other cases (count or dimension mismatch) | wrong or changed model; check `--model` |
| `errs.ErrNotMigrated` | run `bifrost migrate --db PATH` first |
| `errs.ErrInvalidInput` from decode | the payload is not one JSON array (records before the error are processed) |
| `context.Canceled` (Ctrl-C) | interrupted; re-run is safe |

Records that `failed` are listed in the report with index, seller, id, `ErrKind` and the error,
followed by a one-line summary ("3 records failed and were rolled back; see above"), and the exit
code is 3.

## Tests (TDD; unit tests use embed.Fake or stubs, never Ollama)

### internal/catalog/store_test.go (on a migrated fixture copy)

| Case | Expect |
| --- | --- |
| RequireMigrated on a migrated copy | nil |
| RequireMigrated on the raw fixture (00002 pending) | `errs.ErrNotMigrated` |
| ListProducts | 975 rows; Product 113 has Brand ""; ids 1..975 in order |
| FindSellerLink, none | `errs.ErrNotFound` |
| LinkSeller then FindSellerLink | returns the product id |
| LinkSeller twice, same (seller, id) | second is `errs.ErrConflict`, one row |
| LinkSeller, same id for another seller | ok (unique per seller) |
| LinkSeller to a missing Product | error (FK), no row |
| LinkSeller with an update setting Brand on Product 113 (NULL) | link row + Brand set; Category and Name untouched |
| LinkSeller with an update setting only Category | Brand untouched (nil field = keep) |
| LinkSeller whose link conflicts, with an update | `errs.ErrConflict`; Product unchanged (rolled back) |
| LinkSeller(nil link, update) | only the update; no SellerProduct row |
| update on a missing Product id | `errs.ErrNotFound`; link rolled back |
| InsertProductAndLink | new id 976; Brand "" stored as NULL; one link row |
| InsertProductAndLink whose link conflicts | `errs.ErrConflict`; Product count unchanged (rolled back) |
| InsertProductAndLink stores `TestBrand'; SELECT 1; --` verbatim | read back byte-equal; tables intact |
| ctx cancelled | error, nothing written |

### internal/vector/index_test.go (new rows for Replace)

| Case | Expect |
| --- | --- |
| Replace then Nearest with the new vector | same id, score ≈ 1; the old vector no longer scores 1 |
| Replace an unknown id | `ErrNotFound`; Len unchanged |
| Replace with the wrong dimension / zero / NaN | error; old vector still found |
| Replace keeps insertion order (tie-break) | ties still go to the earlier-added id |
| caller mutates vec after Replace | index unaffected |

### internal/embed/ollama_test.go (new rows)

| Case | Expect |
| --- | --- |
| 404 `{"error":"model \"x\" not found"}` | `errors.As` → `*StatusError{Code: 404}`; still `errs.ErrUpstream` |
| 500 | `*StatusError{Code: 500}` with the body snippet |

### internal/dedup/update_test.go (the pure update rule, D6)

`planUpdate(mode, product catalog.Product, rec ingest.Record, categories set) (*AttrUpdate, []Change, []string)`

| Mode | Catalog → entry | Expect |
| --- | --- | --- |
| fill | Brand NULL → `Acme` | set Brand |
| fill | Brand `Levis` → `Levi's` | no change; Note "brand differs: Levi's (entry) vs Levis (catalog), kept" |
| fill | Brand NULL → entry Brand blank | no change, no note |
| none | Brand NULL → `Acme` | no change; Note |
| overwrite | Brand `Levis` → `Levi's` | set Brand |
| overwrite | Category `Photography` → `Photo` (not a catalog category) | no change; Note "Photo is not a catalog category" |
| overwrite | Category `Kitchen` → `Outdoor` (a catalog category) | set Category |
| any | Name differs (`Câmera` vs `Camera`, Portuguese name) | Name never changes; no note (the name drives matching) |
| any | equal after case and whitespace folding (`acme` vs `Acme`) | no change, no note |
| any | entry has suspicious flags (SQLi) | no change; Note "flagged input, not applied" |
| overwrite | product already changed earlier in this run by another record | no change; Note "already updated by record N" (first wins) |

### internal/dedup/decision_test.go

| Case | Expect |
| --- | --- |
| score > threshold | Duplicate |
| score == threshold | Duplicate (boundary is inclusive) |
| score just below | Novel |
| found == false | Novel |
| score -1 | Novel |
| Validate: 0, -0.1, 1.01, NaN | `errs.ErrInvalidInput` |
| Validate: 1 and 0.85 | ok |
| Kind.String for each value and an unknown value | "novel", "duplicate", "Kind(9)" |

### internal/dedup/pipeline_test.go (real catalog on a migrated fixture copy unless noted)

The fake embedder is not semantic: identical identity → score 1, otherwise about 0. With
threshold 0.9, an entry whose identity is a catalog row's is a duplicate, anything else is novel.
A stub embedder that returns chosen vectors covers the exact boundary.

| Case | Expect |
| --- | --- |
| entry identical to Product 21 | linked to 21, score ≈ 1; Product count unchanged; 1 link |
| unseen entry | inserted as 976 + link; vector added (a 2nd seller with the same identity is linked to 976) |
| two entries with the same new identity, dry run | inserted (-1) then linked (-1); DB file bytes unchanged |
| stub embedder, score exactly at threshold / just below | linked / inserted |
| rejected record | rejected; reason and flags in the report; embedder never called for it |
| same (seller, id) twice in one input (fixture 55/76 shape) | 2nd is existing; one link |
| re-run the same input (real run twice) | 2nd run: all existing, 0 inserted/linked; row counts equal |
| SQLi brand with a valid UUID (synthetic) | inserted; Brand stored verbatim; Product +1, SellerProduct +1, nothing else changes |
| store stub fails InsertProductAndLink on record 2 of 3 | 2 is failed, 1 and 3 succeed; Run returns no error |
| store stub fails ListProducts | `*RunError{Stage: "cold start", Index: -1}` |
| embedder fails the preflight | `*RunError{Stage: "preflight"}`; ListProducts never called |
| embedder error on record 2 | `*RunError{Stage: "embed", Index: 2, Processed: 2}`; report has records 0-1; `errors.Is(err, errs.ErrUpstream)` |
| decode error mid-stream (`[{…}, nope`) | results before it kept; `*RunError{Stage: "decode"}`, `errs.ErrInvalidInput` |
| ctx cancelled after the 1st record | `*RunError{Stage: "cancelled"}`; at most one record written |
| store stub returns ErrConflict / other | failed with ErrKind "conflict" / "other" |
| duplicate of Product 113 (Brand NULL), entry Brand `Acme`, fill | linked + updated in one tx; Change{brand, "", Acme}; index vector replaced (an entry with the new identity now scores ≈ 1) |
| same, dry run | Changes reported, DB bytes unchanged; a later record sees the simulated Brand |
| duplicate with Category drift (`Photo` vs `Photography`), overwrite | linked; Note; Product unchanged |
| `existing` record whose Brand fills a NULL | no new link; Product updated (update-only tx) |
| update fails (stub) | the link is rolled back too; failed |
| re-run after updates | 0 links, 0 updates (values already equal) |
| report counts | Inserted+Linked+Existing+Rejected+Failed == len(Results); Updated = distinct products changed; Suspicious counts flagged records |
| full fixture, fake embedder, real run | 3 rejected (92, 180, 268), 1 suspicious (180), 1 existing (76); the rest add exactly the expected rows (the fake makes the semantic pair 57/64, the doubled-space and accent variants "novel"; this case pins counts, not quality) |

### internal/dedup/report_test.go

Summary line counts (including Updated); dry-run header ("DRY RUN: nothing written"); verbose
lines show decision, score (3 decimals) and the nearest match identity; changes as `brand: (null)
→ Acme`; rejects show their reason; failed records list ErrKind and the error; a stopped run ends
with the RunError line; output is stable (golden string).

### cmd/bifrost/main_test.go and hints_test.go

New rows: `ingest` with no flags → 2 "--db is required"; missing --input → 2; `--threshold 1.5` →
2; `--update bogus` → 2; extra argument → 2; `ingest -h` → 0; unmigrated DB → 1 with the
`bifrost migrate` hint; missing input file → 1; `--ollama-url http://127.0.0.1:1` (nothing
listening) → 1 with the "not reachable" hint and the "re-run is safe" line, in well under a
second (preflight). `hint(err)`: one row per row of the hint table above, with wrapped errors.
The full command needs Ollama, so wiring is covered by `make e2e` (an integration test running
`run([]string{"ingest", ...})` on fixture copies with `--dry-run`).
**The existing row "ingest is a stub until ENG-6" has to be replaced** (D11).

### internal/dedup/calibration_integration_test.go (`//go:build integration`, real Ollama)

- Negatives: for each catalog Product, the score of its nearest *other* Product (leave-one-out
  on 975 distinct products) → max, p99, p95.
- Positives: each accepted fixture entry against its known catalog match (the normalized match
  key from docs/data-notes.md resolves 264, plus the two Portuguese pairs 57→21 and 64→28) →
  min, p1, p5.
- Prints both distributions with and without the nomic `clustering: ` prefix (D9) and the
  margin; the test fails if the configured default threshold doesn't separate them.

## Tasks (one commit each)

1. docs: this plan (`docs(plans): ENG-6 decision matrix plan (ENG-6, task 1/8)`).
2. catalog: plain-typed store methods, `UpdateProductAttributes` query, `RequireMigrated`,
   `errs.ErrNotMigrated`; internal/catalog/CLAUDE.md line about WithTx updated (D1).
3. vector: `Index.Replace`. embed: `StatusError`.
4. dedup: `Kind`, `Policy.Decide`, `Policy.Validate`, `UpdateMode`, `planUpdate`.
5. dedup: `Pipeline.Run`, `RunError`, `Result`, `Report`, `WriteText`.
6. cmd: `bifrost ingest` wiring, flags, exit codes, hints; e2e test.
7. Calibration: integration test, `--dry-run` on the fixture with Ollama, default threshold,
   ADR 0002, M1 Decisions, open questions 4-7 and 9 answered in data-notes.
8. ADR 0003 (catalog product updates, D6) and the remaining M1 Decisions lines.

## Decisions

All approved 2026-10-03 (D5-D8 in their revised form).

- **D1. Plain-typed store API in catalog.** dedup can't import `database/sql` (depguard), but
  `db.InsertProductParams` uses `sql.NullString`. catalog wraps the sqlc queries and owns the
  transactions (`LinkSeller`, `InsertProductAndLink`). This differs from
  internal/catalog/CLAUDE.md ("dedup writes through `catalog.WithTx(func(q *db.Queries))`"),
  and I'll update that line.
- **D2. Kind is an int enum starting at 1, and Policy is a struct.** M2 adds `KindAmbiguous`
  and a Policy field without breaking callers.
- **D3. The threshold is inclusive** (`score >= T` is a duplicate). Placeholder default 0.90
  until task 7 sets the calibrated value.
- **D4. No exact-match fast path** (open question 6): every new decision goes through
  embeddings, so calibration and behaviour stay one code path. Idempotency skips are the only
  shortcut.
- **D5. Idempotency key (SellerName, Id), first one wins for the link** (open question 5): a
  repeated key, in the DB or earlier in the run, is `existing`. It is not embedded and no
  second link is made, but the D6 update rule still runs against the product the key is
  linked to, so a seller re-sending an entry with a Brand that was missing fills it in. Fixture
  76 (same Id as 55, differing only in the Name accent) is `existing`, and Name is never
  updated, so it changes nothing.
- **D6. Same seller, same product, different Ids → one link per Id** (open question 4), since
  ids are unique per seller (ADR 0001). **Matched entries may update their Product's
  attributes** (open question 9), controlled by `--update`:
  - `fill` (**default**): set Brand or Category only where the Product has NULL. It never
    overwrites, so it is monotonic and idempotent, and it can't be fought over by two sellers.
    The catalog has 119 NULL Brands and 34 NULL Categories that this can enrich.
  - `overwrite` (opt-in): also replaces a differing value, with these guards:
    - Category only becomes a value already in the catalog's category set. This stops
      `Photography` → `Photo` (entry 87) and keeps the 43-category vocabulary.
    - Brand may change, so `Levis` → `Levi's` (entry 188) is applied.
    - In one run, the first record that changes a product wins, and later disagreements
      become Notes (the result is deterministic and order-stable).
  - `none`: link only, and report drift as Notes.
  - In every mode: **Name is never updated** (it carries the matching signal, and entries
    include Portuguese translations and spacing or accent variants). Equal-after-folding
    values (case or whitespace) are not changes. Flagged (suspicious) inputs are never applied
    to an existing Product (they are still stored verbatim when they create a new one).
  - An update happens in the same transaction as the link (or alone for `existing`), so a
    failed link rolls the update back. The product is then re-embedded and its vector replaced
    (`Index.Replace`), so later records match against current data. That is one extra embed
    per updated product.
  - No audit table (the schema is shared and M1 adds no tables). The report lists every
    change as `field: old → new` with the record that caused it. An ADR (0003) records the
    rule.
  - Why not plain last-write-wins: a seller with bad data could degrade a shared catalog, and
    two sellers would flip a value back and forth on every run.
- **D7 (recommended): the dry run simulates the whole run in memory.** Provisional negative
  ids go into the index for new products, and attribute updates are applied to the in-memory
  map, so a dry run reports the same counts, decisions and changes a real run would. This is
  what makes `--dry-run` usable for calibration. The alternative, evaluating every entry against
  the untouched catalog, is simpler but would report each repeated new product as N inserts
  instead of 1 insert + (N-1) links, and its counts wouldn't match the real run.
- **D8. What stops the run:** decode errors, ctx done, cold-start errors and embedder errors
  (if Ollama is down, every record would fail the same way). A store error on one record is
  `failed` (rolled back), and the run continues. Improvements to the error reporting:
  - **Preflight:** embedding one probe before the cold start, so a missing Ollama or model
    fails in about a second instead of after the 975-row cold start.
  - **`RunError`** carries the stage, the record index and the count processed. stderr prints
    what happened, where, what is already committed, a cause-specific hint (table under
    "cmd/bifrost") and that a re-run is safe.
  - **Exit code 3** means completed with failed records, distinct from 1 (fatal), so scripts
    can tell them apart.
  - **Failed records carry an ErrKind** (conflict, not found, foreign key, other).
  - **`embed.StatusError`** (additive) lets hints match on the HTTP status rather than the
    message text.
- **D9. Nomic task prefix** (deferred by ENG-4): the calibration measures both; the default
  stays no prefix unless `clustering: ` separates clearly better, in which case it is applied
  in the pipeline for catalog rows and entries alike (the identity string itself is
  unchanged).
- **D10. New sentinel `errs.ErrNotMigrated`** for `RequireMigrated`.
- **D11. Replace the cmd test row "ingest is a stub until ENG-6"** with the new ingest rows.
  It is an existing test, and the stub it pins is exactly what this issue removes.
- **D12. "The injection payload is stored verbatim" conflicts with the ENG-3 decision** that
  fixture 180 is rejected (malformed Id). Fixture 180 stays rejected and reported. The
  acceptance criterion is covered by a synthetic entry with a valid UUID and the same SQLi
  brand (catalog and pipeline tests above). Calibration (open question 7) uses the catalog
  leave-one-out negatives instead of a new testdata/calibration set.
