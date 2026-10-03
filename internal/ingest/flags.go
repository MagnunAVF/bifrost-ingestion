package ingest

import "regexp"

// Flag marks suspicious content in one field. Flagged values are still kept as data: SQL
// injection is stopped by parameterized queries, not by rewriting strings.
type Flag struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// Reasons for a Flag, in the order sanitizeField reports them.
const (
	ReasonInvalidUTF8  = "invalid UTF-8 replaced"
	ReasonControlChars = "control or format characters removed"
	ReasonTruncated    = "truncated to 256 runes"
	ReasonSQLLike      = "SQL-like content"
	ReasonMarkup       = "markup-like content"
)

var (
	// A statement separator, a comment marker, or a quote that closes a string and is followed
	// by an SQL keyword or a tautology ("' OR '1'='1"). Plain apostrophes (O'Neill, Kids' and
	// Adults') do not match. "--" inside a value such as "10--12" does: a flag is only a flag.
	sqlLike = regexp.MustCompile(`;|--|/\*|\*/|['"` + "`" +
		`]\s*\)?\s*(?i:\b(?:select|insert|update|delete|drop|union|exec|alter|create|truncate)\b|or\s+['"\d])`)
	markup = regexp.MustCompile(`(?i)<\s*/?\s*script|javascript:`)
)

// sanitizeField returns Sanitize(raw) and the flags for what it had to change or what looks
// like an injection attempt.
func sanitizeField(field, raw string) (string, []Flag) {
	s, c := clean(raw)

	var flags []Flag
	add := func(ok bool, reason string) {
		if ok {
			flags = append(flags, Flag{Field: field, Reason: reason})
		}
	}
	add(c.invalidUTF8, ReasonInvalidUTF8)
	add(c.removed, ReasonControlChars)
	add(c.truncated, ReasonTruncated)
	add(sqlLike.MatchString(s), ReasonSQLLike)
	add(markup.MatchString(s), ReasonMarkup)
	return s, flags
}
