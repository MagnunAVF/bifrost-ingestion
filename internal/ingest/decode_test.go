package ingest_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

const okID = "00112233-4455-4677-8899-aabbccddeeff"

// entry renders one element; a nil value leaves the key out, the string "null" writes null and
// anything else is written verbatim (so callers pass JSON literals such as `"Acme"` or `123`).
func entry(id, seller, name, brand, category *string) string {
	parts := []string{}
	for _, kv := range []struct {
		k string
		v *string
	}{{"Id", id}, {"SellerName", seller}, {"Name", name}, {"Brand", brand}, {"Category", category}} {
		if kv.v != nil {
			parts = append(parts, `"`+kv.k+`":`+*kv.v)
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func lit(s string) *string { return &s }

// valid returns the JSON literals of a clean entry, for tests to override one field.
func valid() (id, seller, name, brand, category *string) {
	return lit(`"` + okID + `"`), lit(`"MegaStore"`), lit(`"Smartphone Galaxy S23"`), lit(`"Samsung"`), lit(`"Electronics"`)
}

func collect(ctx context.Context, t *testing.T, input string) ([]ingest.Record, error) {
	t.Helper()
	var recs []ingest.Record
	for rec, err := range ingest.Decode(ctx, strings.NewReader(input)) {
		if err != nil {
			return recs, err
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func TestDecodeValidEntries(t *testing.T) {
	id, seller, name, brand, category := valid()
	second := entry(lit(`"AABBCCDD-0011-4223-3445-567788990234"`), lit(`"SportsHub"`),
		lit(`"Cable  Organizer Kit"`), lit("null"), lit(`"Accessories"`))

	recs, err := collect(t.Context(), t, "[\n"+entry(id, seller, name, brand, category)+",\n"+second+"\n]\n")
	require.NoError(t, err)
	require.Len(t, recs, 2)

	assert.Equal(t, ingest.Record{
		Index: 0, ID: okID, SellerName: "MegaStore", Name: "Smartphone Galaxy S23", Brand: "Samsung",
		Category: "Electronics", Identity: "Smartphone Galaxy S23 | brand: Samsung | category: Electronics",
	}, recs[0])
	assert.False(t, recs[0].Rejected())

	assert.Equal(t, ingest.Record{
		Index: 1, ID: "aabbccdd-0011-4223-3445-567788990234", SellerName: "SportsHub",
		Name: "Cable Organizer Kit", Category: "Accessories",
		Identity: "Cable Organizer Kit | category: Accessories",
	}, recs[1], "uppercase Id lowercased, null Brand dropped, name collapsed")
}

func TestDecodeEmptyArray(t *testing.T) {
	recs, err := collect(t.Context(), t, " [ ] ")
	require.NoError(t, err)
	assert.Empty(t, recs)
}

func TestDecodeEntries(t *testing.T) {
	type override func(id, seller, name, brand, category **string)
	set := func(f func(id, seller, name, brand, category **string)) override { return f }

	tests := []struct {
		name       string
		element    string // used verbatim when set
		change     override
		wantReason string // "" means accepted
		wantID     string
		wantBrand  string
		wantCat    string
		wantFlags  []ingest.Flag
		wantIdent  string
	}{
		{name: "id missing", change: set(func(id, _, _, _, _ **string) { *id = nil }), wantReason: "Id: missing"},
		{name: "id null", change: set(func(id, _, _, _, _ **string) { *id = lit("null") }), wantReason: "Id: null"},
		{name: "id number", change: set(func(id, _, _, _, _ **string) { *id = lit("42") }), wantReason: "Id: not a string"},
		{name: "id blank", change: set(func(id, _, _, _, _ **string) { *id = lit(`" "`) }), wantReason: "Id: blank"},
		{
			name: "id short first group", wantReason: "Id: not a UUID", wantID: "ddddeee-ffff-4000-1111-222233334444",
			change: set(func(id, _, _, _, _ **string) { *id = lit(`"ddddeee-ffff-4000-1111-222233334444"`) }),
		},
		{
			name: "id four groups", wantReason: "Id: not a UUID", wantID: "09835342345-4678-9abc-def012345678",
			change: set(func(id, _, _, _, _ **string) { *id = lit(`"09835342345-4678-9abc-def012345678"`) }),
		},
		{
			name: "id non-hex", wantReason: "Id: not a UUID", wantID: "uddd0000-eeee-4111-ffff-aaaa22223333",
			change: set(func(id, _, _, _, _ **string) { *id = lit(`"uddd0000-eeee-4111-ffff-aaaa22223333"`) }),
		},
		{
			name: "id with surrounding whitespace is trimmed", wantID: okID,
			change:    set(func(id, _, _, _, _ **string) { *id = lit(`"  ` + okID + `\n"`) }),
			wantBrand: "Samsung", wantCat: "Electronics",
			wantIdent: "Smartphone Galaxy S23 | brand: Samsung | category: Electronics",
		},
		{name: "seller missing", change: set(func(_, s, _, _, _ **string) { *s = nil }), wantReason: "SellerName: missing"},
		{name: "seller null", change: set(func(_, s, _, _, _ **string) { *s = lit("null") }), wantReason: "SellerName: null"},
		{name: "seller object", change: set(func(_, s, _, _, _ **string) { *s = lit(`{"a":1}`) }), wantReason: "SellerName: not a string"},
		{name: "name missing", change: set(func(_, _, n, _, _ **string) { *n = nil }), wantReason: "Name: missing"},
		{name: "name null", change: set(func(_, _, n, _, _ **string) { *n = lit("null") }), wantReason: "Name: null"},
		{name: "name blank", change: set(func(_, _, n, _, _ **string) { *n = lit(`"  \t "`) }), wantReason: "Name: blank"},
		{
			name: "name only controls is blank", change: set(func(_, _, n, _, _ **string) { *n = lit(`"\u0000\u200b"`) }),
			wantReason: "Name: blank", wantFlags: []ingest.Flag{{Field: "Name", Reason: ingest.ReasonControlChars}},
		},
		{name: "name number", change: set(func(_, _, n, _, _ **string) { *n = lit("123") }), wantReason: "Name: not a string"},
		{name: "brand number", change: set(func(_, _, _, b, _ **string) { *b = lit("7") }), wantReason: "Brand: not a string"},
		{name: "category array", change: set(func(_, _, _, _, c **string) { *c = lit(`["x"]`) }), wantReason: "Category: not a string"},
		{
			name: "brand missing", change: set(func(_, _, _, b, _ **string) { *b = nil }),
			wantCat: "Electronics", wantIdent: "Smartphone Galaxy S23 | category: Electronics",
		},
		{
			name: "brand empty", change: set(func(_, _, _, b, _ **string) { *b = lit(`""`) }),
			wantCat: "Electronics", wantIdent: "Smartphone Galaxy S23 | category: Electronics",
		},
		{
			name: "category missing", change: set(func(_, _, _, _, c **string) { *c = nil }),
			wantBrand: "Samsung", wantIdent: "Smartphone Galaxy S23 | brand: Samsung",
		},
		{
			name: "category null and brand blank", change: set(func(_, _, _, b, c **string) { *b, *c = lit(`" "`), lit("null") }),
			wantIdent: "Smartphone Galaxy S23",
		},
		{
			name: "sql payload with a valid id is kept and flagged",
			change: set(func(_, _, n, b, _ **string) {
				*n, *b = lit(`"Security Test Product"`), lit(`"TestBrand'; SELECT 1; --"`)
			}),
			wantBrand: "TestBrand'; SELECT 1; --", wantCat: "Electronics",
			wantFlags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonSQLLike}},
			wantIdent: "Security Test Product | brand: TestBrand'; SELECT 1; -- | category: Electronics",
		},
		{
			name: "sql payload with a malformed id is rejected and still flagged",
			change: set(func(id, _, _, b, _ **string) {
				*id, *b = lit(`"09835342345-4678-9abc-def012345678"`), lit(`"TestBrand'; SELECT 1; --"`)
			}),
			wantReason: "Id: not a UUID", wantID: "09835342345-4678-9abc-def012345678",
			wantBrand: "TestBrand'; SELECT 1; --", wantCat: "Electronics",
			wantFlags: []ingest.Flag{{Field: "Brand", Reason: ingest.ReasonSQLLike}},
		},
		{
			name: "unknown key is ignored", element: `{"Id":"` + okID + `","SellerName":"MegaStore","Name":"Smartphone Galaxy S23","Brand":"Samsung","Category":"Electronics","Price":9.5}`,
			wantBrand: "Samsung", wantCat: "Electronics",
			wantIdent: "Smartphone Galaxy S23 | brand: Samsung | category: Electronics",
		},
		{
			name: "keys are case-sensitive", element: `{"id":"` + okID + `","SellerName":"MegaStore","Name":"x"}`,
			wantReason: "Id: missing",
		},
		{
			name: "duplicate key", element: `{"Id":"` + okID + `","SellerName":"MegaStore","Name":"Real","Name":"Spoof"}`,
			wantReason: `duplicate key "Name"`,
		},
		{name: "element is a number", element: `42`, wantReason: "not an object"},
		{name: "element is a string", element: `"x"`, wantReason: "not an object"},
		{name: "element is null", element: `null`, wantReason: "not an object"},
		{name: "element is an array", element: `[{"Id":"` + okID + `"}]`, wantReason: "not an object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			el := tt.element
			if el == "" {
				id, seller, name, brand, category := valid()
				tt.change(&id, &seller, &name, &brand, &category)
				el = entry(id, seller, name, brand, category)
			}
			id, seller, name, brand, category := valid()
			clean := entry(id, seller, name, brand, category)

			recs, err := collect(t.Context(), t, "["+el+","+clean+"]")
			require.NoError(t, err)
			require.Len(t, recs, 2, "decoding continues after the element")
			assert.False(t, recs[1].Rejected())
			assert.Equal(t, 1, recs[1].Index)

			got := recs[0]
			assert.Equal(t, 0, got.Index)
			assert.Equal(t, tt.wantReason, got.Reason)
			assert.Equal(t, tt.wantReason != "", got.Rejected())
			assert.Equal(t, tt.wantIdent, got.Identity)
			assert.Equal(t, tt.wantFlags, got.Flags)
			if tt.wantID != "" {
				assert.Equal(t, tt.wantID, got.ID)
			}
			if tt.wantReason == "" || tt.wantBrand != "" {
				assert.Equal(t, tt.wantBrand, got.Brand)
				assert.Equal(t, tt.wantCat, got.Category)
			}
		})
	}
}

func TestDecodeStreamErrors(t *testing.T) {
	id, seller, name, brand, category := valid()
	el := entry(id, seller, name, brand, category)

	tests := []struct {
		name     string
		input    string
		wantRecs int // records yielded before the error
	}{
		{name: "empty input", input: ""},
		{name: "top level object", input: `{"Id":"x"}`},
		{name: "top level string", input: `"x"`},
		{name: "syntax error in the first element", input: `[{"Id":}]`},
		{name: "truncated after one element", input: "[" + el + ",", wantRecs: 1},
		{name: "missing closing bracket", input: "[" + el, wantRecs: 1},
		{name: "trailing data", input: "[" + el + "] junk", wantRecs: 1},
		{name: "second array", input: "[" + el + "][]", wantRecs: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs, err := collect(t.Context(), t, tt.input)
			require.Error(t, err)
			require.ErrorIs(t, err, errs.ErrInvalidInput)
			assert.Len(t, recs, tt.wantRecs)
		})
	}
}

func TestDecodePayloadLimit(t *testing.T) {
	id, seller, name, brand, category := valid()
	payload := "[" + entry(id, seller, name, brand, category) + "]"

	run := func(limit int64) error {
		for _, err := range ingest.DecodeWithLimit(t.Context(), strings.NewReader(payload), limit) {
			if err != nil {
				return err
			}
		}
		return nil
	}

	require.NoError(t, run(int64(len(payload))), "exactly at the limit")
	for _, limit := range []int64{int64(len(payload)) - 1, int64(len(payload)) / 2, 1} {
		err := run(limit)
		require.ErrorIs(t, err, errs.ErrInvalidInput, "limit %d", limit)
		assert.ErrorContains(t, err, "exceeds", "limit %d", limit)
	}
	assert.Equal(t, int64(64<<20), int64(ingest.MaxPayloadBytes))
}

func TestDecodeCancelledContext(t *testing.T) {
	id, seller, name, brand, category := valid()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	recs, err := collect(ctx, t, "["+entry(id, seller, name, brand, category)+"]")
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, recs)
}

func TestDecodeStopsWhenConsumerBreaks(t *testing.T) {
	id, seller, name, brand, category := valid()
	el := entry(id, seller, name, brand, category)

	n := 0
	for _, err := range ingest.Decode(t.Context(), strings.NewReader("["+el+","+el+","+el+"]")) {
		require.NoError(t, err)
		n++
		break
	}
	assert.Equal(t, 1, n)
}

func TestDecodeErrorIsNotRecord(t *testing.T) {
	for rec, err := range ingest.Decode(t.Context(), strings.NewReader("{")) {
		require.Error(t, err)
		assert.Equal(t, ingest.Record{}, rec)
		assert.False(t, errors.Is(err, context.Canceled))
	}
}
