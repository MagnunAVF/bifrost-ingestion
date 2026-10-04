# ADR 0005: Exact in-memory vector index, rebuilt at every start

- Status: accepted
- Date: 2026-10-03 (decided in ENG-5, recorded in ENG-7)
- Issue: ENG-7 (plan: docs/plans/ENG-7-architecture-docs.md, decision D1); design in
  docs/plans/ENG-5-vector-index.md

## Context

Every entry needs its nearest catalog product by cosine similarity. The catalog has 975
products; the embeddings have 768 dimensions. The machine has 8 GB of RAM shared with the
embedding model. The catalog file is shared with other systems, and M1 adds no tables to it.
The threshold margins are thin: 0.005 on the negative side and 0.009 on the positive side
(ADR 0002).

## Decision

`internal/vector.Index` stores unit-normalized float32 vectors back to back in one slice and
answers `Nearest` with an exact linear scan of dot products. It is built at every run from the
catalog (`vector.Build`, batches of 64) and lives only in memory. New products are added
(`Add`) and updated products replaced (`Replace`) as the run writes them. The package uses
only the standard library (depguard `vector-stdlib-only`).

## Evidence

Measured on the M1 at 768 dims (`make bench`, `BenchmarkNearest`):

| Products | Memory | Time per query |
| --- | --- | --- |
| 975 | ~3 MB | < 1 ms |
| 10k | 31 MB | 9.0 ms |
| 100k | 308 MB | 91 ms |

## Alternatives rejected

- **sqlite-vec / sqlite-vss.** Loadable C extensions: they need CGO or a custom build (ADR
  0004), and they write new tables into a shared file. At 975 rows they would not be faster
  than a 3 MB scan.
- **An approximate index (HNSW, IVF).** Approximate search can miss the true nearest
  neighbour. With margins under 0.01, a recall loss would change decisions, and it adds tuning
  parameters for no gain at this size.
- **Storing vectors in SQLite as BLOBs.** It would avoid the cold start, but it adds a table
  to a shared schema and ties stored vectors to one model and prefix, with invalidation to
  manage. Not worth it for an ~11 s cold start.

## Consequences

- Every run re-embeds the whole catalog: ~11 s for 975 rows at ~88 texts/s. That grows
  linearly with the catalog.
- The index sees the catalog as it was at cold start, plus this run's own writes. Products
  inserted by another system during a run are not in it.
- The index is not safe for concurrent use. M1 runs on one goroutine.
- Revisit at around 100k products, or when M4 adds concurrency: the M5 vector store
  (pgvector or Weaviate) replaces this index.
