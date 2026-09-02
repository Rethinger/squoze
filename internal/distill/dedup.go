package distill

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
)

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

// DeduplicateHistoricalReads detects identical large blocks (>512 bytes) across turns.
// The LATEST occurrence of each block is kept 100% intact (active working memory).
// EARLIER redundant occurrences are replaced with a concise backwards reference.
func DeduplicateHistoricalReads(blocks []BlockRecord) ([]string, bool) {
	if len(blocks) < 2 {
		out := make([]string, len(blocks))
		for i, b := range blocks {
			out[i] = b.Content
		}
		return out, false
	}

	// 1. Find the latest turn index for each unique content hash
	latestTurn := make(map[uint64]int)
	for _, b := range blocks {
		if len(b.Content) >= 512 {
			latestTurn[b.Hash] = b.TurnIndex
		}
	}

	out := make([]string, len(blocks))
	changed := false

	for i, b := range blocks {
		if len(b.Content) >= 512 {
			latest := latestTurn[b.Hash]
			if b.TurnIndex < latest {
				// This is an obsolete earlier read! Replace with reference marker
				summary := fmt.Sprintf("[... squoze: earlier view (%d bytes) identical to turn %d · ref %s ...]",
					len(b.Content), latest, b.Ref)
				out[i] = summary
				changed = true
				continue
			}
		}
		out[i] = b.Content
	}

	return out, changed
}
