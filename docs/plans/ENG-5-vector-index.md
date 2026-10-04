# ENG-5: In-memory spatial similarity engine (plan)

Status: approved 2026-10-03 (decisions 1-8 below). Plan 1.4, lane B, depends on ENG-1 (closed).
Branch: `feat/ENG-5-vector-index`.

## Goal

Pure-Go cosine similarity and an exact in-memory nearest-neighbour index that the ENG-6 decision
engine queries, plus a cold-start `Build` that embeds catalog identity strings without importing
the database.

## Public API (internal/vector, standard library only)

depguard (`vector-stdlib-only`) forbids importing `internal/embed` and `internal/platform/errs`,
so the package declares its own `Embedder` (same shape as `embed.Embedder`, satisfied by
`embed.Fake` and `embed.Ollama`) and its own sentinels.

```go
var (
	ErrZeroVector        = errors.New("zero or empty vector")
	ErrInvalidVector     = errors.New("vector has NaN or Inf")
	ErrDimensionMismatch = errors.New("dimension mismatch")
	ErrDuplicateID       = errors.New("duplicate id")
	ErrInvalidBatchSize  = errors.New("batch size must be positive")
)

func Normalize(v []float32) ([]float32, error) // new unit-length copy; v untouched
func Cosine(a, b []float32) (float32, error)    // any non-zero vectors, clamped to [-1, 1]

// Index: exact brute-force search over unit vectors in one contiguous []float32 slab.
// Not safe for concurrent use.
func NewIndex(capacity int) *Index // capacity is a hint, in vectors
func (ix *Index) Add(id int64, vec []float32) error
func (ix *Index) Nearest(vec []float32) (id int64, score float32, ok bool, err error)
func (ix *Index) Len() int
func (ix *Index) Dim() int // 0 until the first Add

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
type Item struct {
	ID   int64  // Product.Id
	Text string // ingest.Identity
}
func Build(ctx context.Context, items []Item, e Embedder, batchSize int) (*Index, error)
```

- `Add` copies and normalizes once; the first `Add` fixes the dimension; a failed `Add` leaves the
  index unchanged.
- `Nearest` normalizes a copy of the query, scores by dot product, clamps to [-1, 1]; ties go to
  the earliest insert; `ok == false` only for an empty index.
- `Build` presizes the slab from `len(items)`, embeds `batchSize` texts per call, checks ctx
  between batches, fails on a vector count mismatch, and names the item ID in errors.

## Tests

- `vector_test.go`: Normalize (3-4-5, already unit, zero/empty/nil, NaN/Inf, 1e20 no overflow,
  input untouched); Cosine (identical → 1, scaled → 1, orthogonal → 0, opposite → -1, dimension
  mismatch, zero on either side).
- `index_test.go`: Nearest on empty index, identical/orthogonal/opposite, closest of three,
  ties, wrong dimension, zero/NaN query, score ≤ 1; Add fixes Dim, dimension mismatch and zero
  vector leave the index unchanged, duplicate ID, caller mutation after Add, growth past capacity.
- `build_test.go` (`embed.Fake` + stub): 5 items in batches 2/2/1 then every item finds itself
  at ≈ 1; no items → no Embed call; batchSize ≤ 0; embedder error on batch 2; too few vectors;
  zero vector names the item; dimension change between batches; duplicate IDs; ctx cancelled.
- `bench_test.go`: `BenchmarkNearest` at n=10k and n=100k, d=768, seeded `math/rand/v2`,
  `b.Loop()`, index bytes reported with `b.ReportMetric`.

Memory: 768 × 4 B ≈ 3 KiB per vector (+ 8 B id): catalog 975 ≈ 3 MB, 10k ≈ 31 MB, 100k ≈ 307 MB.

Measured (`make bench`, Apple M1): `BenchmarkNearest` n=10k 9.0 ms/op, n=100k 91 ms/op; one
3 KiB allocation per query (the normalized query copy, which also keeps the float32 dot products
from overflowing on huge inputs).

## Tasks (one commit each)

1. `Normalize` and `Cosine`.
2. `Index` (`Add`, `Nearest`, `Len`, `Dim`).
3. `Build` and the `Embedder` interface.
4. Benchmarks, this plan and the M1 Decisions.

## Decisions (approved 2026-10-03)

1. `Nearest` returns `(id, score, ok, err)`: an empty index is `ok == false`, a dimension
   mismatch or bad query is an error (the issue's 3-value form could not report the mismatch).
2. Sentinel errors live in `vector`, not `internal/platform/errs`, to keep depguard's
   stdlib-only rule.
3. IDs are `int64` (`Product.Id`).
4. Adding an existing ID is `ErrDuplicateID`.
5. No locking: ENG-6 runs single-goroutine; documented as not safe for concurrent use.
6. Scores clamped to [-1, 1]; ties go to the earliest insert.
7. Exact brute-force float32 scan over a contiguous slab; no ANN.
8. No top-k query now; ENG-6 adds it if calibration needs the top-1/top-2 margin.
