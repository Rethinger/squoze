// Package engine hosts the squoze optimization pipeline.
//
// v1 pipeline (MVP step 1): detect format → pass the body through untouched
// → report what would be transformed. Later steps insert block routing and
// compression between detection and encode, always under the quality
// contracts: fail-open, never-elide, savings floor, cache-safe decisions.
package engine

import (
	"github.com/Rethinger/squoze/internal/wire"
)

// Version is reported by `squoze version` and stamped into response headers.
const Version = "0.0.1"

// Result describes one processed request body.
type Result struct {
	Format        wire.Format
	OriginalBytes int
	SentBytes     int
	Transforms    []string // names of applied transforms, in order
}

// Process runs the full pipeline over a request body. The MVP contract:
// unknown or invalid input passes through byte-for-byte (fail-open), and
// valid input is passed through too until transforms land — but the wire
// format is detected and reported so callers can already branch on it.
func Process(body []byte) Result {
	res := Result{
		Format:        wire.Detect(body),
		OriginalBytes: len(body),
		SentBytes:     len(body),
	}
	return res
}
