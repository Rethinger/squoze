// Package squoze is the library facade for the universal, deterministic LLM
// context optimizer ("your context, squoze.").
//
// Typical gateway embedding:
//
//	eng := squoze.NewEngine(squoze.DefaultMemoCapacity)
//	out, res := eng.Apply(requestBody)
//
// One Engine instance per process keeps the decision memo warm: identical
// original bytes always produce byte-identical output, which keeps provider
// prompt caches stable across turns. See README.md for the quality contracts
// (fail-open, never-elide, cache-safe, reversible).
package squoze

import (
	"github.com/Rethinger/squoze/internal/engine"
)

// Version mirrors the CLI version.
const Version = engine.Version

// DefaultMemoCapacity bounds the per-process decision memo (~4k blobs).
const DefaultMemoCapacity = engine.DefaultMemoCapacity

// Engine runs the optimization pipeline with its cache-guard state.
type Engine = engine.Engine

// Result reports what one pipeline pass did to a request body.
type Result = engine.Result

// NewEngine returns an isolated pipeline instance.
func NewEngine(memoCapacity int) *Engine { return engine.NewEngine(memoCapacity) }

// Process runs the default shared engine over a request body.
func Process(body []byte) ([]byte, Result) { return engine.Process(body) }

// Limits bounds the work one Apply may do, in bytes. The zero value is
// v0.3.0 behaviour exactly: no bound is applied unless it is configured.
//
// Bounds are sizes and never durations on purpose. A wall-clock deadline would
// make the output depend on how loaded the host was, and the cache-safe
// contract requires that identical original bytes always produce byte-identical
// output — a request that compresses on an idle box and passes through on a
// busy one breaks every provider prompt cache downstream of it.
type Limits = engine.Limits

// SkipBodyTooLarge is the Result.SkipReason set when MaxBodyBytes rejected a
// body before its format was even sniffed.
const SkipBodyTooLarge = engine.SkipBodyTooLarge

// NewEngineWithLimits is NewEngine with explicit bounds. Engines with different
// Limits must not be shared: the bounds decide what a body turns into, so the
// decision memo is only sound within one set of them.
func NewEngineWithLimits(memoCapacity int, lim Limits) *Engine {
	return engine.NewEngineWithLimits(memoCapacity, lim)
}
