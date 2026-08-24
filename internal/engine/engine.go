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
	"github.com/Rethinger/squoze/internal/profile"
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
	Family         string // model family the request was optimized for
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
	orig *store.Originals
}

// DefaultMemoCapacity bounds pinned decisions per process (~4k blobs).
const DefaultMemoCapacity = 4096

// defaultEngine backs package-level helpers; proxy and tests share it.
var defaultEngine = NewEngine(DefaultMemoCapacity)

// NewEngine returns an isolated pipeline instance with memory-only originals.
func NewEngine(memoCapacity int) *Engine {
	return NewEngineWith(memoCapacity, store.NewOriginals())
}

// NewEngineWith attaches an external originals store (e.g. persisted).
func NewEngineWith(memoCapacity int, orig *store.Originals) *Engine {
	return &Engine{memo: store.NewMemo(memoCapacity), orig: orig}
}

// Process runs the default engine over a request body.
func Process(body []byte) ([]byte, Result) {
	return defaultEngine.Apply(body)
}

// Apply runs the full pipeline: detect → route → squeeze (profile- and
// memo-aware) → report. Unknown or invalid input passes through
// byte-for-byte (fail-open).
func (e *Engine) Apply(body []byte) ([]byte, Result) {
	res := Result{
		Format:        wire.Detect(body),
		OriginalBytes: len(body),
		SentBytes:     len(body), // fail-open default: untouched passthrough
	}
	family := profile.Detect(gjson.GetBytes(body, "model").String())
	res.Family = family.String()
	switch res.Format {
	case wire.FormatOpenAIChat:
		body, res = e.processOpenAIChat(family, body, res)
	case wire.FormatAnthropicMessages:
		body, res = e.processAnthropic(family, body, res)
	default:
		return body, res // unknown: pass through untouched
	}
	res.SentBytes = len(body)
	res.SavedBytes = res.OriginalBytes - res.SentBytes
	return body, res
}

// squeezeText applies memo + router + compress to one text blob, keyed by
// model family so pinned decisions never leak across providers. Returns ""
// when the blob must not be touched or was rejected after compression.
func (e *Engine) squeezeText(f profile.Family, s string) string {
	switch router.Classify(s) {
	case router.KindTestOutput, router.KindLogOutput:
	default:
		return "" // prose/code/json/unknown: never naive-truncate
	}
	key := []byte(f.String() + "\x00" + s)
	if out, ok := e.memo.Get(key); ok {
		return string(out) // cache-guard: pin previous decision
	}
	out, changed := compress.Text(s, profile.ParamsFor(f))
	if !changed {
		return ""
	}
	if _, err := e.orig.Put([]byte(s)); err != nil {
		// Reversibility storage failure must not break the squeeze; the
		// marker still names the ref for later manual recovery attempts.
		_ = err
	}
	e.memo.Put(key, []byte(out))
	return out
}

// processOpenAIChat squeezes role=tool message contents (string form).
func (e *Engine) processOpenAIChat(f profile.Family, body []byte, res Result) ([]byte, Result) {
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
		if out := e.squeezeText(f, c.String()); out != "" {
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
func (e *Engine) processAnthropic(f profile.Family, body []byte, res Result) ([]byte, Result) {
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
				if out := e.squeezeText(f, inner.String()); out != "" {
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
					if out := e.squeezeText(f, item.Get("text").String()); out != "" {
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
