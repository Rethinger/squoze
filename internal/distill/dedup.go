package distill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
)

// dedupMinBytes is the floor for cross-turn dedup. Below it the backwards
// reference is not reliably shorter than the content it replaces.
const dedupMinBytes = 512

// BlockRecord tracks a large tool content block across conversation turns.
type BlockRecord struct {
	TurnIndex int
	Content   string
	Hash      uint64
	Ref       string
}

// ComputeFastHash calculates a 64-bit FNV-1a hash of a string.
func ComputeFastHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// ComputeRef creates a 12-char SHA-256 ref for a string.
func ComputeRef(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:12]
}

// DeduplicateHistoricalReads collapses identical large blocks (>=512 bytes)
// repeated across turns. The FIRST occurrence is kept 100% intact; every LATER
// repeat becomes a backwards reference to it.
//
// The direction is the whole point, and it is the opposite of what looks
// natural. Keeping the newest copy intact and rewriting the older ones reads
// like "the active working set stays sharp", but a conversation reaches the
// provider as a growing prefix: turn N's body is turn N-1's body plus new
// messages. Anything at a position the provider has already seen must stay
// byte-identical forever, or the cached prefix is invalidated from that
// position onward — on a re-read that is nearly the entire conversation. So the
// only copy that may be rewritten is one the provider has not seen yet: the
// newest.
//
// Referencing the first occurrence also makes the marker itself stable. A
// marker that names the LATEST turn changes every turn (turn 3, then 4, then
// 5...), so the rewritten history never settles; the first occurrence never
// moves, so the marker is written once and stays.
//
// Nothing is lost: the full bytes remain in context at the earlier position,
// which the model can attend to, and the marker says where.
func DeduplicateHistoricalReads(blocks []BlockRecord) ([]string, bool) {
	if len(blocks) < 2 {
		out := make([]string, len(blocks))
		for i, b := range blocks {
			out[i] = b.Content
		}
		return out, false
	}

	// Earliest turn index per unique content hash. Ranged in order and written
	// only on first sight, so map iteration order cannot affect the result.
	firstTurn := make(map[uint64]int)
	for _, b := range blocks {
		if len(b.Content) >= dedupMinBytes {
			if _, seen := firstTurn[b.Hash]; !seen {
				firstTurn[b.Hash] = b.TurnIndex
			}
		}
	}

	out := make([]string, len(blocks))
	changed := false

	for i, b := range blocks {
		if len(b.Content) >= dedupMinBytes {
			if first, ok := firstTurn[b.Hash]; ok && b.TurnIndex > first {
				out[i] = fmt.Sprintf(
					"[... squoze: identical to the copy in turn %d above (%d bytes) · ref %s ...]",
					first, len(b.Content), b.Ref)
				changed = true
				continue
			}
		}
		out[i] = b.Content
	}

	return out, changed
}
