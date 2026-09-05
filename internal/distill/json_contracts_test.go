package distill

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

// wrappedList is the shape every paginated API returns: an envelope of scalars
// around the array. Findings (b) and (c) both live here.
func wrappedList(n int) string {
	type row struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Region  string `json:"region"`
		Owner   string `json:"owner"`
		Comment string `json:"comment"`
	}
	// Every column varies across rows. A column with one repeated value is
	// hoisted into the headline and leaves the table, which would silently
	// remove it from the header these tests assert on.
	statuses := []string{"RUNNING", "PENDING", "FAILED"}
	regions := []string{"us-east-prod-1", "eu-west-prod-2", "ap-south-prod-3"}
	owners := []string{"platform-team", "payments-team", "search-team"}

	rows := make([]row, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, row{
			ID:      1000 + i,
			Name:    "service-worker-" + strconv.Itoa(i),
			Status:  statuses[i%len(statuses)],
			Region:  regions[i%len(regions)],
			Owner:   owners[i%len(owners)],
			Comment: "steady state at check " + strconv.Itoa(i) + ", no action needed",
		})
	}
	env := map[string]any{
		"object":      "list",
		"has_more":    true,
		"total_count": n * 4,
		"next_cursor": "cur_8f21ab",
		"data":        rows,
	}
	b, _ := json.MarshalIndent(env, "", "  ")
	return string(b)
}

// TestJSONTabularDeterminism is finding (c): colKeys was built by ranging a Go
// map, so column order changed between identical calls. A body that differs
// run to run breaks the provider's prompt-cache prefix, which is the whole
// point of the cache-safe contract.
func TestJSONTabularDeterminism(t *testing.T) {
	in := wrappedList(80)
	first, changed := DistillJSON(in)
	if !changed {
		t.Fatal("expected tabular lifting")
	}
	for i := 0; i < 24; i++ {
		got, _ := DistillJSON(in)
		if got != first {
			t.Fatalf("run %d differs from run 0\nfirst header: %s\ngot header:   %s",
				i, headerOf(first), headerOf(got))
		}
	}
}

// TestJSONColumnsFollowDocumentOrder pins the ordering rule itself, not just
// its stability: alphabetical would be stable too, but reads worse than the
// order the producer chose.
func TestJSONColumnsFollowDocumentOrder(t *testing.T) {
	out, changed := DistillJSON(wrappedList(60))
	if !changed {
		t.Fatal("expected tabular lifting")
	}
	header := headerOf(out)
	want := []string{"id", "name", "status", "region", "owner", "comment"}
	at := -1
	for _, col := range want {
		i := strings.Index(header, "| "+col+" ")
		if i < 0 {
			t.Fatalf("column %q missing from header: %s", col, header)
		}
		if i < at {
			t.Fatalf("column %q out of document order in header: %s", col, header)
		}
		at = i
	}
}

// TestJSONEnvelopeIsPreserved is finding (b): lifting returned only the table,
// dropping has_more/next_cursor/total_count. A caller that reads "80 rows" and
// no cursor concludes it has the whole list, and stops paginating.
func TestJSONEnvelopeIsPreserved(t *testing.T) {
	out, changed := DistillJSON(wrappedList(80))
	if !changed {
		t.Fatal("expected tabular lifting")
	}
	head := headlineOf(out)
	for _, want := range []string{"has_more", "next_cursor", "total_count", `from "data"`} {
		if !strings.Contains(head, want) {
			t.Errorf("envelope field %q lost from headline: %s", want, head)
		}
	}
}

// TestConstantColumnsAreHoisted is the case the 35% gate exposed: a paginated
// response that repeats the same note in all 800 rows was lifted into a table
// that faithfully reprinted it 800 times, which did not clear the gate and so
// got no compression at all. Stating it once clears it comfortably.
func TestConstantColumnsAreHoisted(t *testing.T) {
	type row struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	const note = "reconciled against ledger batch 4471 with no discrepancy"
	rows := make([]row, 0, 200)
	for i := 0; i < 200; i++ {
		rows = append(rows, row{ID: i, Status: []string{"settled", "failed"}[i%2], Note: note})
	}
	b, _ := json.Marshal(map[string]any{"data": rows})

	out, changed := DistillJSON(string(b))
	if !changed {
		t.Fatal("declined to distil a 200-row table with a constant column")
	}
	if !strings.Contains(out, "all rows: note="+note) {
		t.Errorf("constant column not hoisted:\n%.300s", out)
	}
	if strings.Count(out, note) != 1 {
		t.Errorf("note appears %d times; hoisting should leave exactly one",
			strings.Count(out, note))
	}
	if strings.Contains(headerOf(out), "note") {
		t.Errorf("hoisted column still in the table header: %s", headerOf(out))
	}
	// status varies, so it must stay a column.
	if !strings.Contains(headerOf(out), "status") {
		t.Errorf("varying column was hoisted away: %s", headerOf(out))
	}
	if saved := 1 - float64(len(out))/float64(len(b)); saved < tabularMinSavings {
		t.Errorf("savings %.1f%% still under the %.0f%% gate", saved*100, tabularMinSavings*100)
	}
}

// TestLongConstantColumnIsNotHoisted keeps the disclosure story consistent: a
// constant value too long for a cell would be clipped, and a clipped value in
// the headline has no counter to report itself. It stays a column, where the
// existing truncation notice covers it.
func TestLongConstantColumnIsNotHoisted(t *testing.T) {
	type row struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Blob string `json:"blob"`
	}
	long := strings.Repeat("stack frame detail past the cell cap ", 4)
	rows := make([]row, 0, 20)
	for i := 0; i < 20; i++ {
		rows = append(rows, row{ID: i, Name: "row-" + strconv.Itoa(i), Blob: long})
	}
	b, _ := json.Marshal(map[string]any{"data": rows})

	out, changed := DistillJSON(string(b))
	if !changed {
		t.Skip("distillation declined; nothing to assert")
	}
	if strings.Contains(out, "all rows: blob=") {
		t.Error("hoisted a value that had to be truncated, with no disclosure")
	}
	if strings.Contains(out, "[... squoze table:") {
		if !strings.Contains(headlineOf(out), "cells truncated") {
			t.Errorf("truncation went undisclosed: %s", headlineOf(out))
		}
	}
}

// TestJSONTypeChangeNeedsRealSavings is the third part of finding (b): lifting
// turns valid JSON into prose-shaped Markdown, so a caller can no longer parse
// it. A 10% win did not justify that; pruning keeps the value valid JSON and
// is the better trade at the margin.
func TestJSONTypeChangeNeedsRealSavings(t *testing.T) {
	// Two wide free-text columns: the table keeps every byte of them, so
	// lifting saves only the repeated key names — a marginal win.
	type row struct {
		ID   int    `json:"id"`
		Long string `json:"long"`
		More string `json:"more"`
	}
	// Values must stay under the cell cap: a truncated cell saves bytes by
	// discarding data, which would make the fixture look like a big win and
	// leave the threshold untested. They must also differ per row, or constant-
	// column hoisting removes them from the table entirely — a genuinely large
	// win, but not the marginal case under test here.
	rows := make([]row, 0, 12)
	for i := 0; i < 12; i++ {
		rows = append(rows, row{
			ID:   i,
			Long: "narrative detail no " + strconv.Itoa(i) + " that compresses badly, under cap",
			More: "second wide free-text column no " + strconv.Itoa(i) + ", also under cap",
		})
	}
	// Compact, not MarshalIndent: indentation is pure padding that a table
	// always wins back, which would make any fixture look like a big win and
	// leave the threshold untested.
	b, _ := json.Marshal(map[string]any{"data": rows})
	out, changed := DistillJSON(string(b))
	if !changed {
		return // pruning declined too; nothing was claimed, nothing broken
	}
	if strings.Contains(out, "[... squoze table:") {
		saved := 1 - float64(len(out))/float64(len(b))
		t.Errorf("lifted to Markdown for only %.1f%% savings; threshold is %.0f%%",
			saved*100, tabularMinSavings*100)
	}
	if !strings.Contains(out, "[... squoze table:") && !json.Valid([]byte(out)) {
		t.Error("declined lifting but still returned invalid JSON")
	}
}

// TestJSONTabularStillWinsBigOnNarrowRows guards the other side: the threshold
// must not be so high that it refuses the case tabular lifting exists for.
func TestJSONTabularStillWinsBigOnNarrowRows(t *testing.T) {
	// Genuinely narrow rows — short scalars, no free-text column. This is the
	// shape where key names dominate the payload and lifting pays for itself.
	type row struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		Code   int    `json:"code"`
	}
	rows := make([]row, 0, 400)
	for i := 0; i < 400; i++ {
		rows = append(rows, row{ID: 1000 + i, Status: "RUNNING", Code: 200})
	}
	b, _ := json.Marshal(map[string]any{"object": "list", "data": rows})
	in := string(b)

	out, changed := DistillJSON(in)
	if !changed {
		t.Fatal("expected tabular lifting on 400 narrow rows")
	}
	if !strings.Contains(out, "[... squoze table:") {
		t.Fatalf("declined the canonical win case:\n%s", firstLines(out, 3))
	}
	// Bar sits just above the threshold, not at a round number: compact JSON
	// has far less structural padding to reclaim than indented JSON, so a
	// 40-byte row becoming 24 bytes is the honest ceiling here.
	saved := 1 - float64(len(out))/float64(len(in))
	if saved <= tabularMinSavings {
		t.Errorf("savings %.1f%% at or below the %.0f%% threshold — lifting should not have applied",
			saved*100, tabularMinSavings*100)
	}
}

// TestJSONTruncatedCellsAreDisclosed is finding (g): sanitizeCell caps cells at
// 80 chars, and before this the table said nothing about it. A model reading a
// cut error_message cannot tell the message continued, and the savings figure
// looks like compression when part of it is deletion.
func TestJSONTruncatedCellsAreDisclosed(t *testing.T) {
	type row struct {
		ID    int    `json:"id"`
		Error string `json:"error_message"`
	}
	rows := make([]row, 0, 20)
	for i := 0; i < 20; i++ {
		rows = append(rows, row{
			ID:    i,
			Error: strings.Repeat("stack frame detail that runs well past the cell cap ", 4),
		})
	}
	b, _ := json.Marshal(map[string]any{"data": rows})
	out, changed := DistillJSON(string(b))
	if !changed || !strings.Contains(out, "[... squoze table:") {
		t.Skip("no lifting on this shape; truncation disclosure not reachable")
	}
	head := headlineOf(out)
	if !strings.Contains(head, "cells truncated") {
		t.Errorf("truncation not disclosed in headline: %s", head)
	}
	if !strings.Contains(head, "20 cells truncated") {
		t.Errorf("want 20 truncated cells disclosed, got: %s", head)
	}
}

// TestJSONNoTruncationNoticeWhenCellsFit keeps the headline quiet in the common
// case, so the notice stays a signal rather than decoration.
func TestJSONNoTruncationNoticeWhenCellsFit(t *testing.T) {
	out, changed := DistillJSON(wrappedList(60))
	if !changed {
		t.Fatal("expected tabular lifting")
	}
	if strings.Contains(headlineOf(out), "cells truncated") {
		t.Errorf("disclosed truncation that did not happen: %s", headlineOf(out))
	}
}

func headlineOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "[... squoze table:") {
			return line
		}
	}
	return "<no headline>"
}

func headerOf(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| ") {
			return line
		}
	}
	return "<no header>"
}

func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}
