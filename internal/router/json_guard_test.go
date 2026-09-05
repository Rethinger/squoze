package router

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// oracleIsJSON is the JSON branch exactly as it stood before the closer guard:
// trimmed prefix opens with a bracket and parses. The guard is only allowed to
// skip parses that would have failed, so this is the reference every input is
// checked against rather than a second opinion I assert by hand.
func oracleIsJSON(s string) bool {
	head := s
	if len(head) > probeLen {
		head = head[:probeLen]
	}
	trimmed := strings.TrimSpace(head)
	return trimmed != "" && (trimmed[0] == '{' || trimmed[0] == '[') &&
		json.Valid([]byte(trimmed))
}

func jsonObject(n int) string {
	var b strings.Builder
	b.WriteString(`{"object":"list"`)
	for i := 0; i < n; i++ {
		b.WriteString(`,"field_`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`":"value-`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`"`)
	}
	b.WriteByte('}')
	return b.String()
}

func jsonArray(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":`)
		b.WriteString(strconv.Itoa(1000 + i))
		b.WriteString(`,"status":"RUNNING"}`)
	}
	b.WriteByte(']')
	return b.String()
}

// jsonNestedObject nests an object per row, so the closer matching the
// document's own opener ('}') occurs every few dozen bytes. jsonObject and
// jsonArray cannot serve here: each holds exactly one matching closer, at the
// very last byte, which no padding can move to the probe boundary.
func jsonNestedObject(n int) string {
	var b strings.Builder
	b.WriteString(`{"object":"list"`)
	for i := 0; i < n; i++ {
		b.WriteString(`,"row_`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`":{"id":`)
		b.WriteString(strconv.Itoa(1000 + i))
		b.WriteString(`,"status":"RUNNING"}`)
	}
	b.WriteByte('}')
	return b.String()
}

// padToCloserAt shifts doc right with leading spaces until the byte the probe
// cuts at is the closer MATCHING doc's opener, so the truncated prefix passes
// the closer guard and still fails to parse. Without such an input the guard
// could be a decision rather than a filter and no test would notice.
//
// The closer has to match: padding an array-opened document until a '}' lands
// on the boundary yields '[' ... '}', which the guard rejects, so a fixture
// built that way silently exercises the skipped path instead of the kept one.
func padToCloserAt(tb testing.TB, doc string) string {
	tb.Helper()
	if len(doc) <= probeLen {
		tb.Fatalf("fixture must exceed probeLen (%d bytes), got %d", probeLen, len(doc))
	}
	opener := doc[0]
	want := byte('}')
	if opener == '[' {
		want = ']'
	}
	for pad := 0; pad < 512; pad++ {
		cand := strings.Repeat(" ", pad) + doc
		if cand[probeLen-1] != want {
			continue
		}
		// The two properties the callers depend on, checked rather than assumed.
		trimmed := strings.TrimSpace(cand[:probeLen])
		if trimmed[0] != opener || trimmed[len(trimmed)-1] != want {
			tb.Fatalf("padded prefix is %q..%q, want %q..%q",
				trimmed[0], trimmed[len(trimmed)-1], opener, want)
		}
		if json.Valid([]byte(trimmed)) {
			tb.Fatal("padded prefix parses as JSON; it must pass the guard and fail the parse")
		}
		return cand
	}
	tb.Fatalf("no padding placed a %q at the probe boundary", want)
	return ""
}

func TestJSONCloserGuardMatchesOracle(t *testing.T) {
	small := jsonObject(20)
	if len(small) >= probeLen {
		t.Fatalf("small fixture is not small: %d bytes", len(small))
	}
	// Complete JSON followed by enough whitespace to push the body past the
	// probe. The prefix trims back to the whole document, so this DOES route
	// as JSON above probeLen — the one shape that made a plain length check
	// the wrong guard.
	wsPadded := jsonObject(20) + strings.Repeat("\n", probeLen)

	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"whitespace only", "   \n\t "},
		{"small object", small},
		{"small array", jsonArray(40)},
		{"object over probe", jsonObject(4000)},
		{"array over probe", jsonArray(4000)},
		{"complete json padded past probe", wsPadded},
		{"truncated at a matching closer", padToCloserAt(t, jsonNestedObject(4000))},
		{"prose opening with a brace", "{ the migration plan needs review before Friday, and " +
			strings.Repeat("the rollback path is still undocumented. ", 400)},
		{"bracketed log lines", strings.Repeat("[2026-08-24T10:00:00Z] INFO server ready\n", 300)},
		{"json with trailing garbage", jsonArray(200) + "\nERROR: connection reset\n"},
	}

	for _, tc := range cases {
		wantJSON := oracleIsJSON(tc.in)
		gotJSON := Classify(tc.in) == KindJSON
		if gotJSON != wantJSON {
			t.Errorf("%s: Classify says KindJSON=%v, pre-guard behaviour was %v",
				tc.name, gotJSON, wantJSON)
		}
	}
}

// TestJSONBranchAllocatesNothing is AC-8.1. A failing json.Valid returns a
// heap-allocated *SyntaxError that nobody reads; it was the only allocation
// anywhere in the router, paid on every JSON tool result large enough to be
// worth compressing.
func TestJSONBranchAllocatesNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
	}{
		{"object over probe", jsonObject(4000)},
		{"array over probe", jsonArray(4000)},
		// A prefix that ends on its matching closer belongs to the other side
		// of the guard: the parse legitimately runs and allocates the
		// SyntaxError nobody reads. TestJSONGuardIsAFilter covers that shape.
	} {
		in := tc.in
		if n := testing.AllocsPerRun(50, func() { benchSink = Classify(in) }); n != 0 {
			t.Errorf("%s: %.0f allocs per Classify, want 0", tc.name, n)
		}
	}
}

// TestJSONOverProbeRoutesAsProse states the consequence plainly so a future
// reader does not take it for a bug: the prefix probe cannot validate a
// truncated document, and both call sites in internal/engine treat KindJSON
// and KindProse identically, so nothing observable turns on it.
func TestJSONOverProbeRoutesAsProse(t *testing.T) {
	big := jsonArray(4000)
	if len(big) <= probeLen {
		t.Fatalf("fixture must exceed probeLen, got %d bytes", len(big))
	}
	if got := Classify(big); got != KindProse {
		t.Errorf("got %v, want prose — if this now says json, the prefix probe changed", got)
	}
	if got := Classify(jsonArray(40)); got != KindJSON {
		t.Errorf("small array got %v, want json", got)
	}
}

// TestJSONGuardIsAFilter pins the other side of FR-8: a prefix that ends on the
// closer matching its opener must still reach json.Valid. A guard that also
// rejected those would be deciding the JSON question by itself, and the very
// first shape it got wrong would change a verdict.
//
// The proof is allocation accounting rather than a fixed count: json.Valid's
// failure path heap-allocates the SyntaxError nobody reads, so if the parse ran
// inside Classify it must cost at least what the bare oracle costs on the same
// input. Stated as a comparison, the test survives a Go release that stops
// allocating there — both sides fall to zero together.
func TestJSONGuardIsAFilter(t *testing.T) {
	in := padToCloserAt(t, jsonNestedObject(4000))
	if oracleIsJSON(in) {
		t.Fatal("fixture parses; it cannot show that a failing parse still runs")
	}
	if got := Classify(in); got != KindProse {
		t.Fatalf("got %v, want prose", got)
	}
	oracle := testing.AllocsPerRun(50, func() { benchSinkBool = oracleIsJSON(in) })
	full := testing.AllocsPerRun(50, func() { benchSink = Classify(in) })
	if full < oracle {
		t.Errorf("Classify allocated %.0f, the bare parse of the same prefix allocates %.0f — "+
			"the guard skipped a parse it must let through", full, oracle)
	}
}

var benchSinkBool bool

// BenchmarkJSONProbeSkipped measures the work the closer guard removes. It
// benchmarks oracleIsJSON — the pre-guard condition — on the shapes that always
// failed it, so the figure is the parse now skipped rather than a difference
// between two runs of the whole function. Cross-run sec/op comparison on this
// machine could not resolve it: the second run drifted enough to report
// double-digit "regressions" on branches the change cannot reach.
//
// With the guard the same step costs two byte comparisons, so whatever this
// prints is the saving per large JSON tool result.
func BenchmarkJSONProbeSkipped(b *testing.B) {
	cases := []struct {
		name string
		in   string
	}{
		{"object_over_probe", jsonObject(4000)},
		{"array_over_probe", jsonArray(4000)},
	}
	for _, tc := range cases {
		if oracleIsJSON(tc.in) {
			b.Fatalf("%s: fixture parses, so nothing is being skipped", tc.name)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchSinkBool = oracleIsJSON(tc.in)
			}
		})
	}
}

// BenchmarkJSONProbeKept is the counterweight: a document whose truncated
// prefix does end at a closer still gets parsed, so the guard is a filter and
// not a decision. If this ever collapses to the cost of two comparisons, the
// guard has started rejecting inputs it must let through.
func BenchmarkJSONProbeKept(b *testing.B) {
	// Built before the timer starts. The search rebuilds the fixture per
	// attempt and strconv.Itoa allocates per row, so leaving it inside the
	// measured region charged ~39k setup allocations to the benchmark: B/op
	// then tracked 1/b.N instead of the path, and the product b.N x B/op came
	// out constant at 6.36 MB, which is how the mistake showed itself.
	//
	// A nested object, not jsonArray: its matching closer recurs inside the
	// document, so padding can land one on the probe boundary. Reading the
	// result, 1 alloc/op is the fingerprint of the parse actually running —
	// 0 would mean this benchmark had drifted onto the skipped path.
	in := padToCloserAt(b, jsonNestedObject(4000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = Classify(in)
	}
}
