package ingest_test

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

func FuzzSanitize(f *testing.F) {
	for _, seed := range []string{
		"",
		"Smartphone  Galaxy S23",
		"Câmera Canon EOS R6",
		"Ca\u0302mera",
		"O'Neill",
		"Levi's",
		"TestBrand'; SELECT 1; --",
		`iPad Pro 12.9"`,
		"iPad Pro 12.9''",
		"ab\u0000c\u200bd\u00ade",
		"a\xffb",
		" \t\n\u00a0\u3000 ",
		strings.Repeat("ç ", 200),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, in string) {
		got := ingest.Sanitize(in)

		if !utf8.ValidString(got) {
			t.Fatalf("invalid UTF-8: %q", got)
		}
		for _, r := range got {
			if unicode.In(r, unicode.Cc, unicode.Cf) {
				t.Fatalf("control or format character %U in %q", r, got)
			}
			if unicode.IsSpace(r) && r != ' ' {
				t.Fatalf("whitespace %U other than a space in %q", r, got)
			}
		}
		if !norm.NFC.IsNormalString(got) {
			t.Fatalf("not NFC: %q", got)
		}
		if n := utf8.RuneCountInString(got); n > ingest.MaxFieldRunes {
			t.Fatalf("%d runes, cap is %d", n, ingest.MaxFieldRunes)
		}
		if got != strings.TrimSpace(got) || strings.Contains(got, "  ") {
			t.Fatalf("edge or doubled spaces: %q", got)
		}
		if again := ingest.Sanitize(got); again != got {
			t.Fatalf("not idempotent: %q → %q", got, again)
		}
	})
}
