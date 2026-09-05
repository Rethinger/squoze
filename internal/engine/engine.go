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
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/Rethinger/squoze/internal/compress"
	"github.com/Rethinger/squoze/internal/distill"
	"github.com/Rethinger/squoze/internal/profile"
	"github.com/Rethinger/squoze/internal/router"
	"github.com/Rethinger/squoze/internal/store"
	"github.com/Rethinger/squoze/internal/wire"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Version is reported by `squoze version` and stamped into response headers.
const Version = "0.4.0"

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
	DurationMS     float64

	// Skipped is set when the request came back untouched without being
	// examined, which today means one thing: it was larger than
	// Limits.MaxBodyBytes. A request that was examined and simply had nothing
	// worth squeezing is not "skipped" — it reports SavedBytes 0 instead.
	Skipped    bool
	SkipReason string // one of the Skip* constants; "" unless Skipped
}

// Engine is the pipeline with its cache-guard state attached.
type Engine struct {
	memo *store.Memo
	orig *store.Originals
	lim  Limits

	// prescanOff disables canDistill. It exists so output-neutrality can be
	// tested rather than asserted (see prescan_test.go); nothing outside this
	// package can set it, and no production path does.
	prescanOff bool
}

// DefaultMemoCapacity bounds pinned decisions per process (~4k blobs).
const DefaultMemoCapacity = 4096

// defaultEngine backs package-level helpers; proxy and tests share it.
var defaultEngine = NewEngine(DefaultMemoCapacity)

// NewEngine returns an isolated pipeline instance with memory-only originals.
func NewEngine(memoCapacity int) *Engine {
	return NewEngineWith(memoCapacity, store.NewOriginals())
}

// NewEngineWithLimits returns an isolated instance bounded by lim. The zero
// Limits is v0.3.0 behaviour, so NewEngine(cap) and
// NewEngineWithLimits(cap, Limits{}) are interchangeable.
func NewEngineWithLimits(memoCapacity int, lim Limits) *Engine {
	e := NewEngineWith(memoCapacity, store.NewOriginals())
	e.lim = lim
	return e
}

// NewEngineWith attaches an external originals store (e.g. persisted).
func NewEngineWith(memoCapacity int, orig *store.Originals) *Engine {
	return &Engine{memo: store.NewMemo(memoCapacity), orig: orig}
}

// ResolveOriginal recovers the un-elided original text by its reference hash.
func (e *Engine) ResolveOriginal(ref string) ([]byte, error) {
	if e.orig == nil {
		return nil, errors.New("originals store not configured")
	}
	s, err := e.orig.Resolve(ref)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

// Default returns the package-level shared engine.
func Default() *Engine { return defaultEngine }

// Process runs the default engine over a request body.
func Process(body []byte) ([]byte, Result) {
	return defaultEngine.Apply(body)
}

// Apply runs the full pipeline: detect → route → squeeze (profile- and
// memo-aware) → report. Unknown or invalid input passes through
// byte-for-byte (fail-open).
func (e *Engine) Apply(body []byte) ([]byte, Result) {
	start := time.Now()
	// Body cap first, ahead of wire.Detect: detection unmarshals the body into a
	// probe struct, so it walks every byte. Above the cap we promise not to
	// look, and Format stays "unknown" because we did not read enough to know.
	if m := e.lim.MaxBodyBytes; m > 0 && len(body) > m {
		return body, Result{
			OriginalBytes: len(body),
			SentBytes:     len(body),
			Skipped:       true,
			SkipReason:    SkipBodyTooLarge,
			DurationMS:    float64(time.Since(start).Microseconds()) / 1000.0,
		}
	}
	res := Result{
		Format:        wire.Detect(body),
		OriginalBytes: len(body),
		SentBytes:     len(body), // fail-open default: untouched passthrough
	}
	// Fast bailout: if body has no tool messages, diffs or code fences, pass through instantly.
	hasCandidate := bytes.Contains(body, []byte("tool")) ||
		bytes.Contains(body, []byte("```")) ||
		bytes.Contains(body, []byte("diff --git"))
	if len(body) < 256 || !hasCandidate {
		res.DurationMS = float64(time.Since(start).Microseconds()) / 1000.0
		return body, res
	}

	family := profile.Detect(gjson.GetBytes(body, "model").String())
	res.Family = family.String()
	switch res.Format {
	case wire.FormatOpenAIChat:
		body, res = e.processOpenAIChatFast(family, body, res)
	case wire.FormatAnthropicMessages:
		body, res = e.processAnthropicFast(family, body, res)
	default:
		res.DurationMS = float64(time.Since(start).Microseconds()) / 1000.0
		return body, res // unknown: pass through untouched
	}
	res.SentBytes = len(body)
	res.SavedBytes = res.OriginalBytes - res.SentBytes
	res.DurationMS = float64(time.Since(start).Microseconds()) / 1000.0
	return body, res
}

// squeezeText applies memo + router + compress to one text blob, keyed by
// model family so pinned decisions never leak across providers. Returns ""
// when the blob must not be touched or was rejected after compression.
func (e *Engine) squeezeText(f profile.Family, s string) string {
	if e.blockOutOfBounds(len(s)) {
		return ""
	}
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

// distillText runs the distillation pipeline for text content:
// 1. Check memo cache (cache-guard contract)
// 2. Pass 1: Unified Diff Distillation
// 3. Pass 2: JSON Structural Pruning & Tabular Lifting
// 4. Pass 3: Test & Log Head/Tail Squeezer (Never-Elide 2.0 with profile sensitivity)
// 5. Pass 4: Generic Terminal & ANSI Sanitization
func (e *Engine) distillText(f profile.Family, s string) string {
	params := profile.ParamsFor(f)
	if len(s) < e.minBlock() || e.blockTooLarge(len(s)) {
		return ""
	}
	key := []byte(f.String() + "\x00" + s)
	if out, ok := e.memo.Get(key); ok {
		return string(out)
	}

	// Structural pre-scan, deliberately after the memo lookup and not before
	// it. A memo hit is a pinned decision from an earlier turn: cheap to serve
	// and the whole point of the cache guard. Gating ahead of it could hand
	// back the original bytes for a block an earlier turn had already
	// rewritten, which is exactly the prefix break the memo exists to prevent.
	if !e.prescanOff && !canDistill(s) {
		return ""
	}

	orig := s

	// Pass 1: Unified Diff Distillation
	if distill.IsUnifiedDiff(s) {
		if sDiff, ok := distill.DistillDiff(s); ok {
			e.storeAndMemo(key, orig, sDiff)
			return sDiff
		}
	}

	// Pass 2: JSON Structural Pruning & Tabular Lifting
	trimmed := strings.TrimSpace(s)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		if sJSON, ok := distill.DistillJSON(s); ok {
			e.storeAndMemo(key, orig, sJSON)
			return sJSON
		}
	}

	// Pass 3: Test & Log Head/Tail Squeezer (Never-Elide 2.0)
	switch router.Classify(s) {
	case router.KindTestOutput, router.KindLogOutput:
		// Strip ANSI and normalize progress before test output compression
		cleaned := distill.StripANSI(s)
		cleaned = distill.CleanCarriageReturns(cleaned)
		out, changed := compress.Text(cleaned, params)
		if changed {
			e.storeAndMemo(key, orig, out)
			return out
		}
		// If below profile.MinBytes (e.g. Claude ~2.7KB), pass through untouched
		return ""
	case router.KindCode:
		// Never-touch, and that has to mean before Pass 4 as well: terminal
		// hygiene would rewrite bytes inside a source file the caller is
		// about to compile or diff.
		return ""
	}

	// Pass 4: Generic terminal hygiene for logs/noise
	sClean, termChanged := distill.SanitizeTerminal(s)
	if termChanged && len(sClean) < len(orig)*9/10 {
		e.storeAndMemo(key, orig, sClean)
		return sClean
	}

	return ""
}

func (e *Engine) storeAndMemo(key []byte, orig, out string) {
	if _, err := e.orig.Put([]byte(orig)); err != nil {
		_ = err
	}
	e.memo.Put(key, []byte(out))
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
