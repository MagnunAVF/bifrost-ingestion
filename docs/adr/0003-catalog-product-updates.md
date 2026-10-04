# ADR 0003: Matched entries may fill a Product's missing attributes

- Status: accepted
- Date: 2026-10-03
- Issue: ENG-6 (plan: docs/plans/ENG-6-decision-matrix.md, decisions D5 and D6)

## Context

When an entry matches a catalog Product, its Brand or Category can differ from the catalog's:

- **Missing in the catalog:** 119 Products have a NULL Brand and 34 a NULL Category.
- **Different spelling:** entry 188 has `Levi's` where Product 322 has `Levis`.
- **Outside the vocabulary:** entry 87 has `Photo` where the catalog's 43 categories use
  `Photography`.

Name differences are routine (accents, doubled spaces, inch marks, Portuguese translations).

The catalog is shared with other systems, and M1 adds no tables, so there is no audit log.
Several sellers can send the same product with conflicting values, in one run or across runs.

## Decision

`bifrost ingest --update fill|none|overwrite` decides what a matched entry may change on its
Product. This covers both `linked` entries and `existing` ones (a (seller, Id) that is already
linked, open question 5).

- **`fill` (default):** set Brand or Category only where the Product has NULL. It never
  overwrites, so it is monotonic and idempotent, and two sellers can't fight over a value.
- **`overwrite`:** also replace a differing value, with two guards:
  - A Category only changes to a value already in the catalog's category set at cold start.
    This blocks `Photography` → `Photo`.
  - In one run, the first record that changes a product wins; later disagreements become
    notes. The result is deterministic for a given input order.
- **`none`:** link only.

In every mode:

- **Name is never updated.** It carries the matching signal.
- **Case- or whitespace-only differences are not changes.**
- **Flagged (suspicious) input is never applied to an existing Product.** When it creates a new
  Product, it is still stored verbatim as data.
- **Every difference left alone is a report note.** Every change is listed as
  `field: old → new` with the record that caused it.
- **The update runs in the same transaction as the seller link** (`catalog.LinkSeller`), so a
  failed link leaves the Product untouched. Afterwards the product is re-embedded, and its
  vector in the index is replaced (`vector.Index.Replace`), so later records match against
  current data.
- **A dry run applies updates to its in-memory copy.** It reports the same changes a real run
  would make.

## Alternatives considered

- **Never update (link only):** the safest option, but the 153 NULL attributes stay empty
  forever, even when sellers supply them. It remains available as `--update none`.
- **Last write wins:** one seller with bad data degrades a shared catalog, and two sellers
  flip a value back and forth on every run, so re-runs are not idempotent.
- **A proposals table reviewed by a person:** the right long-term shape, but it is a schema
  change for a shared database, and it needs a review workflow (M2 or later).

## Consequences

- The fixture has no NULL-to-value fills (its three NULL-Brand entries match NULL-Brand
  Products), so `make run` reports notes for 87 and 188 and changes nothing.
- `--update overwrite` applies 188's `Levi's`, but not 87's `Photo`.
- Without an audit table, the report is the only record of a change. Keep the report of a real
  run (`make run WRITE=1 > report.txt`).
- Re-embedding costs one embedding call per changed product.
