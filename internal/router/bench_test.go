package router

import (
	"strconv"
	"strings"
	"testing"
)

// benchSink keeps Classify's result observable so the call is not eliminated.
var benchSink Kind

// repeatTo grows seed by whole copies until it reaches at least n bytes. Whole
// copies rather than a byte slice: cutting a fixture mid-line would change
// which markers the last line carries, and marker counts are what Classify
// measures.
func repeatTo(seed string, n int) string {
	if seed == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(n + len(seed))
	for b.Len() < n {
		b.WriteString(seed)
	}
	return b.String()
}

// goTestPanic is a crashed `go test` run: two test markers and a stack trace.
// Before crashHits existed this scored 2 against a threshold of 3 and routed
// to prose, so it is both the FR-6 fixture and the shape most worth measuring.
const goTestPanic = `=== RUN   TestWorker
--- FAIL: TestWorker (0.00s)
panic: runtime error: index out of range [5] with length 3 [recovered]

goroutine 21 [running]:
testing.tRunner.func1.2({0x104d0a0, 0xc0000180a8})
	/usr/local/go/src/testing/testing.go:1631 +0x24a
github.com/acme/worker.process(...)
	/src/worker/process.go:88
github.com/acme/worker.TestWorker(0xc000106340)
	/src/worker/worker_test.go:42 +0x18f
FAIL	github.com/acme/worker	0.213s
exit status 1
`

// buildDiagnostics is a broken build: almost every line goes through
// looksLikeDiagnostic, which is the most per-line work any branch does.
func buildDiagnostics(n int) string {
	var b strings.Builder
	b.WriteString("# github.com/acme/worker\n")
	for i := 0; i < n; i++ {
		b.WriteString("./process.go:")
		b.WriteString(strconv.Itoa(12 + i))
		b.WriteString(":6: undefined: helperFunctionWithARatherLongName\n")
	}
	b.WriteString("FAIL\tgithub.com/acme/worker [build failed]\n")
	return b.String()
}

const logLine = "2026-08-24T10:00:04Z INFO  server request_id=8f21ab route=/v1/chat/completions status=200 duration_ms=41\n"

// proseParagraph is a pasted design note: multi-line, so hasCodeOpener has
// real lines to walk, and free of column-0 keywords, so it walks all
// codeHeaderLines of them before giving up.
const proseParagraph = `The migration plan needs review before Friday, and the rollback path is
still undocumented. We prefer a gradual rollout behind a feature flag so the
old and new writers can run side by side for a week.

Nobody has measured the tail latency of the new writer under load yet, which
is the one number that would settle the argument about batch sizes.

`

// proseWithKeywords is the same note with language keywords used as ordinary
// words. They sit mid-sentence, never at column 0, which is the asymmetry
// hasCodeOpener relies on — this measures what the code detector costs on the
// blobs it is meant to decline.
const proseWithKeywords = `Both a func and a class are just names to the reviewer; what matters is that
the import list stays short. A const that nobody reads is still a const.

We should return to the question of whether the struct is worth the churn,
because a type that exists only to satisfy an interface is a type we maintain
for nothing.

`

// jsonList builds a paginated API response of n rows. Size decides which
// branch it reaches: Classify validates only the first probeLen bytes, so a
// JSON document longer than that has a truncated — and therefore invalid —
// prefix and never returns KindJSON. See the two json cases below.
func jsonList(n int) string {
	var b strings.Builder
	b.WriteString(`{"object":"list","has_more":true,"data":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"id":`)
		b.WriteString(strconv.Itoa(1000 + i))
		b.WriteString(`,"name":"service-worker-`)
		b.WriteString(strconv.Itoa(i))
		b.WriteString(`","status":"RUNNING"}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

const (
	kb  = 1024
	big = 270 * kb // matches BenchmarkApply270KB in internal/engine
)

// BenchmarkClassify measures one routing decision per shape at a size a coding
// agent actually pastes. Classify runs on every tool block before any
// compression happens, so its cost is paid even by blobs that are never
// touched.
//
// No SetBytes: sampleWindows caps the scan at maxWindows*windowLen, so a
// throughput figure over the nominal input size would fall as the input grows
// while the work stayed flat, which reads as a regression that is not there.
// See BenchmarkClassifyScaling for the cap itself.
func BenchmarkClassify(b *testing.B) {
	cases := []struct {
		name string
		in   string
		want Kind
	}{
		{"code", repeatTo(goSourceEmbeddingTestOutput, big), KindCode},
		{"test_output", repeatTo(goTestPanic, big), KindTestOutput},
		{"build_diagnostics", buildDiagnostics(4000), KindTestOutput},
		{"log", repeatTo(logLine, big), KindLogOutput},
		{"prose", repeatTo(proseParagraph, big), KindProse},
		{"prose_code_words", repeatTo(proseWithKeywords, big), KindProse},
		// Under probeLen: the prefix is the whole document, json.Valid
		// succeeds and the counters are never reached.
		{"json_within_probe", jsonList(120), KindJSON},
		// Over probeLen, which is every JSON tool result worth compressing.
		// KindProse is not a bug being pinned: both call sites in
		// internal/engine treat KindJSON exactly as KindProse (squeezeText
		// declines both, distillText falls past its switch to Pass 4), and
		// distillText has already offered the body to DistillJSON before
		// Classify runs. The cost is what this case measures — json.Valid is
		// skipped, so every test, crash, diagnostic and log counter sweeps a
		// document none of them will ever match.
		{"json_over_probe", jsonList(4000), KindProse},
	}
	for _, tc := range cases {
		// A benchmark that silently changed branch would keep reporting a
		// number while measuring something else, so pin the branch first.
		if got := Classify(tc.in); got != tc.want {
			b.Fatalf("%s: fixture classifies as %v, want %v — the benchmark would measure the wrong branch",
				tc.name, got, tc.want)
		}
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchSink = Classify(tc.in)
			}
		})
	}
}

// BenchmarkClassifyScaling pins the sampling cap. Prose is the worst case —
// every branch is evaluated and every one declines — and the three sizes
// straddle maxWindows*windowLen (96KB): 8KB scans the whole blob, while 270KB
// and 2700KB both scan three 32KB windows and should therefore cost the same.
// A 2700KB row that tracks its size instead of the cap means a full scan crept
// back in somewhere.
func BenchmarkClassifyScaling(b *testing.B) {
	for _, size := range []int{8 * kb, 270 * kb, 2700 * kb} {
		in := repeatTo(proseParagraph, size)
		name := strconv.Itoa(size/kb) + "KB"
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchSink = Classify(in)
			}
		})
	}
}
