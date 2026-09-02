package distill

import (
	"strings"
	"testing"
)

func TestDeduplicateHistoricalReads(t *testing.T) {
	fileContent := strings.Repeat("func ProcessData(chunk []byte) error { return nil }\n", 30) // ~1.5 KB
	hash := ComputeFastHash(fileContent)
	ref := ComputeRef(fileContent)

	blocks := []BlockRecord{
		{TurnIndex: 2, Content: fileContent, Hash: hash, Ref: ref},
		{TurnIndex: 5, Content: "short status ok", Hash: ComputeFastHash("short status ok"), Ref: "ref1"},
		{TurnIndex: 8, Content: fileContent, Hash: hash, Ref: ref}, // Active latest read
	}

	result, changed := DeduplicateHistoricalReads(blocks)
	if !changed {
		t.Fatal("expected DeduplicateHistoricalReads to report changed=true")
	}

	// Turn 2 (earlier read) must be compacted
	if !strings.Contains(result[0], "earlier view") || !strings.Contains(result[0], "identical to turn 8") {
		t.Fatalf("earlier turn was not compacted:\n%s", result[0])
	}
	if !strings.Contains(result[0], ref) {
		t.Fatal("ref missing in earlier turn marker")
	}

	// Turn 5 (short status) untouched
	if result[1] != "short status ok" {
		t.Fatalf("short status was modified: %s", result[1])
	}

	// Turn 8 (latest read) MUST be preserved 100% in full
	if result[2] != fileContent {
		t.Fatal("active working turn content was erroneously modified")
	}
}
