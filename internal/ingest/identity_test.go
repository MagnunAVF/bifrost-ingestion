package ingest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

func TestIdentity(t *testing.T) {
	tests := []struct {
		name                  string
		pname, brand, categor string
		want                  string
	}{
		{
			name:  "all fields",
			pname: "Smartphone Galaxy S23", brand: "Samsung", categor: "Electronics",
			want: "Smartphone Galaxy S23 | brand: Samsung | category: Electronics",
		},
		{
			name:  "blank brand drops its segment",
			pname: "Cable Organizer Kit", brand: "", categor: "Accessories",
			want: "Cable Organizer Kit | category: Accessories",
		},
		{
			name:  "whitespace-only category drops its segment",
			pname: "Cable Organizer Kit", brand: "Acme", categor: " \t ",
			want: "Cable Organizer Kit | brand: Acme",
		},
		{
			name:  "both blank leaves the name",
			pname: "Cable Organizer Kit",
			want:  "Cable Organizer Kit",
		},
		{
			name:  "fields are sanitized",
			pname: "  Smartphone  Galaxy\tS23 ", brand: "Sam\u200bsung", categor: "Ele\u0000ctronics\n",
			want: "Smartphone Galaxy S23 | brand: Samsung | category: Electronics",
		},
		{
			name:  "decomposed accent becomes NFC",
			pname: "Ca\u0302mera Canon EOS R6", brand: "Canon", categor: "Photography",
			want: "C\u00e2mera Canon EOS R6 | brand: Canon | category: Photography",
		},
		{
			name:  "malicious brand is kept as data",
			pname: "Security Test Product", brand: "TestBrand'; SELECT 1; --", categor: "Electronics",
			want: "Security Test Product | brand: TestBrand'; SELECT 1; -- | category: Electronics",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ingest.Identity(tt.pname, tt.brand, tt.categor)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, got, ingest.Identity(tt.pname, tt.brand, tt.categor), "deterministic")
		})
	}
}
