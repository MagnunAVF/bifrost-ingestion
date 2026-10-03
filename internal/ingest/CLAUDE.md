# Ingest rules (untrusted input)

- Treat every field as hostile. Stream with json.Decoder; cap every field's length.
- Per-field pipeline: ensure valid UTF-8 → strip control characters → Unicode NFC →
  collapse whitespace → trim → length cap.
- Never delete or rewrite legitimate characters (quotes, apostrophes, accents, symbols).
  "O'Neill" must survive unchanged. SQL injection is stopped by parameterized queries, not here.
- Suspicious content (quote followed by SQL keywords, ";", "--") is FLAGGED in the run report,
  not removed.
- Missing or null required fields → reject the entry with a reason. Missing, null or blank
  optional fields (Brand, Category) → DROP their segment from the identity string; never write
  an empty label or a placeholder like "unknown" (it makes every brand-less item look alike).
  Format: `<Name> | brand: <Brand> | category: <Category>` (docs/data-notes.md).
- The identity string format is a contract: changing it changes every embedding and every
  threshold. Document it in docs/data-notes.md and keep the golden test in sync.
