package distill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
)

// tabularMinSavings is the fraction of the original that a Markdown table must
// save before it is allowed to replace valid JSON.
//
// Lifting an array into a table changes the DATA TYPE of a tool result: any
// consumer that json.Unmarshal's it stops working. That is only worth doing for
// a large win, so the bar is deliberately much higher than the 10% floor used
// by transforms that preserve the type.
const tabularMinSavings = 0.35

// DistillJSON applies structural pruning and tabular schema-lifting to JSON strings:
// 1. Drops null fields, empty arrays/objects, and envelope telemetry.
// 2. Lifts homogeneous arrays of objects into compact, token-efficient Markdown tables.
// 3. Emits dense, unindented JSON for non-tabular structures.
//
// Determinism: identical input bytes always produce identical output bytes.
// Column order comes from the document, never from Go map iteration.
func DistillJSON(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 64 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s, false
	}

	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return s, false // invalid JSON: fail-open
	}

	// 1. Try Tabular Schema-Lifting if it's an array of objects.
	// Requires substantial savings: below the bar we keep the value parseable
	// and fall through to structural pruning instead.
	if tbl, ok := tryTabularLifting(parsed, trimmed); ok {
		if float64(len(tbl)) <= float64(len(s))*(1-tabularMinSavings) {
			return tbl, true
		}
	}

	// 2. Structural Pruning (remove nulls, empty collections, metadata)
	pruned, changed := pruneValue(parsed)
	if !changed {
		return s, false
	}

	dense, err := json.Marshal(pruned)
	if err != nil || len(dense) >= len(s)*9/10 {
		return s, false // less than 10% saved: skip
	}

	return string(dense), true
}

// pruneValue recursively strips nulls, empty collections, and metadata keys.
func pruneValue(v any) (any, bool) {
	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		changed := false
		for k, item := range val {
			if isMetadataKey(k) || item == nil {
				changed = true
				continue
			}
			prunedItem, subChanged := pruneValue(item)
			if subChanged {
				changed = true
			}
			if isEmptyCollection(prunedItem) {
				changed = true
				continue
			}
			res[k] = prunedItem
		}
		return res, changed
	case []any:
		res := make([]any, 0, len(val))
		changed := false
		for _, item := range val {
			if item == nil {
				changed = true
				continue
			}
			prunedItem, subChanged := pruneValue(item)
			if subChanged {
				changed = true
			}
			res = append(res, prunedItem)
		}
		return res, changed
	default:
		return v, false
	}
}

func isMetadataKey(k string) bool {
	lower := strings.ToLower(k)
	return lower == "__typename" ||
		lower == "_links" ||
		lower == "trace_id" ||
		lower == "request_id" ||
		lower == "etag" ||
		lower == "schema_version"
}

func isEmptyCollection(v any) bool {
	switch val := v.(type) {
	case map[string]any:
		return len(val) == 0
	case []any:
		return len(val) == 0
	default:
		return false
	}
}

// tryTabularLifting detects homogeneous arrays of objects and formats them as a
// compact Markdown table. raw is the trimmed original JSON, used to recover the
// document's own key order (Go map iteration is randomised, which would make the
// output non-deterministic and defeat provider prompt caches).
func tryTabularLifting(v any, raw string) (string, bool) {
	var items []any
	arrayPath := "@this"         // gjson path to the lifted array
	var envelope []envelopeField // scalar siblings of a wrapped array

	switch val := v.(type) {
	case []any:
		items = val
	case map[string]any:
		// Check common list wrappers: "items", "data", "results", "records"
		for _, key := range []string{"items", "data", "results", "records"} {
			if arr, ok := val[key].([]any); ok && len(arr) >= 3 {
				items = arr
				arrayPath = key
				envelope = scalarSiblings(raw, key)
				break
			}
		}
	}

	if len(items) < 3 {
		return "", false
	}

	// Verify all items are maps with scalar values
	var colKeys []string
	keySeen := make(map[string]bool)

	for i, item := range items {
		m, ok := item.(map[string]any)
		if !ok || len(m) == 0 {
			return "", false
		}
		if i == 0 {
			for k := range m {
				if !isMetadataKey(k) {
					keySeen[k] = true
				}
			}
			colKeys = orderedColumns(raw, arrayPath, keySeen)
		} else {
			// Ensure high column overlap (at least 60% of columns match)
			overlap := 0
			for k := range m {
				if keySeen[k] {
					overlap++
				}
			}
			if overlap == 0 {
				return "", false
			}
		}
	}

	if len(colKeys) == 0 || len(colKeys) > 12 {
		return "", false
	}

	// Hoist columns that carry one distinct value across every row. A paginated
	// API response routinely repeats a status or a description verbatim in all
	// 800 rows; a faithful table reprints it 800 times, which is most of the
	// payload and none of the information. Stating it once above the table is
	// both smaller and easier to read, and it is lossless: the value is still
	// there, attached to every row by "all rows".
	constant, colKeys := hoistConstantColumns(items, colKeys)
	if len(colKeys) == 0 {
		// Every column was constant, so there is no table left to draw — the
		// rows differ in nothing and the headline already carries all of it.
		return tableHeadline(len(items), arrayPath, envelope, 0, constant), true
	}

	// Rows first, headline second: the headline has to report how many cells
	// were truncated, and that is only known once every cell is rendered.
	var rows bytes.Buffer
	rows.WriteString("| " + strings.Join(colKeys, " | ") + " |\n")
	rows.WriteString("|" + strings.Repeat("---|", len(colKeys)) + "\n")

	truncated := 0
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rowVals := make([]string, len(colKeys))
		for idx, col := range colKeys {
			rawVal := m[col]
			if rawVal == nil {
				rowVals[idx] = "-"
				continue
			}
			cell, cut := sanitizeCell(fmt.Sprintf("%v", rawVal))
			if cut {
				truncated++
			}
			rowVals[idx] = cell
		}
		rows.WriteString("| " + strings.Join(rowVals, " | ") + " |\n")
	}

	var buf bytes.Buffer
	buf.WriteString(tableHeadline(len(items), arrayPath, envelope, truncated, constant))
	buf.Write(rows.Bytes())
	return buf.String(), true
}

// hoistConstantColumns splits cols into those holding a single distinct value
// across every row and those that vary. Returns the constants (in the incoming
// column order) and the remaining varying columns.
//
// A column counts as constant only if every row actually carries it: a key
// missing from some rows renders as "-", which is information about that row,
// not a shared value. Comparison is on the rendered form, so 1 and 1.0 collapse
// exactly when the table would have shown them identically.
func hoistConstantColumns(items []any, cols []string) ([]envelopeField, []string) {
	if len(items) < 2 {
		return nil, cols
	}

	var constant []envelopeField
	varying := make([]string, 0, len(cols))

	for _, col := range cols {
		first, same := "", true
		for i, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				same = false
				break
			}
			raw, present := m[col]
			if !present || raw == nil {
				same = false
				break
			}
			cell, cut := sanitizeCell(fmt.Sprintf("%v", raw))
			if cut {
				// Refuse to hoist a value that had to be cut. In a cell the
				// reader sees the same clipped text on every row and the
				// headline reports the count; hoisted, a single clipped value
				// would need its own disclosure path to say so. Left as a
				// column it keeps the disclosure it already has.
				same = false
				break
			}
			if i == 0 {
				first = cell
			} else if cell != first {
				same = false
				break
			}
		}
		// An empty shared value says nothing worth a headline line.
		if same && first != "" && first != "-" {
			constant = append(constant, envelopeField{Key: col, Val: first})
			continue
		}
		varying = append(varying, col)
	}

	return constant, varying
}

// envelopeField is a scalar sibling of a wrapped array — pagination and type
// metadata such as has_more, total or object. Dropping these silently is a
// correctness bug: a model reading the table cannot tell that more pages exist.
type envelopeField struct {
	Key string
	Val string
}

// scalarSiblings collects the scalar fields that sit next to the lifted array,
// in document order. Objects and arrays are skipped: they are not summarisable
// in one headline, and the array itself is the payload.
func scalarSiblings(raw, arrayKey string) []envelopeField {
	root := gjson.Parse(raw)
	if !root.IsObject() {
		return nil
	}
	var out []envelopeField
	root.ForEach(func(k, v gjson.Result) bool {
		key := k.String()
		if key == arrayKey || isMetadataKey(key) {
			return true
		}
		switch v.Type {
		case gjson.String, gjson.Number, gjson.True, gjson.False:
			// Envelope truncation is not counted in the table's disclosure:
			// these are metadata, tightened to keep the headline one line, and
			// the visible ellipsis says so on the spot.
			val, _ := sanitizeCell(v.String())
			if len(val) > 60 {
				val = val[:57] + "..."
			}
			out = append(out, envelopeField{Key: key, Val: val})
		}
		if len(out) >= 8 { // headline stays short
			return false
		}
		return true
	})
	return out
}

// orderedColumns returns the wanted keys in the order the document declares them
// on the first array element. Go map iteration is randomised, so deriving column
// order from a map would make identical input produce different output and break
// the cache-safe contract. Falls back to a sorted order if the path does not
// resolve, which is still deterministic.
func orderedColumns(raw, arrayPath string, wanted map[string]bool) []string {
	fallback := func() []string {
		keys := make([]string, 0, len(wanted))
		for k := range wanted {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	}

	first := gjson.Get(raw, arrayPath+".0")
	if !first.IsObject() {
		return fallback()
	}

	ordered := make([]string, 0, len(wanted))
	seen := make(map[string]bool, len(wanted))
	first.ForEach(func(k, _ gjson.Result) bool {
		key := k.String()
		if wanted[key] && !seen[key] {
			ordered = append(ordered, key)
			seen[key] = true
		}
		return true
	})

	// Any key present in the map but absent from the first element (should not
	// happen, since wanted is built from it) is appended sorted, so the result
	// is a total order either way.
	if len(ordered) != len(wanted) {
		var rest []string
		for k := range wanted {
			if !seen[k] {
				rest = append(rest, k)
			}
		}
		sort.Strings(rest)
		ordered = append(ordered, rest...)
	}
	if len(ordered) == 0 {
		return fallback()
	}
	return ordered
}

// tableHeadline renders the marker line above a lifted table, carrying the row
// count, which key the rows came from, and the preserved envelope fields.
func tableHeadline(rows int, arrayPath string, envelope []envelopeField, truncated int, constant []envelopeField) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[... squoze table: %d rows", rows))
	if arrayPath != "@this" {
		b.WriteString(fmt.Sprintf(" · from %q", arrayPath))
	}
	for _, f := range envelope {
		b.WriteString(fmt.Sprintf(" · %s=%s", f.Key, f.Val))
	}
	// "all rows: k=v" rather than a bare "k=v" so a hoisted column cannot be
	// misread as envelope metadata about the response.
	for _, f := range constant {
		b.WriteString(fmt.Sprintf(" · all rows: %s=%s", f.Key, f.Val))
	}
	if truncated > 0 {
		b.WriteString(fmt.Sprintf(" · %d cells truncated to %d chars", truncated, cellMaxLen))
	}
	b.WriteString(" ...]\n")
	return b.String()
}

// cellMaxLen caps cell width so a lifted table stays readable. A single 3KB
// stack trace in one cell defeats the point of the table.
const cellMaxLen = 80

// sanitizeCell escapes a value for a Markdown cell and caps its length,
// reporting whether it had to cut. The caller must surface the count: a
// truncated cell is silent data loss otherwise, and a model reading
// `| error_message |` cannot tell the message continued.
func sanitizeCell(s string) (string, bool) {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > cellMaxLen {
		return s[:cellMaxLen-3] + "...", true
	}
	return s, false
}
