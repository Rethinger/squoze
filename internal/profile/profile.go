// Package profile adapts compression aggressiveness to the target model
// family. The trade-off differs per provider: families with cheap, reliable
// prompt caches deserve gentle treatment (preserve prefixes hard), while
// families without them reward stronger squeezing.
//
// Presets are starting points grounded in that economics, not tuned claims;
// the eval suite (MVP step 6) is where they earn their numbers.
package profile

import (
	"strings"

	"github.com/Rethinger/squoze/internal/compress"
)

// Family identifies a model lineage.
type Family int

const (
	Generic Family = iota
	Claude
	GPT
	DeepSeek
)

func (f Family) String() string {
	switch f {
	case Claude:
		return "claude"
	case GPT:
		return "gpt"
	case DeepSeek:
		return "deepseek"
	default:
		return "generic"
	}
}

var presets = map[Family]compress.Params{
	// Conservative: Anthropic prompt-cache discounts are among the best;
	// breaking a prefix costs more than the bytes saved.
	Claude: {MinBytes: 4096, HeadLines: 30, TailLines: 30, MaxKept: 50,
		Marker: "[... squoze: %d middle lines elided · full text kept locally as %s ...]"},
	// Balanced default.
	Generic: {MinBytes: 2048, HeadLines: 20, TailLines: 20, MaxKept: 50,
		Marker: "[... squoze: %d middle lines elided · full text kept locally as %s ...]"},
	GPT: {MinBytes: 2048, HeadLines: 20, TailLines: 20, MaxKept: 50,
		Marker: "[... squoze: %d middle lines elided · full text kept locally as %s ...]"},
	// Aggressive: no cheap stable prompt cache to protect historically.
	DeepSeek: {MinBytes: 1024, HeadLines: 12, TailLines: 12, MaxKept: 50,
		Marker: "[... squoze: %d middle lines elided · full text kept locally as %s ...]"},
}

// Detect maps a model identifier to its family by substring probes.
func Detect(model string) Family {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return Claude
	case strings.Contains(m, "deepseek"):
		return DeepSeek
	case strings.Contains(m, "gpt"), strings.Contains(m, "o1"), strings.Contains(m, "o3"):
		return GPT
	default:
		return Generic
	}
}

// ParamsFor returns the compression preset for a family.
func ParamsFor(f Family) compress.Params {
	return presets[f]
}
