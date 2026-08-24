// Package eval hosts the deterministic quality-fixture harness: realistic
// tool-output classes with must-survive facts and minimum savings floors.
//
// This is the always-runnable tier of the evaluation strategy. The
// dataset-backed tier (LoCoMo / RULER / BFCL accuracy deltas through a live
// provider) is specified in docs/eval-protocol.md and runs at release time.
package eval

import (
	"fmt"
	"strings"

	"github.com/Rethinger/squoze/internal/engine"
)

// Case is one quality fixture.
type Case struct {
	Name          string   // fixture class
	Model         string   // model id exercising a profile family
	Original      string   // tool output as the agent produced it
	MustKeep      []string // substrings that MUST survive any squeeze
	MinSavingsPct float64  // 0 = squeeze optional; >0 = hard floor
}

// Result reports what squoze did to one case.
type Result struct {
	Case         Case
	Squeezed     bool
	Kept         bool // all MustKeep substrings present after transform
	MissingFacts []string
	OriginalByte int
	SentBytes    int
	SavingsPct   float64
}

func (r Result) String() string {
	status := "kept"
	if !r.Kept {
		status = "LOST FACTS"
	}
	return fmt.Sprintf("%-22s %7d → %7d bytes  saved %.1f%%  [%s]",
		r.Case.Name, r.OriginalByte, r.SentBytes, r.SavingsPct, status)
}

// Run executes cases against a fresh engine and returns per-case reports.
func Run(cases []Case) []Result {
	out := make([]Result, 0, len(cases))
	for _, c := range cases {
		raw, _ := wrapToolContent(c.Model, c.Original)
		body, res := engine.NewEngine(engine.DefaultMemoCapacity).Apply(raw)
		r := Result{
			Case:         c,
			Squeezed:     res.BlocksSqueezed > 0,
			OriginalByte: res.OriginalBytes,
			SentBytes:    res.SentBytes,
		}
		if res.SavedBytes > 0 {
			r.SavingsPct = 100 * float64(res.SavedBytes) / float64(res.OriginalBytes)
		}
		content := extractToolContent(body)
		kept := true
		for _, fact := range c.MustKeep {
			if !strings.Contains(content, fact) && !strings.Contains(string(body), fact) {
				kept = false
				r.MissingFacts = append(r.MissingFacts, fact)
			}
		}
		r.Kept = kept
		out = append(out, r)
	}
	return out
}

// Report renders the classic was→is table over run results and returns
// false if any case violated its contracts.
func Report(results []Result) (string, bool) {
	var b strings.Builder
	ok := true
	for _, r := range results {
		b.WriteString(r.String() + "\n")
		for _, f := range r.MissingFacts {
			fmt.Fprintf(&b, "  !! lost fact: %q\n", f)
		}
		if !r.Kept {
			ok = false
			fmt.Fprintf(&b, "  !! contract violation: must-keep facts lost\n")
		}
		if r.Case.MinSavingsPct > 0 && r.SavingsPct < r.Case.MinSavingsPct {
			ok = false
			fmt.Fprintf(&b, "  !! savings floor missed: %.1f%% < %.1f%%\n",
				r.SavingsPct, r.Case.MinSavingsPct)
		}
	}
	return b.String(), ok
}
