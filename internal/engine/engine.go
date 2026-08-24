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
	"github.com/Rethinger/squoze/internal/store"
	"github.com/Rethinger/squoze/internal/wire"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Version is reported by `squoze version` and stamped into response headers.
const Version = "0.0.3"

// Result describes one processed request body.
type Result struct {
	Format         wire.Format
	OriginalBytes  int
	SentBytes      int
	SavedBytes     int
	BlocksSqueezed int
	MemoHits       int // blocks served byte-identical from the decision memo
	Transforms     []string
}

// Engine is the pipeline with its cache-guard state attached.
type Engine struct {
	memo *store.Memo
}

// DefaultMemoCapacity bounds pinned decisions per process (~4k blobs).
const DefaultMemoCapacity = 4096

// defaultEngine backs package-level helpers; proxy and tests share it.
var defaultEngine = NewEngine(DefaultMemoCapacity)

// NewEngine returns an isolated pipeline instance.
func NewEngine(memoCapacity int) *Engine {
	return &Engine{memo: store.NewMemo(memoCapacity)}
}

// Process runs the default engine over a request body.
func Process(body []byte) ([]byte, Result) {
	return defaultEngine.Apply(body)
}

// Apply runs the full pipeline: detect → route → squeeze (memo-aware) →
// report. Unknown or invalid input passes through byte-for-byte (fail-open).
func (e *Engine) Apply(body []byte) ([]byte, Result) {
	res := Result{
		Format:        wire.Detect(body),
		OriginalBytes: len(body),
		SentBytes:     len(body), // fail-open default: untouched passthrough
	}
	switch res.Format {
	case wire.FormatOpenAIChat:
		body, res = e.processOpenAIChat(body, res)
	case wire.FormatAnthropicMessages:
		body, res = e.processAnthropic(body, res)
	default:
		return body, res // unknown: pass through untouched
	}
	res.SentBytes = len(body)
	res.SavedBytes = res.OriginalBytes - res.SentBytes
	return body, res
}

// squeezeText applies memo + router + compress to one text blob. Returns ""
// when the blob must not be touched or was rejected after compression.
func (e *Engine) squeezeText(s string) string {
	switch router.Classify(s) {
	case router.KindTestOutput, router.KindLogOutput:
	default:
		return "" // prose/code/json/unknown: never naive-truncate
	}
	if out, ok := e.memo.Get([]byte(s)); ok {
		return string(out) // cache-guard: pin previous decision
	}
	out, changed := compress.Text(s, compress.Default)
	if !changed {
		return ""
	}
	e.memo.Put([]byte(s), []byte(out))
	return out
}

// processOpenAIChat squeezes role=tool message contents (string form).
func (e *Engine) processOpenAIChat(body []byte, res Result) ([]byte, Result) {
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
		before := e.memo.Len()
		if out := e.squeezeText(c.String()); out != "" {
			var err error
			body, err = sjson.SetBytes(body, prefix+".content", out)
			if err != nil {
				continue // leave this block untouched
			}
			if e.memo.Len() == before {
				res.MemoHits++
			}
			res.BlocksSqueezed++
		}
	}
	res.Transforms = append(res.Transforms, "rtk_head_tail")
	return body, res
}

// processAnthropic squeezes tool_result blocks. Content may be a plain
// string or an array of {type:"text",text:"..."} items; both handled.
func (e *Engine) processAnthropic(body []byte, res Result) ([]byte, Result) {
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
				before := e.memo.Len()
				if out := e.squeezeText(inner.String()); out != "" {
					var err error
					body, err = sjson.SetBytes(body, blockPrefix+".content", out)
					if err == nil {
						if e.memo.Len() == before {
							res.MemoHits++
						}
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
					before := e.memo.Len()
					if out := e.squeezeText(item.Get("text").String()); out != "" {
						var err error
						body, err = sjson.SetBytes(body, textPath, out)
						if err == nil {
							if e.memo.Len() == before {
								res.MemoHits++
							}
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
