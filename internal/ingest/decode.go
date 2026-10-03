package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"regexp"
	"strings"

	"github.com/MagnunAVF/bifrost-ingestion/internal/platform/errs"
)

// MaxPayloadBytes caps the whole input. json.Decoder buffers one element at a time, so this
// also bounds the memory one hostile element can take. The fixture is 48 KB.
const MaxPayloadBytes = 64 << 20

// Record is one element of the payload: a clean entry, or a rejected one with a Reason.
// Text fields hold sanitized values (also on rejects, for the report); Brand and Category are
// "" when missing, null or blank.
type Record struct {
	Index      int    `json:"index"`
	ID         string `json:"id,omitempty"`
	SellerName string `json:"sellerName,omitempty"`
	Name       string `json:"name,omitempty"`
	Brand      string `json:"brand,omitempty"`
	Category   string `json:"category,omitempty"`
	Identity   string `json:"identity,omitempty"` // "" when rejected
	Reason     string `json:"reason,omitempty"`   // non-empty means rejected
	Flags      []Flag `json:"flags,omitempty"`
}

// Rejected reports whether the entry must not be ingested.
func (r Record) Rejected() bool { return r.Reason != "" }

// Decode stream-decodes a top-level JSON array of product entries, one element at a time.
// A bad element becomes a rejected Record and decoding continues. A payload that cannot be read
// as an array (syntax error, other top-level value, data after the array, more than
// MaxPayloadBytes) ends the sequence with an error wrapping errs.ErrInvalidInput; a cancelled
// ctx ends it with ctx.Err(). Records yielded before an error stay valid.
func Decode(ctx context.Context, r io.Reader) iter.Seq2[Record, error] {
	return decode(ctx, r, MaxPayloadBytes)
}

func decode(ctx context.Context, r io.Reader, limit int64) iter.Seq2[Record, error] {
	return func(yield func(Record, error) bool) {
		lr := &io.LimitedReader{R: r, N: limit + 1}
		fail := func(format string, args ...any) {
			if lr.N <= 0 {
				yield(Record{}, fmt.Errorf("decoding payload: exceeds %d bytes: %w", limit, errs.ErrInvalidInput))
				return
			}
			yield(Record{}, fmt.Errorf("decoding payload: "+format+": %w", append(args, errs.ErrInvalidInput)...))
		}

		dec := json.NewDecoder(lr)
		if tok, err := dec.Token(); err != nil {
			fail("reading the opening bracket: %v", err)
			return
		} else if tok != json.Delim('[') {
			fail("top level is not an array")
			return
		}

		for i := 0; dec.More(); i++ {
			if err := ctx.Err(); err != nil {
				yield(Record{}, fmt.Errorf("decoding payload: %w", err))
				return
			}
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				fail("element %d: %v", i, err)
				return
			}
			if !yield(parseEntry(i, raw), nil) {
				return
			}
		}

		if _, err := dec.Token(); err != nil {
			fail("reading the closing bracket: %v", err)
			return
		}
		if _, err := dec.Token(); !errors.Is(err, io.EOF) {
			fail("data after the array")
			return
		}
		if lr.N <= 0 {
			fail("too large")
		}
	}
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// parseEntry turns one already-valid JSON value into a Record. It never fails: every problem
// becomes the Reason of a rejected Record. The first problem, in field order, wins.
func parseEntry(index int, raw json.RawMessage) Record {
	rec := Record{Index: index}
	fields, reason := objectFields(raw)
	if reason != "" {
		rec.Reason = reason
		return rec
	}

	reject := func(r string) {
		if rec.Reason == "" {
			rec.Reason = r
		}
	}
	text := func(name string, required bool) string {
		v, present := fields[name]
		switch {
		case !present:
			if required {
				reject(name + ": missing")
			}
			return ""
		case bytes.Equal(v, []byte("null")):
			if required {
				reject(name + ": null")
			}
			return ""
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			reject(name + ": not a string")
			return ""
		}
		out, flags := sanitizeField(name, s)
		rec.Flags = append(rec.Flags, flags...)
		if out == "" && required {
			reject(name + ": blank")
		}
		return out
	}

	rec.ID = text("Id", true)
	if rec.ID != "" {
		if uuidShape.MatchString(rec.ID) {
			rec.ID = strings.ToLower(rec.ID)
		} else {
			reject("Id: not a UUID")
		}
	}
	rec.SellerName = text("SellerName", true)
	rec.Name = text("Name", true)
	rec.Brand = text("Brand", false)
	rec.Category = text("Category", false)

	if !rec.Rejected() {
		rec.Identity = Identity(rec.Name, rec.Brand, rec.Category)
	}
	return rec
}

// objectFields splits a JSON object into its raw values by exact key. It walks the tokens
// itself because encoding/json silently keeps the last of duplicate keys.
func objectFields(raw json.RawMessage) (map[string]json.RawMessage, string) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, "not an object"
	}
	fields := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, "not an object"
		}
		key, _ := tok.(string)
		if _, dup := fields[key]; dup {
			return nil, fmt.Sprintf("duplicate key %q", Sanitize(key))
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, "not an object"
		}
		fields[key] = v
	}
	return fields, ""
}
