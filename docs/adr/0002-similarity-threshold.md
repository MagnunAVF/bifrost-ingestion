# ADR 0002: Similarity threshold 0.975 with the nomic `clustering: ` prefix

- Status: accepted
- Date: 2026-10-03
- Issue: ENG-6 (plan: docs/plans/ENG-6-decision-matrix.md)

## Context

`bifrost ingest` links an entry to its nearest catalog Product when the cosine score is at or
above one threshold, and inserts a new Product otherwise. ENG-4 left open whether to embed with
a nomic task prefix. The fixture has only 2 true semantic duplicates (the Portuguese entries 57
and 64), and its only novel entry (180) is rejected for its Id, so it can't calibrate the
"novel" side alone.

The calibration (`internal/dedup/calibration_integration_test.go`, run with `make e2e`, model
nomic-embed-text, 768 dims, on the M1) measures two distributions:

- **Positives:** each of the 266 accepted fixture entries against its known catalog match (264
  by an accent-folded, alphanumeric name key, plus the translations 57 → 21 and 64 → 28).
- **Negatives:** each of the 975 catalog products against its nearest *other* product. All 975
  names are distinct, so each of these scores is a pair of different products, and it stands
  for a new product arriving next to its closest sibling. No synthetic data was needed (open
  question 7).

## Evidence

| | positives min | p1 | negatives max | p99 | p95 | margin |
| --- | --- | --- | --- | --- | --- | --- |
| no prefix | 0.9433 | 0.9707 | 0.9667 | 0.9454 | 0.9157 | −0.0234 |
| `clustering: ` | 0.9509 | 0.9837 | 0.9700 | 0.9432 | 0.9264 | −0.0191 |

**The distributions overlap with either prefix**, so no threshold is perfect. The hardest
pairs:

- Lowest positives: 57 "Roteador WiFi 6 TP-Link" ~ 21 (0.943 / 0.951 prefixed) and 64
  "Processador AMD Ryzen 9 7950X" ~ 28 (0.969 / 0.962). With the prefix, the next one is 87
  (Category `Photo` vs `Photography`) at 0.984.
- Highest negatives: "Hockey Skates Ice" ~ "Hockey Stick Ice" (Bauer, 0.967 / 0.970),
  "Embroidery Machine" ~ "Sewing Machine Computerized" (Brother, 0.953 / 0.951), "Telescope
  Eyepiece Kit" ~ "Telescope Celestron" (0.947 / 0.953). These are same-brand siblings.

Threshold sweep. "Missed" counts duplicates that would be inserted. "Wrong" counts catalog
products that would link to a different product if they arrived new; each pair counts twice.

| threshold | no prefix: missed | wrong | prefix: missed | wrong |
| --- | --- | --- | --- | --- |
| 0.940 | 0 | 10 | 0 | 12 |
| 0.950 | 1 | 4 | 0 | 8 |
| 0.960 | 1 | 2 | 1 | 2 |
| 0.970 | 2 | 0 | 2 | 2 |
| **0.975** | 3 | 0 | **2** | **0** |
| 0.980 | 3 | 0 | 2 | 0 |

## Decision

Ship `dedup.DefaultThreshold = 0.975` with `dedup.DefaultTaskPrefix = "clustering: "`, which
`bifrost ingest` applies to every embedded text (catalog rows, entries, re-embeds). The identity
string contract (docs/data-notes.md) is unchanged. A threshold is only valid with the prefix it
was calibrated for.

The policy is **precision first**:

- A wrong link silently merges two different products: sellers end up attached to the wrong
  item, nothing reports it, and undoing it means finding it first.
- A missed duplicate creates one extra Product. It is visible in the report as `inserted` with
  its nearest match and score (57 → nearest 21 at 0.951), and it can be merged later.

At 0.975 with the prefix, no two distinct catalog products link, and the only misses are the two
translations. The margins are 0.005 on the negative side (hockey pair at 0.970) and 0.009 on the
positive side (entry 87 at 0.984). Without the prefix, 0.97 gives the same counts but leaves
entry 87 only 0.0007 above the line.

## Consequences

- `make run` on the fixture: 263 linked, 2 inserted (57, 64), 1 existing (76), 3 rejected, 1
  suspicious. The lowest linked score is 0.984. A real run followed by a re-run creates nothing
  new.
- Cross-language duplicates are the known weak spot. The M2 ambiguous band (roughly 0.94–0.975
  here) is where they belong, and `Kind` and `Policy` are built to gain it without breaking
  callers.
- The evidence covers one model on one catalog. Changing the model, the prefix or the identity
  format requires re-running the calibration (`make e2e`) and revisiting this ADR. The
  calibration test fails if the shipped pair stops separating the catalog negatives or starts
  missing more than the two translations.
