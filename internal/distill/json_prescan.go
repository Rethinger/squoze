package distill

import (
	"strings"

	"github.com/tidwall/gjson"
)

// The pre-scan in this file exists because DistillJSON is at its most expensive
// exactly when it achieves nothing. json.Unmarshal into `any` builds one map
// entry per field, so a tool result of 2 000 rows x 10 fields costs milliseconds
// of allocation before the transforms even get to look at it — and when neither
// tabular lifting nor structural pruning applies, every one of those
// milliseconds is thrown away. Measured on golang:1.22, 2026-09-05, on a
// payload whose array sits under a key the wrapper allowlist did not know:
// 141 193 B -> 4.152 ms, 565 230 B -> 16.206 ms, both returning ok=false, while
// no other distill pass on the same input exceeded 0.6 ms.
//
// The rule that keeps this safe: the pre-scan may only reject input that
// DistillJSON provably cannot change. It is a NECESSARY condition, never a
// sufficient one. A false positive costs exactly the unmarshal we already pay
// today; a false negative would silently drop savings and break the promise
// that output is byte-identical with the gate and without it.

// FindLiftableArray locates the array a Markdown table could be lifted from,
// without unmarshalling anything: gjson walks raw bytes and skips over values it
// was not asked about, so the cost tracks the document's shape rather than its
// field count.
//
// It returns the key path to the array, deepest key last; an empty path with ok
// means the document root is itself the array. Depth stops at 2 because the path
// becomes a label in the table headline, and a deeper one stops being a useful
// label.
//
// Two conditions have to hold. The array must look like a table — three or more
// elements, the first an object; verifying that every element really is a
// homogeneous object stays tryTabularLifting's job, so this test is deliberately
// the looser superset. And the lift must be lossless: see liftIsLossless.
//
// v0.3.0 searched four hard-coded wrapper names (items, data, results, records)
// and gave up on anything else, so a provider that named its list rows or files
// paid the full parse and got nothing back. The search is structural now; the old
// names still lift, and TestWrapperNamesStillLift holds that line.
func FindLiftableArray(raw string) ([]string, bool) {
	root := gjson.Parse(raw)
	if root.IsArray() {
		return nil, isObjectArray(root)
	}
	if !root.IsObject() {
		return nil, false
	}

	var found []string
	root.ForEach(func(k, v gjson.Result) bool {
		if v.IsArray() && isObjectArray(v) {
			found = []string{k.String()}
			return false
		}
		return true
	})

	if found == nil {
		root.ForEach(func(k1, v1 gjson.Result) bool {
			if !v1.IsObject() {
				return true
			}
			v1.ForEach(func(k2, v2 gjson.Result) bool {
				if v2.IsArray() && isObjectArray(v2) {
					found = []string{k1.String(), k2.String()}
					return false
				}
				return true
			})
			return found == nil
		})
	}

	if found == nil || !liftIsLossless(root, found) {
		return nil, false
	}
	return found, true
}

// liftIsLossless reports whether lifting the array at keys would leave the rest
// of the document accounted for.
//
// A lifted table renders the array and nothing else. Everything beside it
// survives only if the headline prints it, and the headline prints at most eight
// scalar fields from the object immediately around the array. So a response
// shaped {"data": [800 rows], "meta": {...}} lifts to a table with meta simply
// gone — no marker, no note, a whole object erased from what the model sees. That
// is the kind of silent loss the distiller must never trade for bytes: the model
// cannot ask about what it cannot see.
//
// The rule, then: refuse the lift unless every sibling is either printed in the
// headline (a scalar, and within its eight-field budget) or provably worthless —
// null, an empty collection, or one of the metadata keys pruning drops anyway.
// A nested array gets the stricter treatment: its headline reports the scalars
// next to the array, so anything the root carries beside the parent object would
// vanish without a trace, and the root has to hold nothing else at all.
//
// Declining is cheap. The document falls through to structural pruning, which
// preserves everything, and the worst case is savings we did not take — never
// information the caller silently lost.
func liftIsLossless(root gjson.Result, keys []string) bool {
	switch len(keys) {
	case 0:
		return true // the root is the array; there is no sibling to lose
	case 1:
		return siblingsAccountedFor(root, keys[0], headlineFieldBudget)
	default:
		if !siblingsAccountedFor(root, keys[0], 0) {
			return false
		}
		return siblingsAccountedFor(root.Get(gjsonPath(keys[:1])), keys[1], headlineFieldBudget)
	}
}

// headlineFieldBudget mirrors the cap scalarSiblings applies while building the
// headline. A ninth scalar is dropped there, so a ninth scalar means the lift is
// not lossless.
const headlineFieldBudget = 8

// siblingsAccountedFor reports whether every key of parent other than arrayKey
// survives a lift — as a headline field within budget, or as a value pruning
// discards anyway.
func siblingsAccountedFor(parent gjson.Result, arrayKey string, scalarBudget int) bool {
	if !parent.IsObject() {
		return false
	}
	scalars := 0
	ok := true
	parent.ForEach(func(k, v gjson.Result) bool {
		if k.String() == arrayKey || isMetadataKey(k.String()) {
			return true
		}
		switch v.Type {
		case gjson.String, gjson.Number, gjson.True, gjson.False:
			scalars++
			if scalars > scalarBudget {
				ok = false
			}
		case gjson.Null:
			// pruneValue drops nulls, so losing one loses nothing
		default: // object or array
			if !isEmptyContainer(v.Raw) {
				ok = false
			}
		}
		return ok
	})
	return ok
}

// isEmptyContainer reports whether raw is {} or [], whitespace allowed. Such a
// value is dropped by pruning, so a lift that drops it too loses nothing.
func isEmptyContainer(raw string) bool {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return false
	}
	switch raw[0] {
	case '{':
		return closesEmpty(raw, 0, '}')
	case '[':
		return closesEmpty(raw, 0, ']')
	}
	return false
}

// isObjectArray reports whether a gjson array holds at least three elements and
// opens with an object. It stops counting at three: a table needs three rows to
// be worth drawing, and walking an 800-element array to learn "more than three"
// is exactly the wasted work this file is about.
func isObjectArray(arr gjson.Result) bool {
	n := 0
	enough := false
	arr.ForEach(func(_, v gjson.Result) bool {
		if n == 0 && !v.IsObject() {
			return false
		}
		n++
		if n >= 3 {
			enough = true
			return false
		}
		return true
	})
	return enough
}

// hasPrunableFeature reports whether structural pruning could possibly change
// the document — the second way DistillJSON can succeed, and the reason the gate
// cannot simply ask "is there a liftable array".
//
// pruneValue sets changed only for a null value, a metadata key, or a collection
// that ends up empty. All three leave a mark in the raw bytes: the token null,
// one of the six metadata names, or an empty {} / [] pair. One pass over the
// bytes looks for those marks. Matching is case-insensitive because
// isMetadataKey lower-cases before comparing, so ETag counts.
//
// Deliberate over-reach: the token null inside a string value, or the word etag
// inside prose, both return true here. That is a false positive, which costs one
// unmarshal and no correctness. The one shape this cannot see is a metadata key
// written with escaped ASCII (\u005f_links), which no encoder in practice emits;
// the \u00 check covers it rather than leaving a hole in the contract.
func hasPrunableFeature(raw string) bool {
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case 'n', 'N':
			if hasPrefixFold(raw[i:], "null") {
				return true
			}
		case 'e', 'E':
			if hasPrefixFold(raw[i:], "etag") {
				return true
			}
		case 't', 'T':
			if hasPrefixFold(raw[i:], "trace_id") {
				return true
			}
		case 'r', 'R':
			if hasPrefixFold(raw[i:], "request_id") {
				return true
			}
		case 's', 'S':
			if hasPrefixFold(raw[i:], "schema_version") {
				return true
			}
		case '_':
			if hasPrefixFold(raw[i:], "_links") || hasPrefixFold(raw[i:], "__typename") {
				return true
			}
		case '{':
			if closesEmpty(raw, i, '}') {
				return true
			}
		case '[':
			if closesEmpty(raw, i, ']') {
				return true
			}
		case '\\':
			if strings.HasPrefix(raw[i:], "\\u00") {
				return true
			}
		}
	}
	return false
}

// CanDistillJSON reports whether DistillJSON could change s at all. It answers
// with raw-byte scans only, so a caller can decide before paying for
// json.Unmarshal. Cheap test first: the array search is a structural skip-scan,
// while the prunable-feature scan reads every byte.
func CanDistillJSON(trimmed string) bool {
	if _, ok := FindLiftableArray(trimmed); ok {
		return true
	}
	return hasPrunableFeature(trimmed)
}

// hasPrefixFold is strings.HasPrefix with ASCII case folding. lit must be lower
// case. Written out rather than calling strings.EqualFold on a slice so that no
// bounds arithmetic happens per candidate byte in the hot loop above.
func hasPrefixFold(s, lit string) bool {
	if len(s) < len(lit) {
		return false
	}
	for i := 0; i < len(lit); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lit[i] {
			return false
		}
	}
	return true
}

// closesEmpty reports whether the bracket at i is immediately closed, allowing
// the insignificant whitespace a pretty-printer inserts.
func closesEmpty(s string, i int, closer byte) bool {
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case ' ', '\t', '\n', '\r':
		case closer:
			return true
		default:
			return false
		}
	}
	return false
}

// gjsonPath renders a key path for gjson.Get. The empty path addresses the
// document root, which gjson spells @this.
func gjsonPath(keys []string) string {
	if len(keys) == 0 {
		return "@this"
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = escapeKey(k)
	}
	return strings.Join(parts, ".")
}

// arrayLabel renders a key path for humans — the headline prints it, so it
// carries no gjson escaping. The root array has no label.
func arrayLabel(keys []string) string {
	return strings.Join(keys, ".")
}

// escapeKey makes an object key safe inside a gjson path. gjson reads . * ? # @
// | and backslash as syntax, so a key containing one has to arrive escaped or
// the path quietly resolves to something else — or to nothing.
func escapeKey(k string) string {
	if !strings.ContainsAny(k, ".*?#@|\\") {
		return k
	}
	var b strings.Builder
	b.Grow(len(k) + 8)
	for i := 0; i < len(k); i++ {
		switch k[i] {
		case '.', '*', '?', '#', '@', '|', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(k[i])
	}
	return b.String()
}

// arrayAt walks an unmarshalled document to the array FindLiftableArray located.
func arrayAt(v any, keys []string) ([]any, bool) {
	cur := v
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	arr, ok := cur.([]any)
	return arr, ok
}
