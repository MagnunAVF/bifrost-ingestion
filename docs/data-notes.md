# Data notes

Findings from a read-only inspection of the fixtures (Session 0, 2026-10-03). Every number here
was measured, not assumed. The fixtures were opened with `sqlite3 -readonly` and `jq`, and their
SHA-1s were the same before and after:

| File | SHA-1 | Size |
| --- | --- | --- |
| testdata/catalog.db | e7c535cbca9dfc053dfeceb43396cf58458bb059 | 61 440 B |
| testdata/ProductEntry.json | 65bde7947e57e9e5cf38ea1f37e9de949cc9ddc3 | 48 938 B |

## catalog.db

### Schema (verbatim, `.schema`)

```sql
CREATE TABLE Product (Id INTEGER PRIMARY KEY AUTOINCREMENT, Name TEXT NOT NULL, Brand TEXT, Category TEXT);
CREATE TABLE sqlite_sequence(name,seq);
CREATE TABLE SellerProduct (Id INTEGER PRIMARY KEY AUTOINCREMENT, SellerName TEXT NOT NULL, ProductId INTEGER CONSTRAINT FK_Product_Id REFERENCES Product (Id) NOT NULL, SellerProductId INTEGER NOT NULL);
```

There are no indexes, triggers or views.

### Row counts

| Table | Rows |
| --- | --- |
| Product | 975 |
| SellerProduct | 0 |
| sqlite_sequence | 1 (`Product\|975`, no SellerProduct row because nothing was ever inserted) |

### Database settings

- SQLite 3.51.0, encoding UTF-8, `journal_mode=delete`, `user_version=0`
- `PRAGMA foreign_keys` = 0. SQLite turns this off by default for each connection, so the FK
  below is **not enforced** unless our connection turns it on.
- `integrity_check` returns ok, and `foreign_key_check` finds no violations

### SellerProduct.SellerProductId

| Property | Value |
| --- | --- |
| Declared type | `INTEGER` (INTEGER affinity, not STRICT) |
| Constraints | `NOT NULL` only. There is no UNIQUE, CHECK or DEFAULT. |
| Foreign key | none on this column |
| Rowid alias | **no**. Only `SellerProduct.Id` (`INTEGER PRIMARY KEY AUTOINCREMENT`) is the rowid alias. |
| Indexes | none, so nothing is unique on `(SellerName, SellerProductId)` |

The other columns of SellerProduct (`PRAGMA table_info` / `foreign_key_list`):

- `Id INTEGER PRIMARY KEY AUTOINCREMENT` is the rowid alias
- `SellerName TEXT NOT NULL`
- `ProductId INTEGER NOT NULL` has the named constraint `FK_Product_Id`, which references
  `Product(Id)` with `ON UPDATE NO ACTION ON DELETE NO ACTION MATCH NONE`

**How INTEGER affinity behaves, tested on an in-memory copy of the DDL:** a UUID string is
*not* rejected. It is stored as TEXT, so `typeof` is `text`. Numeric-looking strings are
silently converted: `'123'` becomes `123`, `' 42 '` becomes `42`, and `'1e5'` becomes `100000`
(an integer). Two consequences:

1. Raw SQLite would accept UUIDs today. The real breakage is in typed code: sqlc generates
   `int64` for this column, and a TEXT value fails to scan into it.
2. A malformed ID that looks numeric would be silently rewritten. The migration should make the
   column `TEXT NOT NULL`, and should consider `STRICT` or a `CHECK` so values are stored exactly
   as sent.

Because the table is empty, the "without losing rows" requirement is trivially true for this
fixture. The table-rebuild migration still has to copy rows (`INSERT … SELECT`) and keep
`Id` values and the `sqlite_sequence` entry.

### Product contents

- Ids run from 1 to 975 with no gaps. The names are all distinct, even after lowercasing,
  whitespace collapse and punctuation stripping. None is non-ASCII, has a double space or has
  leading or trailing whitespace. Name length ranges from 9 to 36 characters, averaging 21.
- `Brand` is NULL in 119 rows, and empty in none. `Category` is NULL in 34 rows, and empty in none.
  Type combinations of (Brand, Category): 856 text/text, 85 null/text, 34 null/null.
- There are 639 distinct brands and 43 distinct categories. The largest categories are Kitchen
  (133), Sports (105) and Outdoor (63).
- The catalog is English only and its strings are clean, so all the messiness is in the JSON.

## ProductEntry.json

### Shape

- The top level is an array of **269 objects**. There is no BOM, no CR and no tab, and the
  structure is valid JSON.
- Every object has exactly these keys, in this order: `Id, SellerName, Name, Brand, Category`.
  No key is missing, no extra key appears, and no key is duplicated.
- Every value is a string, except `Brand`, which is `null` 3 times:

| Field | Missing | null | Empty/blank | Leading/trailing ws | Max len |
| --- | --- | --- | --- | --- | --- |
| Id | 0 | 0 | 0 | 0 | 36 |
| SellerName | 0 | 0 | 0 | 0 | 14 |
| Name | 0 | 0 | 0 | 0 | 36 |
| Brand | 0 | 3 | 0 | 0 | 24 |
| Category | 0 | 0 | 0 | 0 | 15 |

The 3 entries with a null Brand (indexes 45, 116 and 133) match catalog rows that also have a
NULL Brand (Product 113, 398 and 492).

### Sellers and categories

- There are 20 sellers, each with 13 to 15 entries (MegaStore, GardenStore, GadgetZone, …). The
  seller names don't match the categories they sell; for example GardenStore sells cameras.
- There are 28 categories. All are catalog categories except **`Photo`** (1 entry, index 87),
  where the catalog has `Photography`.

### Id field

- 266 Ids have the canonical lowercase 8-4-4-4-12 hex shape. All of them have version nibble `4`,
  but the variant nibble covers 0 to f, so they are **UUID-shaped, not RFC 4122-valid**.
- 3 Ids are malformed:

| Idx | Id | Problem | Entry |
| --- | --- | --- | --- |
| 92 | `ddddeee-ffff-4000-1111-222233334444` | first group has 7 chars | FitnessCenter, Creatine Monohydrate |
| 180 | `09835342345-4678-9abc-def012345678` | first group has 11 chars, 4 groups | MegaStore, Security Test Product (SQLi brand, see below) |
| 268 | `uddd0000-eeee-4111-ffff-aaaa22223333` | non-hex `u` | SmartHomeStore, "Smart TV Samsung 55" |

- **14 Ids are reused**, in 28 entries:
  - 13 Ids each appear on two entries with *different* sellers and *different* products (indexes
    155–172 and 179–193). Examples: `00112233-…` is GardenStore "Curtain Rod Adjustable" and
    SportsHub "Bookshelf 5-Shelf".
  - 1 Id, `e5e5e5e5-f6f6-4a7a-b8b8-c9c9c9c9c9c9`, appears twice for the same seller and product:
    GardenStore "Câmera Canon EOS R6" (55) and "Camera Canon EOS R6" (76).

### Strings

- **SQL injection payload:** index 180 has `"Brand": "TestBrand'; SELECT 1; --"` with
  `"Name": "Security Test Product"`. It is also the entry with a malformed Id. None of the other
  values contains `<>;`$\{}`, `--`, `/*` or SQL/HTML keywords.
- **Doubled internal spaces:** about 60 Names have them, e.g. `"Smartphone  Galaxy S23"` and
  `"Rain Boots  Rubber"`. There are no tabs or other odd whitespace, and nothing at the ends.
- **Non-ASCII:** only `"Câmera Canon EOS R6"` (index 55). There are no control or zero-width
  characters and no escaped `\u` sequences.
- **Portuguese translations of catalog products:** `"Roteador WiFi 6 TP-Link"` (57) is the same
  product as Product 21 `"Router WiFi 6 TP-Link"`, and `"Processador AMD Ryzen 9 7950X"` (64)
  is Product 28 `"Processor AMD Ryzen 9 7950X"`. These are the real *semantic* duplicates.
- **Inch-mark variants:** `12.9"` (escaped `\"`), `12.9''` (two apostrophes, index 53) and a bare
  `12.9` (267). Likewise `Samsung 55"` vs `Samsung 55` (268).
- **Brand punctuation:** index 188 has `"Levi's"` and the catalog has `"Levis"` (Product 322).

### Overlap with the catalog

How many entry Names match a catalog Name under progressively looser normalization:

| Normalization | Name matches | Name + Brand + Category match |
| --- | --- | --- |
| exact | 202 / 269 | 200 |
| + trim, collapse whitespace, lowercase | 262 | 260 |
| + accent fold (NFKD, drop combining marks) | 263 | 261 |
| + strip non-alphanumerics | 266 | 264 |

Only 3 names never match: the two Portuguese names (which are semantic matches) and
`Security Test Product`. **So the only genuinely novel product in the fixture is the malicious
one.** The highest catalog Id that is matched is 893, so the fixture covers only part of the
catalog.

The pair with a matching name but differing attributes is index 87 (Category `Photo` vs
`Photography`) and index 188 (Brand `Levi's` vs `Levis`).

### Duplicates within the file

- Under the loosest normalization, 64 name groups hold 132 entries. Most are the same product
  sold by different sellers, which is the expected case: one Product with several SellerProduct
  links.
- **11 groups repeat the same seller and product with different Ids:**
  - GardenStore × Canon EOS R6, three times (55, 76 and 87; 55 and 76 share an Id, and 87 has
    `e6e6e5e5-…` and Category `Photo`)
  - 10 Sports entries from indexes 237–246 repeated at 257–266 with a doubled space and a new Id
    (e.g. OfficeSupply "Basketball Spalding Official" with `1111aaaa-…` and with `aaaa1111-…`)

## Identity string (contract, implemented in ENG-3)

The string we embed. It must be built **the same way for catalog rows and incoming entries**,
otherwise the similarity scores are not comparable: both go through `ingest.Identity`. Changing
it changes every embedding and threshold; `internal/ingest/testdata/ProductEntry.golden.jsonl`
pins it for the whole fixture.

```
<Name> | brand: <Brand> | category: <Category>
```

- Drop the `| brand: …` segment when Brand is NULL, and likewise for Category, rather than
  writing "unknown". A placeholder would make every brand-less item look similar to every other.
- Name comes first because it carries most of the signal, and many names already contain the
  brand (`"Notebook Dell Inspiron"`).
- SellerName and Id are excluded: they say nothing about what the product *is*.

Sanitization applied to each field (`ingest.Sanitize`, in this order):

1. Invalid UTF-8 becomes U+FFFD.
2. Every whitespace character becomes a space; other control and format characters (Cc, Cf,
   including zero-width ones) are removed.
3. Unicode NFC.
4. Whitespace runs collapse to a single space, then trim.
5. Cap at 256 runes (`MaxFieldRunes`; the observed maximum is 36), on a rune boundary.
6. Nothing else is rewritten: case, accents, quotes and apostrophes stay (`O'Neill`, `Levi's`,
   `12.9''` and `12.9"` are kept as sent; there is no inch-mark canonicalization). The model
   handles them, and `Câmera`/`Camera` should land close anyway.
7. Malicious content is **data**: it is never interpolated into SQL (sqlc parameters only),
   and it is embedded and stored like any other string unless the record is rejected for
   another reason.

Rejects (`Record.Reason`, first problem in field order wins; decoding continues):

- Id, SellerName, Name: `missing`, `null`, `not a string`, or `blank` after sanitizing.
- Id: `not a UUID` unless it has the 8-4-4-4-12 hex shape (any case; stored lowercased). Not
  full RFC 4122. Rejects fixture entries 92, 180 and 268.
- Brand, Category: `not a string`. Missing, null or blank is fine and drops the segment.
- `duplicate key "<k>"` and `not an object` for the element as a whole. Unknown keys are ignored.
- A payload that is not one JSON array, or is larger than 64 MiB, stops the run
  (`errs.ErrInvalidInput`).

Suspicious flags (`Record.Flags`, reported, never removed; set on rejects too): invalid UTF-8
replaced, control or format characters removed, truncated, SQL-like content (`;`, `--`, `/*`,
`*/`, or a quote followed by an SQL keyword or `OR '…`/`OR 1`), markup-like content
(`<script`, `javascript:`). In the fixture only entry 180's Brand is flagged.

Examples:

- `Smartphone Galaxy S23 | brand: Samsung | category: Electronics`
- `Cable Organizer Kit | category: Accessories` (Brand is null)
- `Security Test Product | brand: TestBrand'; SELECT 1; -- | category: Electronics`

For the report and the exact-match fast path, a separate **match key** could be used:
lowercase, accent-folded, with non-alphanumerics stripped. It resolves 266 of 269 entries
without embedding. ENG-6 decided against the fast path (open question 6); the calibration test
uses the key only to label the known pairs.

**What is embedded (ENG-6):** `dedup.DefaultTaskPrefix + identity`, i.e.
`clustering: <identity>`, for catalog rows and entries alike. The prefix is applied by the
pipeline, not by `ingest.Identity`, so the golden file is unchanged. The threshold (0.975) is
only valid with this prefix and this model (ADR 0002).

## Open questions

Answered so far (details in docs/milestones/M1.md, Decisions):

- 1, 2, 8 (ENG-3): malformed Ids are rejected; 8-4-4-4-12 hex shape, lowercased; the SQLi
  record is rejected for its Id and reported as suspicious.
- 3, 10, 11 (ENG-2): ids unique per seller (UNIQUE index), foreign keys on, `CHECK` on length.
- 4 (ENG-6): one SellerProduct link per Id; the same product under two Ids gets two links.
- 5 (ENG-6): the first (SellerName, Id) wins the link. A repeat is `existing`, is not embedded,
  and can still fill attributes (Name never changes), so 76 changes nothing.
- 6 (ENG-6): no exact-match fast path; every new decision goes through embeddings.
- 7 (ENG-6): no synthetic set; catalog leave-one-out nearest neighbours are the negatives
  (ADR 0002).
- 9 (ENG-6): `--update fill` (default) fills NULL Brand/Category only; `overwrite` replaces,
  but only with a category already in the catalog, and the first record in a run wins; `none`
  links only. Differences left alone are report notes (87 `Photo`, 188 `Levi's`). ADR 0003.

1. **Malformed Ids (92, 180, 268).** Reject the record with a reason, or store the raw string
   because the column becomes TEXT anyway? Index 92 is otherwise a clean, valid product.
2. **Id validation strictness.** Check the 8-4-4-4-12 hex shape only, or full RFC 4122 (which
   would reject almost every fixture Id because of the variant nibble)? Normalize case?
3. **Uniqueness and idempotency key.** Is `SellerProductId` unique per seller, which would make
   the 13 cross-seller Id collisions legitimate, or globally, which would make them conflicts?
   The migration should add a UNIQUE index on whichever key you choose, so that re-running
   `ingest` creates nothing new.
4. **Same seller, same product, different Ids** (the 10 Sports pairs and Canon 55/76/87). Is each
   Id its own SellerProduct link to the same Product, or do we collapse them per seller and
   product and report the extras?
5. **Repeated Id with differing Name** (55 `Câmera` vs 76 `Camera`, same Id and seller). First
   one wins, last one wins, or report a conflict?
6. **Exact-match fast path.** May we link exact and normalized-key matches without calling
   Ollama, or must every decision go through embeddings? The fast path makes the M1 cold start
   cheaper, but the calibration evidence would then rest on very few semantic cases.
7. **Threshold calibration set.** The fixture has only 2 true semantic duplicates (the
   Portuguese ones) and 1 true novel entry (the malicious one). Can we add labelled synthetic
   pairs, e.g. in testdata/calibration, to calibrate on?
8. **The SQLi record.** Is "Security Test Product" meant to be inserted as a novel Product, with
   the payload stored verbatim as data, or rejected? Its Id is malformed too, so question 1
   might decide this already.
9. **Attribute drift on a match** (`Photo` vs `Photography`, `Levi's` vs `Levis`). Never update
   the catalog Product (link only) and just report the difference?
10. **Foreign keys.** Should the app run `PRAGMA foreign_keys = ON` on every connection, and
    should the migration's table rebuild follow the official 12-step procedure (FKs off,
    `foreign_key_check` before commit)?
11. **Column strictness.** Is `SellerProductId TEXT NOT NULL` enough, or do you want `STRICT`
    or a `CHECK(length(SellerProductId) BETWEEN 1 AND 64)` to stop silent affinity conversion?
