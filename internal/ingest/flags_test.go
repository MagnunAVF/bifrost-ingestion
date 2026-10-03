package ingest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

func TestSanitizeFieldFlags(t *testing.T) {
	sqlLike := ingest.Flag{Field: "Brand", Reason: ingest.ReasonSQLLike}

	tests := []struct {
		name      string
		raw       string
		wantValue string
		wantFlags []ingest.Flag
	}{
		{name: "fixture sql payload", raw: "TestBrand'; SELECT 1; --", wantValue: "TestBrand'; SELECT 1; --", wantFlags: []ingest.Flag{sqlLike}},
		{name: "classic or 1=1", raw: "x' OR '1'='1", wantValue: "x' OR '1'='1", wantFlags: []ingest.Flag{sqlLike}},
		{name: "quote then union", raw: `x" UNION SELECT name FROM Product`, wantValue: `x" UNION SELECT name FROM Product`, wantFlags: []ingest.Flag{sqlLike}},
		{name: "block comment", raw: "Acme /* hi */", wantValue: "Acme /* hi */", wantFlags: []ingest.Flag{sqlLike}},
		{name: "script tag", raw: "<SCRIPT>alert(1)</script>", wantValue: "<SCRIPT>alert(1)</script>", wantFlags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonMarkup}}},
		{name: "apostrophe name", raw: "O'Neill", wantValue: "O'Neill"},
		{name: "brand apostrophe", raw: "Levi's", wantValue: "Levi's"},
		{name: "possessive then and", raw: "Kids' and Adults' Shoes", wantValue: "Kids' and Adults' Shoes"},
		{name: "rock n roll", raw: "Rock 'n' Roll", wantValue: "Rock 'n' Roll"},
		{name: "inch marks", raw: "iPad Pro 12.9''", wantValue: "iPad Pro 12.9''"},
		{name: "single hyphen", raw: "TP-Link - Router", wantValue: "TP-Link - Router"},
		{name: "doubled space is not suspicious", raw: "Rain Boots  Rubber", wantValue: "Rain Boots Rubber"},
		{name: "non-ascii is not suspicious", raw: "C\u00e2mera", wantValue: "C\u00e2mera"},
		{
			name: "control characters removed", raw: "Acme\u0000\u200b", wantValue: "Acme",
			wantFlags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonControlChars}},
		},
		{
			name: "tab is whitespace, not a removed control", raw: "Acme\tCorp", wantValue: "Acme Corp",
		},
		{
			name: "invalid utf-8", raw: "Acme\xff", wantValue: "Acme\ufffd",
			wantFlags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonInvalidUTF8}},
		},
		{
			name: "truncated", raw: strings.Repeat("a", ingest.MaxFieldRunes+1), wantValue: strings.Repeat("a", ingest.MaxFieldRunes),
			wantFlags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonTruncated}},
		},
		{
			name: "several reasons in a fixed order", raw: "x'; DROP TABLE Product; --\u0000\xff",
			wantValue: "x'; DROP TABLE Product; --\ufffd",
			wantFlags: []ingest.Flag{
				{Field: "Brand", Reason: ingest.ReasonInvalidUTF8},
				{Field: "Brand", Reason: ingest.ReasonControlChars},
				{Field: "Brand", Reason: ingest.ReasonSQLLike},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, flags := ingest.SanitizeField("Brand", tt.raw)
			assert.Equal(t, tt.wantValue, got)
			assert.Equal(t, ingest.Sanitize(tt.raw), got, "same value as Sanitize")
			assert.Equal(t, tt.wantFlags, flags)
		})
	}
}
