package dedup

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/MagnunAVF/bifrost-ingestion/internal/ingest"
)

// Outcome is what happened to one record.
type Outcome string

// The outcomes of a record.
const (
	OutcomeInserted Outcome = "inserted" // novel: Product + SellerProduct
	OutcomeLinked   Outcome = "linked"   // duplicate: SellerProduct only
	OutcomeExisting Outcome = "existing" // (seller, id) already linked: no new link
	OutcomeRejected Outcome = "rejected" // ingest rejected the record
	OutcomeFailed   Outcome = "failed"   // store error; its transaction rolled back
)

// Result is the decision and outcome for one record.
type Result struct {
	Index           int
	SellerName      string
	SellerProductID string
	Identity        string
	Outcome         Outcome
	Kind            Kind    // 0 when not embedded (existing, rejected)
	Score           float32 // nearest score, when embedded
	MatchID         int64   // nearest Product.Id, when embedded
	MatchIdentity   string  // its identity string (after any update in this run)
	ProductID       int64   // the product linked to; < 0 is provisional (dry run)
	Reason          string  // reject reason or error text
	ErrKind         string  // for failed: "conflict", "not found" or "other"
	Flags           []ingest.Flag
	Changes         []Change // attribute updates applied (or simulated) on ProductID
	Notes           []string // differences left alone, with why
}

// Suspicious reports whether the record had flags (rejects included).
func (r Result) Suspicious() bool { return len(r.Flags) > 0 }

// Report is the run report: counts, every result in input order, and why the run stopped.
type Report struct {
	DryRun      bool
	Threshold   float32
	Update      UpdateMode
	CatalogSize int

	Inserted, Linked, Existing, Rejected, Failed, Suspicious int
	Updated                                                  int // distinct Products changed

	Results []Result
	Stopped *RunError // nil when the run finished
}

func (r *Report) add(res Result) {
	switch res.Outcome {
	case OutcomeInserted:
		r.Inserted++
	case OutcomeLinked:
		r.Linked++
	case OutcomeExisting:
		r.Existing++
	case OutcomeRejected:
		r.Rejected++
	case OutcomeFailed:
		r.Failed++
	}
	if res.Suspicious() {
		r.Suspicious++
	}
	r.Results = append(r.Results, res)
}

// Stage says where a run stopped.
type Stage string

// The stages a run can stop at.
const (
	StagePreflight Stage = "preflight"
	StageColdStart Stage = "cold start"
	StageDecode    Stage = "decode"
	StageEmbed     Stage = "embed"
	StageCancelled Stage = "cancelled"
)

// RunError is a fatal error with where the run stopped and how far it got. errors.Is and
// errors.As see through it to the cause.
type RunError struct {
	Stage     Stage
	Index     int // record index being handled; -1 before the first record
	Processed int // records fully handled before the stop
	Err       error
}

func (e *RunError) Error() string {
	if e.Index < 0 {
		return fmt.Sprintf("stopped at %s: %v", e.Stage, e.Err)
	}
	return fmt.Sprintf("stopped at %s (record %d, %d done): %v", e.Stage, e.Index, e.Processed, e.Err)
}

func (e *RunError) Unwrap() error { return e.Err }

// WriteText prints the summary, then sections for rejected, failed, suspicious and inserted
// records, attribute changes and notes. verbose adds one line per record with its decision,
// score and nearest match. A stopped run ends with the RunError.
func (r Report) WriteText(w io.Writer, verbose bool) error {
	bw := bufio.NewWriter(w)
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(bw, format, args...) }

	if r.DryRun {
		p("Bifröst ingest report (DRY RUN: nothing written)\n")
	} else {
		p("Bifröst ingest report\n")
	}
	p("threshold %.3f, update %s, catalog %d products\n", r.Threshold, r.Update, r.CatalogSize)
	p("records %d: inserted %d, linked %d, existing %d, rejected %d, failed %d, suspicious %d, products updated %d\n",
		len(r.Results), r.Inserted, r.Linked, r.Existing, r.Rejected, r.Failed, r.Suspicious, r.Updated)

	section := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		p("\n%s (%d):\n", title, len(lines))
		for _, l := range lines {
			p("  %s\n", l)
		}
	}
	var rejected, failed, suspicious, inserted, changes, notes []string
	for _, res := range r.Results {
		who := fmt.Sprintf("#%d %s %s", res.Index, res.SellerName, res.SellerProductID)
		switch res.Outcome {
		case OutcomeRejected:
			rejected = append(rejected, who+": "+res.Reason)
		case OutcomeFailed:
			failed = append(failed, fmt.Sprintf("%s: %s: %s", who, res.ErrKind, res.Reason))
		case OutcomeInserted:
			line := fmt.Sprintf("%s → product %d %q", who, res.ProductID, res.Identity)
			if res.MatchID != 0 {
				line += fmt.Sprintf(" (nearest %d at %.3f)", res.MatchID, res.Score)
			}
			inserted = append(inserted, line)
		}
		if res.Suspicious() {
			flags := make([]string, len(res.Flags))
			for i, f := range res.Flags {
				flags[i] = f.Field + ": " + f.Reason
			}
			suspicious = append(suspicious, who+": "+strings.Join(flags, "; "))
		}
		for _, c := range res.Changes {
			old := c.Old
			if old == "" {
				old = "(null)"
			}
			changes = append(changes, fmt.Sprintf("#%d product %d: %s: %s → %s", res.Index, res.ProductID, c.Field, old, c.New))
		}
		for _, n := range res.Notes {
			notes = append(notes, fmt.Sprintf("#%d product %d: %s", res.Index, res.ProductID, n))
		}
	}
	section("rejected", rejected)
	section("failed", failed)
	section("suspicious", suspicious)
	section("inserted", inserted)
	section("changes", changes)
	section("notes", notes)

	if verbose && len(r.Results) > 0 {
		p("\ndecisions:\n")
		for _, res := range r.Results {
			switch res.Outcome {
			case OutcomeRejected:
				p("  #%d rejected: %s\n", res.Index, res.Reason)
			case OutcomeFailed:
				p("  #%d failed: %s: %s\n", res.Index, res.ErrKind, res.Reason)
			case OutcomeExisting:
				p("  #%d existing → product %d\n", res.Index, res.ProductID)
			default:
				p("  #%d %s %s %.3f → product %d %q, nearest %d %q\n", res.Index, res.Outcome, res.Kind,
					res.Score, res.ProductID, res.Identity, res.MatchID, res.MatchIdentity)
			}
		}
	}
	if r.Stopped != nil {
		p("\nstopped: %v\n", r.Stopped)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}
	return nil
}
