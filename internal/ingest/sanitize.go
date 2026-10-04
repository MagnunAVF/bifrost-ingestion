package ingest

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// MaxFieldRunes caps every sanitized field. The longest value in the fixture is 36 runes.
const MaxFieldRunes = 256

// Sanitize normalizes one untrusted field: invalid UTF-8 becomes U+FFFD, whitespace becomes a
// space, other control and format characters (Cc, Cf) are dropped, the result is NFC, whitespace
// runs collapse to one space, the ends are trimmed and the length is capped at MaxFieldRunes.
// Quotes, apostrophes, accents and symbols are never touched. Sanitize is idempotent.
func Sanitize(s string) string {
	out, _ := clean(s)
	return out
}

// changes records what clean had to alter beyond whitespace, for the suspicious flags.
type changes struct {
	invalidUTF8 bool
	removed     bool // control or format characters dropped
	truncated   bool
}

func clean(s string) (string, changes) {
	var c changes
	if !utf8.ValidString(s) {
		c.invalidUTF8 = true
		s = strings.ToValidUTF8(s, "�")
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.In(r, unicode.Cc, unicode.Cf):
			c.removed = true
			return -1
		}
		return r
	}, s)
	s = norm.NFC.String(s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MaxFieldRunes {
		c.truncated = true
		s = strings.TrimSpace(truncate(s, MaxFieldRunes))
	}
	return s, c
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
