package engine

import (
	"strings"

	"github.com/Rethinger/squoze/internal/distill"
	"github.com/Rethinger/squoze/internal/router"
)

// canDistill reports whether any pass of distillText could possibly change s.
//
// Why this exists. Measured on golang:1.22 with a 141 KB JSON tool result whose
// rows are not liftable, pass 2 spent 4.152 ms and returned ok=false; at 565 KB
// it spent 16.206 ms, also ok=false. The cost is json.Unmarshal into any, which
// allocates a map per row — and it was paid precisely when nothing came of it.
// The other passes are an order cheaper (IsUnifiedDiff 0.541 ms, Classify
// 0.270 ms, SanitizeTerminal 0.088 ms at that size), so the point of the gate
// is to reach a verdict before the allocation, not to shave the cheap passes.
//
// The safety rule is asymmetric and worth stating plainly. A false positive
// (saying yes to a block no pass will touch) costs exactly the work v0.3.0
// already does, so it is always safe. A false negative would drop savings the
// engine used to find, which is why every condition below is a *necessary*
// condition of its pass succeeding, never a guess at a sufficient one. Each
// disjunct is therefore a superset of the outcomes its pass can produce.
//
// Ordering is by cost, cheapest decisive check first, with one exception: the
// JSON test comes first even though it is not the cheapest, because it is the
// pass whose cost the gate exists to remove and its condition is exact rather
// than merely necessary.
func canDistill(s string) bool {
	// Pass 2 — JSON structural pruning and tabular lifting. distill.CanDistillJSON
	// answers exactly the question DistillJSON is about to answer expensively:
	// is there a liftable array of objects, or any prunable feature at all.
	if trimmed := strings.TrimSpace(s); len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if distill.CanDistillJSON(trimmed) {
			return true
		}
	}

	// Pass 1 — unified diff. All three shapes IsUnifiedDiff accepts contain a
	// double dash ("diff --git ", "--- a/", "--- "), so one IndexByte-driven
	// scan for "--" rejects prose, logs and JSON before the three Contains
	// passes over the whole block.
	if strings.Contains(s, "--") && distill.IsUnifiedDiff(s) {
		return true
	}

	// Pass 4 — terminal hygiene. SanitizeTerminal rewrites ANSI escapes and
	// carriage returns and nothing else, so absent both bytes it cannot change
	// a thing. Two IndexByte scans, no allocation.
	if strings.IndexByte(s, 0x1b) >= 0 || strings.IndexByte(s, '\r') >= 0 {
		return true
	}

	// Pass 3 — test and log squeezing, gated on the classifier. This is the
	// most expensive check left, so it runs last; note that a Test/Log verdict
	// is necessary but not sufficient (compress.Text still has a per-family
	// size floor), and erring towards doing the work is the safe direction.
	switch router.Classify(s) {
	case router.KindTestOutput, router.KindLogOutput:
		return true
	}

	return false
}
