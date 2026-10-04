# Bifröst

Bifröst ingests seller catalogs into a shared SQLite product catalog and deduplicates them
semantically. For every seller entry it finds the nearest catalog product with local
embeddings (Ollama) and either links the seller to it or inserts a new product. Every run ends
with a report: inserted, linked, existing, rejected, failed and suspicious entries.

M1 (v0.1.0) is a local CLI built for an Apple M1 with 8 GB of RAM. How it works and why:
[docs/architecture.md](docs/architecture.md).

## Quick start

Prerequisites: Go 1.27.1 (see `go.mod`) and [Ollama](https://ollama.com).

```sh
# 1. Install Ollama and start it (or open the Ollama app)
brew install ollama
ollama serve

# 2. In another terminal, pull the embedding model (274 MB)
ollama pull nomic-embed-text

# 3. Dry run on the fixtures: copies testdata/catalog.db to tmp/catalog.work.db,
#    migrates the copy and reports every decision without writing
make run

# 4. Real run on the working copy, then start again from a fresh copy
make run WRITE=1
make reset-db
```

`make run` never touches `testdata/`. It works on `tmp/catalog.work.db`; override the paths
with `make run DB=... INPUT=...`.

The dry run on the fixture prints (the first lines; ~16 s on the M1, most of it embedding the
975-product catalog):

```
Bifröst ingest report (DRY RUN: nothing written)
threshold 0.975, update fill, catalog 975 products
records 269: inserted 2, linked 263, existing 1, rejected 3, failed 0, suspicious 1, products updated 0

rejected (3):
  #92 FitnessCenter ddddeee-ffff-4000-1111-222233334444: Id: not a UUID
  #180 MegaStore 09835342345-4678-9abc-def012345678: Id: not a UUID
  #268 SmartHomeStore uddd0000-eeee-4111-ffff-aaaa22223333: Id: not a UUID

suspicious (1):
  #180 MegaStore 09835342345-4678-9abc-def012345678: Brand: SQL-like content

inserted (2):
  #57 FootwearHub a7a7a7a7-b8b8-4c9c-d0d0-e1e1e1e1e1e1 → product -1 "Roteador WiFi 6 TP-Link | brand: TP-Link | category: Networking" (nearest 21 at 0.951)
  #64 SuperMart b4b4b4b4-c5c5-4d6d-e7e7-f8f8f8f8f8f8 → product -2 "Processador AMD Ryzen 9 7950X | brand: AMD | category: Components" (nearest 28 at 0.962)

notes (2):
  #87 product 18: category differs: Photo (entry) vs Photography (catalog), kept
  #188 product 322: brand differs: Levi's (entry) vs Levis (catalog), kept

decisions:
  #0 linked duplicate 1.000 → product 2 "Smartphone Galaxy S23 | brand: Samsung | category: Electronics", ...
```

`make run WRITE=1` reports the same counts. Running it again creates nothing: all 266 accepted
entries are `existing`.

## CLI reference

```
bifrost migrate --db PATH
bifrost ingest  --db PATH --input PATH [--model nomic-embed-text] [--ollama-url URL]
                [--threshold 0.975] [--update fill|none|overwrite] [--dry-run]
bifrost version
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--db` | required | An existing catalog.db. `ingest` refuses an unmigrated one; run `migrate` first |
| `--input` | required | ProductEntry.json, a JSON array of `{Id, SellerName, Name, Brand, Category}` (streamed, 64 MiB max) |
| `--model` | `nomic-embed-text` | Ollama embedding model. The default threshold is calibrated for this model only |
| `--ollama-url` | `http://localhost:11434` | Ollama base URL |
| `--threshold` | `0.975` | Cosine score in (0, 1] at or above which an entry links to its nearest product ([ADR 0002](docs/adr/0002-similarity-threshold.md)) |
| `--update` | `fill` | What a matched entry may change on its product: `fill` NULL Brand/Category only, `overwrite`, or `none` ([ADR 0003](docs/adr/0003-catalog-product-updates.md)) |
| `--dry-run` | off | Decide and report every entry, write nothing |

Exit codes of `ingest`:

| Code | Meaning |
| --- | --- |
| 0 | The run completed |
| 1 | Fatal error (Ollama unreachable, model missing, unmigrated DB, broken payload, Ctrl-C). The partial report is printed, with a hint; re-running is safe |
| 2 | Usage error |
| 3 | The run completed, but some records failed and were rolled back |

The report goes to stdout and the logs to stderr, so `make run WRITE=1 > report.txt` keeps the
report. Keep it: it is the only record of attribute changes.

## Development

| Command | What it does |
| --- | --- |
| `make check` | Lint (golangci-lint with depguard and gosec), sqlc diff, `go mod tidy` diff, unit tests with `-race`, govulncheck. What CI runs; never needs Ollama |
| `make e2e` | Integration tests against a real Ollama, including the threshold calibration |
| `make fuzz` | Fuzz the field sanitizer (`FUZZTIME=60s` by default) |
| `make bench` | Benchmarks, including the vector index |
| `make generate` | Regenerate the sqlc code after editing `internal/catalog/queries` |
| `make help` | Every target |

`make check` needs [golangci-lint](https://golangci-lint.run) v2 on the PATH. sqlc, goose and
govulncheck run as Go tool directives, so `go.mod` pins them.

The fixtures in `testdata/` are read-only. Tests copy them into `t.TempDir()`.

## Documentation

- [Architecture](docs/architecture.md): pipeline, components, decision matrix, trade-offs,
  defensive strategy, limits and the path to M2-M5
- [Data notes](docs/data-notes.md): the fixtures, measured, and the identity-string contract
- ADRs: [0001 SellerProductId as TEXT](docs/adr/0001-seller-product-id-text.md),
  [0002 similarity threshold](docs/adr/0002-similarity-threshold.md),
  [0003 catalog product updates](docs/adr/0003-catalog-product-updates.md),
  [0004 pure-Go SQLite](docs/adr/0004-pure-go-sqlite.md),
  [0005 in-memory exact index](docs/adr/0005-in-memory-exact-index.md),
  [0006 embedding model](docs/adr/0006-embedding-model.md)
- [M1 milestone](docs/milestones/M1.md) and [roadmap](docs/plans/ROADMAP.md)

## License

See [LICENSE](LICENSE).
