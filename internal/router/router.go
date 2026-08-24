// Package router classifies text blobs so the engine can decide WHO gets
// squeezed and who must pass through untouched.
//
// Quality contract: only provably-safe kinds are compressed today — machine
// output (tests, logs). Prose, code and JSON blobs are routed but NOT mutated:
// naive truncation of JSON breaks validity, and prose/code carry signal that
// elision destroys. Structural JSON pruning is future work (A-series).
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
	logLevelHits  = []string{"INFO", "WARN", "ERROR", "DEBUG", "TRACE", "FATAL"}
	logPrefixHits = []string{"[", "level=", "lvl="}
)

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
	for _, line := range strings.Split(s, "\n") {
		if len(line) > 20 && (line[4] == '-' && line[7] == '-' || // 2026-08-24
			line[2] == ':' && line[5] == ':') { // 12:34:56
			n++
		}
		if n >= 3 {
			break
		}
	}
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

	if trimmed != "" && (trimmed[0] == '{' || trimmed[0] == '[') {
		if json.Valid([]byte(trimmed)) {
			return KindJSON
		}
	}

	testScore := countSampled(s, testHits)
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
