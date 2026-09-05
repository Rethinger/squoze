package distill

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDistillJSON_TabularLifting(t *testing.T) {
	// Homogeneous array of 10 objects
	type Record struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		Status    string `json:"status"`
		Cluster   string `json:"cluster"`
		TraceID   string `json:"trace_id"`
		ExtraNull *int   `json:"extra_null"`
	}

	var items []Record
	for i := 1; i <= 10; i++ {
		items = append(items, Record{
			ID:        1000 + i,
			Name:      "service-worker-" + string(rune('a'+i)),
			Status:    "RUNNING",
			Cluster:   "us-east-prod-1",
			TraceID:   "tr-987214981729487192847",
			ExtraNull: nil,
		})
	}

	rawBytes, _ := json.MarshalIndent(items, "", "  ")
	rawStr := string(rawBytes)

	distilled, changed := DistillJSON(rawStr)
	if !changed {
		t.Fatal("expected DistillJSON to report changed=true")
	}

	// 1. Must be lifted into a Markdown table. Matched on the prefix, not the
	// whole headline: the headline is an open-ended list of facts about the
	// table (envelope fields, hoisted constants, truncation counts), so pinning
	// it verbatim fails on every addition without any of them being wrong.
	if !strings.Contains(distilled, "[... squoze table: 10 rows") {
		t.Fatalf("expected squoze table marker, got:\n%s", distilled)
	}
	if !strings.Contains(distilled, "| id |") || !strings.Contains(distilled, "| name |") {
		t.Fatalf("table headers missing:\n%s", distilled)
	}

	// status and cluster hold one value across all 10 rows, so they belong in
	// the headline rather than in 10 identical cells.
	for _, want := range []string{"all rows: status=RUNNING", "all rows: cluster=us-east-prod-1"} {
		if !strings.Contains(distilled, want) {
			t.Errorf("constant column not hoisted (%s):\n%s", want, distilled)
		}
	}

	// 2. Metadata trace_id must be stripped
	if strings.Contains(distilled, "tr-987214981729487192847") {
		t.Fatal("trace_id was not stripped from table")
	}

	// 3. Significant token savings (>50%)
	savingsPct := float64(len(rawStr)-len(distilled)) / float64(len(rawStr)) * 100.0
	if savingsPct < 40.0 {
		t.Fatalf("insufficient token savings: only %.1f%% (raw=%d, distilled=%d)",
			savingsPct, len(rawStr), len(distilled))
	}
}

func TestDistillJSON_Pruning(t *testing.T) {
	rawJSON := `{
		"model": "gpt-4",
		"trace_id": "abc-123-xyz",
		"request_id": "req-999",
		"payload": {
			"valid_field": "hello world",
			"empty_object": {},
			"null_field": null,
			"nested_list": ["item1", null, "item2"]
		}
	}`

	distilled, changed := DistillJSON(rawJSON)
	if !changed {
		t.Fatal("expected changed=true for JSON pruning")
	}

	if strings.Contains(distilled, "null_field") {
		t.Fatal("null_field was not pruned")
	}
	if strings.Contains(distilled, "trace_id") || strings.Contains(distilled, "request_id") {
		t.Fatal("metadata fields were not pruned")
	}
	if !strings.Contains(distilled, "valid_field") || !strings.Contains(distilled, "item1") {
		t.Fatal("valid data fields were lost")
	}
}

func TestDistillJSON_InvalidJSONFailOpen(t *testing.T) {
	invalid := "{not a valid json at all..."
	distilled, changed := DistillJSON(invalid)
	if changed || distilled != invalid {
		t.Fatal("invalid JSON did not fail-open")
	}
}
