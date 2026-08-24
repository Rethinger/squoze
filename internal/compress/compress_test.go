package compress

import (
	"strings"
	"testing"
)

func bigBlob(lines int, errEvery int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		if errEvery > 0 && i%errEvery == 0 {
			b.WriteString("--- FAIL: TestSomething\n")
		} else {
			b.WriteString("ok  line of verbose machine output padding padding\n")
		}
	}
	return b.String()
}

func TestTextSqueezesBigBlobs(t *testing.T) {
	in := bigBlob(400, 0)
	out, changed := Text(in, Default)
	if !changed {
		t.Fatal("expected compression")
	}
	if len(out) >= len(in) {
		t.Fatalf("no savings: %d -> %d", len(in), len(out))
	}
	if !strings.Contains(out, markerPrefix) {
		t.Fatal("marker missing")
	}
	got := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	want := strings.Split(strings.TrimSuffix(in, "\n"), "\n")
	if got[0] != want[0] || got[len(got)-1] != want[len(want)-1] {
		t.Fatalf("head/tail not preserved:\n head got=%q want=%q\n tail got=%q want=%q",
			got[0], want[0], got[len(got)-1], want[len(want)-1])
	}
}

func TestTextIdempotent(t *testing.T) {
	in := bigBlob(400, 25) // includes FAIL lines in the middle
	out1, _ := Text(in, Default)
	out2, changed := Text(out1, Default)
	if changed || out2 != out1 {
		t.Fatalf("second pass mutated: %d -> %d bytes", len(out1), len(out2))
	}
}

func TestTextKeepsErrorLines(t *testing.T) {
	in := bigBlob(400, 25)
	out, changed := Text(in, Default)
	if !changed {
		t.Fatal("expected compression")
	}
	if strings.Count(out, "--- FAIL") != strings.Count(in, "--- FAIL") {
		t.Fatalf("error lines lost: in=%d out=%d",
			strings.Count(in, "--- FAIL"), strings.Count(out, "--- FAIL"))
	}
}

func TestTextSmallBlobPassesThrough(t *testing.T) {
	small := strings.Repeat("short\n", 10)
	if out, changed := Text(small, Default); changed || out != small {
		t.Fatal("small blob must pass through untouched")
	}
}

func TestTextSavingsFloorSkipsMarginalWins(t *testing.T) {
	// head+tail cover nearly everything: nothing to save.
	lines := make([]string, 0, 45)
	for i := 0; i < 44; i++ {
		lines = append(lines, strings.Repeat("x", 80))
	}
	in := strings.Join(lines, "\n")
	if out, changed := Text(in, Default); changed || out != in {
		t.Fatal("marginal squeeze must be rejected by savings floor")
	}
}
