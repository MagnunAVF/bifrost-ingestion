package ingest

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// MaxFieldRunes caps every sanitized field. The longest value in the fixture is 36 runes.
const MaxFieldRunes = 256

// Sanitize normalizes one untrusted field: invalid UTF-8 becomes U+FFFD, whitespace becomes a
// space, other control and format characters (Cc, Cf) are dropped, the result is NFC, whitespace
// runs collapse to one space, the ends are trimmed and the length is capped at MaxFieldRunes.
// Quotes, apostrophes, accents and symbols are never touched. Sanitize is idempotent.
func Sanitize(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.In(r, unicode.Cc, unicode.Cf):
			return -1
		}
		return r
	}, s)
	s = norm.NFC.String(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(truncate(s, MaxFieldRunes))
}

// truncate cuts s to at most n runes, on a rune boundary.
func truncate(s string, n int) string {
	i := 0
	for j := range s {
		if i == n {
			return s[:j]
		}
		i++
	}
	return s
}
