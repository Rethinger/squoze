// Package router classifies text blobs so the engine can decide WHO gets
// squeezed and who must pass through untouched.
//
// Quality contract: only provably-safe kinds are compressed today — machine
// output (tests, logs). Prose, code and JSON blobs are routed but NOT mutated
// by the head/tail squeezer: naive truncation of JSON breaks validity, and
// prose/code carry signal that elision destroys. JSON has its own structural
// path (tabular lifting), which rewrites rather than truncates.
//
// KindCode is detected by a column-0 opener plus marker density, and the
// engine returns it untouched before terminal hygiene runs — sanitizing a
// source file would rewrite bytes the caller is about to compile.
package router

import (
	"encoding/json"
	"strings"
)

// Kind classifies a text blob found inside a message.
type Kind int

const (
	KindUnknown    Kind = iota
	KindProse           // human-written text: never touch
	KindCode            // source code: never touch
	KindJSON            // structured data: never naive-truncate
	KindTestOutput      // go test / pytest / vitest style output
	KindLogOutput       // timestamped/leveled log lines
)

func (k Kind) String() string {
	switch k {
	case KindProse:
		return "prose"
	case KindCode:
		return "code"
	case KindJSON:
		return "json"
	case KindTestOutput:
		return "test_output"
	case KindLogOutput:
		return "log_output"
	default:
		return "unknown"
	}
}

const probeLen = 8192 // classify on a prefix; blobs are big or they skip compression anyway

var (
	testHits = []string{
		"--- FAIL", "--- PASS", "--- SKIP",
		"=== RUN", "=== CONT", "=== PAUSE",
		"go test", "testing:",
		"pytest", "PASSED", "FAILED",
		"vitest", "jest", "✓ ", "✗ ",
		"assert ", "AssertionError", "unittest",
	}
	// crashHits are line-anchored markers of a crashed or summarised run.
	// testHits alone underweights the case that matters most: a `go test` panic
	// is mostly stack frames, so it scores 2 (`=== RUN`, `--- FAIL`) against a
	// threshold of 3 and routes to prose — the single most compressible blob a
	// coding agent ever pastes, never compressed. These are anchored rather
	// than counted as substrings because "panic:" and "FAIL" appear inside
	// ordinary sentences, but never at the start of one in machine output.
	crashHits = []string{
		"panic:", "goroutine ", "exit status ", "signal: ",
		"FAIL", "ok  ", "PASS", "--- FAIL", "--- PASS", "--- SKIP",
		"Traceback (most recent call last):", "E   ", "OK (",
		"Caused by:", "at java.", "Error: ", "AssertionError",
	}
	logLevelHits  = []string{"INFO", "WARN", "ERROR", "DEBUG", "TRACE", "FATAL"}
	logPrefixHits = []string{"[", "level=", "lvl="}

	// codeOpeners are line-start tokens that open a source file or a source
	// snippet. Machine output never begins with one: `go test` opens with
	// `=== RUN`, a crash with `panic:` or `goroutine `, a log with a timestamp
	// or a level. That asymmetry is what makes the opener a safe anchor — it
	// lets us claim "this is code, keep every byte" without stealing real test
	// output, which is the only kind we are allowed to elide.
	codeOpeners = []string{
		"package ", "import ", "from ", "func ", "type ", "const ", "var ",
		"def ", "async def ", "class ", "struct ", "impl ", "trait ",
		"enum ", "interface ", "namespace ", "using ", "module ", "export ",
		"fn ", "pub ", "#include", "#!", "@interface", "<?php",
	}

	// codeMarkers confirm density once an opener matched: one stray `func ` in
	// a paragraph is prose, twenty of them are a file.
	codeMarkers = []string{
		"func ", "def ", "class ", "type ", "const ", "var ", "let ",
		"import ", "from ", "return ", "fn ", "pub ", "impl ", "struct ",
		"public ", "private ", "static ", "}",
	}

	// commentOpeners may precede the real opener: license headers, package
	// doc comments, shebang notes.
	commentOpeners = []string{"//", "/*", "*", "#", "--", ";"}
)

// codeHeaderLines is how deep we look for an opener before giving up. A file
// can carry a long license header, but not an unbounded one.
const codeHeaderLines = 12

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// hasCodeOpener reports whether one of the first codeHeaderLines non-blank,
// non-comment lines opens a source file.
//
// The opener must sit at column 0. Top-level declarations always do; the
// source that pytest echoes underneath a failure header is always indented:
//
//	_____________________ test_thing _____________________
//	    def test_thing():
//	>       assert add(1, 2) == 4
//
// Without the column-0 rule that `def ` reads as an opener and we hand back
// real test output as untouchable code. Losing an indented snippet to prose
// costs savings; stealing test output costs the feature.
func hasCodeOpener(s string) bool {
	seen, found := 0, false
	forEachLine(s, func(line string) bool {
		if hasAnyPrefix(line, codeOpeners) {
			found = true
			return false
		}
		t := strings.TrimSpace(line)
		if t == "" || hasAnyPrefix(t, commentOpeners) {
			return true
		}
		seen++
		return seen < codeHeaderLines
	})
	return found
}

// looksLikeDiagnostic reports whether a line opens with the compiler/linter
// diagnostic shape `path:line:` or `path:line:col:` — the format Go, clang,
// gcc, tsc, eslint and rustc all emit. A broken build is mostly these lines
// and almost nothing else, so without them a 60-error build log scores 1 and
// routes to prose.
//
// Timestamped lines are excluded up front: in `2026-08-24T10:00:00Z INFO ...`
// the first colon is the clock's, and `2026-08-24T10` reads as a path — the
// whole line would count as a diagnostic and steal the log branch.
func looksLikeDiagnostic(line string) bool {
	if looksTimePrefixed(line) {
		return false
	}
	i := strings.IndexByte(line, ':')
	if i <= 0 || i == len(line)-1 {
		return false
	}
	allDigits := true
	for j := 0; j < i; j++ {
		if line[j] < '0' || line[j] > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return false
	}
	rest := line[i+1:]
	digits := 0
	for digits < len(rest) && rest[digits] >= '0' && rest[digits] <= '9' {
		digits++
	}
	if digits == 0 || digits == len(rest) {
		return false
	}
	return rest[digits] == ':'
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// looksTimePrefixed reports whether a line opens with a date (2026-08-24) or a
// wall clock (10:00:00), covering ISO-8601 with or without the T separator.
func looksTimePrefixed(line string) bool {
	if len(line) >= 10 && isDigits(line[:4]) && line[4] == '-' &&
		isDigits(line[5:7]) && line[7] == '-' && isDigits(line[8:10]) {
		return true
	}
	if len(line) >= 8 && isDigits(line[:2]) && line[2] == ':' &&
		isDigits(line[3:5]) && line[5] == ':' && isDigits(line[6:8]) {
		return true
	}
	return false
}

// forEachLine walks the lines of s without allocating a slice for them.
// Classify runs on every tool block on the hot path, and strings.Split of a
// 32KB window allocates a slice of several hundred headers per call — three
// windows times three counters is a measurable amount of garbage for work that
// only ever reads.
func forEachLine(s string, fn func(line string) bool) {
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		var line string
		if i < 0 {
			line, s = s, ""
		} else {
			line, s = s[:i], s[i+1:]
		}
		if !fn(line) {
			return
		}
	}
}

func countDiagnosticLines(s string) int {
	n := 0
	for _, w := range sampleWindows(s) {
		forEachLine(w, func(line string) bool {
			if looksLikeDiagnostic(strings.TrimSpace(line)) {
				n++
			}
			return true
		})
	}
	return n
}

// countLineAnchored counts lines whose first token matches subs, across
// sampled windows. Anchoring to the line start is what separates a `func `
// declaration from the word "func" inside a sentence.
func countLineAnchored(s string, subs []string) int {
	n := 0
	for _, w := range sampleWindows(s) {
		forEachLine(w, func(line string) bool {
			if t := strings.TrimSpace(line); t != "" && hasAnyPrefix(t, subs) {
				n++
			}
			return true
		})
	}
	return n
}

func countAny(s string, subs []string) int {
	n := 0
	for _, sub := range subs {
		n += strings.Count(s, sub)
	}
	return n
}

// looksLikeTimestamped checks for common log line prefixes: ISO dates,
// unix seconds, or HH:MM:SS clocks at line starts.
func looksLikeTimestamped(s string) int {
	n := 0
	forEachLine(s, func(line string) bool {
		if len(line) > 20 && (line[4] == '-' && line[7] == '-' || // 2026-08-24
			line[2] == ':' && line[5] == ':') { // 12:34:56
			n++
		}
		return n < 3
	})
	return n
}

const (
	windowLen  = 32 * 1024 // per-window scan budget
	maxWindows = 3         // head / middle / tail
)

// sampleWindows returns up to three slices of s for counting: the head, the
// middle and the tail. Machine output scatters its markers (FAIL lines,
// levels) across the whole blob; a prefix-only probe misclassifies big
// outputs with sparse errors as prose.
func sampleWindows(s string) []string {
	if len(s) <= maxWindows*windowLen {
		return []string{s}
	}
	mid := len(s) / 2
	return []string{
		s[:windowLen],
		s[mid-windowLen/2 : mid+windowLen/2],
		s[len(s)-windowLen:],
	}
}

// countSampled counts substring hits across sampled windows.
func countSampled(s string, subs []string) int {
	n := 0
	for _, w := range sampleWindows(s) {
		n += countAny(w, subs)
	}
	return n
}

func countTimestampedLines(s string) int {
	n := 0
	for _, w := range sampleWindows(s) {
		n += looksLikeTimestamped(w)
	}
	return n
}

// Classify returns the kind of content in s using cheap heuristics over
// sampled windows. Deterministic by construction — same bytes, same kind.
func Classify(s string) Kind {
	head := s
	if len(head) > probeLen {
		head = head[:probeLen]
	}
	trimmed := strings.TrimSpace(head)

	// Only a body whose trimmed prefix closes with the bracket matching its
	// opener can possibly be valid JSON, so the closer check can never reject
	// an input json.Valid would have accepted — it only skips parses that were
	// going to fail. That is worth a guard because head is a *truncated*
	// prefix: any JSON tool result over probeLen is invalid by construction,
	// and parsing it cost a full 8KB scan plus the SyntaxError the failure
	// allocates and nobody reads.
	if trimmed != "" && (trimmed[0] == '{' || trimmed[0] == '[') {
		last := trimmed[len(trimmed)-1]
		if (trimmed[0] == '{' && last == '}') || (trimmed[0] == '[' && last == ']') {
			if json.Valid([]byte(trimmed)) {
				return KindJSON
			}
		}
	}

	// Code before test output, deliberately. Fixture generators and golden
	// files embed machine output verbatim — that is their job — so they carry
	// enough FAIL/RUN markers to pass the test gate and get elided. An opener
	// at column 0 plus marker density says "file", not "output".
	if hasCodeOpener(head) && countLineAnchored(s, codeMarkers) >= 2 {
		return KindCode
	}

	testScore := countSampled(s, testHits) + countLineAnchored(s, crashHits) +
		countDiagnosticLines(s)
	if testScore >= 3 {
		return KindTestOutput
	}

	logScore := countSampled(s, logLevelHits) + countTimestampedLines(s)
	timestamped := countTimestampedLines(s) >= 3
	hasPrefixMarks := countAny(head, logPrefixHits) > 0
	if logScore >= 3 && (hasPrefixMarks || timestamped) {
		return KindLogOutput
	}

	// Default: treat as prose (safest routing decision).
	return KindProse
}
