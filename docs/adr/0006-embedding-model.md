# ADR 0006: nomic-embed-text through a local Ollama

- Status: accepted
- Date: 2026-10-03 (decided in ENG-4, calibrated in ENG-6, recorded in ENG-7)
- Issue: ENG-7 (plan: docs/plans/ENG-7-architecture-docs.md, decision D2); design in
  docs/plans/ENG-4-ollama-embedder.md

## Context

Deduplication is semantic: "Roteador WiFi 6 TP-Link" must land near "Router WiFi 6 TP-Link".
The embeddings must run locally on an M1 with 8 GB of RAM, next to the pipeline, and catalog
data must not leave the machine.

## Decision

Embed with `nomic-embed-text` (768 dimensions) served by a local Ollama
(`http://localhost:11434`, `POST /api/embed`). The model is a flag (`--model`), but the
shipped threshold is valid only for this model with the `clustering: ` task prefix
(ADR 0002). `internal/embed` talks to Ollama with `net/http` only, sends batches of 64 texts,
applies a 2 min timeout per request (the first one loads the model), and caps responses at
32 MiB.

## Evidence

Measured on the M1:

- 274 MB on disk.
- ~1.1 s to load the model on the first request; ~88 texts/s warm; the 975-row catalog cold
  start takes ~11 s.
- The translation pair (entry 57 vs Product 21) scores 0.94, against 0.55 for an unrelated
  row.
- With the `clustering: ` prefix and threshold 0.975, no two distinct catalog products link,
  and the only missed duplicates are the two Portuguese translations (ADR 0002).

## Alternatives

**No other model was benchmarked.** The ENG-4 plan names nomic-embed-text as the model, no
comparison is recorded, and the choice is validated only by the measurements above. Candidates for a comparison, which would re-run
`TestCalibration` (`make e2e`) per model:

- `all-minilm` (384 dims): smaller and faster, likely weaker on the cross-language pairs.
- `mxbai-embed-large` (1024 dims): larger, more RAM and slower cold start on 8 GB.
- A hosted embedding API: rejected for M1, because data must stay local.

## Consequences

- Changing the model, the task prefix or the identity string format means re-running the
  calibration and revisiting ADR 0002. `TestCalibration` fails if the shipped threshold stops
  separating the catalog negatives.
- Cross-language duplicates score below the threshold. That is the M2 ambiguous band.
- `make e2e` needs Ollama running with the model pulled (`ollama pull nomic-embed-text`).
  `make check` never does: unit tests use the deterministic `embed.Fake`.
