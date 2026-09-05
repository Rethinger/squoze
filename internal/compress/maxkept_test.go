package compress

import (
	"strconv"
	"strings"
	"testing"
)

// buildFailures returns test output carrying n distinct failure lines in the
// middle region, padded so the blob clears MinBytes and the head/tail budget.
func buildFailures(n int) string {
	var b strings.Builder
	// 60 lines each side, not 20: the fixture has to clear MinBytes (2048) even
	// at n=3, or Text passes it through and the assertion tests nothing.
	for i := 0; i < 60; i++ {
		b.WriteString("=== RUN   TestSetupWithALongishName" + strconv.Itoa(i) + "\n")
	}
	for i := 0; i < n; i++ {
		b.WriteString("--- FAIL: TestCase" + strconv.Itoa(i) + " (0.01s)\n")
		b.WriteString("    case_test.go:" + strconv.Itoa(100+i) + ": want 4 got 5\n")
		b.WriteString("    some quiet context line that carries no signal\n")
	}
	for i := 0; i < 60; i++ {
		b.WriteString("ok  \tgithub.com/acme/pkg/quiet" + strconv.Itoa(i) + "\t0.01s\n")
	}
	return b.String()
}

// TestMaxKeptIsDisclosed is finding (e): past the cap, failure lines were
// dropped with nothing in the marker to say so. A reader told "50 kept" cannot
// distinguish 50 failures from 500, which makes never-elide unverifiable.
func TestMaxKeptIsDisclosed(t *testing.T) {
	p := Default
	in := buildFailures(200)
	out, changed := Text(in, p)
	if !changed {
		t.Fatal("expected compression")
	}
	if !strings.Contains(out, "over cap="+strconv.Itoa(p.MaxKept)) {
		t.Errorf("marker does not disclose the cap:\n%s", firstMarker(out))
	}
	if !strings.Contains(out, "more failure lines") {
		t.Errorf("marker does not disclose dropped failures:\n%s", firstMarker(out))
	}
}

// TestMaxKeptCountIsAccurate pins the disclosed number to what was actually
// refused, so the disclosure cannot drift into decoration.
func TestMaxKeptCountIsAccurate(t *testing.T) {
	p := Default
	p.ContextAfter = 0 // one kept line per failure, so arithmetic is exact
	p.MaxKept = 10
	const failures = 40
	out, changed := Text(buildFailures(failures), p)
	if !changed {
		t.Fatal("expected compression")
	}
	// Each failure contributes 2 mustKeep lines: the `--- FAIL` header and its
	// `want 4 got 5` detail (mustKeep matches FAIL:, and the detail line does
	// not — so exactly one per failure).
	want := failures - p.MaxKept
	if !strings.Contains(out, strconv.Itoa(want)+" more failure lines") {
		t.Errorf("want %d dropped disclosed, marker was:\n%s", want, firstMarker(out))
	}
}

// TestNoDisclosureWhenNothingDropped keeps the marker quiet in the common case:
// a blob under the cap must produce the same bytes as before this change.
func TestNoDisclosureWhenNothingDropped(t *testing.T) {
	out, changed := Text(buildFailures(3), Default)
	if !changed {
		t.Fatal("expected compression")
	}
	if strings.Contains(out, "more failure lines") {
		t.Errorf("disclosed a drop that did not happen:\n%s", firstMarker(out))
	}
}

// TestDisclosureStaysIdempotent guards the cache contract: the marker must
// still be recognised as ours, so a second pass is a no-op.
func TestDisclosureStaysIdempotent(t *testing.T) {
	once, changed := Text(buildFailures(200), Default)
	if !changed {
		t.Fatal("expected compression")
	}
	twice, changedAgain := Text(once, Default)
	if changedAgain {
		t.Error("second pass mutated an already-squeezed blob")
	}
	if twice != once {
		t.Error("second pass changed bytes; prompt-cache prefix would break")
	}
}

// TestDisclosureKeepsMarkerOnOneLine matters because the marker's shape is the
// idempotency guard. A newline inside it would create a line that no longer
// carries markerPrefix.
func TestDisclosureKeepsMarkerOnOneLine(t *testing.T) {
	out, _ := Text(buildFailures(200), Default)
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, markerPrefix) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("expected exactly 1 marker line, got %d", n)
	}
}

func firstMarker(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, markerPrefix) {
			return line
		}
	}
	return "<no marker>"
}
