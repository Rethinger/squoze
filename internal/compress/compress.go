// Package compress implements the first real transform: head/tail elision
// of verbose machine output, under the quality contracts.
//
// Contracts enforced here:
//   - never-elide: lines that look like failures/errors survive the middle cut
//   - idempotency: an already-elided blob is never compressed again (provider
//     prompt caches see a stable body across turns)
//   - savings floor: if the squeeze saves less than 10%, skip it — breaking
//     a prefix for pocket change is a net loss
package compress

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Params controls one compression pass.
type Params struct {
	MinBytes     int    // blobs below this size pass through
	HeadLines    int    // leading lines kept verbatim
	TailLines    int    // trailing lines kept verbatim (most recent signal)
	MaxKept      int    // max middle error-lines rescued
	ContextAfter int    // extra trailing lines rescued with each error line
	Marker       string // elision marker line
}

// Default is the standard preset.
var Default = Params{
	MinBytes:     2048,
	HeadLines:    20,
	TailLines:    20,
	MaxKept:      50,
	ContextAfter: 2,
	Marker:       "[... squoze: %d middle lines elided · full text kept locally as %s ...]",
}

// RefPrefix is how much of the original's SHA256 hex is embedded in markers.
const RefHexLen = 12

func refOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:RefHexLen]
}

const savingsFloor = 0.9 // require out <= in*floor to accept the mutation

// mustKeep reports whether a middle line carries failure signal and must
// survive truncation. Deliberately narrow: false positives cost bytes, false
// negatives cost information the model needed.
func mustKeep(line string) bool {
	u := strings.ToUpper(line)
	for _, pat := range []string{
		"--- FAIL", "FAIL:", "FAILED ", "PANIC:", "FATAL", "ERROR", "EXCEPTION",
		"ASSERTIONERROR", "TRACEBACK", "BUILD FAILED", "✗",
	} {
		if strings.Contains(u, pat) {
			return true
		}
	}
	return false
}

// markerPrefix identifies our own elision markers; its presence means the
// blob was already squeezed and must stay byte-stable (cache contract).
const markerPrefix = "[... squoze:"

// Text squeezes one text blob. Returns the result plus applied flag.
func Text(s string, p Params) (string, bool) {
	if len(s) < p.MinBytes || strings.Contains(s, markerPrefix) {
		return s, false
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= p.HeadLines+p.TailLines+2 {
		return s, false
	}

	head := lines[:p.HeadLines]
	tail := lines[len(lines)-p.TailLines:]
	middle := lines[p.HeadLines : len(lines)-p.TailLines]

	kept := make([]string, 0, p.MaxKept)
	for idx := 0; idx < len(middle); idx++ {
		if len(kept) >= p.MaxKept {
			break
		}
		if mustKeep(middle[idx]) {
			// Rescue the error line plus its immediate context: the detail
			// ("want X got Y") usually lives on the next line, and a failure
			// without its detail is an elision of the signal itself.
			end := idx + 1 + p.ContextAfter
			if end > len(middle) {
				end = len(middle)
			}
			for _, l := range middle[idx:end] {
				if len(kept) >= p.MaxKept {
					break
				}
				kept = append(kept, l)
			}
			idx = end - 1
		}
	}

	marker := p.Marker
	marker = strings.ReplaceAll(marker, "%d", itoa(len(middle)-len(kept)))
	marker = strings.ReplaceAll(marker, "%s", refOf(s))

	out := strings.Join(head, "\n") + "\n" + marker + "\n"
	out += strings.Join(kept, "\n")
	if len(kept) > 0 {
		out += "\n"
	}
	out += strings.Join(tail, "\n")

	if strings.HasSuffix(s, "\n") && !strings.HasSuffix(out, "\n") {
		out += "\n" // preserve the original trailing-newline shape
	}

	if float64(len(out)) > float64(len(s))*savingsFloor {
		return s, false // savings floor: not worth breaking cache stability
	}
	return out, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
