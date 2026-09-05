package engine

// Limits bounds the work one Engine is willing to do on a request.
//
// Every field is opt-in: the zero value is v0.3.0 behaviour, byte for byte.
// That is not a convention but a tested contract — TestZeroLimitsMatchV030
// runs the corpus through NewEngine and NewEngineWithLimits(cap, Limits{}) and
// requires bytes.Equal, so an embedder that never mentions Limits cannot be
// affected by this type existing.
//
// The bounds are sizes, never durations. A wall-clock deadline would make the
// output depend on how loaded the host was, and squoze's cache-safety contract
// is that identical original bytes always produce byte-identical output — a
// deadline would silently break every provider prompt cache downstream of it.
type Limits struct {
	// MaxBodyBytes skips the whole request, without so much as sniffing its
	// wire format, once the body exceeds this many bytes. 0 means no cap.
	//
	// This is the only limit that reports itself: Result.Skipped is set and
	// Result.SkipReason says why, so a gateway can surface "this request was
	// too big to optimize" instead of a silent zero-savings pass.
	MaxBodyBytes int

	// MaxBlockBytes leaves an individual content block alone once it exceeds
	// this many bytes. 0 means no cap. Use it when a single pathological
	// tool_result should not be allowed to dominate a request's latency
	// budget while the rest of the messages still get optimized.
	MaxBlockBytes int

	// MinBlockBytes replaces the built-in 64-byte floor below which a block
	// is not worth examining. 0 keeps the built-in floor; a value below it
	// is ignored, because under 64 bytes no pass can produce savings that
	// survive the marker it would have to write.
	MinBlockBytes int
}

// Skip reasons reported in Result.SkipReason. They are stable strings: 2papi
// forwards them verbatim in the X-Gateway-Squoze-Skip response header, so
// renaming one is a breaking change for anything parsing that header.
const (
	SkipBodyTooLarge = "body_too_large"
)

// defaultMinBlockBytes is the floor distillText and squeezeText apply when
// Limits.MinBlockBytes is unset. It matches v0.3.0's hardcoded literal.
const defaultMinBlockBytes = 64

// Limits reports the bounds this Engine was built with. The zero value means
// unbounded, which is what NewEngine produces.
func (e *Engine) Limits() Limits { return e.lim }

// minBlock is the effective lower bound for one content block.
func (e *Engine) minBlock() int {
	if e.lim.MinBlockBytes > defaultMinBlockBytes {
		return e.lim.MinBlockBytes
	}
	return defaultMinBlockBytes
}

// blockTooLarge reports whether a block exceeds the configured per-block cap.
func (e *Engine) blockTooLarge(n int) bool {
	return e.lim.MaxBlockBytes > 0 && n > e.lim.MaxBlockBytes
}

// blockOutOfBounds is blockTooLarge plus the opt-in lower bound, for the paths
// that had no floor of their own in v0.3.0. Only configured values apply here:
// a zero Limits must not introduce a floor where there was none.
func (e *Engine) blockOutOfBounds(n int) bool {
	return e.blockTooLarge(n) || (e.lim.MinBlockBytes > 0 && n < e.lim.MinBlockBytes)
}
