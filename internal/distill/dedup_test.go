package distill

import (
	"strings"
	"testing"
)

// TestDeduplicateHistoricalReads pins the direction of the collapse: the first
// occurrence survives untouched, later repeats become references to it.
//
// This test previously asserted the inverse (compact turn 2, keep turn 8 whole)
// on an "active working memory" rationale. That is cache-hostile: turn 2 sits
// inside the prefix the provider has already cached, so rewriting it
// invalidates everything downstream of it on every re-read.
func TestDeduplicateHistoricalReads(t *testing.T) {
	fileContent := strings.Repeat("func ProcessData(chunk []byte) error { return nil }\n", 30) // ~1.5 KB
	hash := ComputeFastHash(fileContent)
	ref := ComputeRef(fileContent)

	blocks := []BlockRecord{
		{TurnIndex: 2, Content: fileContent, Hash: hash, Ref: ref}, // already sent and cached
		{TurnIndex: 5, Content: "short status ok", Hash: ComputeFastHash("short status ok"), Ref: "ref1"},
		{TurnIndex: 8, Content: fileContent, Hash: hash, Ref: ref}, // the new re-read
	}

	result, changed := DeduplicateHistoricalReads(blocks)
	if !changed {
		t.Fatal("expected DeduplicateHistoricalReads to report changed=true")
	}

	if result[0] != fileContent {
		t.Fatalf("turn 2 was rewritten; it is inside the cached prefix:\n%.200s", result[0])
	}

	if !strings.Contains(result[2], "identical to the copy in turn 2 above") {
		t.Fatalf("turn 8 was not collapsed onto turn 2:\n%s", result[2])
	}
	if !strings.Contains(result[2], ref) {
		t.Fatal("ref missing in the backwards-reference marker")
	}
	if len(result[2]) >= len(fileContent) {
		t.Fatalf("marker is not shorter than the content it replaces: %d vs %d",
			len(result[2]), len(fileContent))
	}

	if result[1] != "short status ok" {
		t.Fatalf("short status was modified: %s", result[1])
	}
}

// TestDedupMarkerIsStableAsTurnsAccumulate is why the reference points backwards
// rather than forwards. A marker naming the LATEST duplicate is rewritten every
// time another copy arrives, so the history never settles and the prefix breaks
// on each turn; naming the FIRST occurrence produces the same bytes forever.
func TestDedupMarkerIsStableAsTurnsAccumulate(t *testing.T) {
	fileContent := strings.Repeat("payload line for the dedup fixture\n", 30)
	hash := ComputeFastHash(fileContent)
	ref := ComputeRef(fileContent)

	blocks := []BlockRecord{
		{TurnIndex: 1, Content: fileContent, Hash: hash, Ref: ref},
		{TurnIndex: 2, Content: fileContent, Hash: hash, Ref: ref},
	}
	twoTurns, _ := DeduplicateHistoricalReads(blocks)

	blocks = append(blocks, BlockRecord{TurnIndex: 3, Content: fileContent, Hash: hash, Ref: ref})
	threeTurns, _ := DeduplicateHistoricalReads(blocks)

	for i := range twoTurns {
		if twoTurns[i] != threeTurns[i] {
			t.Errorf("block %d changed when a third copy arrived:\n  was: %.100s\n  now: %.100s",
				i, twoTurns[i], threeTurns[i])
		}
	}
	if threeTurns[2] != threeTurns[1] {
		t.Error("the two later copies produced different markers for identical content")
	}
}

// TestDedupIgnoresSmallBlocks keeps the floor meaningful: below it a marker is
// not reliably shorter than the content, so replacing it can cost bytes.
func TestDedupIgnoresSmallBlocks(t *testing.T) {
	small := strings.Repeat("x", dedupMinBytes-1)
	hash := ComputeFastHash(small)

	blocks := []BlockRecord{
		{TurnIndex: 1, Content: small, Hash: hash, Ref: ComputeRef(small)},
		{TurnIndex: 2, Content: small, Hash: hash, Ref: ComputeRef(small)},
	}
	result, changed := DeduplicateHistoricalReads(blocks)
	if changed {
		t.Error("a sub-floor block was deduped")
	}
	for i, r := range result {
		if r != small {
			t.Errorf("block %d was modified", i)
		}
	}
}
