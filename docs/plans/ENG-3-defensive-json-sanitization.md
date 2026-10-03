# ENG-3: Defensive JSON parsing and deep sanitization (plan)

Status: approved 2026-10-03 (decisions 1-7 below). Plan 1.2, lane B, depends on ENG-1 (closed).
Branch: `feat/ENG-3-defensive-json-sanitization`.

## Goal

Turn the untrusted `ProductEntry.json` payload into clean, deterministic identity strings, one
element at a time, with rejects and suspicious content reported instead of panics or deletions.

## Public API (internal/ingest)

```go
const MaxFieldRunes = 256

// Sanitize: valid UTF-8 → whitespace controls to spaces → drop other Cc/Cf → NFC → collapse
// whitespace → trim → cap at MaxFieldRunes → trim. Never touches quotes, accents or symbols.
func Sanitize(s string) string

// Identity sanitizes each field and builds "<Name> | brand: <Brand> | category: <Category>",
// dropping a blank Brand or Category segment. Used for entries and catalog rows alike.
func Identity(name, brand, category string) string

type Flag struct{ Field, Reason string }

type Record struct {
	Index                                 int
	ID, SellerName, Name, Brand, Category string
	Identity                              string // "" when rejected
	Reason                                string // non-empty means rejected
	Flags                                 []Flag
}
func (r Record) Rejected() bool

// Decode streams the top-level array (json.Decoder, one json.RawMessage per element).
// Per-element problems become rejected Records; broken JSON, a non-array top level, trailing
// data or input over MaxPayloadBytes end the stream with an error wrapping errs.ErrInvalidInput.
func Decode(ctx context.Context, r io.Reader) iter.Seq2[Record, error]
```

Field rules: Id, SellerName, Name are required (missing, null, non-string or blank after
sanitizing → reject). Brand, Category are optional (missing, null or blank → "" and the segment
is dropped; non-string → reject). Unknown keys are ignored. Non-object elements are rejected.

Suspicious flags: `;`, `--`, `/*`, `*/`, a quote followed by an SQL keyword, `<script`, removed
control/format characters or invalid UTF-8, truncation. Collapsed whitespace is not flagged.

## Tests

- `TestSanitize`: doubled/edge whitespace, `O'Neill`, the SQLi brand, NFD → NFC, Cc/Cf removal,
  invalid UTF-8, inch marks unchanged, rune-boundary cap, empty/blank.
- `TestIdentity`: all fields, blank Brand, blank Category, both blank, sanitized fields, SQLi.
- `TestFlags`: SQLi flagged; `O'Neill`, `Levi's`, `12.9''` not; controls and truncation flagged.
- `TestDecode`: valid array, `[]`, each required field missing/null/blank/non-string, optional
  fields absent, non-string Brand, non-object element, duplicate key, unknown key, malformed
  Ids, uppercase Id, SQLi with a valid Id, broken top level / truncated / trailing data,
  oversized input, cancelled ctx, early break.
- `TestDecodeFixtureGolden`: `internal/ingest/testdata/ProductEntry.golden.jsonl`, `-update`
  regenerates it; also asserts 269 records and the reject/flag counts.
- `FuzzSanitize`: valid UTF-8, no Cc/Cf, NFC, idempotent, ≤ 256 runes, no edge/double spaces.

## Tasks (one commit each)

1. Sanitize + FuzzSanitize.
2. Identity + flags.
3. Decode (+ errs.ErrInvalidInput).
4. Golden file for the fixture.
5. docs/data-notes.md identity/rejection contract and M1 Decisions.

## Decisions (approved 2026-10-03)

1. Malformed Ids are rejected (`Id: not a UUID`), still flagged; rejects 92, 180 and 268.
2. Id check is the 8-4-4-4-12 hex shape only, case-insensitive, lowercased.
3. Input capped with io.LimitReader at 64 MiB (MaxPayloadBytes).
4. Duplicate keys inside an element → reject.
5. `golang.org/x/text` becomes a direct dependency (already in go.mod) for unicode/norm.
6. No inch-mark canonicalization; data-notes.md is updated to match.
7. Over-length fields are truncated to 256 runes and flagged.
