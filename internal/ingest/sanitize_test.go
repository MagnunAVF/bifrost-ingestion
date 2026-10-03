package ingest_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "clean string unchanged", in: "Smartphone Galaxy S23", want: "Smartphone Galaxy S23"},
		{name: "doubled internal space", in: "Smartphone  Galaxy S23", want: "Smartphone Galaxy S23"},
		{name: "edge and mixed whitespace", in: "  x \t y \n", want: "x y"},
		{name: "unicode spaces become one space", in: "a\u00a0\u2003b\u3000c", want: "a b c"},
		{name: "apostrophe survives", in: "O'Neill", want: "O'Neill"},
		{name: "brand apostrophe survives", in: "Levi's", want: "Levi's"},
		{name: "sql payload kept as data", in: "TestBrand'; SELECT 1; --", want: "TestBrand'; SELECT 1; --"},
		{name: "html kept as data", in: `<script>alert("x")</script>`, want: `<script>alert("x")</script>`},
		{name: "inch mark unchanged", in: `iPad Pro 12.9"`, want: `iPad Pro 12.9"`},
		{name: "two apostrophes unchanged", in: "iPad Pro 12.9''", want: "iPad Pro 12.9''"},
		{name: "precomposed accent unchanged", in: "Câmera Canon EOS R6", want: "Câmera Canon EOS R6"},
		{name: "decomposed accent becomes NFC", in: "Ca\u0302mera", want: "Câmera"},
		{name: "control and format characters dropped", in: "ab\u0000c\u200bd\u00ade\ufefff\u0007", want: "abcdef"},
		{name: "control between words leaves no double space", in: "a \u0000 b", want: "a b"},
		{name: "invalid utf-8 replaced", in: "a\xffb", want: "a\ufffdb"},
		{name: "empty", in: "", want: ""},
		{name: "blank", in: " \t\n ", want: ""},
		{name: "only controls", in: "\u0000\u200b", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ingest.Sanitize(tt.in))
		})
	}
}

func TestSanitizeCapsLength(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "ascii over the cap", in: strings.Repeat("a", 300), want: strings.Repeat("a", ingest.MaxFieldRunes)},
		{name: "multibyte cut on a rune boundary", in: strings.Repeat("ç", 300), want: strings.Repeat("ç", ingest.MaxFieldRunes)},
		{name: "exactly at the cap", in: strings.Repeat("b", ingest.MaxFieldRunes), want: strings.Repeat("b", ingest.MaxFieldRunes)},
		{name: "cut lands on a space", in: strings.Repeat("a", ingest.MaxFieldRunes-1) + " tail", want: strings.Repeat("a", ingest.MaxFieldRunes-1)},
		{name: "cap counts after collapsing", in: strings.Repeat("a  ", 100), want: strings.TrimSpace(strings.Repeat("a ", 100))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ingest.Sanitize(tt.in)
			assert.Equal(t, tt.want, got)
			assert.LessOrEqual(t, utf8.RuneCountInString(got), ingest.MaxFieldRunes)
		})
	}
}
