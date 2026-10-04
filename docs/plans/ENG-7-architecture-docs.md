# ENG-7: Technical documentation (plan)

Status: approved 2026-10-03 (decisions D1-D5 below). Plan 1.6, depends on ENG-6 (closed; the
issue body says ENG-5, the milestone table says 1.5). Branch: `docs/ENG-7-architecture-docs`.

## Goal

A professional architecture document for the Bifröst M1 system, a README quick start, and ADRs
that are linked and consistent with the code. No Go code changes.

## Findings before writing

- No README exists.
- Three of the four trade-offs the issue asks for (pure-Go SQLite vs CGO, in-memory index vs
  SQLite vector extensions, embedding model) have no ADR.
- The nomic-embed-text choice has no recorded comparison against other models, only
  measurements (ENG-4, ENG-6).
- ADR 0001 says the handling of malformed ids is "still an open question"; ENG-3 answered it.
- All 11 M1 open questions are answered, but M1.md still says "Pending answers".

## Public API

None.

## Files

| File | Change |
| --- | --- |
| `README.md` (new) | What it is; quick start (install Ollama, `ollama pull nomic-embed-text`, `make run`, `make run WRITE=1`, `make reset-db`); CLI reference (`migrate`, `ingest`, flags, defaults, exit codes 0/1/2/3); a real report from `make run`; dev commands; links |
| `docs/architecture.md` (new) | Context; pipeline diagram (Mermaid); components and depguard boundaries; data flow (sequence diagram); decision matrix and `--update` rules; trade-offs; defensive strategy; hardware budget; known limits and the path to M2-M5; ADR index |
| `docs/adr/0004-pure-go-sqlite.md` (new) | modernc.org/sqlite over a CGO driver |
| `docs/adr/0005-in-memory-exact-index.md` (new) | Exact brute-force scan in RAM over sqlite-vec / ANN |
| `docs/adr/0006-embedding-model.md` (new) | nomic-embed-text via local Ollama |
| `docs/adr/0001-seller-product-id-text.md` | One dated "Update" line: ENG-3 answered the open question; the original text is unchanged |
| `docs/milestones/M1.md` | ENG-7 Decisions lines; "Open questions" marked all answered |

## Verification (docs only; replaces the test table)

| Acceptance criterion | Check |
| --- | --- |
| architecture.md content | Every section present; every number traced to a code constant or a dated Decisions line |
| Trade-offs | Each one: choice, alternatives, evidence, when to revisit; unmeasured claims labelled as such |
| Defensive strategy | Each claim points to the code or test that enforces it |
| Known limits, M2-M5 | Each limit mapped to a ROADMAP issue |
| ADRs linked and consistent | Relative links resolve (one-off script, not committed); flags and defaults match `cmd/bifrost/ingest.go` and `internal/dedup/pipeline.go` |
| README quick start | Run verbatim from a fresh `make reset-db`; the pasted report is real output |
| Mermaid | Renders in the PR's GitHub preview |
| Gate | `make check` green |

## Tasks (one commit each)

1. docs: this plan (`docs(plans): ENG-7 architecture docs plan (ENG-7, task 1/5)`).
2. `docs/architecture.md`.
3. ADRs 0004-0006 and the ADR 0001 update line.
4. `README.md` quick start with real output.
5. M1.md Decisions and open-questions status.

## Decisions

All approved 2026-10-03.

- **D1. Backfill three short ADRs** (0004 modernc, 0005 exact in-memory scan, 0006
  nomic-embed-text); architecture.md summarizes and links them.
- **D2. Embedding model is documented as chosen and measured** (local, 274 MB, 768 dims, fits
  8 GB, calibrated in ADR 0002); no other model was benchmarked, and that is listed as a known
  limit.
- **D3. Accepted ADRs are not rewritten.** ADR 0001 gets a dated "Update" line.
- **D4. Pure-Go vs CGO:** reasons only (no C toolchain, cross-compilation, CGO-free CI); the
  performance cost is stated as not measured. No CGO driver is added to measure it.
- **D5. README stays short** (quick start, CLI reference, links); the depth lives in
  docs/architecture.md.
