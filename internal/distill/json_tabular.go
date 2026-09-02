package distill

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// DistillJSON applies structural pruning and tabular schema-lifting to JSON strings:
// 1. Drops null fields, empty arrays/objects, and envelope telemetry.
// 2. Lifts homogeneous arrays of objects into compact, token-efficient Markdown tables.
// 3. Emits dense, unindented JSON for non-tabular structures.
func DistillJSON(s string) (string, bool) {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 64 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return s, false
	}

	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return s, false // invalid JSON: fail-open
	}

	// 1. Try Tabular Schema-Lifting if it's an array of objects
	if tbl, ok := tryTabularLifting(parsed); ok {
		if len(tbl) < len(s)*9/10 {
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

// tryTabularLifting detects homogeneous arrays of objects and formats them as a compact Markdown table.
func tryTabularLifting(v any) (string, bool) {
	var items []any
	switch val := v.(type) {
	case []any:
		items = val
	case map[string]any:
		// Check common list wrappers: "items", "data", "results", "records"
		for _, key := range []string{"items", "data", "results", "records"} {
			if arr, ok := val[key].([]any); ok && len(arr) >= 3 {
				items = arr
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
					colKeys = append(colKeys, k)
					keySeen[k] = true
				}
			}
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

	// Render compact Markdown Table
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("[... squoze table: %d rows ...]\n", len(items)))

	// Header row
	buf.WriteString("| " + strings.Join(colKeys, " | ") + " |\n")
	buf.WriteString("|" + strings.Repeat("---|", len(colKeys)) + "\n")

	// Data rows
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
			} else {
				rowVals[idx] = sanitizeCell(fmt.Sprintf("%v", rawVal))
			}
		}
		buf.WriteString("| " + strings.Join(rowVals, " | ") + " |\n")
	}

	return buf.String(), true
}

func sanitizeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
