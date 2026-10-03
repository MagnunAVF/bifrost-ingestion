package ingest

import "strings"

// Identity builds the string that gets embedded, the same way for catalog rows and entries:
//
//	<Name> | brand: <Brand> | category: <Category>
//
// Each field goes through Sanitize first. A blank Brand or Category drops its whole segment;
// there is never an empty label or a placeholder. Changing this format changes every embedding
// and threshold: keep docs/data-notes.md and the golden file in sync.
func Identity(name, brand, category string) string {
	var b strings.Builder
	b.WriteString(Sanitize(name))
	if s := Sanitize(brand); s != "" {
		b.WriteString(" | brand: ")
		b.WriteString(s)
	}
	if s := Sanitize(category); s != "" {
		b.WriteString(" | category: ")
		b.WriteString(s)
	}
	return b.String()
}
