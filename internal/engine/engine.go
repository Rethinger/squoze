// Package engine hosts the squoze optimization pipeline.
//
// v2 pipeline: detect wire format → walk message structures with targeted
// JSON-path edits (gjson/sjson) → squeeze only provably-safe text (machine
// output classified by the router) → report savings. Untouched bytes stay
// byte-identical: sjson edits in place instead of re-marshalling maps, which
// is what keeps provider prompt caches stable.
//
// Contracts: fail-open (unknown/invalid input passes through), never-elide
// (error lines survive), savings floor, idempotent markers.
package engine

import (
	"github.com/Rethinger/squoze/internal/compress"
	"github.com/Rethinger/squoze/internal/router"
	"github.com/Rethinger/squoze/internal/wire"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Version is reported by `squoze version` and stamped into response headers.
const Version = "0.0.2"

// Result describes one processed request body.
type Result struct {
	Format        wire.Format
	OriginalBytes int
	SentBytes     int
	SavedBytes    int
	BlocksSqueezed int
	Transforms    []string // names of applied transforms, in order
}

// Process runs the full pipeline over a request body. Returns the (possibly
// rewritten) body to send upstream plus the report. Unknown or invalid input
// passes through byte-for-byte (fail-open contract).
func Process(body []byte) ([]byte, Result) {
	res := Result{
		Format:        wire.Detect(body),
		OriginalBytes: len(body),
		SentBytes:     len(body), // fail-open default: untouched passthrough
	}
	switch res.Format {
	case wire.FormatOpenAIChat:
		body, res = processOpenAIChat(body, res)
	case wire.FormatAnthropicMessages:
		body, res = processAnthropic(body, res)
	default:
		return body, res // unknown: pass through untouched
	}
	res.SentBytes = len(body)
	res.SavedBytes = res.OriginalBytes - res.SentBytes
	return body, res
}

// squeezeText applies the router+compress pair to one text blob. Returns ""
// when the blob must not be touched.
func squeezeText(s string) string {
	switch router.Classify(s) {
	case router.KindTestOutput, router.KindLogOutput:
		out, changed := compress.Text(s, compress.Default)
		if !changed {
			return ""
		}
		return out
	default:
		return "" // prose/code/json/unknown: never naive-truncate
	}
}

// processOpenAIChat squeezes role=tool message contents (string form).
func processOpenAIChat(body []byte, res Result) ([]byte, Result) {
	n := int(gjson.GetBytes(body, "messages.#").Int())
	for i := 0; i < n; i++ {
		prefix := "messages." + itoa(i)
		if role := gjson.GetBytes(body, prefix+".role").String(); role != "tool" {
			continue
		}
		c := gjson.GetBytes(body, prefix+".content")
		if c.Type != gjson.String {
			continue
		}
		if out := squeezeText(c.String()); out != "" {
			var err error
			body, err = sjson.SetBytes(body, prefix+".content", out)
			if err != nil {
				continue // leave this block untouched
			}
			res.BlocksSqueezed++
		}
	}
	res.Transforms = append(res.Transforms, "rtk_head_tail")
	return body, res
}

// processAnthropic squeezes tool_result blocks. Content may be a plain
// string or an array of {type:"text",text:"..."} items; both handled.
func processAnthropic(body []byte, res Result) ([]byte, Result) {
	n := int(gjson.GetBytes(body, "messages.#").Int())
	for i := 0; i < n; i++ {
		msgPrefix := "messages." + itoa(i)
		content := gjson.GetBytes(body, msgPrefix+".content")
		if !content.IsArray() {
			continue
		}
		blocks := content.Array()
		for j, blk := range blocks {
			if blk.Get("type").String() != "tool_result" {
				continue
			}
			blockPrefix := msgPrefix + ".content." + itoa(j)
			inner := blk.Get("content")
			switch {
			case inner.Type == gjson.String:
				if out := squeezeText(inner.String()); out != "" {
					var err error
					body, err = sjson.SetBytes(body, blockPrefix+".content", out)
					if err == nil {
						res.BlocksSqueezed++
					}
				}
			case inner.IsArray():
				texts := inner.Array()
				for k, item := range texts {
					if item.Get("type").String() != "text" {
						continue
					}
					textPath := blockPrefix + ".content." + itoa(k) + ".text"
					if out := squeezeText(item.Get("text").String()); out != "" {
						var err error
						body, err = sjson.SetBytes(body, textPath, out)
						if err == nil {
							res.BlocksSqueezed++
						}
					}
				}
			}
		}
	}
	res.Transforms = append(res.Transforms, "rtk_head_tail")
	return body, res
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
